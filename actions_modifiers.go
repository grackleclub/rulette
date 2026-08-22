package main

import (
	"net/http"

	sqlc "github.com/grackleclub/rulette/db/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

// pendingModifierSpin returns the pending spin after confirming its
// modifier effect matches what the caller is trying to resolve.
func (a *actionRequest) pendingModifierSpin(effect string) (sqlc.SpinPendingModifierRow, error) {
	lastSpin, err := queries.SpinPendingModifier(a.r.Context(), a.gameID)
	if err != nil {
		return lastSpin, serverErr("check spin log modifier", err)
	}
	if !lastSpin.ModifierEffect.Valid {
		return lastSpin, failf(http.StatusConflict, "no pending modifier")
	}
	if lastSpin.ModifierEffect.String != effect {
		return lastSpin, failf(http.StatusConflict, "no pending %s", effect)
	}
	return lastSpin, nil
}

// resolveModifier finishes a modifier action: shred the used modifier
// card, return the game to the turn state, and pass initiative on.
func (a *actionRequest) resolveModifier() error {
	ctx := a.r.Context()
	if c, ok := a.state.playerCardOfType(a.caller(), "modifier"); ok {
		if err := queries.GameCardShred(ctx, sqlc.GameCardShredParams{
			ID:     c.ID,
			GameID: a.gameID,
		}); err != nil {
			// best-effort: the modifier resolved even if its card
			// lingers, so log and continue.
			a.log.Error("shred used modifier card",
				"error", err,
				"game_card_id", c.ID,
			)
		}
	}
	if err := setGameState(ctx, queries, a.state, a.gameID, stateTurn); err != nil {
		return serverErr("transition to turn", err)
	}
	if err := advanceTurn(ctx, a.log, queries, a.gameID); err != nil {
		return serverErr("advance initiative after modifier", err)
	}
	return nil
}

// actionFlip resolves a flip modifier: turn one of the caller's own
// cards over.
func actionFlip(a *actionRequest) error {
	ctx := a.r.Context()
	if _, err := a.pendingModifierSpin(modFlip); err != nil {
		return err
	}
	gcID, err := a.queryInt("game_card_id")
	if err != nil {
		return err
	}
	if !a.state.ownsCard(gcID, a.caller(), "") {
		return failf(http.StatusForbidden, "card not owned by player")
	}
	if err := queries.GameCardFlip(ctx, sqlc.GameCardFlipParams{
		ID:     gcID,
		GameID: a.gameID,
	}); err != nil {
		return serverErr("flip card", err)
	}
	if err := recordEvent(ctx, a.log, queries, sqlc.EventCreateParams{
		GameID:     a.gameID,
		EventType:  "flip",
		ActorID:    pgInt(a.caller()),
		GameCardID: pgInt(gcID),
	}); err != nil {
		return serverErr("record flip event", err)
	}
	if err := a.resolveModifier(); err != nil {
		return err
	}
	a.log.Info("card flipped, modifier resolved and shredded",
		"game_card_id", gcID,
	)
	a.refresh("refreshTable")
	return nil
}

// actionShred resolves a shred modifier: destroy one of the caller's
// own cards.
func actionShred(a *actionRequest) error {
	ctx := a.r.Context()
	if _, err := a.pendingModifierSpin(modShred); err != nil {
		return err
	}
	gcID, err := a.queryInt("game_card_id")
	if err != nil {
		return err
	}
	if !a.state.ownsCard(gcID, a.caller(), "") {
		return failf(http.StatusForbidden, "card not owned by player")
	}
	if err := queries.GameCardShred(ctx, sqlc.GameCardShredParams{
		ID:     gcID,
		GameID: a.gameID,
	}); err != nil {
		return serverErr("shred card", err)
	}
	if err := recordEvent(ctx, a.log, queries, sqlc.EventCreateParams{
		GameID:     a.gameID,
		EventType:  "shred",
		ActorID:    pgInt(a.caller()),
		GameCardID: pgInt(gcID),
	}); err != nil {
		return serverErr("record shred event", err)
	}
	if err := a.resolveModifier(); err != nil {
		return err
	}
	a.log.Info("card shredded, modifier resolved",
		"game_card_id", gcID,
	)
	a.refresh("refreshTable")
	return nil
}

// actionClone resolves a clone modifier: copy one of the caller's cards
// to another player.
func actionClone(a *actionRequest) error {
	return modifierGive(a, modClone)
}

// actionTransfer resolves a transfer modifier: move one of the caller's
// cards to another player.
func actionTransfer(a *actionRequest) error {
	return modifierGive(a, modTransfer)
}

// modifierGive handles the shared shape of clone and transfer: pick one
// of your own cards and a target player, then copy or move the card to
// them.
func modifierGive(a *actionRequest, effect string) error {
	ctx := a.r.Context()
	if _, err := a.pendingModifierSpin(effect); err != nil {
		return err
	}
	gcID, err := a.queryInt("game_card_id")
	if err != nil {
		return err
	}
	targetID, err := a.queryInt("target_player_id")
	if err != nil {
		return err
	}
	if !a.state.ownsCard(gcID, a.caller(), "") {
		return failf(http.StatusForbidden, "card not owned by player")
	}
	if !a.state.hasPlayer(targetID) {
		return failf(http.StatusBadRequest, "target player not in game")
	}
	switch effect {
	case modClone:
		err = queries.GameCardClone(ctx, sqlc.GameCardCloneParams{
			ID:       gcID,
			GameID:   a.gameID,
			PlayerID: pgtype.Int4{Int32: targetID, Valid: true},
		})
	case modTransfer:
		err = queries.GameCardMove(ctx, sqlc.GameCardMoveParams{
			ID:       gcID,
			GameID:   a.gameID,
			PlayerID: pgtype.Int4{Int32: targetID, Valid: true},
		})
	}
	if err != nil {
		return serverErr(effect+" card", err)
	}
	// giver -> recipient, for the feed and the recipient's sound
	if err := recordEvent(ctx, a.log, queries, sqlc.EventCreateParams{
		GameID:     a.gameID,
		EventType:  effect,
		ActorID:    pgInt(a.caller()),
		TargetID:   pgInt(targetID),
		GameCardID: pgInt(gcID),
	}); err != nil {
		return serverErr("record "+effect+" event", err)
	}
	if err := a.resolveModifier(); err != nil {
		return err
	}
	a.log.Info("modifier resolved and shredded",
		"effect", effect,
		"game_card_id", gcID,
		"target_player_id", targetID,
	)
	a.refresh("refreshTable")
	return nil
}
