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
	case 6: // game over
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
			log.Info("initiative initiated", "state", "ready", "initiative", 1)

			// invalidate cache for this game
			cache.Delete(gameID)
			w.Header().Set("HX-Trigger", "refreshTable")
			w.WriteHeader(http.StatusOK)
			return
		default:
			log.Info(ErrActionInvaid.Error())
			http.Error(w, ErrActionInvaid.Error(), http.StatusTooEarly)
			return
		}
	case 5, 4, 3, 2: // game in progress
		switch action {
		case "points":
			if !state.isHost(cookieKey) {
				log.Info("prohibiting non-host from updating points")
				http.Error(w, "only host can update points", http.StatusForbidden)
				return
			}
			// TODO: implement add points

		case "spin":
			if !state.isPlayerTurn(cookieKey) {
				log.Info("prohibiting non-turn player from spinning",
					"cookie_id", cookieID,
				)
				http.Error(w, "not your turn", http.StatusConflict)
				return
			}
			id, err := strconv.Atoi(cookieID)
			if err != nil {
				log.Error("invalid game id",
					"game_id", gameID,
					"error", err,
				)
				http.Error(w, "invalid game id", http.StatusBadRequest)
				return
			}
			args := sqlc.GameCardsWheelSpinParams{
				ID:       gameID,
				PlayerID: pgtype.Int4{Int32: int32(id), Valid: true},
			}
			gcID, err := queries.GameCardsWheelSpin(r.Context(), args)
			if err != nil {
				log.Error("spin wheel",
					"error", err,
					"game_id", gameID,
					"player_id", id,
				)
				http.Error(w, "server error", http.StatusInternalServerError)
				return
			}
			log.Info("wheel spun",
				"game_id", gameID,
				"game_card_id", gcID,
				"player_id", id,
			)

			// check if drawn card is a modifier via spin log
			lastSpin, err := queries.SpinLogPendingModifier(
				r.Context(), gameID,
			)
			if err != nil {
				log.Error("check spin log modifier",
					"error", err,
					"game_id", gameID,
				)
				http.Error(w, "server error", http.StatusInternalServerError)
				return
			}
			if lastSpin.ModifierEffect.Valid {
				log.Info("modifier drawn, entering pending state",
					"game_id", gameID,
					"effect", lastSpin.ModifierEffect.String,
					"player_id", id,
				)
				err = queries.GameUpdate(r.Context(), sqlc.GameUpdateParams{
					ID:      gameID,
					StateID: 4, // pending
					InitiativeCurrent: pgtype.Int4{
						Int32: state.Game.InitiativeCurrent.Int32,
						Valid: true,
					},
				})
				if err != nil {
					log.Error("transition to pending",
						"error", err,
						"game_id", gameID,
					)
					http.Error(w, "server error", http.StatusInternalServerError)
					return
				}
			}

			cache.Delete(gameID)
			w.Header().Set("HX-Trigger", "refreshTable")
			w.WriteHeader(http.StatusOK)

		case "next":
			if !state.isHost(cookieKey) {
				log.Info("prohibiting non-host from advancing initiative")
				http.Error(w, "only host can advance initiative", http.StatusForbidden)
				return
			}
			err := queries.InitiativeAdvance(r.Context(), gameID)
			if err != nil {
				log.Error("fail to advance initiative", "error", err)
				http.Error(w, "server error", http.StatusInternalServerError)
				return
			}
			cache.Delete(gameID)
			w.Header().Set("HX-Trigger", "refreshTable")
			w.WriteHeader(http.StatusOK)
			return

		case "flip":
			if !state.isPlayerTurn(cookieKey) {
				log.Info("prohibiting non-turn player from flipping",
					"game_id", gameID,
					"cookie_id", cookieID,
				)
				http.Error(w, "not your turn", http.StatusConflict)
				return
			}
			if state.Game.StateID != 4 {
				log.Info("flip requires pending state",
					"game_id", gameID,
					"state_id", state.Game.StateID,
				)
				http.Error(w, "no pending modifier", http.StatusConflict)
				return
			}
			lastSpin, err := queries.SpinLogPendingModifier(
				r.Context(), gameID,
			)
			if err != nil {
				log.Error("check spin log modifier",
					"error", err,
					"game_id", gameID,
				)
				http.Error(w, "server error", http.StatusInternalServerError)
				return
			}
			if !lastSpin.ModifierEffect.Valid {
				log.Info("no pending modifier",
					"game_id", gameID,
				)
				http.Error(w, "no pending modifier", http.StatusConflict)
				return
			}
			if lastSpin.ModifierEffect.String != "flip" {
				log.Info("no pending flip modifier",
					"game_id", gameID,
				)
				http.Error(w, "no pending flip", http.StatusConflict)
				return
			}
			cardStr := r.URL.Query().Get("card_id")
			if cardStr == "" {
				log.Info("flip: missing card_id", "game_id", gameID)
				http.Error(w, "missing card_id", http.StatusBadRequest)
				return
			}
			cardID, err := strconv.Atoi(cardStr)
			if err != nil {
				log.Error("invalid card_id",
					"error", err,
					"game_id", gameID,
				)
				http.Error(w, "invalid card_id", http.StatusBadRequest)
				return
			}
			err = queries.GameCardFlip(r.Context(), sqlc.GameCardFlipParams{
				GameID: gameID,
				CardID: int32(cardID),
			})
			if err != nil {
				log.Error("flip card",
					"error", err,
					"game_id", gameID,
					"card_id", cardID,
				)
				http.Error(w, "server error", http.StatusInternalServerError)
				return
			}

			// resolve: back to turn state and advance initiative
			err = queries.GameUpdate(r.Context(), sqlc.GameUpdateParams{
				ID:      gameID,
				StateID: 3, // turn
				InitiativeCurrent: pgtype.Int4{
					Int32: state.Game.InitiativeCurrent.Int32,
					Valid: true,
				},
			})
			if err != nil {
				log.Error("transition to turn",
					"error", err,
					"game_id", gameID,
				)
				http.Error(w, "server error", http.StatusInternalServerError)
				return
			}
			err = queries.InitiativeAdvance(r.Context(), gameID)
			if err != nil {
				log.Error("advance initiative after flip",
					"error", err,
					"game_id", gameID,
				)
				http.Error(w, "server error", http.StatusInternalServerError)
				return
			}
			log.Info("card flipped, modifier resolved",
				"game_id", gameID,
				"card_id", cardID,
			)
			cache.Delete(gameID)
			w.Header().Set("HX-Trigger", "refreshTable")
			w.WriteHeader(http.StatusOK)
		case "shred":
			// TODO: implement
			log.Error("not implemented")
			http.Error(w, "not implemented", http.StatusNotImplemented)
		case "clone":
			// TODO: implement
			log.Error("not implemented")
			http.Error(w, "not implemented", http.StatusNotImplemented)
		case "transfer":
			// TODO: implement
			log.Error("not implemented")
			http.Error(w, "not implemented", http.StatusNotImplemented)
		case "accuse":
			// {game_id}/accuse?accuser_id={accuser_id}&defendant_id={defendant_id}&rule_id={rule_id}
			// TODO: implement
			log.Error("not implemented")
			http.Error(w, "not implemented", http.StatusNotImplemented)
		case "decide":
			// - POST:
			// {game_id}/decide?infraction_id={infraction_id}&verdict={verdict}
			// TODO: implement
			log.Error("not implemented")
			http.Error(w, "not implemented", http.StatusNotImplemented)
		case "end":
			if !state.isHost(cookieKey) {
				log.Info("prohibiting non-host from ending game")
				http.Error(w, "only host can end game", http.StatusForbidden)
				return
			}
			err := queries.GameUpdate(r.Context(), sqlc.GameUpdateParams{
				ID:                gameID,
				StateID:           6, // game over
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
