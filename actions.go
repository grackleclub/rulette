package main

import (
	"log/slog"
	"net/http"
	"strconv"

	sqlc "github.com/grackleclub/rulette/db/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

func flipHandler(w http.ResponseWriter, r *http.Request)  {}
func shredHandler(w http.ResponseWriter, r *http.Request) {}

func cloneHandler(w http.ResponseWriter, r *http.Request) {}

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
