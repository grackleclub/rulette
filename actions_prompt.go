package main

import (
	"net/http"

	sqlc "github.com/grackleclub/rulette/db/sqlc"
)

// actionPromptRuling records the host's ruling on a prompt challenge.
// "succeed" awards the spinner 1 point plus 1 per rule held and may be
// called at any time; "fail" awards nothing and is gated by the grace
// allowance so it can't land before the spinner's time is genuinely up.
// Either way the prompt card leaves play. A success with rules held
// pauses in the prompt-shred state; otherwise the turn advances.
func actionPromptRuling(a *actionRequest) error {
	ctx := a.r.Context()
	spin, err := queries.SpinPendingModifier(ctx, a.gameID)
	if err != nil || spin.Type != "prompt" || !spin.PlayerID.Valid {
		return &actionError{
			status: http.StatusConflict,
			msg:    "no active prompt",
			err:    err, // may be nil: a non-prompt spin is pending
		}
	}
	if a.action == "fail" {
		elapsed, err := queries.SpinLatestElapsedSeconds(ctx, a.gameID)
		if err != nil {
			return serverErr("measure prompt elapsed", err)
		}
		if elapsed < promptGraceSeconds {
			return failf(http.StatusTooEarly, "challenge still in progress")
		}
	}

	spinnerID := spin.PlayerID.Int32
	// reward for completing a prompt is 1 plus 1 additional point for
	// every rule held; find that count and the prompt card to remove,
	// both from the spinner's revealed cards.
	var rulesHeld, promptCardID int32
	for _, c := range a.state.CardsPlayers {
		if c.PlayerID.Int32 != spinnerID {
			continue
		}
		switch c.Type {
		case "rule":
			rulesHeld++
		case "prompt":
			promptCardID = c.ID
		}
	}

	tx, txq, err := a.begin()
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// the prompt is a one-shot challenge: remove it from play once
	// ruled on so it doesn't linger in the spinner's hand.
	if promptCardID != 0 {
		if err := txq.GameCardShred(ctx, sqlc.GameCardShredParams{
			ID:     promptCardID,
			GameID: a.gameID,
		}); err != nil {
			return serverErr("shred prompt card", err)
		}
	}

	if a.action == "succeed" {
		award := rulesHeld + 1
		if err := txq.GamePointsAdjust(ctx, sqlc.GamePointsAdjustParams{
			Points:   pgInt(award),
			GameID:   a.gameID,
			PlayerID: spinnerID,
		}); err != nil {
			return serverErr("award prompt points", err)
		}
		pcID, err := txq.PointChangeCreate(ctx, sqlc.PointChangeCreateParams{
			GameID:   a.gameID,
			PlayerID: pgInt(spinnerID),
			Delta:    award,
		})
		if err != nil {
			return serverErr("record prompt point change", err)
		}
		// a "prompt" event carrying a points delta reads as a
		// completion; the spin gives the feed the prompt's text.
		if err := recordEvent(ctx, a.log, txq, sqlc.EventCreateParams{
			GameID:        a.gameID,
			EventType:     "prompt",
			TargetID:      pgInt(spinnerID),
			SpinID:        pgInt(spin.ID),
			PointChangeID: pgInt(pcID),
		}); err != nil {
			return serverErr("record prompt event", err)
		}
	} else {
		// no points: a "prompt" event without a delta reads as a
		// failure.
		if err := recordEvent(ctx, a.log, txq, sqlc.EventCreateParams{
			GameID:    a.gameID,
			EventType: "prompt",
			TargetID:  pgInt(spinnerID),
			SpinID:    pgInt(spin.ID),
		}); err != nil {
			return serverErr("record prompt event", err)
		}
	}

	// a completed prompt earns the spinner the chance to shred one of
	// their own rule cards. hold in the prompt-shred state so their
	// chooser can open; the turn advances only once they shred or skip.
	// otherwise (a fail, or nothing to shred) return to normal play and
	// pass the turn on, just as acknowledging a drawn rule would.
	holdForShred := a.action == "succeed" && rulesHeld > 0
	nextState := int32(stateTurn)
	if holdForShred {
		nextState = statePromptShred
	}
	if err := setGameState(ctx, txq, a.state, a.gameID, nextState); err != nil {
		return serverErr("transition state after prompt", err)
	}
	if !holdForShred {
		if err := advanceTurn(ctx, a.log, txq, a.gameID); err != nil {
			return serverErr("advance after prompt", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return serverErr("commit prompt ruling", err)
	}
	a.log.Info("prompt ruled",
		"ruling", a.action,
		"player_id", spinnerID,
		"rules_held", rulesHeld,
	)
	a.refresh("refreshTable")
	return nil
}

// actionPromptShred is the spinner's bonus after a succeeded prompt:
// shred one of their own rule cards, or skip (by omitting game_card_id).
// Either way the turn then advances.
func actionPromptShred(a *actionRequest) error {
	ctx := a.r.Context()
	// a card was chosen (skip omits it): validate before the
	// transaction so we can bail early on bad input.
	var cardID int32
	shredCard := a.r.URL.Query().Get("game_card_id") != ""
	if shredCard {
		var err error
		cardID, err = a.queryInt("game_card_id")
		if err != nil {
			return err
		}
		if !a.state.ownsCard(cardID, a.caller(), "rule") {
			return failf(http.StatusForbidden, "card not a rule owned by player")
		}
	}

	tx, txq, err := a.begin()
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if shredCard {
		if err := txq.GameCardShred(ctx, sqlc.GameCardShredParams{
			ID:     cardID,
			GameID: a.gameID,
		}); err != nil {
			return serverErr("shred card after prompt", err)
		}
		if err := recordEvent(ctx, a.log, txq, sqlc.EventCreateParams{
			GameID:     a.gameID,
			EventType:  "shred",
			ActorID:    pgInt(a.caller()),
			GameCardID: pgInt(cardID),
		}); err != nil {
			return serverErr("record shred event", err)
		}
		a.log.Info("card shredded after prompt", "card_id", cardID)
	} else {
		a.log.Info("prompt shred skipped")
	}
	// the bonus is resolved: back to turn state and pass initiative on.
	if err := setGameState(ctx, txq, a.state, a.gameID, stateTurn); err != nil {
		return serverErr("transition to turn after prompt shred", err)
	}
	if err := advanceTurn(ctx, a.log, txq, a.gameID); err != nil {
		return serverErr("advance after prompt shred", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return serverErr("commit prompt shred transaction", err)
	}
	a.refresh("refreshTable")
	return nil
}
