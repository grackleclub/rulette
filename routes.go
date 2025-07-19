package main

import (
	"log/slog"
	"net/http"
)

func stateHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	result, err := queries.Games(r.Context(), 1)
	if err != nil {
		slog.Error("failed to get games", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	slog.Info("retrieved games", "count", len(result), "result", result)
	response := `{"status": "okey dokey"}`
	if _, err := w.Write([]byte(response)); err != nil {
		slog.Error("failed to write response", "error", err)
	}
}

// rootHandler
// - GET: show make game button that can POST to /create

// createHandler handles the '/create' endpoint to make a new game with requester as host.
// - POST

// gameHandler handles the '/{game_id}' endpoint
// This endpoint serves as a lobby pregame, and for primary play.
// - GET: if no cookie, join, if cookie, get state
//   - if host: host view
//   - if player: player view

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
