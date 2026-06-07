package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	sqlc "github.com/grackleclub/rulette/db/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

// envRequired returns the value of the environment variable named by the key,
// or panics.
// WARNING: should only be used for required startup vars.
func envRequired(key string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	err := fmt.Errorf("required environment variable missing: %s", key)
	panic(err)
}

// pgInt wraps a player or row id as a non-NULL pgtype.Int4.
func pgInt(v int32) pgtype.Int4 {
	return pgtype.Int4{Int32: v, Valid: true}
}

// recordEvent adds one row to the event log. Detail references that don't
// apply to the event type are left as the zero pgtype.Int4, which stores NULL.
// On failure it logs (with the event type) and returns a wrapped error, so
// callers only need to handle the error, not log it.
func recordEvent(ctx context.Context, log *slog.Logger, q *sqlc.Queries, p sqlc.EventCreateParams) error {
	if _, err := q.EventCreate(ctx, p); err != nil {
		log.Error("record event", "error", err, "event_type", p.EventType)
		return fmt.Errorf("record %s event: %w", p.EventType, err)
	}
	return nil
}

// advanceTurn moves initiative to the next player and adds a turn event for
// whoever holds it now.
func advanceTurn(ctx context.Context, log *slog.Logger, q *sqlc.Queries, gameID string) error {
	if err := q.InitiativeAdvance(ctx, gameID); err != nil {
		return fmt.Errorf("advance initiative: %w", err)
	}
	playerID, err := q.InitiativeCurrentPlayer(ctx, gameID)
	if err != nil {
		return fmt.Errorf("find current turn player: %w", err)
	}
	return recordEvent(ctx, log, q, sqlc.EventCreateParams{
		GameID:    gameID,
		EventType: "turn",
		TargetID:  pgInt(playerID),
	})
}
