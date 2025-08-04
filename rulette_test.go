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

// FIXME: redo testing
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
	// join game
	t.Run("GET /{game_id}/join", func(t *testing.T) {
		path := fmt.Sprintf("/%s/join", gameID)
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		joinHandler(w, req)
		require.Equal(t, http.StatusOK, w.Result().StatusCode,
			"failed to join game with GET request",
		)
	})
	t.Run("POST /{game_id}/join", func(t *testing.T) {
		users := []string{"Bobson Dugnut", "Mike Truk"}
		for _, username := range users {
			values := url.Values{}
			values.Set("username", username)
			u := &url.URL{
				Path:     fmt.Sprintf("/%s/join", gameID),
				RawQuery: values.Encode(),
			}
			req := httptest.NewRequest(http.MethodPost, u.String(), nil)
			w := httptest.NewRecorder()
			joinHandler(w, req)
			require.Equal(t, http.StatusSeeOther, w.Result().StatusCode,
				"failed to join game with username: %s", username,
			)
			t.Logf("joined game: %s", username)
		}
		// duplicate should fail
		values := url.Values{}
		values.Set("username", users[0])
		u := &url.URL{
			Path:     fmt.Sprintf("/%s/join", gameID),
			RawQuery: values.Encode(),
		}
		req := httptest.NewRequest(http.MethodPost, u.String(), nil)
		w := httptest.NewRecorder()
		joinHandler(w, req)
		require.Equal(t, http.StatusConflict, w.Result().StatusCode,
			"should not be able to join game with duplicate username: %s", users[0],
		)
	})
	// send invites
	// accept invites
	// create initiative + start game
	// game loop
	// - spin
	// - accuse, judge
	//   - absolve
	//   - convict
	// - consequences
	// - end game
}
