package main

import (
	"context"
	"fmt"
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
func recordEvent(ctx context.Context, q *sqlc.Queries, p sqlc.EventCreateParams) error {
	if _, err := q.EventCreate(ctx, p); err != nil {
		return fmt.Errorf("create %s event: %w", p.EventType, err)
	}
	return nil
}
