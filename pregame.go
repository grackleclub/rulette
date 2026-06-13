package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	mathrand "math/rand"
	"net/http"
	"path"
	"strings"

	sqlc "github.com/grackleclub/rulette/db/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	sessionCookieName = "session"
	secretLength      = 32 // crypto/rand used for session key
)

// TODO: implement card selection stage of the game between invitation and spin.

// setCookieErr make logs messages and sets HTTP status responses appropriately.
func setCookieErr(w http.ResponseWriter, err error) {
	switch err {
	case ErrCookieMissing:
		log.Debug(ErrCookieMissing.Error())
		http.Error(w, "session cookie missing", http.StatusUnauthorized)
	case ErrCookieInvalid:
		log.Debug(ErrCookieInvalid.Error())
		http.Error(w, "invalid session cookie", http.StatusForbidden)
	default:
		log.Error("unexpected error getting cookie", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}
}

// alertMessages maps the ?alert= code carried on a redirect to the popup
// copy shown on the destination page. Unknown or empty codes render no
// popup. Shared by rootHandler and joinHandler's GET render.
var alertMessages = map[string]string{
	"in-progress": "Game in progress cannot be joined.",
	"over":        "Game over.",
	"not-found":   "Game does not exist.",
	"name-taken":  "Name taken, choose another.",
	"error":       "Server error, please try again.",
}

// indexView is the data for index.html: just an optional popup message.
type indexView struct {
	Alert string
}

// joinView is the data for tmpl.join.html. The game state row is embedded so
// the template's promoted fields (.ID, .StateName, ...) keep working, plus an
// optional popup message.
type joinView struct {
	sqlc.GameStateRow
	Alert string
}

// redirectAlert bounces a full-page visitor home with a popup. code is a
// fixed slug, so it needs no escaping. every alert redirect is a 303, so the
// HTTP status no longer tells these cases apart in traces -- the game.alert
// attribute does. genuine server errors are also marked on the span so they
// keep counting in error-rate metrics despite the 3xx status.
func redirectAlert(w http.ResponseWriter, r *http.Request, code string) {
	span := trace.SpanFromContext(r.Context())
	span.SetAttributes(attrAlert.String(code))
	if code == "error" {
		span.SetStatus(codes.Error, "server error")
	}
	http.Redirect(w, r, "/?alert="+code, http.StatusSeeOther)
}

// rootHandler provides the initial welcome page (index.html),
// from which a user can start a new game with a POST to /create.
func rootHandler(w http.ResponseWriter, r *http.Request) {
	indexPath := path.Join("static", "html", "index.html")
	data := indexView{Alert: alertMessages[r.URL.Query().Get("alert")]}
	if err := renderPage(r.Context(), w, indexPath, baseURL(r), true, data); err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
}

// createHandler handles the '/create' endpoint to make a new game with requester as host.
// - POST: create a new game with the requester as host
func createHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// construct random hex game identifier and create new game
	gamecode := fmt.Sprintf("%06x", mathrand.Intn(0xffffff+1))
	log.Debug("new game", "code", gamecode)
	err := queries.GameCreate(r.Context(), sqlc.GameCreateParams{
		ID: string(gamecode),
		// TODO: missing owner_id for now
	})
	if err != nil {
		log.Error("create game", "error", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/%s/join", gamecode), http.StatusSeeOther)
}

// joinHandler handles the '/{game_id}/join' endpoint where players may join a game.
func joinHandler(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.Trim(r.URL.Path, "/") // remove leading slash
	parts := strings.Split(trimmed, "/")
	if len(parts) != 2 {
		log.Debug("invalid join path", "path", r.URL.Path)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	gameID := parts[0]
	trace.SpanFromContext(r.Context()).SetAttributes(
		attrGameID.String(gameID),
	)
	log := log.With("handler", "joinHandler", "game_id", gameID, "method", r.Method)

	// fetch game state
	game, err := queries.GameState(r.Context(), gameID)
	if err != nil {
		log.Warn("game not found", "error", err)
		redirectAlert(w, r, "not-found")
		return
	}
	switch r.Method {
	case http.MethodGet:
		// if the visitor is already a player in this game, don't show them
		// the join form — bounce them back to the game page. Fail closed
		// on state lookup errors: a cookied visitor reaching the form is
		// the bug we're trying to prevent.
		if _, cookieKey, err := cookie(r); err == nil {
			s, err := stateFromCacheOrDB(r.Context(), &cache, gameID)
			if err != nil {
				log.Error("fetch game state for rejoin check",
					"error", err,
					"game_id", gameID,
				)
				redirectAlert(w, r, "error")
				return
			}
			if s.isPlayerInGame(cookieKey) {
				log.Warn("player attempted to rejoin, redirecting to game",
					"game_id", gameID,
				)
				http.Redirect(w, r, fmt.Sprintf("/%s", gameID), http.StatusSeeOther)
				return
			}
		}
		w.Header().Set("Content-Type", "text/html")
		templateFilepath := path.Join("static", "html", "tmpl.join.html")
		data := joinView{GameStateRow: game, Alert: alertMessages[r.URL.Query().Get("alert")]}
		if err := renderPage(r.Context(), w, templateFilepath, baseURL(r), true, data); err != nil {
			log.Error("render template",
				"error", err,
				"template", templateFilepath,
				"game_id", gameID,
			)
			redirectAlert(w, r, "error")
			return
		}
	case http.MethodPost:
		// require username
		username := r.FormValue("username")
		if username == "" {
			http.Error(w, "missing required field: username", http.StatusBadRequest)
			return
		}

		// reject rejoin: if the caller already has a session for this game,
		// bounce to the game page rather than minting a new player/session
		// (which would orphan the original identity and host initiative).
		if _, cookieKey, err := cookie(r); err == nil {
			s, err := stateFromCacheOrDB(r.Context(), &cache, gameID)
			if err != nil {
				log.Error("fetch game state for rejoin check",
					"error", err,
					"game_id", gameID,
				)
				redirectAlert(w, r, "error")
				return
			}
			if s.isPlayerInGame(cookieKey) {
				log.Warn("POST rejoin blocked, redirecting to game",
					"game_id", gameID,
				)
				http.Redirect(w, r, fmt.Sprintf("/%s", gameID), http.StatusSeeOther)
				return
			}
		}

		switch game.StateID {
		case stateOver:
			log.Warn("join attempt to closed game")
			redirectAlert(w, r, "over")
			return

		case statePending, stateTurn, stateReady:
			log.Warn("join attempt to game in progress",
				"state_id", game.StateID,
				"state_name", game.StateName,
			)
			redirectAlert(w, r, "in-progress")
			return
		case stateInviting, stateCreated:
			// first join updates state from 'created' to 'inviting'
			// NOTE: first to join is automatically set as host (initiative 0)
			var firstJoin bool
			if game.StateID == stateCreated {
				firstJoin = true
				log.Debug("created game has first join, updating state to inviting")
				err := queries.GameUpdate(r.Context(), sqlc.GameUpdateParams{
					StateID:           stateInviting,
					InitiativeCurrent: pgtype.Int4{Int32: 0, Valid: true},
					ID:                gameID,
				})
				if err != nil {
					log.Error("update game from created to inviting", "error", err)
					redirectAlert(w, r, "error")
					return
				}
			}
			// check for active players
			players, err := queries.GamePlayerPoints(r.Context(), gameID)
			if err != nil {
				log.Error("failed to get game players",
					"error", err,
					"game_id", gameID,
				)
				redirectAlert(w, r, "error")
				return
			}
			var initiativeMax int
			for _, player := range players {
				if player.Initiative.Int32 > int32(initiativeMax) {
					log.Debug("updating max initiative",
						"old", initiativeMax,
						"new", player.Initiative.Int32,
					)
					initiativeMax = int(player.Initiative.Int32)
				}
				// enforce no duplicate player names
				if player.Name == username {
					log.Warn("player already exists in game",
						"game_id", gameID,
						"username", username,
					)
					trace.SpanFromContext(r.Context()).
						SetAttributes(attrAlert.String("name-taken"))
					http.Redirect(w, r,
						fmt.Sprintf("/%s/join?alert=name-taken", gameID),
						http.StatusSeeOther,
					)
					return
				}
			}
			id, err := queries.PlayerCreate(r.Context(), username)
			if err != nil {
				log.Error("create player", "error", err, "username", username)
				redirectAlert(w, r, "error")
				return
			}
			// generate session key for game player and add them to the game.
			secret := make([]byte, secretLength)
			_, err = rand.Read(secret)
			if err != nil {
				log.Error("make secret", "error", err)
				redirectAlert(w, r, "error")
				return
			}
			secretStr := hex.EncodeToString(secret)

			// first join is host
			initiative := pgtype.Int4{Int32: 0, Valid: true}
			if !firstJoin {
				initiative = pgtype.Int4{Int32: int32(initiativeMax + 1), Valid: true}
			}
			err = queries.GamePlayerCreate(r.Context(), sqlc.GamePlayerCreateParams{
				PlayerID:   id,
				GameID:     gameID,
				SessionKey: pgtype.Text{String: secretStr, Valid: true},
				Initiative: initiative,
			})
			if err != nil {
				log.Error("add player to game",
					"error", err,
					"game_id", gameID,
					"player_id", id,
				)
				redirectAlert(w, r, "error")
				return
			}

			// give the player their session cookie
			http.SetCookie(w, &http.Cookie{
				Name:  "session",
				Value: fmt.Sprintf("%v:%s", id, secretStr),
				Path:  fmt.Sprintf("/%s", gameID),
			})
			log.Info("player joined game",
				"game_id", gameID,
				"player_id", id,
				"username", username,
			)
			// NOTE: it's necessary to invalidate the cache for this game
			// to prevent a new joiner from being declined due to a stale cache
			// that doesn't yet know about their join.
			cache.Delete(gameID)
			http.Redirect(w, r, fmt.Sprintf("/%s", gameID), http.StatusSeeOther)
		}
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
