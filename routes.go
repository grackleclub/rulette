package main

import (
	"log/slog"
	"net/http"
)

func stateHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	result, err := queries.Games(r.Context(), 1)
	if err != nil {
		slog.Error("failed to get games", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	slog.Info("retrieved games", "count", len(result), "result", result)
	response := `{"status": "okey dokey"}`
	if _, err := w.Write([]byte(response)); err != nil {
		slog.Error("failed to write response", "error", err)
	}
}
