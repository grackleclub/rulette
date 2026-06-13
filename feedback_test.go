package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/grackleclub/postgres"
	sqlc "github.com/grackleclub/rulette/db/sqlc"
	"github.com/stretchr/testify/require"
)

// TestAdminAuth covers the admin gate without a database: isAdmin and the
// adminOnly middleware should fail closed when no password is set and reject
// wrong or missing headers.
func TestAdminAuth(t *testing.T) {
	initLogger(nil)
	const secret = "hunter2"

	t.Run("fails closed when unset", func(t *testing.T) {
		adminPassword = ""
		req := httptest.NewRequest(http.MethodGet, "/bugs", nil)
		req.Header.Set(adminHeader, "anything")
		require.False(t, isAdmin(req))
	})

	adminPassword = secret
	t.Cleanup(func() { adminPassword = "" })

	cases := []struct {
		name   string
		header string
		want   bool
	}{
		{"correct", secret, true},
		{"wrong", "nope", false},
		{"missing", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/bugs", nil)
			if c.header != "" {
				req.Header.Set(adminHeader, c.header)
			}
			require.Equal(t, c.want, isAdmin(req))
		})
	}

	t.Run("delete without admin is rejected", func(t *testing.T) {
		adminPassword = ""
		req := httptest.NewRequest(http.MethodDelete, "/bugs?id=1", nil)
		w := httptest.NewRecorder()
		bugsHandler(w, req)
		require.Equal(t, http.StatusUnauthorized, w.Result().StatusCode)
	})
}

// TestFeedbackValidation checks that submissions missing a required field are
// rejected before any database call, so it needs no database.
func TestFeedbackValidation(t *testing.T) {
	initLogger(nil)
	t.Run("bug missing fields", func(t *testing.T) {
		body := strings.NewReader(url.Values{"game_url": {"x"}}.Encode())
		req := httptest.NewRequest(http.MethodPost, "/bugs", body)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		bugsHandler(w, req)
		require.Equal(t, http.StatusBadRequest, w.Result().StatusCode)
	})
	t.Run("suggestion missing fields", func(t *testing.T) {
		body := strings.NewReader(url.Values{"front": {"x"}}.Encode())
		req := httptest.NewRequest(http.MethodPost, "/suggestions", body)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		suggestionsHandler(w, req)
		require.Equal(t, http.StatusBadRequest, w.Result().StatusCode)
	})
}

// TestFeedback exercises the full submit -> list -> triage round trip against
// an ephemeral database.
func TestFeedback(t *testing.T) {
	initLogger(nil)
	ctx := context.Background()
	db, teardown, err := postgres.NewTestDB(ctx, testDBOpts(t))
	require.NoError(t, err)
	defer teardown()
	pool, err := db.Pool(ctx)
	require.NoError(t, err)
	dbPool = pool
	queries = sqlc.New(pool)
	_, err = db.Conn.ExecContext(ctx, dbSchema)
	require.NoError(t, err)

	const secret = "triage-pw"
	adminPassword = secret
	t.Cleanup(func() { adminPassword = "" })

	// route through a mux to mirror production dispatch
	mux := http.NewServeMux()
	mux.HandleFunc("/bugs", bugsHandler)
	mux.HandleFunc("/suggestions", suggestionsHandler)

	t.Run("submit bug", func(t *testing.T) {
		form := url.Values{
			"game_url":    {"http://localhost/abc123"},
			"os":          {"Linux"},
			"browser":     {"Firefox"},
			"version":     {"v1.2.3"},
			"description": {"the wheel spun forever"},
		}
		req := httptest.NewRequest(http.MethodPost, "/bugs", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		require.Equal(t, http.StatusNoContent, w.Result().StatusCode)
	})

	t.Run("list requires admin", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/bugs", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		require.Equal(t, http.StatusUnauthorized, w.Result().StatusCode)
	})

	var bugID int32
	t.Run("admin lists new bug", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/bugs", nil)
		req.Header.Set(adminHeader, secret)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Result().StatusCode)
		var bugs []sqlc.Bugs
		require.NoError(t, json.NewDecoder(w.Result().Body).Decode(&bugs))
		require.Len(t, bugs, 1)
		require.Equal(t, "the wheel spun forever", bugs[0].Description)
		require.Equal(t, "new", bugs[0].Status)
		bugID = bugs[0].ID
	})

	t.Run("patch marks filed and drops from new list", func(t *testing.T) {
		body, _ := json.Marshal(patchBody{Status: "filed", IssueURL: "http://gh/1", Notes: "dupe of #4"})
		req := httptest.NewRequest(http.MethodPatch, bugPath(bugID), strings.NewReader(string(body)))
		req.Header.Set(adminHeader, secret)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		require.Equal(t, http.StatusNoContent, w.Result().StatusCode)

		stored, err := queries.BugGet(ctx, bugID)
		require.NoError(t, err)
		require.Equal(t, "filed", stored.Status)
		require.Equal(t, "http://gh/1", stored.IssueUrl.String)

		// no longer surfaced to the triage list
		req = httptest.NewRequest(http.MethodGet, "/bugs", nil)
		req.Header.Set(adminHeader, secret)
		w = httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		var bugs []sqlc.Bugs
		require.NoError(t, json.NewDecoder(w.Result().Body).Decode(&bugs))
		require.Empty(t, bugs)
	})

	t.Run("patch rejects invalid status", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"status": "bogus"})
		req := httptest.NewRequest(http.MethodPatch, bugPath(bugID), strings.NewReader(string(body)))
		req.Header.Set(adminHeader, secret)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		require.Equal(t, http.StatusBadRequest, w.Result().StatusCode)
	})

	t.Run("suggestion submit and delete", func(t *testing.T) {
		form := url.Values{"front": {"in a whisper"}, "back": {"too loudly"}}
		req := httptest.NewRequest(http.MethodPost, "/suggestions", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		require.Equal(t, http.StatusNoContent, w.Result().StatusCode)

		list, err := queries.SuggestionsNew(ctx)
		require.NoError(t, err)
		require.Len(t, list, 1)
		id := list[0].ID

		req = httptest.NewRequest(http.MethodDelete, suggestionPath(id), nil)
		req.Header.Set(adminHeader, secret)
		w = httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		require.Equal(t, http.StatusNoContent, w.Result().StatusCode)

		list, err = queries.SuggestionsNew(ctx)
		require.NoError(t, err)
		require.Empty(t, list)
	})
}

func bugPath(id int32) string        { return "/bugs?id=" + strconv.Itoa(int(id)) }
func suggestionPath(id int32) string { return "/suggestions?id=" + strconv.Itoa(int(id)) }
