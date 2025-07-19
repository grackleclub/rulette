package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/grackleclub/postgres"
)

var portDefault = 7777

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/state", stateHandler)
	ctx := context.Background()
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

func stateHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	response := `{"status": "okey dokey"}`
	if _, err := w.Write([]byte(response)); err != nil {
		slog.Error("failed to write response", "error", err)
	}
}
