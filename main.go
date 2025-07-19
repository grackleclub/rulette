package main

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/grackleclub/postgres"
)

var portDefault = 7777

//go:embed db/schema.sql
var dbSchema string

func main() {
	mux := http.NewServeMux()
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
	slog.Info("created test database", "db", db)

	slog.Info("starting server", "port", portDefault)
	http.ListenAndServe(fmt.Sprintf(":%d", portDefault), mux)

	slog.Info("all done")
}
