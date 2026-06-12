package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"

	sqlc "github.com/grackleclub/rulette/db/sqlc"
	"github.com/jackc/pgx/v5"
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

// baseURL returns the absolute "scheme://host" for the request, so
// templates can build absolute links (link-preview crawlers require
// absolute og:image and og:url). Honors the X-Forwarded-Proto and
// X-Forwarded-Host headers a reverse proxy sets in production.
func baseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if proto := firstToken(r.Header.Get("X-Forwarded-Proto")); proto != "" {
		scheme = proto
	}
	host := r.Host
	if fwd := firstToken(r.Header.Get("X-Forwarded-Host")); fwd != "" {
		host = fwd
	}
	return scheme + "://" + host
}

// firstToken returns the first comma-separated value of a header,
// trimmed. Across a proxy chain X-Forwarded-* arrives comma-joined
// ("https, http"); only the left-most value — set by the proxy closest
// to the client — is meaningful, so use it rather than the raw header.
func firstToken(v string) string {
	if i := strings.IndexByte(v, ','); i >= 0 {
		v = v[:i]
	}
	return strings.TrimSpace(v)
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

// writeEvent records an event and, on failure, writes a 500 and returns the
// error, so a handler can just: if err := writeEvent(...); err != nil { return }
func writeEvent(w http.ResponseWriter, r *http.Request, log *slog.Logger, q *sqlc.Queries, p sqlc.EventCreateParams) error {
	if err := recordEvent(r.Context(), log, q, p); err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return err
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
	if errors.Is(err, pgx.ErrNoRows) {
		// initiative landed on a gap (e.g. a player left); the turn still
		// advanced, so just skip the turn event rather than failing
		log.Warn("no player at current initiative", "game_id", gameID)
		return nil
	}
	if err != nil {
		return fmt.Errorf("find current turn player: %w", err)
	}
	return recordEvent(ctx, log, q, sqlc.EventCreateParams{
		GameID:    gameID,
		EventType: "turn",
		TargetID:  pgInt(playerID),
	})
}
