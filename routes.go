package main

import (
	"log/slog"
	"net/http"
)

func stateHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	response := `{"status": "okey dokey"}`
	if _, err := w.Write([]byte(response)); err != nil {
		slog.Error("failed to write response", "error", err)
	}
}
