package main

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// TODO: interface?
// type cacher interface {
// 	set() error
// 	get() (state, error)
// 	clean() error
// }

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
	log.Debug("cache updated", "game_id", gameID)

	return stateFresh, nil
}

// fetchStateFromDB retrieves the game state and players from the database for the given gameID.
func fetchStateFromDB(ctx context.Context, gameID string) (state, error) {
	// get game state
	game, err := queries.GameState(ctx, gameID)
	if err != nil {
		return state{}, ErrStateNoGame
	}
	// get game players
	players, err := queries.GamePlayerPoints(ctx, gameID)
	if err != nil {
		return state{}, ErrFetchPlayers
	}
	// get revealed player cardsPlayers and wheel cardsPlayers
	cardsPlayers, err := queries.GameCardsPlayerView(ctx, gameID)
	if err != nil {
		return state{}, fmt.Errorf("fetch cards for game: %w", err)
	}
	// get wheel cards
	cardsWheel, err := queries.GameCardsWheelView(ctx, gameID)
	if err != nil {
		return state{}, fmt.Errorf("fetch wheel cards for game: %w", err)
	}

	log.Debug("fetched game state and players",
		"player_count", len(players),
		"game_id", gameID,
		"game_name", game.Name,
		"game_state", game.StateName,
	)
	return state{
		Game:         game,
		Players:      players,
		Updated:      time.Now().UTC(),
		CardsWheel:   cardsWheel,
		CardsPlayers: cardsPlayers,
	}, nil
}
