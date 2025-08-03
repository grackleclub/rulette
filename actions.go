package main

import (
	"net/http"
	"strings"
)

func actionHandler(w http.ResponseWriter, r *http.Request) {
	pathLong := strings.TrimPrefix(r.URL.Path, "/")
	parts := strings.Split(pathLong, "/")
	if len(parts) != 3 {
		http.Error(w, "invalid path", http.StatusBadRequest)
	}
	gameID := parts[0]
	action := parts[2]
	log := log.With("handler", "dataHandler", "game_id", gameID, "action", action)
	log.Info("actionHandler called")
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
	case 1, 0: // pregame
		log.Info("game not started yet, no actions allowed")
		http.Error(w, "game not started yet", http.StatusTooEarly)
	case 4, 3, 2: // game in progress
		switch action {
		case "flip":
			// TODO: implement
			log.Error("not implmented")
			http.Error(w, "not implemented", http.StatusNotImplemented)
		case "shred":
			// TODO: implement
			log.Error("not implmented")
			http.Error(w, "not implemented", http.StatusNotImplemented)
		case "clone":
			// TODO: implement
			log.Error("not implmented")
			http.Error(w, "not implemented", http.StatusNotImplemented)
		case "transfer":
			// TODO: implement
			log.Error("not implmented")
			http.Error(w, "not implemented", http.StatusNotImplemented)
		case "accuse":
			// {game_id}/accuse?accuser_id={accuser_id}&defendant_id={defendant_id}&rule_id={rule_id}
			// TODO: implement
			log.Error("not implmented")
			http.Error(w, "not implemented", http.StatusNotImplemented)
		case "judge":
			// - POST:
			// {game_id}/judge?infraction_id={infraction_id}&verdict={verdict}
			// TODO: implement
			log.Error("not implmented")
			http.Error(w, "not implemented", http.StatusNotImplemented)
		case "spin":
			// TODO: implement
			log.Error("not implmented")
			http.Error(w, "not implemented", http.StatusNotImplemented)
		default:
			log.Info("unsupported action requested")
			http.Error(w, "unsupported action", http.StatusNotImplemented)
		}
	}
	return
}
