package main

import (
	"net/http"
	"strconv"
	"strings"

	sqlc "github.com/grackleclub/rulette/db/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

func actionHandler(w http.ResponseWriter, r *http.Request) {
	pathLong := strings.TrimPrefix(r.URL.Path, "/")
	parts := strings.Split(pathLong, "/")
	if len(parts) != 3 {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	gameID := parts[0]
	action := parts[2]
	log := log.With("handler", "actionHandler", "game_id", gameID, "action", action)
	log.Info("actionHandler called")
	cookieID, cookieKey, err := cookie(r)
	if err != nil {
		setCookieErr(w, err)
		return
	}
	state, err := stateFromCacheOrDB(r.Context(), &cache, gameID)
	if err != nil {
		if err == ErrStateNoGame {
			log.Info(ErrStateNoGame.Error(), "game_id", gameID)
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
		switch action {
		case "start":
			err := queries.GameUpdate(r.Context(), sqlc.GameUpdateParams{
				ID:                gameID,
				StateID:           2, // in progress
				InitiativeCurrent: pgtype.Int4{Int32: 0, Valid: true},
			})
			if err != nil {
				log.Error("start game", "error", err)
				http.Error(w, "server error", http.StatusInternalServerError)
				return
			}
			log.Info("game started")
			err = queries.GameUpdate(r.Context(), sqlc.GameUpdateParams{
				ID:                gameID,
				StateID:           3,
				InitiativeCurrent: pgtype.Int4{Int32: 1, Valid: true},
			})
			if err != nil {
				log.Error("update initiative", "error", err)
				http.Error(w, "server error", http.StatusInternalServerError)
			}
			log.Info("game started", "state", "ready", "initiative", 1)

			// invalidate cache for this game
			cache.Delete(gameID)
			w.WriteHeader(http.StatusOK)
			return
		default:
			log.Info(ErrActionInvaid.Error())
			http.Error(w, ErrActionInvaid.Error(), http.StatusTooEarly)
			return
		}
	case 4, 3, 2: // game in progress
		switch action {
		case "spin":
			// TODO: implement
			log.Error("not implmented")
			http.Error(w, "not implemented", http.StatusNotImplemented)
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
		case "decide":
			// - POST:
			// {game_id}/decide?infraction_id={infraction_id}&verdict={verdict}
			// TODO: implement
			log.Error("not implmented")
			http.Error(w, "not implemented", http.StatusNotImplemented)
		case "points":
			if !state.isHost(cookieKey) {
				log.Info("prohibiting non-host from adjusting points", "key", cookieKey)
				http.Error(w, "only host can adjust points", http.StatusForbidden)
				return
			}
			playerID := r.URL.Query().Get("playerId")
			if playerID == "" {
				http.Error(w, "required param missing: playerId", http.StatusBadRequest)
			}
			playerIdInt, err := strconv.Atoi(playerID)
			if err != nil {
				http.Error(w, "playerId must be an int", http.StatusBadRequest)
			}

			points := r.URL.Query().Get("points")
			if points == "" {
				http.Error(w, "required param missing: points", http.StatusBadRequest)
			}
			pointsInt, err := strconv.Atoi(points)
			if err != nil {
				http.Error(w, "points value must be an int", http.StatusBadRequest)
			}
			for _, player := range state.Players {
				if player.PlayerID == int32(playerIdInt) {
					p := player.Points.Int32 + int32(pointsInt)
					// TODO: update database with new points
					log.Info("mock: updated points",
						"points_prior", player.Points.Int32,
						"points_new", p,
						"player_name", player.Name,
					)
				}
			}
		case "end":
			if !state.isHost(cookieKey) {
				log.Info("prohibiting non-host from ending game")
				http.Error(w, "only host can end game", http.StatusForbidden)
				return
			}
			err := queries.GameUpdate(r.Context(), sqlc.GameUpdateParams{
				ID:                gameID,
				StateID:           5, // game over
				InitiativeCurrent: pgtype.Int4{Int32: 0, Valid: true},
			})
			if err != nil {
				log.Error("end game", "error", err)
				http.Error(w, "server error", http.StatusInternalServerError)
				return
			}
			log.Info("game ended")
			w.WriteHeader(http.StatusGone)
			return
		default:
			log.Info("unsupported action requested")
			http.Error(w, "unsupported action", http.StatusNotImplemented)
		}
	}
	return
}
