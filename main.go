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
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	queries                *sqlc.Queries
	dbPool                 *pgxpool.Pool
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

// Cards of type "modifier" have specific consequences,
// defined below and in the schema.
const (
	modFlip     = "flip"
	modShred    = "shred"
	modClone    = "clone"
	modTransfer = "transfer"
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
	mux.Handle("/static/html/", logMW(rateMW(http.FileServer(http.FS(static)))))
	mux.Handle("/static/css/", logMW(rateMW(http.FileServer(http.FS(static)))))
	mux.Handle("/static/js/", logMW(rateMW(http.FileServer(http.FS(static)))))
	// pregame.go
	mux.Handle("/", logMW(rateMW(http.HandlerFunc(rootHandler))))
	mux.Handle("/create", logMW(rateMW(http.HandlerFunc(createHandler))))
	mux.Handle("/{game_id}/join", logMW(rateMW(http.HandlerFunc(joinHandler))))
	// game.go
	mux.Handle("/{game_id}", logMW(rateMW(http.HandlerFunc(gameHandler))))
	mux.Handle("/{game_id}/data/{topic}", logMW(rateMW(http.HandlerFunc(dataHandler))))
	mux.Handle("/{game_id}/action/{action}", logMW(rateMW(http.HandlerFunc(actionHandler))))

	ctx := context.Background()
	// TODO: setup
	opts := postgres.PostgresOpts{
		Host:     "localhost",
		User:     "postgres",
		Password: "TODO:replace-temporary",
		Port:     "5432",
		Sslmode:  "disable",
	}
	// FIXME: replace with prod
	db, close, err := postgres.NewTestDB(ctx, opts)
	if err != nil {
		panic(fmt.Sprintf("create test database: %v", err))
	}
	defer close()
	pool, err := db.Pool(ctx)
	if err != nil {
		panic(fmt.Sprintf("create test database pool: %v", err))
	}
	dbPool = pool
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
