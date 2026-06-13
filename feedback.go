package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	sqlc "github.com/grackleclub/rulette/db/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

// validStatuses are the lifecycle values a bug or suggestion may hold.
// Submissions start at "new"; the triage CLI moves them to "filed" once a
// GitHub issue exists, or "rejected" when dropped.
var validStatuses = map[string]bool{
	"new":      true,
	"filed":    true,
	"rejected": true,
}

// patchBody is the JSON the triage CLI sends to update a ticket's status.
type patchBody struct {
	Status   string `json:"status"`
	IssueURL string `json:"issue_url"`
	Notes    string `json:"notes"`
}

// pgText wraps an optional string as a pgtype.Text, storing NULL when empty.
func pgText(s string) pgtype.Text {
	return pgtype.Text{String: s, Valid: s != ""}
}

// bugsHandler serves the /bugs resource. POST is public (submit a report); GET,
// PATCH, and DELETE are admin-only (list new reports, set a report's status,
// remove a report). PATCH and DELETE take the row id from the "id" query
// parameter — the game routes own the /{game_id}/... wildcard, so a /bugs/{id}
// path can't be registered without a router conflict.
func bugsHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		game := strings.TrimSpace(r.FormValue("game_url"))
		os := strings.TrimSpace(r.FormValue("os"))
		browser := strings.TrimSpace(r.FormValue("browser"))
		desc := strings.TrimSpace(r.FormValue("description"))
		if game == "" || os == "" || browser == "" || desc == "" {
			http.Error(w, "missing required field", http.StatusBadRequest)
			return
		}
		id, err := queries.BugCreate(r.Context(), sqlc.BugCreateParams{
			GameUrl:     game,
			Os:          os,
			Browser:     browser,
			Version:     pgText(strings.TrimSpace(r.FormValue("version"))),
			Description: desc,
		})
		if err != nil {
			log.Error("create bug", "error", err)
			http.Error(w, "server error", http.StatusInternalServerError)
			return
		}
		log.Info("bug submitted", "id", id)
		w.WriteHeader(http.StatusNoContent)
	case http.MethodGet:
		if !requireAdmin(w, r) {
			return
		}
		bugs, err := queries.BugsNew(r.Context())
		if err != nil {
			log.Error("list bugs", "error", err)
			http.Error(w, "server error", http.StatusInternalServerError)
			return
		}
		writeJSON(w, bugs)
	case http.MethodPatch:
		if !requireAdmin(w, r) {
			return
		}
		id, body, ok := readUpdate(w, r)
		if !ok {
			return
		}
		err := queries.BugSetStatus(r.Context(), sqlc.BugSetStatusParams{
			ID:       id,
			Status:   body.Status,
			IssueUrl: pgText(body.IssueURL),
			Notes:    pgText(body.Notes),
		})
		if err != nil {
			log.Error("update bug", "error", err, "id", id)
			http.Error(w, "server error", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case http.MethodDelete:
		if !requireAdmin(w, r) {
			return
		}
		id, err := queryID(r)
		if err != nil {
			http.Error(w, "bad id", http.StatusBadRequest)
			return
		}
		if err := queries.BugDelete(r.Context(), id); err != nil {
			log.Error("delete bug", "error", err, "id", id)
			http.Error(w, "server error", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// suggestionsHandler serves the /suggestions resource, mirroring bugsHandler:
// public POST to suggest a rule, admin-only GET/PATCH/DELETE to triage.
func suggestionsHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		front := strings.TrimSpace(r.FormValue("front"))
		back := strings.TrimSpace(r.FormValue("back"))
		if front == "" || back == "" {
			http.Error(w, "missing required field", http.StatusBadRequest)
			return
		}
		id, err := queries.SuggestionCreate(r.Context(), sqlc.SuggestionCreateParams{
			Front: front,
			Back:  back,
		})
		if err != nil {
			log.Error("create suggestion", "error", err)
			http.Error(w, "server error", http.StatusInternalServerError)
			return
		}
		log.Info("suggestion submitted", "id", id)
		w.WriteHeader(http.StatusNoContent)
	case http.MethodGet:
		if !requireAdmin(w, r) {
			return
		}
		suggestions, err := queries.SuggestionsNew(r.Context())
		if err != nil {
			log.Error("list suggestions", "error", err)
			http.Error(w, "server error", http.StatusInternalServerError)
			return
		}
		writeJSON(w, suggestions)
	case http.MethodPatch:
		if !requireAdmin(w, r) {
			return
		}
		id, body, ok := readUpdate(w, r)
		if !ok {
			return
		}
		err := queries.SuggestionSetStatus(r.Context(), sqlc.SuggestionSetStatusParams{
			ID:       id,
			Status:   body.Status,
			IssueUrl: pgText(body.IssueURL),
			Notes:    pgText(body.Notes),
		})
		if err != nil {
			log.Error("update suggestion", "error", err, "id", id)
			http.Error(w, "server error", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case http.MethodDelete:
		if !requireAdmin(w, r) {
			return
		}
		id, err := queryID(r)
		if err != nil {
			http.Error(w, "bad id", http.StatusBadRequest)
			return
		}
		if err := queries.SuggestionDelete(r.Context(), id); err != nil {
			log.Error("delete suggestion", "error", err, "id", id)
			http.Error(w, "server error", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// requireAdmin writes a 401 and returns false when the request lacks a valid
// admin password, so a handler can guard a branch with one if.
func requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	if !isAdmin(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}
	return true
}

// readUpdate parses the id query parameter and the JSON status body shared by
// the PATCH branches, writing a 400 and returning ok=false on bad input.
func readUpdate(w http.ResponseWriter, r *http.Request) (int32, patchBody, bool) {
	id, err := queryID(r)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return 0, patchBody{}, false
	}
	var body patchBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return 0, patchBody{}, false
	}
	if !validStatuses[body.Status] {
		http.Error(w, "invalid status", http.StatusBadRequest)
		return 0, patchBody{}, false
	}
	return id, body, true
}

// queryID reads the "id" query parameter as a row id.
func queryID(r *http.Request) (int32, error) {
	n, err := strconv.ParseInt(r.URL.Query().Get("id"), 10, 32)
	if err != nil {
		return 0, errors.New("invalid id")
	}
	return int32(n), nil
}

// writeJSON encodes v as the JSON response body, logging on failure (the
// header is already sent by then, so the caller can't recover).
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Error("encode json response", "error", err)
	}
}
