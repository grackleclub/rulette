package main

import (
	"context"
	"embed"
	_ "embed"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
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
	mux.Handle("/static/img/", logMW(rateMW(http.FileServer(http.FS(static)))))
	// pregame.go
	mux.Handle("/", logMW(rateMW(http.HandlerFunc(rootHandler))))
	mux.Handle("/create", logMW(rateMW(http.HandlerFunc(createHandler))))
	mux.Handle("/{game_id}/join", logMW(rateMW(http.HandlerFunc(joinHandler))))
	// game.go
	mux.Handle("/{game_id}", logMW(rateMW(http.HandlerFunc(gameHandler))))
	mux.Handle("/{game_id}/data/{topic}", logMW(rateMW(http.HandlerFunc(dataHandler))))
	mux.Handle("/{game_id}/action/{action}", logMW(rateMW(http.HandlerFunc(actionHandler))))

	ctx := context.Background()
	// RULETTE_PG_URL is a postgres connection string, e.g.:
	// postgres://user@host/rulette or postgres://user:pass@host:5432/db?sslmode=require
	// Port defaults to 5432; sslmode defaults to the driver default if omitted.
	dbURL, err := url.Parse(envRequired("RULETTE_PG_URL"))
	if err != nil {
		panic(fmt.Sprintf("parse database URL: %v", err))
	}
	if dbURL.User == nil || dbURL.User.Username() == "" {
		panic("database URL must include a username")
	}
	dbName := strings.Trim(dbURL.Path, "/")
	if dbName == "" {
		panic("database URL must include database name (e.g. /dbname)")
	}
	pass, _ := dbURL.User.Password()
	opts := postgres.PostgresOpts{
		Host:     dbURL.Hostname(),
		User:     dbURL.User.Username(),
		Password: pass,
		Name:     dbName,
		Port:     dbURL.Port(),
		Sslmode:  dbURL.Query().Get("sslmode"),
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
	defer pool.Close()
	dbPool = pool
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
		port = os.Getenv("PORT")
	}
	if port == "" {
		port = fmt.Sprintf("%d", portDefault)
	}
	log.Info("starting server", "port", port)
	if err := http.ListenAndServe(fmt.Sprintf(":%v", port), mux); err != nil {
		log.Error("server failed", "error", err)
		panic(fmt.Sprintf("server failed: %v", err))
	}
}
