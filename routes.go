package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	sqlc "github.com/grackleclub/rulette/db/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

// func stateHandler(w http.ResponseWriter, r *http.Request) {
// 	w.Header().Set("Content-Type", "application/json")
// 	w.WriteHeader(http.StatusOK)
//
// 	result, err := queries.Games(r.Context(), 1)
// 	if err != nil {
// 		slog.Error("failed to get games", "error", err)
// 		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
// 		return
// 	}
// 	slog.Info("retrieved games", "count", len(result), "result", result)
// 	response := `{"status": "okey dokey"}`
// 	if _, err := w.Write([]byte(response)); err != nil {
// 		slog.Error("failed to write response", "error", err)
// 	}
// }

// rootHandler
// - GET: show make game button that can POST to /create
func rootHandler(w http.ResponseWriter, r *http.Request) {
	slog.Debug("rootHandler called", "path", r.URL.Path)
	http.ServeFile(w, r, "./static/html/index.html")
}

// gameHandler handles the '/{game_id}' endpoint
// This endpoint serves as a lobby pregame, and for primary play.
// - GET: if no cookie, join, if cookie, get state
//   - if host: host view
//   - if player: player view
func gameHandler(w http.ResponseWriter, r *http.Request) {
	slog.Debug("gameHandler called", "path", r.URL.Path)
	gameID := strings.Replace(r.URL.Path, "/", "", 1)
	results, err := queries.GameState(r.Context(), fmt.Sprintf("/%s", gameID))
	// TODO: check len of results?
	if err != nil {
		slog.Warn("fetch attempt to non-existent game", "error", err, "game_id", gameID)
		http.Error(w, "Game not found", http.StatusNotFound)
		return
	}
	slog.Debug("loading game page", "game_id", gameID, "results", results)
	http.ServeFile(w, r, "./static/html/game.html")
}

// createHandler handles the '/create' endpoint to make a new game with requester as host.
// - POST: create a new game with the requester as host
func createHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	name := "TODO:bobson"
	// TODO: get host player name
	id, err := queries.PlayerCreate(r.Context(), name)
	slog.Debug("found player ID", "id", id, "name", name)
	// TODO: how the fuck do I get the player ID without RETURNING working?
	if err != nil {
		slog.Error("create player", "error", err, "name", name)
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	slog.Debug("created player", "name", name)
	err = queries.GameCreate(r.Context(), sqlc.GameCreateParams{})
	if err != nil {
		slog.Error("create game", "error", err)
		http.Error(w, "Bad Request", http.StatusBadRequest)
	}
}

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
func flipHandler(w http.ResponseWriter, r *http.Request)  {}
func shredHandler(w http.ResponseWriter, r *http.Request) {}
func cloneHandler(w http.ResponseWriter, r *http.Request) {}
