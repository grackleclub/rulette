package main

import (
	"context"
	"embed"
	_ "embed"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	logger "github.com/grackleclub/log"

	"github.com/grackleclub/postgres"
	sqlc "github.com/grackleclub/rulette/db/sqlc"
)

var (
	queries                *sqlc.Queries
	cache                  sync.Map
	log                    *slog.Logger
	maxCacheAge                   = 500 * time.Millisecond
	portDefault                   = 7777
	defaultFrontendRefresh string = fmt.Sprintf("%dms", 500)
)

var (
	ErrCookieMissing     = fmt.Errorf("session cookie missing")
	ErrCookieInvalid     = fmt.Errorf("invalid session cookie")
	ErrStateNoGame       = fmt.Errorf("no game found")
	ErrFetchPlayers      = fmt.Errorf("fetching players failed")
	ErrTopicInvalid      = fmt.Errorf("topic invalid for context or does not exist")
	ErrActionInvaid      = fmt.Errorf("action invalid for context or does not exist")
	ErrReadParseTemplate = fmt.Errorf("cannot read and parse template")
)

//go:embed db/schema.sql
var dbSchema string

//go:embed static
var static embed.FS

func init() {
	var err error
	log, err = logger.New(slog.HandlerOptions{})
	if err != nil {
		panic(fmt.Sprintf("create slog handler: %v", err))
	}
}

func main() {
	mux := http.NewServeMux()
	// static embed.FS
	mux.Handle("/static/html/", logMW(http.FileServer(http.FS(static))))
	mux.Handle("/static/css/", logMW(http.FileServer(http.FS(static))))
	mux.Handle("/static/js/", logMW(http.FileServer(http.FS(static))))
	// pregame.go
	mux.Handle("/", logMW(http.HandlerFunc(rootHandler)))
	mux.Handle("/create", logMW(http.HandlerFunc(createHandler)))
	mux.Handle("/{game_id}/join", logMW(http.HandlerFunc(joinHandler)))
	// game.go
	mux.Handle("/{game_id}", logMW(http.HandlerFunc(gameHandler)))
	mux.Handle("/{game_id}/data/{topic}", logMW(http.HandlerFunc(dataHandler)))
	mux.Handle("/{game_id}/action/{action}", logMW(http.HandlerFunc(actionHandler)))

	// actions.go
	// mux.HandleFunc("/{game_id}/spin/{card_id}", spinHandler)
	// mux.HandleFunc("/{game_id}/transfer/{card_id}", transferHandler)
	// mux.HandleFunc("/{game_id}/flip/{card_id}", flipHandler)
	// mux.HandleFunc("/{game_id}/shred/{card_id}", shredHandler)
	// mux.HandleFunc("/{game_id}/clone/{card_id}", cloneHandler)

	ctx := context.Background()
	// TODO: setup
	opts := postgres.PostgresOpts{
		Host:     "localhost",
		User:     "postgres",
		Password: "TODO:replace-temporary",
		Port:     "5432",
		Sslmode:  "disable",
	}
	db, close, err := postgres.NewTestDB(ctx, opts)
	if err != nil {
		panic(fmt.Sprintf("create test database: %v", err))
	}
	defer close()
	pool, err := db.Pool(ctx)
	if err != nil {
		panic(fmt.Sprintf("create test database pool: %v", err))
	}
	queries = sqlc.New(pool)
	log.Info("created test database and sqlc queries", "db", db)

	_, err = db.Conn.ExecContext(ctx, dbSchema)
	if err != nil {
		log.Error("schema migration", "error", err)
		panic(fmt.Sprintf("schema migration: %v", err))
	}
	log.Info("database schema migrated")
	port := os.Getenv("PORT")
	if port == "" {
		port = fmt.Sprintf("%d", portDefault)
	}
	log.Info("starting server", "port", port)
	err = http.ListenAndServe(fmt.Sprintf(":%v", port), mux)
	if err != nil {
		log.Error("server failed", "error", err)
		panic(fmt.Sprintf("server failed: %v", err))
	}
}
