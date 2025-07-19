package main

import (
	"context"
	"fmt"
	"testing"

	"github.com/grackleclub/postgres"
	"github.com/stretchr/testify/require"
)

func TestMain(t *testing.T) {
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

	fmt.Println(dbSchema)
}
