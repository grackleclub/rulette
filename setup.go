package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
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
	secretLength      = 32 // crypto/rand
)

var gameNameSeedLength = int(math.Pow(2, 16)) // math/rand

// rootHandler provides the initial welcome page (index.html),
// from which a user can start a new game with a POST to /create.
func rootHandler(w http.ResponseWriter, r *http.Request) {
	slog.Debug("rootHandler called", "path", r.URL.Path)
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
	slog.Debug("short hash for game name", "gamename", gamename, "hash", gamecode)
	err := queries.GameCreate(r.Context(), sqlc.GameCreateParams{
		Name: gamename,
		ID:   string(gamecode),
	})
	if err != nil {
		slog.Error("create game", "error", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// return game ID as html response
	w.WriteHeader(http.StatusCreated)
	w.Header().Set("Content-Type", "text/html")
	templateFilepath := path.Join("static", "html", "join.html.tmpl")
	tmpl, err := readParse(static, templateFilepath)
	err = tmpl.Execute(w, map[string]interface{}{
		"game_id":   gamecode,
		"game_name": gamename,
	})
	if err != nil {
		slog.Error("execute template",
			"error", err,
			"template", templateFilepath,
			"game_id", gamecode,
		)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
}

// joinHandler handles the '/{game_id}/join' endpoint where players may join a game.
func joinHandler(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.Trim(r.URL.Path, "/") // remove leading slash
	parts := strings.Split(trimmed, "/")
	if len(parts) != 2 {
		slog.Debug("invalid join path", "path", r.URL.Path)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	gameID := parts[0]
	slog.With("handler", "joinHandler", "game_id", gameID)

	// require username
	username := r.FormValue("username")
	if username == "" {
		http.Error(w, "missing required field: username", http.StatusBadRequest)
		return
	}

	// fetch game state
	game, err := queries.GameState(r.Context(), gameID)
	if err != nil {
		slog.Warn("game not found", "error", err)
		http.Error(w, "game not found", http.StatusNotFound)
		return
	}
	switch game.StateID {
	case 5:
		slog.Info("join attempt to closed game")
		http.Error(w, "game over", http.StatusGone)
		return

	case 4, 3, 2:
		slog.Info("join attempt to game in progress",
			"state_id", game.StateID,
			"state_name", game.StateName,
		)
		http.Error(w, "game in progress", http.StatusConflict)
		return
	case 1, 0:
		// first join updates state from 'created' to 'inviting'
		if game.StateID == 0 {
			slog.Debug("created game has first join, updating state to inviting")
			err := queries.GameUpdate(r.Context(), sqlc.GameUpdateParams{
				StateID:           1,
				InitiativeCurrent: pgtype.Int4{Int32: 0, Valid: true},
				ID:                gameID,
			})
			if err != nil {
				slog.Error("update game from created to inviting", "error", err)
				http.Error(w, "internal server error", http.StatusInternalServerError)
				return
			}
		}
		// check for active players
		players, err := queries.GamePlayerPoints(r.Context(), gameID)
		if err != nil {
			slog.Error("failed to get game players",
				"error", err,
				"game_id", gameID,
			)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		// enforce no duplicate player names
		for _, player := range players {
			if player.Name == username {
				slog.Debug("player already exists in game",
					"game_id", gameID,
					"username", username,
				)
				http.Error(w, "player already exists in game", http.StatusConflict)
				return
			}
		}
		id, err := queries.PlayerCreate(r.Context(), username)
		if err != nil {
			slog.Error("create player", "error", err, "username", username)
			http.Error(w, "username: bad request", http.StatusBadRequest)
			return
		}
		// generate session key for game player and add them to the game.
		secret := make([]byte, secretLength)
		_, err = rand.Read(secret)
		if err != nil {
			slog.Error("make secret", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		secretStr := hex.EncodeToString(secret)
		err = queries.GamePlayerCreate(r.Context(), sqlc.GamePlayerCreateParams{
			PlayerID:   id,
			GameID:     gameID,
			SessionKey: pgtype.Text{String: secretStr, Valid: true},
		})

		// give the player their session cookie
		http.SetCookie(w, &http.Cookie{
			Name:  "session",
			Value: fmt.Sprintf("%v:%s", id, secretStr),
			Path:  fmt.Sprintf("/%s", gameID),
		})
		slog.Info("player joined game",
			"game_id", gameID,
			"player_id", id,
			"username", username,
		)
		http.Redirect(w, r, fmt.Sprintf("/%s", gameID), http.StatusSeeOther)
	}
}

// setCookieErr make logs messages and sets HTTP status responses appropriately.
func setCookieErr(w http.ResponseWriter, err error) {
	switch err {
	case ErrCookieMissing:
		slog.Debug(ErrCookieMissing.Error())
		http.Error(w, "session cookie missing", http.StatusUnauthorized)
	case ErrCookieInvalid:
		slog.Debug(ErrCookieInvalid.Error())
		http.Error(w, "invalid session cookie", http.StatusForbidden)
	default:
		slog.Error("unexpected error getting cookie", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}
}
