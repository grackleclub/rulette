package main

import (
	"errors"
	"fmt"
	"net/http"

	sqlc "github.com/grackleclub/rulette/db/sqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// actionAccuse opens an infraction: any player charges that another broke
// a rule they hold. The game holds in the challenge state until the host
// decides.
func actionAccuse(a *actionRequest) error {
	ctx := a.r.Context()
	defendantID, err := a.formInt("defendant_id")
	if err != nil {
		return err
	}
	gcID, err := a.formInt("game_card_id")
	if err != nil {
		return err
	}
	// the accused card must be a rule held by the defendant
	if !a.state.ownsCard(gcID, defendantID, "rule") {
		return failf(http.StatusBadRequest, "card not a rule held by defendant")
	}
	tx, txq, err := a.begin()
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	infractionID, err := txq.InfractionCreate(ctx, sqlc.InfractionCreateParams{
		GameID:     a.gameID,
		GameCardID: gcID,
		Accused:    defendantID,
		Accuser:    a.caller(),
	})
	if err != nil {
		return serverErr("create infraction", err)
	}
	if err := setGameState(ctx, txq, a.state, a.gameID, stateChallenge); err != nil {
		return serverErr("transition to challenge", err)
	}
	// add an event: the accuser accuses the accused
	if err := recordEvent(ctx, a.log, txq, sqlc.EventCreateParams{
		GameID:       a.gameID,
		EventType:    "accuse",
		ActorID:      pgInt(a.caller()),
		TargetID:     pgInt(defendantID),
		InfractionID: pgInt(infractionID),
	}); err != nil {
		return serverErr("record accuse event", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return serverErr("commit accuse transaction", err)
	}
	a.log.Info("infraction created",
		"infraction_id", infractionID,
		"accused", defendantID,
		"game_card_id", gcID,
	)
	a.refresh(fmt.Sprintf(
		`{"refreshTable":null,"infractionCreated":{"id":%d}}`,
		infractionID,
	))
	return nil
}

// actionDecide records the host's verdict on an infraction. An affirm
// deducts the chosen points from the accused; when the accuser also
// holds a rule card, play then holds in the accusation-transfer state so
// they may give the accused a card. Otherwise play resumes.
func actionDecide(a *actionRequest) error {
	ctx := a.r.Context()
	infID, err := a.formInt("infraction_id")
	if err != nil {
		return err
	}
	verdict := a.r.FormValue("verdict")
	if verdict == "" {
		return failf(http.StatusBadRequest, "missing verdict")
	}
	if verdict != "affirm" && verdict != "absolve" {
		return failf(http.StatusBadRequest, "verdict must be affirm or absolve")
	}

	// verify infraction exists, is active, and belongs to this game
	infraction, err := queries.InfractionGet(ctx, infID)
	if errors.Is(err, pgx.ErrNoRows) {
		return failf(http.StatusNotFound, "infraction not found")
	}
	if err != nil {
		return serverErr("get infraction", err)
	}
	if !infraction.Active.Bool {
		return failf(http.StatusConflict, "infraction already decided")
	}
	if infraction.GameID != a.gameID {
		return failf(http.StatusForbidden, "infraction not in this game")
	}

	affirmed := verdict == "affirm"
	var penalty int32
	if affirmed {
		pts, err := a.formInt("amount")
		if err != nil {
			return err
		}
		// the affirm UI sends a positive penalty; negate it so the
		// ledger deducts (GamePointsAdjust adds the delta).
		penalty = -pts
	}

	tx, txq, err := a.begin()
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	_, err = txq.InfractionDecide(ctx, sqlc.InfractionDecideParams{
		ID:       infID,
		Affirmed: pgtype.Bool{Bool: affirmed, Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// lost a race with another decide
		return failf(http.StatusConflict, "infraction already decided")
	}
	if err != nil {
		return serverErr("decide infraction", err)
	}

	// adjust points if affirmed (a zero penalty means guilty but no
	// points change, so skip the adjustment and its event)
	if affirmed && penalty != 0 {
		if err := txq.GamePointsAdjust(ctx, sqlc.GamePointsAdjustParams{
			Points:   pgtype.Int4{Int32: penalty, Valid: true},
			GameID:   a.gameID,
			PlayerID: infraction.Accused,
		}); err != nil {
			return serverErr("adjust points", err)
		}
		// record the points change, linked to the infraction that
		// caused it
		pcID, err := txq.PointChangeCreate(ctx, sqlc.PointChangeCreateParams{
			GameID:       a.gameID,
			PlayerID:     pgtype.Int4{Int32: infraction.Accused, Valid: true},
			Delta:        penalty,
			InfractionID: pgtype.Int4{Int32: infID, Valid: true},
		})
		if err != nil {
			return serverErr("record point change", err)
		}
		// the accused lost points: event for the feed and their sound
		if err := recordEvent(ctx, a.log, txq, sqlc.EventCreateParams{
			GameID:        a.gameID,
			EventType:     "points",
			TargetID:      pgInt(infraction.Accused),
			PointChangeID: pgInt(pcID),
		}); err != nil {
			return serverErr("record points event", err)
		}
	}

	// record the verdict (feed + the accuser's sound) before choosing
	// the next state.
	if err := recordEvent(ctx, a.log, txq, sqlc.EventCreateParams{
		GameID:       a.gameID,
		EventType:    "decide",
		TargetID:     pgInt(infraction.Accuser),
		InfractionID: pgInt(infID),
	}); err != nil {
		return serverErr("record decide event", err)
	}

	// an upheld accusation lets the accuser give one of their own rule
	// cards to the accused. if they hold a rule, hold the game in the
	// transfer state so their chooser can open; the resume logic runs
	// once they give a card or skip. otherwise resume now.
	_, accuserHasRule := a.state.playerCardOfType(infraction.Accuser, "rule")
	if affirmed && accuserHasRule {
		if err := txq.InfractionTransferQueue(ctx, infID); err != nil {
			return serverErr("queue transfer", err)
		}
		if err := setGameState(ctx, txq, a.state, a.gameID,
			stateAccusationTransfer); err != nil {
			return serverErr("transition to accusation-transfer", err)
		}
	} else if err := resumeAfterChallenge(ctx, txq, a.state, a.gameID); err != nil {
		return serverErr("resume after decide", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return serverErr("commit decide transaction", err)
	}
	a.log.Info("infraction decided",
		"infraction_id", infID,
		"verdict", verdict,
		"points", penalty,
	)
	a.refresh("refreshTable")
	return nil
}

// actionAccusationTransfer lets the accuser give one of their own rule
// cards to the accused after an upheld accusation, or skip (by omitting
// game_card_id). Either way play resumes.
func actionAccusationTransfer(a *actionRequest) error {
	ctx := a.r.Context()
	inf, err := queries.InfractionTransferPending(ctx, a.gameID)
	if errors.Is(err, pgx.ErrNoRows) {
		return failf(http.StatusConflict, "no transfer pending")
	}
	if err != nil {
		return serverErr("get transfer-pending infraction", err)
	}
	if a.caller() != inf.Accuser {
		return failf(http.StatusForbidden, "only the accuser may give a card")
	}

	tx, txq, err := a.begin()
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// a card was chosen (skip omits it): give it to the accused after
	// confirming the caller owns it and it's a rule.
	if a.r.URL.Query().Get("game_card_id") != "" {
		cardID, err := a.queryInt("game_card_id")
		if err != nil {
			return err
		}
		if !a.state.ownsCard(cardID, a.caller(), "rule") {
			return failf(http.StatusForbidden, "card not a rule owned by you")
		}
		if err := txq.GameCardMove(ctx, sqlc.GameCardMoveParams{
			ID:       cardID,
			GameID:   a.gameID,
			PlayerID: pgtype.Int4{Int32: inf.Accused, Valid: true},
		}); err != nil {
			return serverErr("give card to accused", err)
		}
		if err := recordEvent(ctx, a.log, txq, sqlc.EventCreateParams{
			GameID:     a.gameID,
			EventType:  "transfer",
			ActorID:    pgInt(a.caller()),
			TargetID:   pgInt(inf.Accused),
			GameCardID: pgInt(cardID),
		}); err != nil {
			return serverErr("record transfer event", err)
		}
		a.log.Info("card given to accused after accusation",
			"card_id", cardID,
			"accused", inf.Accused,
		)
	} else {
		a.log.Info("accusation transfer skipped")
	}
	if err := txq.InfractionTransferResolve(ctx, inf.ID); err != nil {
		return serverErr("resolve transfer", err)
	}
	if err := resumeAfterChallenge(ctx, txq, a.state, a.gameID); err != nil {
		return serverErr("resume after transfer", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return serverErr("commit transfer transaction", err)
	}
	a.refresh("refreshTable")
	return nil
}
