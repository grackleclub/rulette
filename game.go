package main

import (
	"log/slog"
	"net/http"
	"path"
	"strconv"
	"strings"

	sqlc "github.com/grackleclub/rulette/db/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

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
		setCookieErr(w, err)
		return
	}

	state, err := stateFromCacheOrDB(r.Context(), &cache, gameID)
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
		setCookieErr(w, err)
		return
	}
	state, err := stateFromCacheOrDB(r.Context(), &cache, gameID)
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
