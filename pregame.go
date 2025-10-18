package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math"
	mathrand "math/rand"
	"net/http"
	"path"
	"strings"
	"time"

	sqlc "github.com/grackleclub/rulette/db/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

const (
	sessionCookieName = "session"
	secretLength      = 32 // crypto/rand used for session key
)

// TODO: implement card selection stage of the game between invitation and spin.

var gameNameSeedLength = int(math.Pow(2, 16)) // math/rand used for game id

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

// rootHandler provides the initial welcome page (index.html),
// from which a user can start a new game with a POST to /create.
func rootHandler(w http.ResponseWriter, r *http.Request) {
	indexPath := path.Join("static", "html", "index.html")
	file, err := static.ReadFile(indexPath)
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	http.ServeContent(w, r, "index.html", time.Now(), bytes.NewReader(file))
}

// createHandler handles the '/create' endpoint to make a new game with requester as host.
// - POST: create a new game with the requester as host
func createHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	gamename := r.FormValue("gamename")
	if gamename == "" {
		http.Error(w, "missing required field: gamename", http.StatusBadRequest)
		return
	}

	// construct random hex game identifier and create new game
	gamecode := fmt.Sprintf("%06x", mathrand.Intn(0xffffff+1))
	log.Debug("short hash for game name", "gamename", gamename, "hash", gamecode)
	err := queries.GameCreate(r.Context(), sqlc.GameCreateParams{
		Name: gamename,
		ID:   string(gamecode),
		// TODO: missing owner_id for now
	})
	if err != nil {
		log.Error("create game", "error", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// TODO: this is a temporary measure to set some default card
	// immediately upon game creation.
	// Future intention is that players will set this together during pregame.
	err = queries.GameCardsInit(r.Context(), gamecode)
	if err != nil {
		log.Error("initialize game cards", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
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
	log.With("handler", "joinHandler", "game_id", gameID, "method", r.Method)

	// fetch game state
	state, err := stateFromCacheOrDB(r.Context(), &cache, gameID)
	if err != nil {
		log.Warn("game not found", "error", err)
		http.Error(w, "game not found", http.StatusNotFound)
		return
	}
	_, cookieKey, _ := cookie(r)
	if state.isPlayerInGame(cookieKey) {
		http.Error(w, "player already in game", http.StatusConflict)
		return
	}

	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "text/html")
		templateFilepath := path.Join("static", "html", "tmpl.join.html")
		tmpl, err := readParse(static, templateFilepath)
		err = tmpl.Execute(w, state.Game)
		if err != nil {
			log.Error("execute template",
				"error", err,
				"template", templateFilepath,
				"game_id", gameID,
			)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
	case http.MethodPost:
		// require username
		username := r.FormValue("username")
		if username == "" {
			http.Error(w, "missing required field: username", http.StatusBadRequest)
			return
		}

		switch state.Game.StateID {
		case 5:
			log.Info("join attempt to closed game")
			http.Error(w, "game over", http.StatusGone)
			return

		case 4, 3, 2:
			log.Info("join attempt to game in progress",
				"state_id", state.Game.StateID,
				"state_name", state.Game.StateName,
			)
			http.Error(w, "game in progress", http.StatusConflict)
			return
		case 1, 0:
			// first join updates state from 'created' to 'inviting'
			// NOTE: first to join is automatically set as host (initiative 0)
			if state.Game.StateID == 0 {
				log.Debug("created game has first join, updating state to inviting")
				err := queries.GameUpdate(r.Context(), sqlc.GameUpdateParams{
					StateID:           1,
					InitiativeCurrent: pgtype.Int4{Int32: 0, Valid: true},
					ID:                gameID,
				})
				if err != nil {
					log.Error("update game from created to inviting", "error", err)
					http.Error(w, "internal server error", http.StatusInternalServerError)
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
				http.Error(w, "internal server error", http.StatusInternalServerError)
				return
			}
			// enforce no duplicate player names
			var initiativeMax int
			for _, player := range players {
				if player.Initiative.Int32 > int32(initiativeMax) {
					log.Debug("updating max initiative",
						"old", initiativeMax,
						"new", player.Initiative.Int32,
					)
					initiativeMax = int(player.Initiative.Int32)
				}
				if player.Name == username {
					log.Debug("player already exists in game",
						"game_id", gameID,
						"username", username,
					)
					http.Error(w, "player already exists in game", http.StatusConflict)
					return
				}
			}
			id, err := queries.PlayerCreate(r.Context(), username)
			if err != nil {
				log.Error("create player", "error", err, "username", username)
				http.Error(w, "username: bad request", http.StatusBadRequest)
				return
			}
			// generate session key for game player and add them to the game.
			secret := make([]byte, secretLength)
			_, err = rand.Read(secret)
			if err != nil {
				log.Error("make secret", "error", err)
				http.Error(w, "internal server error", http.StatusInternalServerError)
				return
			}
			secretStr := hex.EncodeToString(secret)
			err = queries.GamePlayerCreate(r.Context(), sqlc.GamePlayerCreateParams{
				PlayerID:   id,
				GameID:     gameID,
				SessionKey: pgtype.Text{String: secretStr, Valid: true},
				Initiative: pgtype.Int4{Int32: int32(initiativeMax + 1), Valid: true},
			})

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
			// NOTE: it's necessary to invalidate the caache for this game
			// to prevent a new joiner from being declined due to a stale cache
			// that doesn't yet know about their join.
			cache.Delete(gameID)
			http.Redirect(w, r, fmt.Sprintf("/%s", gameID), http.StatusSeeOther)
		}
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
