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
	mux.HandleFunc("/state", stateHandler)
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
	slog.Info("created test database", "db", db)

	slog.Info("starting server", "port", portDefault)
	http.ListenAndServe(fmt.Sprintf(":%d", portDefault), mux)

	slog.Info("all done")
}
