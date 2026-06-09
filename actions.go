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
	"go.opentelemetry.io/otel/trace"
)

const minimumPlayers = 2 // number of non-host players required to start

func actionHandler(w http.ResponseWriter, r *http.Request) {
	pathLong := strings.TrimPrefix(r.URL.Path, "/")
	parts := strings.Split(pathLong, "/")
	if len(parts) != 3 {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	gameID := parts[0]
	action := parts[2]
	span := trace.SpanFromContext(r.Context())
	span.SetAttributes(
		attrGameID.String(gameID),
		attrAction.String(action),
	)
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
	span.SetAttributes(attrPlayerID.String(cookieID))
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
	err = state.callerInfo(cookieKey)
	if err != nil {
		log.Error("populate caller info", "error", err)
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	span.SetAttributes(
		attrStateID.Int(int(state.Game.StateID)),
		attrCallerName.String(state.CallerName),
	)
	switch state.Game.StateID {
	case stateOver: // game over
		log.Info("request to ended game", "game_id", gameID)
		http.Error(w, "game over", http.StatusGone)
		return
	case stateInviting, stateCreated: // pregame
		switch action {
		case "start":
			if !state.isHost(cookieKey) {
				log.Warn("non-host attempted to start game", "game_id", gameID)
				http.Error(w, "only the host can start the game", http.StatusForbidden)
				return
			}
			// require a minimum of non-host players, otherwise no player holds
			// the starting initiative and the game would soft-lock. surface a
			// notice via HX-Trigger (200, no swap) rather than a raw http error
			if state.nonHostPlayers() < minimumPlayers {
				log.Warn("host attempted to start game without enough players",
					"game_id", gameID,
					"non_host_players", state.nonHostPlayers(),
					"minimum", minimumPlayers,
				)
				w.Header().Set("HX-Trigger", fmt.Sprintf(
					`{"notice":"Need at least %d non-host players to start the game."}`,
					minimumPlayers,
				))
				w.WriteHeader(http.StatusOK)
				return
			}
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
				StateID:           stateReady, // in progress
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
				StateID:           stateTurn,
				InitiativeCurrent: pgtype.Int4{Int32: 1, Valid: true},
			})
			if err != nil {
				log.Error("update initiative", "error", err)
				http.Error(w, "server error", http.StatusInternalServerError)
			}
			log.Info("initiative initiated", "state", "ready", "initiative", 1)

			// log the start, and the first player's turn so they hear the ding.
			// state is already committed, so these are best-effort: a failure
			// shouldn't 500 a game that has already started.
			if err := recordEvent(r.Context(), log, queries, sqlc.EventCreateParams{
				GameID:    gameID,
				EventType: "start",
			}); err != nil {
				log.Error("log start event", "error", err, "game_id", gameID)
			}
			firstPlayer, err := queries.InitiativeCurrentPlayer(r.Context(), gameID)
			if err != nil {
				log.Error("find first turn player", "error", err, "game_id", gameID)
			} else if err := recordEvent(r.Context(), log, queries, sqlc.EventCreateParams{
				GameID:    gameID,
				EventType: "turn",
				TargetID:  pgInt(firstPlayer),
			}); err != nil {
				log.Error("log first turn event", "error", err, "game_id", gameID)
			}

			// invalidate cache for this game
			cache.Delete(gameID)
			w.Header().Set("HX-Trigger", "refreshTable")
			w.WriteHeader(http.StatusOK)
			return
		default:
			log.Info(ErrActionInvalid.Error())
			http.Error(w, ErrActionInvalid.Error(), http.StatusTooEarly)
			return
		}
	case stateEnding, stateChallenge, statePending, stateTurn, stateReady: // in progress (6 = deck spent, host to end)
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
			if playerStr == "" {
				log.Info("missing player_id", "game_id", gameID)
				http.Error(w, "missing player_id", http.StatusBadRequest)
				return
			}
			amountStr := r.FormValue("amount")
			if amountStr == "" {
				log.Info("missing amount", "game_id", gameID)
				http.Error(w, "missing amount", http.StatusBadRequest)
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
			if amount == 0 {
				// no-op adjustment: nothing to record
				w.Header().Set("HX-Trigger", "refreshTable")
				w.WriteHeader(http.StatusOK)
				return
			}
			tx, err := dbPool.Begin(r.Context())
			if err != nil {
				log.Error("begin transaction", "error", err, "game_id", gameID)
				http.Error(w, "server error", http.StatusInternalServerError)
				return
			}
			defer tx.Rollback(r.Context())
			txq := queries.WithTx(tx)

			err = txq.GamePointsAdjust(r.Context(), sqlc.GamePointsAdjustParams{
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
			// record the points change; no infraction here, just a host adjustment
			pcID, err := txq.PointChangeCreate(r.Context(), sqlc.PointChangeCreateParams{
				GameID:       gameID,
				PlayerID:     pgtype.Int4{Int32: int32(targetID), Valid: true},
				Delta:        int32(amount),
				InfractionID: pgtype.Int4{Valid: false},
			})
			if err != nil {
				log.Error("record point change",
					"error", err,
					"game_id", gameID,
					"player_id", targetID,
				)
				http.Error(w, "server error", http.StatusInternalServerError)
				return
			}
			// add an event so the change shows in the feed and plays a sound
			if err := writeEvent(w, r, log, txq, sqlc.EventCreateParams{
				GameID:        gameID,
				EventType:     "points",
				TargetID:      pgInt(int32(targetID)),
				PointChangeID: pgInt(pcID),
			}); err != nil {
				return
			}
			if err = tx.Commit(r.Context()); err != nil {
				log.Error("commit points transaction", "error", err, "game_id", gameID)
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
			if state.Game.StateID != stateTurn {
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
			prevSpin, err := queries.SpinPendingModifier(r.Context(), gameID)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				log.Error("check previous spin", "error", err, "game_id", gameID)
				http.Error(w, "server error", http.StatusInternalServerError)
				return
			}
			if err == nil &&
				prevSpin.PlayerID.Int32 == int32(id) &&
				!prevSpin.ModifierEffect.Valid {
				log.Info("player already spun this turn",
					"game_id", gameID,
					"player_id", id,
				)
				http.Error(w, "already spun this turn", http.StatusConflict)
				return
			}
			args := sqlc.GameCardsWheelSpinParams{
				ID:       gameID,
				PlayerID: pgtype.Int4{Int32: int32(id), Valid: true},
			}
			gcID, err := queries.GameCardsWheelSpin(r.Context(), args)
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					// the deck is spent: don't end outright. move to the
					// "ending" state so everyone sees the end was rolled, and
					// leave the host a button to actually end the game.
					log.Info("deck slot exhausted, waiting on host to end",
						"game_id", gameID,
					)
					err := queries.GameUpdate(r.Context(), sqlc.GameUpdateParams{
						ID:      gameID,
						StateID: stateEnding,
						InitiativeCurrent: pgtype.Int4{
							Int32: state.Game.InitiativeCurrent.Int32,
							Valid: true,
						},
					})
					if err != nil {
						log.Error("update game state to ending",
							"error", err,
							"game_id", gameID,
						)
						http.Error(w, "server error while ending game", http.StatusInternalServerError)
						return
					}
					if err := writeEvent(w, r, log, queries, sqlc.EventCreateParams{
						GameID:    gameID,
						EventType: "rolled-end",
						ActorID:   pgInt(int32(id)),
					}); err != nil {
						return
					}
					cache.Delete(gameID)
					w.Header().Set("HX-Trigger", "refreshTable")
					w.WriteHeader(http.StatusOK)
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
			lastSpin, err := queries.SpinPendingModifier(
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
			// add an event for the spin (feed + the spinner's sound)
			if err := writeEvent(w, r, log, queries, sqlc.EventCreateParams{
				GameID:    gameID,
				EventType: "spin",
				ActorID:   pgInt(int32(id)),
				SpinID:    pgInt(lastSpin.ID),
			}); err != nil {
				return
			}
			if !lastSpin.ModifierEffect.Valid {
				err = advanceTurn(r.Context(), log, queries, gameID)
				if err != nil {
					log.Error("advance initiative after spin",
						"error", err,
						"game_id", gameID,
					)
					http.Error(w, "server error", http.StatusInternalServerError)
					return
				}
				cache.Delete(gameID)
				fresh, err := stateFromCacheOrDB(r.Context(), &cache, gameID)
				if err != nil {
					log.Error("refresh state after spin", "error", err)
					w.Header().Set("HX-Trigger", "refreshTable")
					w.WriteHeader(http.StatusOK)
					return
				}
				cardContent := ""
				for _, c := range fresh.CardsPlayers {
					if c.ID == gcID {
						if s, ok := c.Content.(string); ok {
							cardContent = s
						}
						break
					}
				}
				trigger := `{"refreshTable":null`
				if cardContent != "" {
					// newRule shows the toast but stays silent; the spin event
					// already dings the spinner, so notice would double up.
					trigger += `,"newRule":` + strconv.Quote("new rule: "+cardContent)
				}
				trigger += `}`
				w.Header().Set("HX-Trigger", trigger)
				w.WriteHeader(http.StatusOK)
				return
			}

			// check if player has any rule cards to target
			var hasRuleCards bool
			for _, c := range state.CardsPlayers {
				if c.PlayerID.Int32 == int32(id) && c.Type == "rule" {
					hasRuleCards = true
					break
				}
			}
			if !hasRuleCards {
				err = queries.GameCardShred(r.Context(), sqlc.GameCardShredParams{
					ID:     gcID,
					GameID: gameID,
				})
				if err != nil {
					log.Error("shred unresolvable modifier",
						"error", err,
						"game_id", gameID,
						"game_card_id", gcID,
					)
					http.Error(w, "server error", http.StatusInternalServerError)
					return
				}
				log.Info("modifier drawn but player has no rule cards, shredded and skipping pending",
					"game_id", gameID,
					"effect", lastSpin.ModifierEffect.String,
					"player_id", id,
					"game_card_id", gcID,
				)
				cache.Delete(gameID)
				w.Header().Set("HX-Trigger",
					`{"refreshTable":null,"modifierShredded":"`+lastSpin.ModifierEffect.String+`"}`)
				w.WriteHeader(http.StatusOK)
				return
			}

			log.Info("modifier drawn, entering pending state",
				"game_id", gameID,
				"effect", lastSpin.ModifierEffect.String,
				"player_id", id,
			)
			err = queries.GameUpdate(r.Context(), sqlc.GameUpdateParams{
				ID:      gameID,
				StateID: statePending,
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
			cache.Delete(gameID)
			w.Header().Set("HX-Trigger", `{"refreshTable":null,"loadModifier":null}`)
			w.WriteHeader(http.StatusOK)

		case "pause":
			if !state.isHost(cookieKey) {
				log.Info("prohibiting non-host from pausing")
				http.Error(w, "only host can pause", http.StatusForbidden)
				return
			}
			if state.Game.StateID != stateTurn {
				log.Info("pause requires turn state",
					"game_id", gameID,
					"state_id", state.Game.StateID,
				)
				http.Error(w, "cannot pause in current state", http.StatusConflict)
				return
			}
			err := queries.GameUpdate(r.Context(), sqlc.GameUpdateParams{
				ID:      gameID,
				StateID: stateReady,
				InitiativeCurrent: pgtype.Int4{
					Int32: state.Game.InitiativeCurrent.Int32,
					Valid: true,
				},
			})
			if err != nil {
				log.Error("pause game", "error", err, "game_id", gameID)
				http.Error(w, "server error", http.StatusInternalServerError)
				return
			}
			log.Info("game paused", "game_id", gameID)
			if err := writeEvent(w, r, log, queries, sqlc.EventCreateParams{
				GameID:    gameID,
				EventType: "pause",
			}); err != nil {
				return
			}
			cache.Delete(gameID)
			w.Header().Set("HX-Trigger", "refreshTable")
			w.WriteHeader(http.StatusOK)
			return

		case "resume":
			if !state.isHost(cookieKey) {
				log.Info("prohibiting non-host from resuming")
				http.Error(w, "only host can resume", http.StatusForbidden)
				return
			}
			if state.Game.StateID != stateReady {
				log.Info("resume requires ready state",
					"game_id", gameID,
					"state_id", state.Game.StateID,
				)
				http.Error(w, "cannot resume in current state", http.StatusConflict)
				return
			}
			err := queries.GameUpdate(r.Context(), sqlc.GameUpdateParams{
				ID:      gameID,
				StateID: stateTurn,
				InitiativeCurrent: pgtype.Int4{
					Int32: state.Game.InitiativeCurrent.Int32,
					Valid: true,
				},
			})
			if err != nil {
				log.Error("resume game", "error", err, "game_id", gameID)
				http.Error(w, "server error", http.StatusInternalServerError)
				return
			}
			log.Info("game resumed", "game_id", gameID)
			if err := writeEvent(w, r, log, queries, sqlc.EventCreateParams{
				GameID:    gameID,
				EventType: "resume",
			}); err != nil {
				return
			}
			cache.Delete(gameID)
			w.Header().Set("HX-Trigger", "refreshTable")
			w.WriteHeader(http.StatusOK)
			return

		case "endgame":
			// the host finalizes a game whose deck has run out. only valid in
			// the "ending" state, which a spent deck puts the game into.
			if !state.isHost(cookieKey) {
				log.Info("prohibiting non-host from ending game")
				http.Error(w, "only host can end the game", http.StatusForbidden)
				return
			}
			if state.Game.StateID != stateEnding {
				log.Info("endgame requires ending state",
					"game_id", gameID,
					"state_id", state.Game.StateID,
				)
				http.Error(w, "cannot end game in current state", http.StatusConflict)
				return
			}
			err := queries.GameUpdate(r.Context(), sqlc.GameUpdateParams{
				ID:      gameID,
				StateID: stateOver,
			})
			if err != nil {
				log.Error("end game", "error", err, "game_id", gameID)
				http.Error(w, "server error", http.StatusInternalServerError)
				return
			}
			log.Info("game ended by host", "game_id", gameID)
			if err := writeEvent(w, r, log, queries, sqlc.EventCreateParams{
				GameID:    gameID,
				EventType: "end",
			}); err != nil {
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
			if state.Game.StateID != statePending {
				log.Info("flip requires pending state",
					"game_id", gameID,
					"state_id", state.Game.StateID,
				)
				http.Error(w, "no pending modifier", http.StatusConflict)
				return
			}
			lastSpin, err := queries.SpinPendingModifier(
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
			// add an event for the flip
			if err := writeEvent(w, r, log, queries, sqlc.EventCreateParams{
				GameID:     gameID,
				EventType:  "flip",
				ActorID:    pgInt(int32(playerID)),
				GameCardID: pgInt(int32(gcID)),
			}); err != nil {
				return
			}
			// shred the modifier card that was just used
			for _, c := range state.CardsPlayers {
				if c.PlayerID.Int32 == int32(playerID) && c.Type == "modifier" {
					err = queries.GameCardShred(r.Context(), sqlc.GameCardShredParams{
						ID:     c.ID,
						GameID: gameID,
					})
					if err != nil {
						log.Error("shred used modifier card",
							"error", err,
							"game_id", gameID,
							"game_card_id", c.ID,
						)
					}
					break
				}
			}

			// resolve: back to turn state and advance initiative
			err = queries.GameUpdate(r.Context(), sqlc.GameUpdateParams{
				ID:      gameID,
				StateID: stateTurn,
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
			err = advanceTurn(r.Context(), log, queries, gameID)
			if err != nil {
				log.Error("advance initiative after flip",
					"error", err,
					"game_id", gameID,
				)
				http.Error(w, "server error", http.StatusInternalServerError)
				return
			}
			log.Info("card flipped, modifier resolved and shredded",
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
			if state.Game.StateID != statePending {
				log.Info("shred requires pending state",
					"game_id", gameID,
					"state_id", state.Game.StateID,
				)
				http.Error(w, "no pending modifier", http.StatusConflict)
				return
			}
			lastSpin, err := queries.SpinPendingModifier(
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
			// add an event for the shred
			if err := writeEvent(w, r, log, queries, sqlc.EventCreateParams{
				GameID:     gameID,
				EventType:  "shred",
				ActorID:    pgInt(int32(shredPlayerID)),
				GameCardID: pgInt(int32(cardID)),
			}); err != nil {
				return
			}
			// shred the modifier card that was just used
			for _, c := range state.CardsPlayers {
				if c.PlayerID.Int32 == int32(shredPlayerID) && c.Type == "modifier" {
					err = queries.GameCardShred(r.Context(), sqlc.GameCardShredParams{
						ID:     c.ID,
						GameID: gameID,
					})
					if err != nil {
						log.Error("shred used modifier card",
							"error", err,
							"game_id", gameID,
							"game_card_id", c.ID,
						)
					}
					break
				}
			}

			// resolve: back to turn state and advance initiative
			err = queries.GameUpdate(r.Context(), sqlc.GameUpdateParams{
				ID:      gameID,
				StateID: stateTurn,
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
			err = advanceTurn(r.Context(), log, queries, gameID)
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
			if state.Game.StateID != statePending {
				log.Info("clone requires pending state",
					"game_id", gameID,
					"state_id", state.Game.StateID,
				)
				http.Error(w, "no pending modifier", http.StatusConflict)
				return
			}
			lastSpin, err := queries.SpinPendingModifier(
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
			// add an event for the clone (cloner -> recipient)
			if err := writeEvent(w, r, log, queries, sqlc.EventCreateParams{
				GameID:     gameID,
				EventType:  "clone",
				ActorID:    pgInt(int32(clonePlayerID)),
				TargetID:   pgInt(int32(targetID)),
				GameCardID: pgInt(int32(cardID)),
			}); err != nil {
				return
			}
			// shred the modifier card that was just used
			for _, c := range state.CardsPlayers {
				if c.PlayerID.Int32 == int32(clonePlayerID) && c.Type == "modifier" {
					err = queries.GameCardShred(r.Context(), sqlc.GameCardShredParams{
						ID:     c.ID,
						GameID: gameID,
					})
					if err != nil {
						log.Error("shred used modifier card",
							"error", err,
							"game_id", gameID,
							"game_card_id", c.ID,
						)
					}
					break
				}
			}
			err = queries.GameUpdate(r.Context(), sqlc.GameUpdateParams{
				ID:      gameID,
				StateID: stateTurn,
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
			err = advanceTurn(r.Context(), log, queries, gameID)
			if err != nil {
				log.Error("advance initiative after clone",
					"error", err,
					"game_id", gameID,
				)
				http.Error(w, "server error", http.StatusInternalServerError)
				return
			}
			log.Info("card cloned, modifier resolved and shredded",
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
			if state.Game.StateID != statePending {
				log.Info("transfer requires pending state",
					"game_id", gameID,
					"state_id", state.Game.StateID,
				)
				http.Error(w, "no pending modifier", http.StatusConflict)
				return
			}
			lastSpin, err := queries.SpinPendingModifier(
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
			// add an event for the transfer (sender -> recipient)
			if err := writeEvent(w, r, log, queries, sqlc.EventCreateParams{
				GameID:     gameID,
				EventType:  "transfer",
				ActorID:    pgInt(int32(xferPlayerID)),
				TargetID:   pgInt(int32(targetID)),
				GameCardID: pgInt(int32(cardID)),
			}); err != nil {
				return
			}
			// shred the modifier card that was just used
			for _, c := range state.CardsPlayers {
				if c.PlayerID.Int32 == int32(xferPlayerID) && c.Type == "modifier" {
					err = queries.GameCardShred(r.Context(), sqlc.GameCardShredParams{
						ID:     c.ID,
						GameID: gameID,
					})
					if err != nil {
						log.Error("shred used modifier card",
							"error", err,
							"game_id", gameID,
							"game_card_id", c.ID,
						)
					}
					break
				}
			}
			err = queries.GameUpdate(r.Context(), sqlc.GameUpdateParams{
				ID:      gameID,
				StateID: stateTurn,
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
			err = advanceTurn(r.Context(), log, queries, gameID)
			if err != nil {
				log.Error("advance initiative after transfer",
					"error", err,
					"game_id", gameID,
				)
				http.Error(w, "server error", http.StatusInternalServerError)
				return
			}
			log.Info("card transferred, modifier resolved and shredded",
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
			tx, err := dbPool.Begin(r.Context())
			if err != nil {
				log.Error("begin transaction", "error", err, "game_id", gameID)
				http.Error(w, "server error", http.StatusInternalServerError)
				return
			}
			defer tx.Rollback(r.Context())
			txq := queries.WithTx(tx)

			infractionID, err := txq.InfractionCreate(
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
			err = txq.GameUpdate(r.Context(), sqlc.GameUpdateParams{
				ID:      gameID,
				StateID: stateChallenge,
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
			// add an event: the accuser accuses the accused
			if err := writeEvent(w, r, log, txq, sqlc.EventCreateParams{
				GameID:       gameID,
				EventType:    "accuse",
				ActorID:      pgInt(int32(accuserID)),
				TargetID:     pgInt(int32(defendantID)),
				InfractionID: pgInt(infractionID),
			}); err != nil {
				return
			}
			if err = tx.Commit(r.Context()); err != nil {
				log.Error("commit accuse transaction", "error", err, "game_id", gameID)
				http.Error(w, "server error", http.StatusInternalServerError)
				return
			}
			log.Info("infraction created",
				"game_id", gameID,
				"infraction_id", infractionID,
				"accused", defendantID,
				"accuser", accuserID,
				"game_card_id", gcID,
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
			if state.Game.StateID != stateChallenge {
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
			pointsPlayerID := infraction.Accused
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
						"error", err,
						"amount", ptsStr,
						"game_id", gameID,
					)
					http.Error(w, "invalid amount", http.StatusBadRequest)
					return
				}
				// the affirm UI sends a positive penalty; negate it so the
				// ledger deducts (GamePointsAdjust adds the delta).
				penalty = -int32(pts)
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

			// adjust points if affirmed (a zero penalty means guilty but no
			// points change, so skip the adjustment and its event)
			if affirmed && penalty != 0 {
				err = txq.GamePointsAdjust(
					r.Context(), sqlc.GamePointsAdjustParams{
						Points:   pgtype.Int4{Int32: penalty, Valid: true},
						GameID:   gameID,
						PlayerID: pointsPlayerID,
					},
				)
				if err != nil {
					log.Error("adjust points",
						"error", err,
						"game_id", gameID,
						"player_id", pointsPlayerID,
						"points", penalty,
					)
					http.Error(w, "server error", http.StatusInternalServerError)
					return
				}
				// record the points change, linked to the infraction that caused it
				pcID, err := txq.PointChangeCreate(r.Context(), sqlc.PointChangeCreateParams{
					GameID:       gameID,
					PlayerID:     pgtype.Int4{Int32: pointsPlayerID, Valid: true},
					Delta:        penalty,
					InfractionID: pgtype.Int4{Int32: int32(infID), Valid: true},
				})
				if err != nil {
					log.Error("record point change",
						"error", err,
						"game_id", gameID,
						"infraction_id", infID,
					)
					http.Error(w, "server error", http.StatusInternalServerError)
					return
				}
				// the accused lost points: event for the feed and their sound
				if err := writeEvent(w, r, log, txq, sqlc.EventCreateParams{
					GameID:        gameID,
					EventType:     "points",
					TargetID:      pgInt(pointsPlayerID),
					PointChangeID: pgInt(pcID),
				}); err != nil {
					return
				}
			}

			// stay in challenge while infractions remain queued, so the
			// host keeps getting prompted for the next one; otherwise return
			// to turn state
			remaining, err := txq.InfractionsActiveCount(r.Context(), gameID)
			if err != nil {
				log.Error("count active infractions",
					"error", err,
					"game_id", gameID,
				)
				http.Error(w, "server error", http.StatusInternalServerError)
				return
			}
			nextState := int32(stateTurn) // turn
			if remaining > 0 {
				nextState = stateChallenge // challenge
			}
			err = txq.GameUpdate(r.Context(), sqlc.GameUpdateParams{
				ID:      gameID,
				StateID: nextState,
				InitiativeCurrent: pgtype.Int4{
					Int32: state.Game.InitiativeCurrent.Int32,
					Valid: true,
				},
			})
			if err != nil {
				log.Error("transition state after decide",
					"error", err,
					"game_id", gameID,
				)
				http.Error(w, "server error", http.StatusInternalServerError)
				return
			}

			// add an event for the verdict (feed + the accuser's sound)
			if err := writeEvent(w, r, log, txq, sqlc.EventCreateParams{
				GameID:       gameID,
				EventType:    "decide",
				TargetID:     pgInt(infraction.Accuser),
				InfractionID: pgInt(int32(infID)),
			}); err != nil {
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
				StateID:           stateOver, // game over
				InitiativeCurrent: pgtype.Int4{Int32: 0, Valid: true},
			})
			if err != nil {
				log.Error("end game", "error", err)
				http.Error(w, "server error", http.StatusInternalServerError)
				return
			}
			if err := writeEvent(w, r, log, queries, sqlc.EventCreateParams{
				GameID:    gameID,
				EventType: "end",
			}); err != nil {
				return
			}
			cache.Delete(gameID)
			log.Info("game ended")
			w.WriteHeader(http.StatusGone)
			return
		default:
			log.Info("unsupported action requested")
			http.Error(w, "unsupported action", http.StatusNotImplemented)
		}
	}
}
