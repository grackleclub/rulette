package main

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/grackleclub/postgres"
	sqlc "github.com/grackleclub/rulette/db/sqlc"
)

var (
	portDefault = 7777
	queries     *sqlc.Queries
)

//go:embed db/schema.sql
var dbSchema string

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/", rootHandler)
	mux.HandleFunc("/{game_id}", gameHandler)
	mux.HandleFunc("/{game_id}/spin/{card_id}", spinHandler)
	mux.HandleFunc("/{game_id}/transfer/{card_id}", transferHandler)
	mux.HandleFunc("/{game_id}/flip/{card_id}", flipHandler)
	mux.HandleFunc("/{game_id}/shred/{card_id}", shredHandler)
	mux.HandleFunc("/{game_id}/clone/{card_id}", cloneHandler)
	mux.HandleFunc("/create", createHandler)

	// use debug slog handler
	slog.SetLogLoggerLevel(slog.LevelDebug)
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
	slog.Info("created test database and sqlc queries", "db", db)

	_, err = db.Conn.ExecContext(ctx, dbSchema)
	if err != nil {
		slog.Error("schema migration", "error", err)
		panic(fmt.Sprintf("schema migration: %v", err))
	}
	slog.Info("starting server", "port", portDefault)
	http.ListenAndServe(fmt.Sprintf(":%d", portDefault), mux)

	slog.Info("all done")
}
