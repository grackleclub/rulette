package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/grackleclub/postgres"
	sqlc "github.com/grackleclub/rulette/db/sqlc"
	"github.com/stretchr/testify/require"
)

type testuser struct {
	username string
	cookie   *http.Cookie
}

var users = map[string]testuser{
	"bob": {
		username: "Bobson Dugnut",
		cookie:   nil,
	},
	"mike": {
		username: "Mike Truk",
		cookie:   nil,
	},
}

func TestMain(t *testing.T) {
	t.Log("setting up db")
	opts := postgres.PostgresOpts{
		Host:     "localhost",
		User:     "postgres",
		Password: "TODO:replace-temporary",
		Port:     "5432",
		Sslmode:  "disable",
	}
	ctx := context.Background()
	db, teardown, err := postgres.NewTestDB(ctx, opts)
	require.NoError(t, err)
	defer teardown()
	require.NotNil(t, db)
	t.Logf("database opened on %s:%s", db.Host, db.Port)

	t.Log("creating queries")
	pool, err := db.Pool(ctx)
	require.NoError(t, err)
	require.NotNil(t, pool)
	queries = sqlc.New(pool)
	require.NotNil(t, queries)
	t.Log("queries created")

	t.Run("run migration", func(t *testing.T) {
		result, err := db.Conn.ExecContext(ctx, dbSchema)
		require.NoError(t, err)
		t.Log("schema up: ", result)
	})

	// welcome screen
	t.Run("GET /", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()
		rootHandler(w, req)
		require.Equal(t, http.StatusOK, w.Result().StatusCode)
	})
	// new game
	var gameID string
	t.Run("POST /create", func(t *testing.T) {
		// create new game with gamename="Test Game"
		req := httptest.NewRequest(http.MethodPost, "/create", nil)
		w := httptest.NewRecorder()
		req.Form = map[string][]string{
			"gamename": {"Test Game"},
		}
		createHandler(w, req)
		require.Equal(t, http.StatusSeeOther, w.Result().StatusCode)
		// capture the redirect location
		location := w.Result().Header.Get("Location")
		t.Logf("redirected to: %s", location)
		parts := strings.Split(location, "/")
		require.Len(t, parts, 3)
		require.Equal(t, "join", parts[2])
		gameID = parts[1]
		t.Logf("game created: %s", gameID)
	})
	t.Run("GET /{game_id}/data/status (no cookie)", func(t *testing.T) {
		path := fmt.Sprintf("/%s/data/status", gameID)
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		dataHandler(w, req)
		require.Equal(t, http.StatusUnauthorized, w.Result().StatusCode)
	})
	// join game
	t.Run("POST /{game_id}/join", func(t *testing.T) {
		for k, user := range users {
			values := url.Values{}
			values.Set("username", user.username)
			u := &url.URL{
				Path:     fmt.Sprintf("/%s/join", gameID),
				RawQuery: values.Encode(),
			}
			req := httptest.NewRequest(http.MethodPost, u.String(), nil)
			w := httptest.NewRecorder()
			joinHandler(w, req)
			require.Equal(t, http.StatusSeeOther, w.Result().StatusCode,
				"failed to join game with username: %s", user,
			)
			cookies := w.Result().Cookies()
			var found bool
			for _, cookie := range cookies {
				if cookie.Name == "session" {
					user.cookie = cookie
					users[k] = user
					found = true
				}
			}
			if !found {
				t.Fatalf("session cookie not set after joining game with username: %s", user.username)
			}
			t.Logf("joined game username=%s", user.username)
		}
		// duplicate should fail
		values := url.Values{}
		values.Set("username", users["bob"].username)
		u := &url.URL{
			Path:     fmt.Sprintf("/%s/join", gameID),
			RawQuery: values.Encode(),
		}
		req := httptest.NewRequest(http.MethodPost, u.String(), nil)
		w := httptest.NewRecorder()
		joinHandler(w, req)
		require.Equal(t, http.StatusConflict, w.Result().StatusCode,
			"should not be able to join game with duplicate username: %s", users["bob"].username,
		)
	})

	t.Run("GET /{game_id}/data/status (expect inviting)", func(t *testing.T) {
		path := fmt.Sprintf("/%s/data/status", gameID)
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(users["bob"].cookie)
		w := httptest.NewRecorder()
		dataHandler(w, req)
		require.Equal(t, http.StatusOK, w.Result().StatusCode)
		status := w.Body.String()
		require.Contains(t, status, "inviting")
	})
	t.Run("GET /{game_id}/data/players", func(t *testing.T) {
		path := fmt.Sprintf("/%s/data/players", gameID)
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(users["bob"].cookie)
		w := httptest.NewRecorder()
		dataHandler(w, req)
		require.Equal(t, http.StatusOK, w.Result().StatusCode)
	})
	t.Run("GET /{game_id}/data/table", func(t *testing.T) {
		path := fmt.Sprintf("/%s/data/table", gameID)
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(users["bob"].cookie)
		w := httptest.NewRecorder()
		dataHandler(w, req)
		require.Equal(t, http.StatusOK, w.Result().StatusCode)
	})
	t.Run("POST /{game_id}/action/start", func(t *testing.T) {
		path := fmt.Sprintf("/%s/action/start", gameID)
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.AddCookie(users["bob"].cookie)
		w := httptest.NewRecorder()
		actionHandler(w, req)
		require.Equal(t, http.StatusOK, w.Result().StatusCode)
	})
	t.Run("GET /{game_id}/data/status (expect=ready)", func(t *testing.T) {
		path := fmt.Sprintf("/%s/data/status", gameID)
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(users["bob"].cookie)
		w := httptest.NewRecorder()
		dataHandler(w, req)
		require.Equal(t, http.StatusOK, w.Result().StatusCode)
		status := w.Body.String()
		require.Contains(t, status, "ready")
	})
	// spin
	// TODO: implement the rest
	t.Run("POST /{game_id}/action/spin", func(t *testing.T) {})
	// - accuse, judge
	//   - absolve
	//   - convict
	// - consequences
	// - end game
	t.Run("POST /{game_id}/action/end", func(t *testing.T) {
		path := fmt.Sprintf("/%s/action/end", gameID)
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.AddCookie(users["bob"].cookie)
		w := httptest.NewRecorder()
		actionHandler(w, req)
		require.Equal(t, http.StatusGone, w.Result().StatusCode)
	})
}
