package main

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	sqlc "github.com/grackleclub/rulette/db/sqlc"
)

// state is a struct to populate the global game cache.
type state struct {
	Updated time.Time
	Game    sqlc.GameStateRow
	Players []sqlc.GamePlayerPointsRow
	Config  map[string]string // generic baggage (e.g. frontend refresh rate)
}

// isPlayerInGame returns true when cookieKey exists in game_players.
func (s *state) isPlayerInGame(cookieKey string) bool {
	for _, player := range s.Players {
		if player.SessionKey.String == cookieKey {
			return true
		}
	}
	return false
}

// stateFromCacheOrDB returns the current state of the game specified by gameID,
// drawing from cache if newer than maxCacheAge, otherwise fetching from the database.
func stateFromCacheOrDB(ctx context.Context, cache *sync.Map, gameID string) (state, error) {
	log := log.With("caller", "stateFromCache", "game_id", gameID)

	// cache hit
	if value, ok := cache.Load(gameID); ok {
		cachedState := value.(*state)
		cacheAge := time.Since(cachedState.Updated)
		if cacheAge < maxCacheAge {
			log.Info("cache hit", "cache_age", cacheAge)
			return *cachedState, nil
		}
		log.Info("cache stale", "cache_age", cacheAge)
	}

	// cache miss
	log.Info("cache miss")
	stateFresh, err := fetchStateFromDB(ctx, gameID)
	if err != nil {
		return state{}, err
	}
	log.Debug("new state fetched from DB", "data", stateFresh)

	// Add any default config
	stateFresh.Config = make(map[string]string)
	stateFresh.Config["refresh"] = defaultFrontendRefresh

	// Update the cache
	cache.Store(gameID, &stateFresh)
	log.Info("cache updated", "game_id", gameID)

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
	stateFresh.Game = game
	// get game players
	players, err := queries.GamePlayerPoints(ctx, gameID)
	if err != nil {
		return state{}, ErrFetchPlayers
	}
	stateFresh.Players = players
	stateFresh.Updated = time.Now().UTC()
	log.Info("fetched game state and players",
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
		log.Info("invalid session cookie format",
			"cookie_value", cookie.Value,
		)
		return "", "", ErrCookieInvalid
	}
	cookieID = parts[0]
	cookieKey = parts[1]
	return cookieID, cookieKey, nil
}
