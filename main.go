package main

import (
	"fmt"
	"log/slog"
	"net/http"
)

var portDefault = 7777

func main() {
	slog.Info("starting server", "port", portDefault)

	mux := http.NewServeMux()
	mux.HandleFunc("/state", stateHandler)

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
