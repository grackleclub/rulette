package main

import (
	"context"
	"fmt"
	"log/slog"
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

// stateFromCache returns the current state of the game specified by gameID,
// drawing from cache if newer than maxCacheAge, otherwise fetching from the database.
func stateFromCache(ctx context.Context, cache *sync.Map, gameID string) (state, error) {
	slog.With("caller", "state", "game_id", gameID)

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
	stateFresh, err := fetchState(ctx, gameID)
	if err != nil {
		return state{}, err
	}
	// Update the cache
	cache.Store(gameID, &stateFresh)
	slog.Debug("cache updated", "game_id", gameID)
	return stateFresh, nil
}

// fetchState retrieves the game state and players from the database for the given gameID.
func fetchState(ctx context.Context, gameID string) (state, error) {
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
