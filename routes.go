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
	"strconv"
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

// gameHandler handles the '/{game_id}' endpoint
// This endpoint serves as a lobby pregame, and for primary play.
func gameHandler(w http.ResponseWriter, r *http.Request) {
	gameID := strings.Replace(r.URL.Path, "/", "", 1)
	slog.With("handler", "gameHandler", "game_id", gameID)

	if r.Method != http.MethodGet {
		slog.Debug("unsupported method", "method", r.Method)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}

	cookieID, cookieKey, err := cookie(r)
	if err != nil {
		switch err {
		case ErrCookieMissing:
			slog.Debug("cookie missing")
			http.Error(w, "session cookie missing", http.StatusUnauthorized)
			return
		case ErrCookieInvalid:
			slog.Debug("cookie invalid")
			http.Error(w, "invalid session cookie", http.StatusUnauthorized)
			return
		default:
			slog.Error("unexpected error getting cookie", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
	}

	state, err := stateFromCache(r.Context(), &cache, gameID)
	if err != nil {
		slog.Error("game state from cache", "error", err, "game_id", gameID)
		if err == ErrStateNoGame {
			slog.Info("game not found", "game_id", gameID)
			http.Error(w, "game not found", http.StatusNotFound)
			return
		}
		slog.Error("unexpected error fetching game state", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !state.isPlayerInGame(cookieKey) {
		slog.Info(
			"prohibiting unauthorized player access",
			"cookie_key", cookieKey,
			"cookie_id", cookieID,
		)
		http.Error(w, "player not in game", http.StatusForbidden)
		return
	}

	filepath := path.Join("static", "html", "game.html.tmpl")
	tmpl, err := readParse(static, filepath)
	if err != nil {
		slog.Error("read and parse template",
			"error", err,
			"template", filepath,
		)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	err = tmpl.Execute(w, map[string]interface{}{
		"game_id":            gameID,
		"game_name":          state.game.Name,
		"game_state":         state.game.StateName,
		"owner_id":           state.game.OwnerID,
		"initiative_current": state.game.InitiativeCurrent,
	})
	if err != nil {
		slog.Error("execute template",
			"error", err,
			"template", filepath,
			"game_id", gameID,
		)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
}

func tableHandler(w http.ResponseWriter, r *http.Request) {
	pathLong := strings.TrimPrefix(r.URL.Path, "/")
	parts := strings.Split(pathLong, "/")
	if len(parts) == 0 { // NOTE: probably impossible
		http.Error(w, "game ID is required", http.StatusBadRequest)
		return
	}
	gameID := parts[0]

	slog.With("handler", "tableHandler", "game_id", gameID)
	slog.Info("tableHandler called", "game_id", gameID)
	if r.Method != http.MethodGet {
		slog.Debug("unsupported method", "method", r.Method)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	cookieID, cookieKey, err := cookie(r)
	if err != nil {
		switch err {
		case ErrCookieMissing:
			slog.Debug("cookie missing")
			http.Error(w, "session cookie missing", http.StatusUnauthorized)
			return
		case ErrCookieInvalid:
			slog.Debug("cookie invalid")
			http.Error(w, "invalid session cookie", http.StatusUnauthorized)
			return
		default:
			slog.Error("unexpected error getting cookie", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
	}
	state, err := stateFromCache(r.Context(), &cache, gameID)
	if err != nil {
		if err == ErrStateNoGame {
			slog.Info("game not found", "game_id", gameID)
			http.Error(w, "game not found", http.StatusNotFound)
			return
		}
		slog.Error("unexpected error getting state", "error", err, "game_id", gameID)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !state.isPlayerInGame(cookieKey) {
		slog.Info(
			"prohibiting unauthorized player access",
			"cookie_key", cookieKey,
			"cookie_id", cookieID,
		)
		http.Error(w, "player not in game", http.StatusForbidden)
		return
	}
	switch state.game.StateID {
	case 5:
		slog.Info("game is over", "game_id", gameID)
		http.Error(w, "game over", http.StatusGone)
		return
	case 4, 3, 2:
		// TODO: return spinning wheel
		filepath := path.Join("static", "html", "table.spin.html.tmpl")
		tmpl, err := readParse(static, filepath)
		if err != nil {
			slog.Error("read and parse template", "filepath", filepath, "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		err = tmpl.Execute(w, map[string]interface{}{
			"game_state": state.game.StateID,
			"game_id":    gameID,
		})
		if err != nil {
			slog.Error("execute template",
				"error", err,
				"template", filepath,
			)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

	case 1, 0:
		filepath := path.Join("static", "html", "table.invite.html.tmpl")
		tmpl, err := readParse(static, filepath)
		if err != nil {
			slog.Error("read and parse template", "filepath", filepath, "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		err = tmpl.Execute(w, map[string]interface{}{
			"game_state": state.game.StateID,
			"game_id":    gameID,
		})
		if err != nil {
			slog.Error("execute template",
				"error", err,
				"template", filepath,
			)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
	}
}

func spinHandler(w http.ResponseWriter, r *http.Request) {
	slog.Debug("spinHandler called", "path", r.URL.Path)
	playerID := r.URL.Query().Get("player_id")
	if playerID == "" {
		http.Error(w, "Player ID is required", http.StatusBadRequest)
		return
	}
	// get player_id that spun it
	gameID := r.URL.Query().Get("game_id")
	if gameID == "" {
		http.Error(w, "Game ID is required", http.StatusBadRequest)
		return
	}
	// TODO: get initiative to enforce player turn
	// enforce the correct players turn
	// players, err := queries.GamePlayerPoints(r.Context(), gameID)

	// select card
	// transfer card
	// [optional] be in prompt flow
}

func transferHandler(w http.ResponseWriter, r *http.Request) {
	// check path
	path := r.URL.Path
	slog.Debug("transferHandler called", "path", path)

	gameID := r.URL.Query().Get("game_id")
	if gameID == "" {
		http.Error(w, "Game ID is required", http.StatusBadRequest)
		return
	}
	fromPlayerID := r.URL.Query().Get("from")
	// it's okay to have a null from, because that's what happens when it moves form the wheel.
	// TODO: maybe ensure if a fromPLayerID is passed, that it's valid?
	toPlayerID := r.URL.Query().Get("to")

	toPlayerIDInt, err := strconv.Atoi(toPlayerID)
	if err != nil && toPlayerID != "" {
		slog.Error("invalid to player ID", "error", err, "to_player_id", toPlayerID)
		http.Error(w, "Invalid Player ID", http.StatusBadRequest)
		return
	}
	toPlayerIDpg := pgtype.Int4{
		Int32: int32(toPlayerIDInt),
		Valid: true,
	}
	err = queries.GameCardMove(r.Context(), sqlc.GameCardMoveParams{
		PlayerID: toPlayerIDpg,
		GameID:   gameID,
		// CardID:   cardID, FIXME: empty
	})
	if err != nil {
		slog.Error("failed to move card",
			"error", err,
			"game_id", gameID,
			"from_player_id", fromPlayerID,
			"to_player_id", toPlayerID,
		)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// joinPlayerToGame is an idempotent function to join a player to a game,
// setting up secrets and cookie.
// If the player is already in the game, the
// func joinPlayerToGame(r *http.Request) (sqlc.GamePlayerPointsRow, error) {
// }

func flipHandler(w http.ResponseWriter, r *http.Request)  {}
func shredHandler(w http.ResponseWriter, r *http.Request) {}
func cloneHandler(w http.ResponseWriter, r *http.Request) {}

// TODO: implement card selection stage of the game between invitation and spin.

// {game_id}/spin
// - POST: spin the wheel, update game state
// {game_id}/accuse?accuser_id={accuser_id}&defendant_id={defendant_id}&rule_id={rule_id}
// - POST:
// {game_id}/judge?infraction_id={infraction_id}&verdict={verdict}
//{game_id}/
//

// {game_id}/cards/{card_id}/{action}?ake
// PATCH, PATCH, DELETE, POST
// transfer, flip, shred, clone
