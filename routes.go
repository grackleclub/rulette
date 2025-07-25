package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"text/template"
	"time"

	sqlc "github.com/grackleclub/rulette/db/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

// rootHandler provides the initial welcome page (index.html),
// from which a user can start a new game with a POST to /create.
func rootHandler(w http.ResponseWriter, r *http.Request) {
	slog.Debug("rootHandler called", "path", r.URL.Path)
	file, err := static.ReadFile("static/html/index.html")
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	http.ServeContent(w, r, "index.html", time.Now(), bytes.NewReader(file))
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
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	gamename := r.FormValue("gamename")
	if gamename == "" {
		http.Error(w, "missing required field: gamename", http.StatusBadRequest)
		return
	}
	username := r.FormValue("username")
	if username == "" {
		http.Error(w, "missing required field: username", http.StatusBadRequest)
		return
	}
	id, err := queries.PlayerCreate(r.Context(), username)
	slog.Debug("createHandler made new user", "id", id, "username", username)
	if err != nil {
		slog.Error("create player", "error", err, "username", username)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	slog.Debug("created player", "name", username)
	hash := sha256.Sum256([]byte(gamename))
	shortHash := hex.EncodeToString(hash[:3])
	slog.Debug("short hash for game name", "gamename", gamename, "hash", shortHash)
	err = queries.GameCreate(r.Context(), sqlc.GameCreateParams{
		Name:    gamename,
		OwnerID: id,
		ID:      string(shortHash),
	})
	if err != nil {
		slog.Error("create game", "error", err)
		http.Error(w, "bad request", http.StatusBadRequest)
	}
	// return game ID as html response
	w.WriteHeader(http.StatusCreated)
	w.Header().Set("Content-Type", "text/html")
	// TODO: use templates.
	templateFilepath := "static/html/join.html.tmpl"
	tmplData, err := static.ReadFile(templateFilepath)
	if err != nil {
		slog.Error("read template file",
			"error", err,
			"filepath", templateFilepath,
		)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	tmpl, err := template.New("join").Parse(string(tmplData))
	if err != nil {
		slog.Error("parse template",
			"error", err,
			"template", templateFilepath)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	err = tmpl.Execute(w, map[string]interface{}{
		"game_id":   shortHash,
		"game_name": gamename,
	})
	if err != nil {
		slog.Error("execute template",
			"error", err,
			"template", templateFilepath,
			"game_id", shortHash,
		)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
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
