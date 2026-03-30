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

// Cards of type "motifier" have specific consequences,
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
	opts := postgres.PostgresOpts{
		Host:     envOr("RULETTE_DB_HOST", "localhost"),
		User:     envOr("RULETTE_DB_USER", "postgres"),
		Password: envOr("RULETTE_DB_PASS", randHex(16)),
		Name:     envOr("RULETTE_DB_NAME", "rulette"),
		Port:     envOr("RULETTE_DB_PORT", "5432"),
		Sslmode:  envOr("RULETTE_DB_SSL", "disable"),
	}

	_, local := os.LookupEnv("RULETTE_DB_LOCAL")
	if local {
		log.Info("using local test container database")
		db, close, err := postgres.NewTestDB(ctx, opts)
		if err != nil {
			panic(fmt.Sprintf("create test database: %v", err))
		}
		defer close()
		pool, err := db.Pool(ctx)
		if err != nil {
			panic(fmt.Sprintf("create database pool: %v", err))
		}
		queries = sqlc.New(pool)
		log.Info("database ready", "host", db.Host, "port", db.Port)

		_, err = db.Conn.ExecContext(ctx, dbSchema)
		if err != nil {
			panic(fmt.Sprintf("schema migration: %v", err))
		}
		log.Info("database schema migrated")
	} else {
		db, err := postgres.NewDB(ctx, opts)
		if err != nil {
			panic(fmt.Sprintf("connect to database: %v", err))
		}
		defer db.Conn.Close()
		pool, err := db.Pool(ctx)
		if err != nil {
			panic(fmt.Sprintf("create database pool: %v", err))
		}
		queries = sqlc.New(pool)
		err = pool.Ping(ctx)
		if err != nil {
			log.Error("ping external database failed, set RULETTE_DB_LOCAL for dev mode",
				"user", db.User,
				"name", db.Name,
				"host", db.Host,
				"port", db.Port,
				"sslmode", db.Sslmode,
				"error", err,
			)
			panic(fmt.Sprintf("ping database: %v", err))
		}

		log.Info("database ready",
			"host", db.Host,
			"port", db.Port,
			"name", db.Name,
		)

		_, err = db.Conn.ExecContext(ctx, dbSchema)
		if err != nil {
			panic(fmt.Sprintf("schema migration: %v", err))
		}
		log.Info("database schema migrated")
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = fmt.Sprintf("%d", portDefault)
	}
	log.Info("starting server", "port", port)
	if err := http.ListenAndServe(fmt.Sprintf(":%v", port), mux); err != nil {
		log.Error("server failed", "error", err)
		panic(fmt.Sprintf("server failed: %v", err))
	}
}
