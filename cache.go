package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	sqlc "github.com/grackleclub/rulette/db/sqlc"
)

var (
	cache            sync.Map
	maxCacheAge      = 500 * time.Millisecond
	ErrCookieMissing = fmt.Errorf("session cookie missing")
	ErrCookieInvalid = fmt.Errorf("invalid session cookie")
	ErrStateNoGame   = fmt.Errorf("no game found")
	ErrFetchPlayers  = fmt.Errorf("fetching players failed")
)

// state is a struct to populate the global game cache.
type state struct {
	updated time.Time
	game    sqlc.GameStateRow
	players []sqlc.GamePlayerPointsRow
}

// isPlayerInGame returns true when cookieKey exists in game_players.
func (s *state) isPlayerInGame(cookieKey string) bool {
	for _, player := range s.players {
		if player.SessionKey.String == cookieKey {
			return true
		}
	}
	return false
}

// stateFromCacheOrDB returns the current state of the game specified by gameID,
// drawing from cache if newer than maxCacheAge, otherwise fetching from the database.
func stateFromCacheOrDB(ctx context.Context, cache *sync.Map, gameID string) (state, error) {
	slog := slog.With("caller", "stateFromCache", "game_id", gameID)

	// cache hit
	if value, ok := cache.Load(gameID); ok {
		cachedState := value.(*state)
		cacheAge := time.Since(cachedState.updated)
		if cacheAge < maxCacheAge {
			slog.Debug("cache hit", "cache_age", cacheAge)
			return *cachedState, nil
		}
		slog.Debug("cache stale", "cache_age", cacheAge)
	}
	// cache miss
	slog.Debug("cache miss")
	stateFresh, err := fetchStateFromDB(ctx, gameID)
	if err != nil {
		return state{}, err
	}
	// Update the cache
	cache.Store(gameID, &stateFresh)
	slog.Debug("cache updated", "game_id", gameID)
	return stateFresh, nil
}

// fetchStateFromDB retrieves the game state and players from the database for the given gameID.
func fetchStateFromDB(ctx context.Context, gameID string) (state, error) {
	var stateFresh state
	// get game state
	game, err := queries.GameState(ctx, gameID)
	if err != nil {
		return state{}, ErrStateNoGame
	}
	stateFresh.game = game
	// get game players
	players, err := queries.GamePlayerPoints(ctx, gameID)
	if err != nil {
		return state{}, ErrFetchPlayers
	}
	stateFresh.players = players
	stateFresh.updated = time.Now().UTC()
	slog.Debug("fetched game state and players",
		"player_count", len(players),
		"game_id", gameID,
		"game_name", game.Name,
		"game_state", game.StateName,
	)
	return stateFresh, nil
}

// cookie inspects the request for cookie and returns
// the player ID and session key, or any error.
//
// Cookie format is: {player_id}:{session_key}
func cookie(r *http.Request) (string, string, error) {
	var cookieID string
	var cookieKey string
	cookie, err := r.Cookie("session")
	if err != nil {
		return "", "", ErrCookieMissing
	}
	parts := strings.Split(cookie.Value, ":")
	if len(parts) != 2 {
		slog.Debug("invalid session cookie format",
			"cookie_value", cookie.Value,
		)
		return "", "", ErrCookieInvalid
	}
	cookieID = parts[0]
	cookieKey = parts[1]
	return cookieID, cookieKey, nil
}
