package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grackleclub/postgres"
	sqlc "github.com/grackleclub/rulette/db/sqlc"
	"github.com/stretchr/testify/require"
)

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

	t.Run("get state", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		w := httptest.NewRecorder()

		stateHandler(w, req)

		require.Equal(t, http.StatusOK, w.Result().StatusCode)
		t.Logf("Response: %s", w.Body.String())
	})
}
