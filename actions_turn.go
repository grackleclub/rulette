package main

import (
	"errors"
	"net/http"
	"strconv"

	sqlc "github.com/grackleclub/rulette/db/sqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// actionSpin draws a card from the wheel for the turn player and routes
// by what came up: a prompt starts a timed challenge, a rule waits for
// the spinner's acknowledgement, and a modifier enters the pending state
// (or is shredded outright when it has no legal target). A spent deck
// parks the game in the ending state for the host to end or continue.
func actionSpin(a *actionRequest) error {
	ctx := a.r.Context()
	id := a.caller()
	prevSpin, err := queries.SpinPendingModifier(ctx, a.gameID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return serverErr("check previous spin", err)
	}
	if err == nil && prevSpin.PlayerID.Int32 == id &&
		!prevSpin.ModifierEffect.Valid {
		return failf(http.StatusConflict, "already spun this turn")
	}
	gcID, err := queries.GameCardsWheelSpin(ctx, sqlc.GameCardsWheelSpinParams{
		ID:       a.gameID,
		PlayerID: pgtype.Int4{Int32: id, Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// the deck is spent: don't end outright. move to the "ending"
		// state so everyone sees the end was rolled, and leave the host
		// a button to actually end the game.
		a.log.Info("deck slot exhausted, waiting on host to end")
		if err := setGameState(ctx, queries, a.state, a.gameID, stateEnding); err != nil {
			return serverErr("update game state to ending", err)
		}
		if err := recordEvent(ctx, a.log, queries, sqlc.EventCreateParams{
			GameID:    a.gameID,
			EventType: "rolled-end",
			ActorID:   pgInt(id),
		}); err != nil {
			return serverErr("record rolled-end event", err)
		}
		a.refresh("refreshTable")
		return nil
	}
	if err != nil {
		return serverErr("spin wheel", err)
	}
	a.log.Info("wheel spun", "game_card_id", gcID)

	// check if drawn card is a modifier via spin log
	lastSpin, err := queries.SpinPendingModifier(ctx, a.gameID)
	if err != nil {
		return serverErr("check spin log modifier", err)
	}
	// add an event for the spin (feed + the spinner's sound)
	if err := recordEvent(ctx, a.log, queries, sqlc.EventCreateParams{
		GameID:    a.gameID,
		EventType: "spin",
		ActorID:   pgInt(id),
		SpinID:    pgInt(lastSpin.ID),
	}); err != nil {
		return serverErr("record spin event", err)
	}
	if lastSpin.Type == "prompt" {
		// a prompt card: a timed challenge the spinner performs and the
		// host judges. enter the prompt state so the host gets the
		// succeed/fail controls and other actions hold off. the turn
		// advances only once the host rules on it.
		if err := setGameState(ctx, queries, a.state, a.gameID, statePrompt); err != nil {
			return serverErr("transition to prompt", err)
		}
		a.log.Info("prompt drawn, entering prompt state",
			"prompt", lastSpin.Front,
		)
		// newPrompt opens the spinner's challenge popup and starts their
		// local countdown; the spin event already dinged the spinner.
		// carry the window so the client matches the server's promptSeconds.
		a.refresh(`{"refreshTable":null,"newPrompt":{"prompt":` +
			strconv.Quote(lastSpin.Front) +
			`,"window":` + strconv.Itoa(promptSeconds) + `}}`)
		return nil
	}
	if !lastSpin.ModifierEffect.Valid {
		// a rule card: hold the turn here. show the player the card they
		// drew and wait for them to acknowledge (POST /action/acknowledge),
		// which advances the turn. initiative stays on them until
		// they've seen it.
		cache.Delete(a.gameID)
		fresh, err := stateFromCacheOrDB(ctx, &cache, a.gameID)
		if err != nil {
			a.log.Error("refresh state after spin", "error", err)
			a.w.Header().Set("HX-Trigger", "refreshTable")
			a.w.WriteHeader(http.StatusOK)
			return nil
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
		// newCard shows the drawn card; the spin event already dinged
		// the spinner, so this stays silent.
		a.w.Header().Set("HX-Trigger", `{"refreshTable":null,"newCard":`+
			strconv.Quote(cardContent)+`}`)
		a.w.WriteHeader(http.StatusOK)
		return nil
	}

	effect := lastSpin.ModifierEffect.String
	// a modifier needs a target: without a rule card in the spinner's
	// hand (or another player, for clone and transfer) shred it outright
	// and skip the pending state.
	if _, ok := a.state.playerCardOfType(id, "rule"); !ok {
		return a.shredUnresolvableModifier(gcID, effect,
			"player has no rule cards")
	}
	if (effect == modClone || effect == modTransfer) &&
		a.state.nonHostPlayers() < 2 {
		return a.shredUnresolvableModifier(gcID, effect,
			"no other player to target")
	}

	a.log.Info("modifier drawn, entering pending state", "effect", effect)
	if err := setGameState(ctx, queries, a.state, a.gameID, statePending); err != nil {
		return serverErr("transition to pending", err)
	}
	a.refresh(`{"refreshTable":null,"loadModifier":null}`)
	return nil
}

// shredUnresolvableModifier discards a just-drawn modifier that has no
// legal target, skipping the pending state entirely.
func (a *actionRequest) shredUnresolvableModifier(
	gcID int32,
	effect, reason string,
) error {
	if err := queries.GameCardShred(a.r.Context(), sqlc.GameCardShredParams{
		ID:     gcID,
		GameID: a.gameID,
	}); err != nil {
		return serverErr("shred unresolvable modifier", err)
	}
	a.log.Info("modifier shredded, skipping pending: "+reason,
		"effect", effect,
		"game_card_id", gcID,
	)
	a.refresh(`{"refreshTable":null,"modifierShredded":"` + effect + `"}`)
	return nil
}

// actionAcknowledge advances the spinner's own turn after they've seen
// the rule card they drew.
func actionAcknowledge(a *actionRequest) error {
	ctx := a.r.Context()
	id := a.caller()
	lastSpin, err := queries.SpinPendingModifier(ctx, a.gameID)
	if errors.Is(err, pgx.ErrNoRows) ||
		(err == nil && (lastSpin.PlayerID.Int32 != id ||
			lastSpin.ModifierEffect.Valid)) {
		return failf(http.StatusConflict, "nothing to acknowledge")
	}
	if err != nil {
		return serverErr("check spin to acknowledge", err)
	}
	if err := advanceTurn(ctx, a.log, queries, a.gameID); err != nil {
		return serverErr("advance after acknowledge", err)
	}
	a.refresh("refreshTable")
	return nil
}

// actionAdvance is the host's escape hatch: advance a turn stuck on an
// unacknowledged card, or skip a chooser (prompt-shred or accusation-
// transfer) the player never used, so play can't wedge.
func actionAdvance(a *actionRequest) error {
	ctx := a.r.Context()
	switch a.state.Game.StateID {
	case stateTurn:
		if !a.state.AwaitingAck {
			return failf(http.StatusConflict, "nothing to advance")
		}
		if err := advanceTurn(ctx, a.log, queries, a.gameID); err != nil {
			return serverErr("advance turn by host", err)
		}
		a.log.Info("host advanced initiative")
	case statePromptShred:
		// the spinner never shredded: skip for them and advance.
		tx, txq, err := a.begin()
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		if err := setGameState(ctx, txq, a.state, a.gameID, stateTurn); err != nil {
			return serverErr("host skip prompt shred", err)
		}
		if err := advanceTurn(ctx, a.log, txq, a.gameID); err != nil {
			return serverErr("advance after host prompt-shred skip", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return serverErr("commit host prompt-shred skip", err)
		}
		a.log.Info("host skipped prompt shred")
	case stateAccusationTransfer:
		// the accuser never gave a card: skip for them and resume.
		inf, lookupErr := queries.InfractionTransferPending(ctx, a.gameID)
		if lookupErr != nil && !errors.Is(lookupErr, pgx.ErrNoRows) {
			return serverErr("get transfer-pending infraction for host skip",
				lookupErr)
		}
		tx, txq, err := a.begin()
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		if lookupErr == nil {
			if err := txq.InfractionTransferResolve(ctx, inf.ID); err != nil {
				return serverErr("resolve transfer for host skip", err)
			}
		}
		if err := resumeAfterChallenge(ctx, txq, a.state, a.gameID); err != nil {
			return serverErr("resume after host transfer skip", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return serverErr("commit host transfer skip", err)
		}
		a.log.Info("host skipped accusation transfer")
	}
	a.refresh("refreshTable")
	return nil
}
