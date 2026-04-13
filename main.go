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
	defaultFrontendRefresh string = fmt.Sprintf("%dms", 500) // passed to templates; htmx-refresh
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
	opts := postgres.PostgresOpts{
		Host:     envRequired("RULETTE_DB_HOST"),
		User:     envRequired("RULETTE_DB_USER"),
		Password: envRequired("RULETTE_DB_PASS"),
		Name:     envRequired("RULETTE_DB_NAME"),
		Port:     envRequired("RULETTE_DB_PORT"),
		Sslmode:  envRequired("RULETTE_DB_SSL"),
	}

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
	if err := pool.Ping(ctx); err != nil {
		log.Error("ping database failed",
			"user", db.User,
			"name", db.Name,
			"host", db.Host,
			"port", db.Port,
			"sslmode", db.Sslmode,
			"error", err,
		)
		panic(fmt.Sprintf("ping database: %v", err))
	}
	_, err = db.Conn.ExecContext(ctx, dbSchema)
	if err != nil {
		panic(fmt.Sprintf("schema migration: %v", err))
	}
	log.Info("database ready",
		"host", db.Host, "port", db.Port, "name", db.Name,
	)
	port := os.Getenv("RULETTE_PORT")
	if port == "" {
		port = fmt.Sprintf("%d", portDefault)
	}
	log.Info("starting server", "port", port)
	if err := http.ListenAndServe(fmt.Sprintf(":%v", port), mux); err != nil {
		log.Error("server failed", "error", err)
		panic(fmt.Sprintf("server failed: %v", err))
	}
}
