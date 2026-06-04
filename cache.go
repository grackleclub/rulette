package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

// stateFromCacheOrDB returns the current state of the game specified by gameID,
// drawing from cache if newer than maxCacheAge, otherwise fetching from the database.
func stateFromCacheOrDB(ctx context.Context, cache *sync.Map, gameID string) (state, error) {
	ctx, span := otel.Tracer(otelScope).Start(ctx, "cache.lookup")
	defer span.End()
	span.SetAttributes(attrGameID.String(gameID))
	log := log.With("caller", "stateFromCache", "game_id", gameID)

	// cache hit
	if value, ok := cache.Load(gameID); ok {
		cachedState := value.(*state)
		cacheAge := time.Since(cachedState.Updated)
		if cacheAge < maxCacheAge {
			log.Debug("cache hit", "cache_age", cacheAge)
			span.SetAttributes(attribute.Bool("cache.hit", true))
			if cacheHits != nil {
				cacheHits.Add(ctx, 1)
			}
			return *cachedState, nil
		}
		log.Debug("cache stale", "cache_age", cacheAge)
	}

	// cache miss
	log.Debug("cache miss")
	span.SetAttributes(attribute.Bool("cache.hit", false))
	if cacheMisses != nil {
		cacheMisses.Add(ctx, 1)
	}
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

// cacheJanitor periodically evicts cache entries older than cacheTTL.
// Runs until ctx is cancelled. The cache has no other eviction policy,
// so without this, entries accumulate indefinitely (game ended, last
// player left, etc.) and grow process memory unbounded.
func cacheJanitor(ctx context.Context, cache *sync.Map) {
	tick := time.NewTicker(cacheJanitorInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			now := time.Now()
			cache.Range(func(k, v any) bool {
				s, ok := v.(*state)
				if !ok {
					return true
				}
				if now.Sub(s.Updated) > cacheTTL {
					cache.Delete(k)
					log.Debug("cache evicted",
						"game_id", k,
						"age", now.Sub(s.Updated),
					)
				}
				return true
			})
		}
	}
}

// fetchStateFromDB retrieves the game state and players from the database for the given gameID.
func fetchStateFromDB(ctx context.Context, gameID string) (state, error) {
	tr := otel.Tracer(otelScope)
	ctx, span := tr.Start(ctx, "db.fetchState")
	defer span.End()
	span.SetAttributes(attrGameID.String(gameID))

	gctx, gspan := tr.Start(ctx, "db.GameState")
	game, err := queries.GameState(gctx, gameID)
	gspan.End()
	if err != nil {
		return state{}, ErrStateNoGame
	}
	pctx, pspan := tr.Start(ctx, "db.GamePlayerPoints")
	players, err := queries.GamePlayerPoints(pctx, gameID)
	pspan.End()
	if err != nil {
		return state{}, ErrFetchPlayers
	}
	cpctx, cpspan := tr.Start(ctx, "db.GameCardsPlayerView")
	cardsPlayers, err := queries.GameCardsPlayerView(cpctx, gameID)
	cpspan.End()
	if err != nil {
		return state{}, fmt.Errorf("fetch cards for game: %w", err)
	}
	cwctx, cwspan := tr.Start(ctx, "db.GameCardsWheelView")
	cardsWheel, err := queries.GameCardsWheelView(cwctx, gameID)
	cwspan.End()
	if err != nil {
		return state{}, fmt.Errorf("fetch wheel cards for game: %w", err)
	}
	ictx, ispan := tr.Start(ctx, "db.InfractionsByGame")
	infractions, err := queries.InfractionsByGame(ictx, gameID)
	ispan.End()
	if err != nil {
		return state{}, fmt.Errorf("fetch infractions for game: %w", err)
	}
	if len(infractions) == 0 {
		log.Debug("no infractions to fetch", "game_id", gameID)
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
		Infractions:  infractions,
	}, nil
}
