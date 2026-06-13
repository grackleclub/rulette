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
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/grackleclub/postgres"
	sqlc "github.com/grackleclub/rulette/db/sqlc"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	queries                *sqlc.Queries
	dbPool                 *pgxpool.Pool
	cache                  sync.Map
	log                    *slog.Logger
	version                       = "dev" // set via -ldflags "-X main.version=..."
	maxCacheAge                   = 500 * time.Millisecond
	cacheTTL                      = 5 * time.Minute
	cacheJanitorInterval          = 1 * time.Minute
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

// game.state_id values, mirroring the game_states rows in db/schema.sql.
const (
	stateCreated   = 0 // game created, no members joined
	stateInviting  = 1 // at least one player has joined
	stateReady     = 2 // joining closed, ready to start (or paused)
	stateTurn      = 3 // a player is mid-turn
	statePending   = 4 // a rule modifier choice is pending
	stateChallenge = 5 // a points challenge is pending
	stateEnding    = 6 // deck spent, waiting on host to end
	stateOver      = 7 // game over
)

//go:embed db/schema.sql
var dbSchema string

//go:embed static
var static embed.FS

func initLogger(otelHandler slog.Handler) {
	var err error
	if otelHandler != nil {
		log, err = logger.NewWithHandlers(
			slog.HandlerOptions{}, otelHandler,
		)
	} else {
		log, err = logger.New(slog.HandlerOptions{})
	}
	if err != nil {
		panic(fmt.Sprintf("create slog handler: %v", err))
	}
	log = log.With("service.version", version)
}

func main() {
	ctx := context.Background()

	otelShutdown, otelLogHandler, err := initOtel(ctx)
	if err != nil {
		panic(fmt.Sprintf("init otel: %v", err))
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(
			context.Background(), 5*time.Second,
		)
		defer cancel()
		if err := otelShutdown(shutdownCtx); err != nil {
			// log may be nil if shutdown runs after a panic in
			// initOtel (before initLogger ran).
			if log != nil {
				log.Error("otel shutdown", "error", err)
			} else {
				slog.Error("otel shutdown", "error", err)
			}
		}
	}()

	initLogger(otelLogHandler)

	mux := http.NewServeMux()
	// static embed.FS
	mux.Handle("/static/html/", logMW(rateMW(http.FileServer(http.FS(static)))))
	mux.Handle("/static/css/", logMW(rateMW(http.FileServer(http.FS(static)))))
	mux.Handle("/static/js/", logMW(rateMW(http.FileServer(http.FS(static)))))
	mux.Handle("/static/img/", logMW(rateMW(http.FileServer(http.FS(static)))))
	mux.Handle("/static/fonts/", logMW(rateMW(http.FileServer(http.FS(static)))))
	mux.Handle("/static/audio/", logMW(rateMW(http.FileServer(http.FS(static)))))
	// pregame.go
	mux.Handle("/", logMW(rateMW(http.HandlerFunc(rootHandler))))
	mux.Handle("/create", logMW(rateMW(http.HandlerFunc(createHandler))))
	mux.Handle("/{game_id}/join", logMW(rateMW(http.HandlerFunc(joinHandler))))
	// game.go
	mux.Handle("/{game_id}", logMW(rateMW(http.HandlerFunc(gameHandler))))
	mux.Handle("/{game_id}/qr", logMW(rateMW(http.HandlerFunc(qrHandler))))
	mux.Handle("/{game_id}/data/{topic}", logMW(rateMW(http.HandlerFunc(dataHandler))))
	mux.Handle("/{game_id}/action/{action}", logMW(rateMW(http.HandlerFunc(actionHandler))))
	// feedback.go: public POST to submit, admin-only GET/PATCH/DELETE to triage
	mux.Handle("/bugs", logMW(rateMW(http.HandlerFunc(bugsHandler))))
	mux.Handle("/suggestions", logMW(rateMW(http.HandlerFunc(suggestionsHandler))))

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
	if err := initMetrics(&cache); err != nil {
		log.Error("init metrics, continuing without", "error", err)
	} else {
		log.Info("metrics initialized")
	}
	go cacheJanitor(ctx, &cache)
	// RULETTE_ADMIN_PASSWORD guards the bug/suggestion triage endpoints. When
	// unset, those admin endpoints fail closed (see middleware.go).
	adminPassword = os.Getenv("RULETTE_ADMIN_PASSWORD")
	if adminPassword == "" {
		log.Warn("RULETTE_ADMIN_PASSWORD unset; bug/suggestion admin endpoints disabled")
	}
	port := os.Getenv("RULETTE_PORT")
	if port == "" {
		port = os.Getenv("PORT")
	}
	if port == "" {
		port = fmt.Sprintf("%d", portDefault)
	}
	log.Info("starting server", "port", port)
	handler := otelhttp.NewHandler(mux, "rulette")
	if err := http.ListenAndServe(fmt.Sprintf(":%v", port), handler); err != nil {
		log.Error("server failed", "error", err)
		panic(fmt.Sprintf("server failed: %v", err))
	}
}
