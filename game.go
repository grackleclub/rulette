package main

import (
	"encoding/json"
	"net/http"
	"path"
	"strings"
)

// gameHandler handles the '/{game_id}' endpoint
// This endpoint serves as a lobby pregame, and for primary play.
func gameHandler(w http.ResponseWriter, r *http.Request) {
	gameID := strings.Replace(r.URL.Path, "/", "", 1)
	log.With("handler", "gameHandler", "game_id", gameID)

	if r.Method != http.MethodGet {
		log.Debug("unsupported method", "method", r.Method)
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
			log.Info("game not found", "game_id", gameID)
			http.Error(w, "game not found", http.StatusNotFound)
			return
		}
		log.Error("unexpected error fetching game state", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !state.isPlayerInGame(cookieKey) {
		log.Info(
			"prohibiting unauthorized player access",
			"cookie_key", cookieKey,
			"cookie_id", cookieID,
		)
		http.Error(w, "player not in game", http.StatusForbidden)
		return
	}

	filepath := path.Join("static", "html", "tmpl.game.html")
	tmpl, err := readParse(static, filepath)
	if err != nil {
		log.Error("read and parse template",
			"error", err,
			"template", filepath,
		)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	err = tmpl.Execute(w, state)
	if err != nil {
		log.Error("execute template",
			"error", err,
			"template", filepath,
			"game_id", gameID,
		)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
}

// dataHandler returns game data or html elements.
// (e.g. player cards, table data)
func dataHandler(w http.ResponseWriter, r *http.Request) {
	pathLong := strings.TrimPrefix(r.URL.Path, "/")
	parts := strings.Split(pathLong, "/")
	if len(parts) != 3 {
		http.Error(w, "invalid path", http.StatusBadRequest)
	}
	gameID := parts[0]
	topic := parts[2]
	log := log.With("handler", "dataHandler", "game_id", gameID, "topic", topic)
	log.Info("dataHandler called")
	if r.Method != http.MethodGet {
		log.Debug("unsupported method", "method", r.Method)
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
			log.Info("game not found", "game_id", gameID)
			http.Error(w, "game not found", http.StatusNotFound)
			return
		}
		log.Error("unexpected error getting state", "error", err, "game_id", gameID)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !state.isPlayerInGame(cookieKey) {
		log.Info(
			"prohibiting unauthorized player access",
			"cookie_key", cookieKey,
			"cookie_id", cookieID,
		)
		http.Error(w, "player not in game", http.StatusForbidden)
		return
	}
	switch state.Game.StateID {
	case 5: // game over
		log.Info("request to ended game", "game_id", gameID)
		http.Error(w, "game over", http.StatusGone)
		return
	case 4, 3, 2, 1, 0: // game in progress
		switch topic {
		case "players":
			filepath := path.Join("static", "html", "tmpl.players.html")
			tmpl, err := readParse(static, filepath)
			if err != nil {
				log.Error(ErrReadParseTemplate.Error(), "filepath", filepath, "error", err)
				http.Error(w, "internal server error", http.StatusInternalServerError)
				return
			}
			err = tmpl.Execute(w, state)
			if err != nil {
				log.Error("execute template",
					"error", err,
					"template", filepath,
				)
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
			return
		case "table":
			filepath := path.Join("static", "html", "tmpl.table.html")
			tmpl, err := readParse(static, filepath)
			if err != nil {
				log.Error(ErrReadParseTemplate.Error(), "filepath", filepath, "error", err)
				http.Error(w, "internal server error", http.StatusInternalServerError)
				return
			}
			err = tmpl.Execute(w, state)
			if err != nil {
				log.Error("execute template",
					"error", err,
					"template", filepath,
				)
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
			return
		case "status": // TODO: this and state should be the same thing
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
			_, err := w.Write([]byte(state.Game.StateName))
			if err != nil {
				log.Error("write status response", "error", err)
				http.Error(w, "server error", http.StatusInternalServerError)
				return
			}
		case "state": // NOTE: debug endpoint
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			err := json.NewEncoder(w).Encode(state)
			if err != nil {
				log.Error("encode state response", "error", err)
				http.Error(w, "server error", http.StatusInternalServerError)
				return
			}
		default:
			log.Info(ErrTopicInvalid.Error())
			http.Error(w, ErrTopicInvalid.Error(), http.StatusBadRequest)
			return
		}

	}
}
