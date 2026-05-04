package main

import (
	"bytes"
	"fmt"
	"image/png"
	"net/http"
	"strings"

	"github.com/grackleclub/rulette/internal/qr"
)

// qrHandler serves a PNG QR code for the game's join URL.
// Only available while the game is in the invite (pre-game) state.
func qrHandler(w http.ResponseWriter, r *http.Request) {
	gameID := r.PathValue("game_id")
	log := log.With("handler", "qrHandler", "game_id", gameID)

	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	state, err := stateFromCacheOrDB(r.Context(), &cache, gameID)
	if err != nil {
		if err == ErrStateNoGame {
			log.Warn("game not found")
			http.Error(w, "game not found", http.StatusNotFound)
			return
		}
		log.Error("fetch game state", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// state 0 = created, 1 = inviting; both are pre-game
	if state.Game.StateID != 0 && state.Game.StateID != 1 {
		log.Info("qr requested outside invite state",
			"state_id", state.Game.StateID,
		)
		http.Error(w, "game not accepting invites", http.StatusConflict)
		return
	}

	scheme := "https"
	if r.TLS == nil && !strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "http"
	}
	joinURL := fmt.Sprintf("%s://%s/%s/join", scheme, r.Host, gameID)

	img, err := qr.Encode(joinURL, 0)
	if err != nil {
		log.Error("encode qr", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		log.Error("encode png", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "image/png")
	if _, err := w.Write(buf.Bytes()); err != nil {
		log.Error("write response", "error", err)
	}
}
