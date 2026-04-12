package main

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	sqlc "github.com/grackleclub/rulette/db/sqlc"
	"github.com/jackc/pgx/v5"
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
	log := log.With(
		"handler", "actionHandler",
		"game_id", gameID,
		"action", action,
	)
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
		http.Error(w, "server error", http.StatusInternalServerError)
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
			// populate and shuffle the deck
			err = queries.GameCardsInitGeneric(r.Context(), gameID)
			if err != nil {
				log.Error("init deck",
					"error", err,
					"game_id", gameID,
				)
				http.Error(w, "server error", http.StatusInternalServerError)
				return
			}
			err = queries.GameCardsShuffle(r.Context(), gameID)
			if err != nil {
				log.Error("shuffle deck",
					"error", err,
					"game_id", gameID,
				)
				http.Error(w, "server error", http.StatusInternalServerError)
				return
			}
			log.Info("deck initialized and shuffled",
				"game_id", gameID,
			)

			// set game to in-progress
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

			// start initiative with first non-host player
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
			if err := r.ParseForm(); err != nil {
				log.Error("parse form", "error", err, "game_id", gameID)
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			playerStr := r.FormValue("player_id")
			amountStr := r.FormValue("amount")
			if playerStr == "" || amountStr == "" {
				log.Info("missing player_id or amount", "game_id", gameID)
				http.Error(w, "missing player_id or amount", http.StatusBadRequest)
				return
			}
			targetID, err := strconv.Atoi(playerStr)
			if err != nil {
				log.Error("invalid player_id",
					"error", err,
					"game_id", gameID,
				)
				http.Error(w, "invalid player_id", http.StatusBadRequest)
				return
			}
			amount, err := strconv.Atoi(amountStr)
			if err != nil {
				log.Error("invalid amount",
					"error", err,
					"game_id", gameID,
				)
				http.Error(w, "invalid amount", http.StatusBadRequest)
				return
			}
			err = queries.GamePointsAdjust(r.Context(), sqlc.GamePointsAdjustParams{
				Points:   pgtype.Int4{Int32: int32(amount), Valid: true},
				GameID:   gameID,
				PlayerID: int32(targetID),
			})
			if err != nil {
				log.Error("adjust points",
					"error", err,
					"game_id", gameID,
					"player_id", targetID,
					"amount", amount,
				)
				http.Error(w, "server error", http.StatusInternalServerError)
				return
			}
			log.Info("points adjusted",
				"game_id", gameID,
				"player_id", targetID,
				"amount", amount,
			)
			cache.Delete(gameID)
			w.Header().Set("HX-Trigger", "refreshTable")
			w.WriteHeader(http.StatusOK)

		case "spin":
			if state.Game.StateID != 3 {
				log.Info("spin requires turn state",
					"game_id", gameID,
					"state_id", state.Game.StateID,
				)
				http.Error(w, "cannot spin in current state", http.StatusConflict)
				return
			}
			if !state.isPlayerTurn(cookieKey) {
				log.Info("prohibiting non-turn player from spinning",
					"cookie_id", cookieID,
				)
				http.Error(w, "not your turn", http.StatusForbidden)
				return
			}
			id, err := strconv.Atoi(cookieID)
			if err != nil {
				log.Error("invalid player id",
					"game_id", gameID,
					"error", err,
				)
				http.Error(w, "invalid player id", http.StatusBadRequest)
				return
			}
			args := sqlc.GameCardsWheelSpinParams{
				ID:       gameID,
				PlayerID: pgtype.Int4{Int32: int32(id), Valid: true},
			}
			gcID, err := queries.GameCardsWheelSpin(r.Context(), args)
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					log.Info("game over, deck slot exhausted",
						"game_id", gameID,
					)
					err := queries.GameUpdate(r.Context(), sqlc.GameUpdateParams{
						ID:      gameID,
						StateID: 6, // game over
					})
					if err != nil {
						log.Error("update game state to game over",
							"error", err,
							"game_id", gameID,
						)
						http.Error(w, "server error while ending game", http.StatusInternalServerError)
						return
					}
					http.Error(w, "game over, deck slot exhausted", http.StatusGone)
					return
				}
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
			if state.Game.StateID != 3 {
				log.Info("next requires turn state",
					"game_id", gameID,
					"state_id", state.Game.StateID,
				)
				http.Error(w, "cannot advance in current state", http.StatusConflict)
				return
			}
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
				http.Error(w, "not your turn", http.StatusForbidden)
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
			if lastSpin.ModifierEffect.String != modFlip {
				log.Info("no pending flip modifier",
					"game_id", gameID,
				)
				http.Error(w, "no pending flip", http.StatusConflict)
				return
			}
			gcStr := r.URL.Query().Get("game_card_id")
			if gcStr == "" {
				log.Info("flip: missing game_card_id", "game_id", gameID)
				http.Error(w, "missing game_card_id", http.StatusBadRequest)
				return
			}
			gcID, err := strconv.Atoi(gcStr)
			if err != nil {
				log.Error("invalid game_card_id",
					"error", err,
					"game_id", gameID,
				)
				http.Error(w, "invalid game_card_id", http.StatusBadRequest)
				return
			}
			playerID, err := strconv.Atoi(cookieID)
			if err != nil {
				log.Error("invalid player id", "error", err, "game_id", gameID)
				http.Error(w, "invalid player id", http.StatusBadRequest)
				return
			}
			var ownedCard bool
			for _, c := range state.CardsPlayers {
				if c.ID == int32(gcID) && c.PlayerID.Int32 == int32(playerID) {
					ownedCard = true
					break
				}
			}
			if !ownedCard {
				log.Info("flip: card not owned by player",
					"game_id", gameID,
					"game_card_id", gcID,
					"player_id", playerID,
				)
				http.Error(w, "card not owned by player", http.StatusForbidden)
				return
			}
			err = queries.GameCardFlip(r.Context(), sqlc.GameCardFlipParams{
				ID:     int32(gcID),
				GameID: gameID,
			})
			if err != nil {
				log.Error("flip card",
					"error", err,
					"game_id", gameID,
					"game_card_id", gcID,
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
				"game_card_id", gcID,
			)
			cache.Delete(gameID)
			w.Header().Set("HX-Trigger", "refreshTable")
			w.WriteHeader(http.StatusOK)
		case "shred":
			if !state.isPlayerTurn(cookieKey) {
				log.Info("prohibiting non-turn player from shredding",
					"game_id", gameID,
					"cookie_id", cookieID,
				)
				http.Error(w, "not your turn", http.StatusForbidden)
				return
			}
			if state.Game.StateID != 4 {
				log.Info("shred requires pending state",
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
			if lastSpin.ModifierEffect.String != modShred {
				log.Info("pending modifier is not shred",
					"game_id", gameID,
					"effect", lastSpin.ModifierEffect.String,
				)
				http.Error(w, "no pending shred", http.StatusConflict)
				return
			}
			cardStr := r.URL.Query().Get("game_card_id")
			if cardStr == "" {
				log.Info("missing game_card_id", "game_id", gameID)
				http.Error(w, "missing game_card_id", http.StatusBadRequest)
				return
			}
			cardID, err := strconv.Atoi(cardStr)
			if err != nil {
				log.Error("invalid game_card_id",
					"error", err,
					"game_id", gameID,
				)
				http.Error(w, "invalid game_card_id", http.StatusBadRequest)
				return
			}
			shredPlayerID, err := strconv.Atoi(cookieID)
			if err != nil {
				log.Error("invalid player id", "error", err, "game_id", gameID)
				http.Error(w, "invalid player id", http.StatusBadRequest)
				return
			}
			var ownedShred bool
			for _, c := range state.CardsPlayers {
				if c.ID == int32(cardID) &&
					c.PlayerID.Int32 == int32(shredPlayerID) {
					ownedShred = true
					break
				}
			}
			if !ownedShred {
				log.Info("shred: card not owned by player",
					"game_id", gameID,
					"game_card_id", cardID,
					"player_id", shredPlayerID,
				)
				http.Error(w, "card not owned by player", http.StatusForbidden)
				return
			}
			err = queries.GameCardShred(r.Context(), sqlc.GameCardShredParams{
				ID:     int32(cardID),
				GameID: gameID,
			})
			if err != nil {
				log.Error("shred card",
					"error", err,
					"game_id", gameID,
					"game_card_id", cardID,
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
				log.Error("advance initiative after shred",
					"error", err,
					"game_id", gameID,
				)
				http.Error(w, "server error", http.StatusInternalServerError)
				return
			}
			log.Info("card shredded, modifier resolved",
				"game_id", gameID,
				"card_id", cardID,
			)
			cache.Delete(gameID)
			w.Header().Set("HX-Trigger", "refreshTable")
			w.WriteHeader(http.StatusOK)
		case "clone":
			if !state.isPlayerTurn(cookieKey) {
				log.Info("prohibiting non-turn player from cloning",
					"game_id", gameID,
					"cookie_id", cookieID,
				)
				http.Error(w, "not your turn", http.StatusForbidden)
				return
			}
			if state.Game.StateID != 4 {
				log.Info("clone requires pending state",
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
			if lastSpin.ModifierEffect.String != modClone {
				log.Info("pending modifier is not clone",
					"game_id", gameID,
					"effect", lastSpin.ModifierEffect.String,
				)
				http.Error(w, "no pending clone", http.StatusConflict)
				return
			}
			cardStr := r.URL.Query().Get("game_card_id")
			if cardStr == "" {
				log.Info("missing game_card_id", "game_id", gameID)
				http.Error(w, "missing game_card_id", http.StatusBadRequest)
				return
			}
			targetStr := r.URL.Query().Get("target_player_id")
			if targetStr == "" {
				log.Info("missing target_player_id", "game_id", gameID)
				http.Error(w, "missing target_player_id", http.StatusBadRequest)
				return
			}
			cardID, err := strconv.Atoi(cardStr)
			if err != nil {
				log.Error("invalid game_card_id",
					"error", err,
					"game_id", gameID,
				)
				http.Error(w, "invalid game_card_id", http.StatusBadRequest)
				return
			}
			targetID, err := strconv.Atoi(targetStr)
			if err != nil {
				log.Error("invalid target_player_id",
					"error", err,
					"game_id", gameID,
				)
				http.Error(w, "invalid target_player_id", http.StatusBadRequest)
				return
			}
			clonePlayerID, err := strconv.Atoi(cookieID)
			if err != nil {
				log.Error("invalid player id", "error", err, "game_id", gameID)
				http.Error(w, "invalid player id", http.StatusBadRequest)
				return
			}
			var ownedClone bool
			for _, c := range state.CardsPlayers {
				if c.ID == int32(cardID) &&
					c.PlayerID.Int32 == int32(clonePlayerID) {
					ownedClone = true
					break
				}
			}
			if !ownedClone {
				log.Info("clone: source card not owned by player",
					"game_id", gameID,
					"game_card_id", cardID,
					"player_id", clonePlayerID,
				)
				http.Error(w, "card not owned by player", http.StatusForbidden)
				return
			}
			var targetInGame bool
			for _, p := range state.Players {
				if p.PlayerID == int32(targetID) {
					targetInGame = true
					break
				}
			}
			if !targetInGame {
				log.Info("clone: target player not in game",
					"game_id", gameID,
					"target_player_id", targetID,
				)
				http.Error(w, "target player not in game", http.StatusBadRequest)
				return
			}
			err = queries.GameCardClone(r.Context(), sqlc.GameCardCloneParams{
				ID:     int32(cardID),
				GameID: gameID,
				PlayerID: pgtype.Int4{
					Int32: int32(targetID),
					Valid: true,
				},
			})
			if err != nil {
				log.Error("clone card",
					"error", err,
					"game_id", gameID,
					"game_card_id", cardID,
					"target_player_id", targetID,
				)
				http.Error(w, "server error", http.StatusInternalServerError)
				return
			}
			err = queries.GameUpdate(r.Context(), sqlc.GameUpdateParams{
				ID:      gameID,
				StateID: 3,
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
				log.Error("advance initiative after clone",
					"error", err,
					"game_id", gameID,
				)
				http.Error(w, "server error", http.StatusInternalServerError)
				return
			}
			log.Info("card cloned, modifier resolved",
				"game_id", gameID,
				"card_id", cardID,
				"target_player_id", targetID,
			)
			cache.Delete(gameID)
			w.Header().Set("HX-Trigger", "refreshTable")
			w.WriteHeader(http.StatusOK)

		case "transfer":
			if !state.isPlayerTurn(cookieKey) {
				log.Info("prohibiting non-turn player from transferring",
					"game_id", gameID,
					"cookie_id", cookieID,
				)
				http.Error(w, "not your turn", http.StatusForbidden)
				return
			}
			if state.Game.StateID != 4 {
				log.Info("transfer requires pending state",
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
			if lastSpin.ModifierEffect.String != modTransfer {
				log.Info("pending modifier is not transfer",
					"game_id", gameID,
					"effect", lastSpin.ModifierEffect.String,
				)
				http.Error(w, "no pending transfer", http.StatusConflict)
				return
			}
			cardStr := r.URL.Query().Get("game_card_id")
			if cardStr == "" {
				log.Info("missing game_card_id", "game_id", gameID)
				http.Error(w, "missing game_card_id", http.StatusBadRequest)
				return
			}
			targetStr := r.URL.Query().Get("target_player_id")
			if targetStr == "" {
				log.Info("missing target_player_id", "game_id", gameID)
				http.Error(w, "missing target_player_id", http.StatusBadRequest)
				return
			}
			cardID, err := strconv.Atoi(cardStr)
			if err != nil {
				log.Error("invalid game_card_id",
					"error", err,
					"game_id", gameID,
				)
				http.Error(w, "invalid game_card_id", http.StatusBadRequest)
				return
			}
			targetID, err := strconv.Atoi(targetStr)
			if err != nil {
				log.Error("invalid target_player_id",
					"error", err,
					"game_id", gameID,
				)
				http.Error(w, "invalid target_player_id", http.StatusBadRequest)
				return
			}
			xferPlayerID, err := strconv.Atoi(cookieID)
			if err != nil {
				log.Error("invalid player id", "error", err, "game_id", gameID)
				http.Error(w, "invalid player id", http.StatusBadRequest)
				return
			}
			var ownedXfer bool
			for _, c := range state.CardsPlayers {
				if c.ID == int32(cardID) &&
					c.PlayerID.Int32 == int32(xferPlayerID) {
					ownedXfer = true
					break
				}
			}
			if !ownedXfer {
				log.Info("transfer: card not owned by player",
					"game_id", gameID,
					"game_card_id", cardID,
					"player_id", xferPlayerID,
				)
				http.Error(w, "card not owned by player", http.StatusForbidden)
				return
			}
			var xferTargetInGame bool
			for _, p := range state.Players {
				if p.PlayerID == int32(targetID) {
					xferTargetInGame = true
					break
				}
			}
			if !xferTargetInGame {
				log.Info("transfer: target player not in game",
					"game_id", gameID,
					"target_player_id", targetID,
				)
				http.Error(w, "target player not in game", http.StatusBadRequest)
				return
			}
			err = queries.GameCardMove(r.Context(), sqlc.GameCardMoveParams{
				ID:     int32(cardID),
				GameID: gameID,
				PlayerID: pgtype.Int4{
					Int32: int32(targetID),
					Valid: true,
				},
			})
			if err != nil {
				log.Error("transfer card",
					"error", err,
					"game_id", gameID,
					"game_card_id", cardID,
					"target_player_id", targetID,
				)
				http.Error(w, "server error", http.StatusInternalServerError)
				return
			}
			err = queries.GameUpdate(r.Context(), sqlc.GameUpdateParams{
				ID:      gameID,
				StateID: 3,
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
				log.Error("advance initiative after transfer",
					"error", err,
					"game_id", gameID,
				)
				http.Error(w, "server error", http.StatusInternalServerError)
				return
			}
			log.Info("card transferred, modifier resolved",
				"game_id", gameID,
				"card_id", cardID,
				"target_player_id", targetID,
			)
			cache.Delete(gameID)
			w.Header().Set("HX-Trigger", "refreshTable")
			w.WriteHeader(http.StatusOK)

		case "accuse":
			if !state.isGameActive() {
				log.Info("accuse requires active game state",
					"game_id", gameID,
					"state_id", state.Game.StateID,
				)
				http.Error(w, "cannot accuse in current state", http.StatusConflict)
				return
			}
			defendantStr := r.FormValue("defendant_id")
			if defendantStr == "" {
				log.Info("missing defendant_id", "game_id", gameID)
				http.Error(w, "missing defendant_id", http.StatusBadRequest)
				return
			}
			gcStr := r.FormValue("game_card_id")
			if gcStr == "" {
				log.Info("missing game_card_id", "game_id", gameID)
				http.Error(w, "missing game_card_id", http.StatusBadRequest)
				return
			}
			defendantID, err := strconv.Atoi(defendantStr)
			if err != nil {
				log.Error("invalid defendant_id",
					"error", err,
					"game_id", gameID,
				)
				http.Error(w, "invalid defendant_id", http.StatusBadRequest)
				return
			}
			gcID, err := strconv.Atoi(gcStr)
			if err != nil {
				log.Error("invalid game_card_id",
					"error", err,
					"game_id", gameID,
				)
				http.Error(w, "invalid game_card_id", http.StatusBadRequest)
				return
			}
			// validate game_card belongs to defendant and is a rule
			var validCard bool
			for _, c := range state.CardsPlayers {
				if c.ID == int32(gcID) &&
					c.PlayerID.Int32 == int32(defendantID) &&
					c.Type == "rule" {
					validCard = true
					break
				}
			}
			if !validCard {
				log.Info("invalid accusation target",
					"game_id", gameID,
					"game_card_id", gcID,
					"defendant_id", defendantID,
				)
				http.Error(w, "card not a rule held by defendant", http.StatusBadRequest)
				return
			}

			accuserID, err := strconv.Atoi(cookieID)
			if err != nil {
				log.Error("invalid accuser cookie",
					"error", err,
					"game_id", gameID,
				)
				http.Error(w, "invalid accuser", http.StatusBadRequest)
				return
			}
			infractionID, err := queries.InfractionCreate(
				r.Context(), sqlc.InfractionCreateParams{
					GameID:     gameID,
					GameCardID: int32(gcID),
					Accused:    int32(defendantID),
					Accuser:    int32(accuserID),
				},
			)
			if err != nil {
				log.Error("create infraction",
					"error", err,
					"game_id", gameID,
				)
				http.Error(w, "server error", http.StatusInternalServerError)
				return
			}
			// transition to challenge state
			err = queries.GameUpdate(r.Context(), sqlc.GameUpdateParams{
				ID:      gameID,
				StateID: 5, // challenge
				InitiativeCurrent: pgtype.Int4{
					Int32: state.Game.InitiativeCurrent.Int32,
					Valid: true,
				},
			})
			if err != nil {
				log.Error("transition to challenge",
					"error", err,
					"game_id", gameID,
				)
				http.Error(w, "server error", http.StatusInternalServerError)
				return
			}
			log.Info("infraction created",
				"game_id",       gameID,
				"infraction_id", infractionID,
				"accused",       defendantID,
				"accuser",       accuserID,
				"game_card_id",  gcID,
			)
			cache.Delete(gameID)
			w.Header().Set("HX-Trigger", fmt.Sprintf(
				`{"refreshTable":null,"infractionCreated":{"id":%d}}`,
				infractionID,
			))
			w.WriteHeader(http.StatusOK)

		case "decide":
			if !state.isHost(cookieKey) {
				log.Info("prohibiting non-host from deciding")
				http.Error(w, "only host can decide", http.StatusForbidden)
				return
			}
			if state.Game.StateID != 5 {
				log.Info("decide requires challenge state",
					"game_id", gameID,
					"state_id", state.Game.StateID,
				)
				http.Error(w, "no active challenge", http.StatusConflict)
				return
			}
			infStr := r.FormValue("infraction_id")
			if infStr == "" {
				log.Info("missing infraction_id", "game_id", gameID)
				http.Error(w, "missing infraction_id", http.StatusBadRequest)
				return
			}
			verdict := r.FormValue("verdict")
			if verdict == "" {
				log.Info("missing verdict", "game_id", gameID)
				http.Error(w, "missing verdict", http.StatusBadRequest)
				return
			}
			if verdict != "affirm" && verdict != "absolve" {
				log.Info("invalid verdict",
					"game_id", gameID,
					"verdict", verdict,
				)
				http.Error(w, "verdict must be affirm or absolve", http.StatusBadRequest)
				return
			}
			infID, err := strconv.Atoi(infStr)
			if err != nil {
				log.Error("invalid infraction_id",
					"error", err,
					"game_id", gameID,
				)
				http.Error(w, "invalid infraction_id", http.StatusBadRequest)
				return
			}

			// verify infraction exists, is active, and belongs to this game
			infraction, err := queries.InfractionGet(r.Context(), int32(infID))
			if errors.Is(err, pgx.ErrNoRows) {
				log.Info("infraction not found",
					"game_id", gameID,
					"infraction_id", infID,
				)
				http.Error(w, "infraction not found", http.StatusNotFound)
				return
			}
			if err != nil {
				log.Error("get infraction",
					"error", err,
					"game_id", gameID,
					"infraction_id", infID,
				)
				http.Error(w, "server error", http.StatusInternalServerError)
				return
			}
			if !infraction.Active.Bool {
				log.Info("infraction already decided",
					"game_id", gameID,
					"infraction_id", infID,
				)
				http.Error(w, "infraction already decided", http.StatusConflict)
				return
			}
			if infraction.GameID != gameID {
				log.Info("infraction does not belong to this game",
					"game_id", gameID,
					"infraction_game_id", infraction.GameID,
					"infraction_id", infID,
				)
				http.Error(w, "infraction not in this game", http.StatusForbidden)
				return
			}

			affirmed := verdict == "affirm"
			var penalty int32
			var pointsPlayerID int32
			if affirmed {
				ptsStr := r.FormValue("amount")
				if ptsStr == "" {
					log.Info("missing amount for affirm", "game_id", gameID)
					http.Error(w, "missing amount", http.StatusBadRequest)
					return
				}
				pts, err := strconv.Atoi(ptsStr)
				if err != nil {
					log.Error("affirm: invalid amount",
						"error",   err,
						"amount",  ptsStr,
						"game_id", gameID,
					)
					http.Error(w, "invalid amount", http.StatusBadRequest)
					return
				}
				penalty = int32(pts)

				pidStr := r.FormValue("player_id")
				if pidStr == "" {
					log.Info("missing player_id for affirm", "game_id", gameID)
					http.Error(w, "missing player_id", http.StatusBadRequest)
					return
				}
				pid, err := strconv.Atoi(pidStr)
				if err != nil {
					log.Error("affirm: invalid accused player_id",
						"error",     err,
						"player_id", pidStr,
						"game_id",   gameID,
					)
					http.Error(w, "invalid player_id", http.StatusBadRequest)
					return
				}
				pointsPlayerID = int32(pid)
			}

			tx, err := dbPool.Begin(r.Context())
			if err != nil {
				log.Error("begin transaction",
					"error", err,
					"game_id", gameID,
				)
				http.Error(w, "server error", http.StatusInternalServerError)
				return
			}
			defer tx.Rollback(r.Context())
			txq := queries.WithTx(tx)

			_, err = txq.InfractionDecide(r.Context(), sqlc.InfractionDecideParams{
				ID:       int32(infID),
				Affirmed: pgtype.Bool{Bool: affirmed, Valid: true},
				Points:   pgtype.Int4{Int32: penalty, Valid: true},
			})
			if errors.Is(err, pgx.ErrNoRows) {
				log.Info("infraction already decided (race)",
					"game_id", gameID,
					"infraction_id", infID,
				)
				http.Error(w, "infraction already decided", http.StatusConflict)
				return
			}
			if err != nil {
				log.Error("decide infraction",
					"error", err,
					"game_id", gameID,
					"infraction_id", infID,
				)
				http.Error(w, "server error", http.StatusInternalServerError)
				return
			}

			// adjust points if affirmed
			if affirmed {
				err = txq.GamePointsAdjust(
					r.Context(), sqlc.GamePointsAdjustParams{
						Points:   pgtype.Int4{Int32: penalty, Valid: true},
						GameID:   gameID,
						PlayerID: pointsPlayerID,
					},
				)
				if err != nil {
					log.Error("adjust points",
						"error",     err,
						"game_id",   gameID,
						"player_id", pointsPlayerID,
						"points",    penalty,
					)
					http.Error(w, "server error", http.StatusInternalServerError)
					return
				}
			}

			// return to turn state
			err = txq.GameUpdate(r.Context(), sqlc.GameUpdateParams{
				ID:      gameID,
				StateID: 3,
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

			err = tx.Commit(r.Context())
			if err != nil {
				log.Error("commit decide transaction",
					"error", err,
					"game_id", gameID,
				)
				http.Error(w, "server error", http.StatusInternalServerError)
				return
			}
			log.Info("infraction decided",
				"game_id", gameID,
				"infraction_id", infID,
				"verdict", verdict,
				"points", penalty,
			)
			cache.Delete(gameID)
			w.Header().Set("HX-Trigger", "refreshTable")
			w.WriteHeader(http.StatusOK)
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
