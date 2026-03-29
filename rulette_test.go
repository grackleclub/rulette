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
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

type testuser struct {
	username string
	cookie   *http.Cookie
}

// ordered to match bin/mock: first player is host (initiative 0)
var users = []testuser{
	{username: "Sam"},
	{username: "Oscar"},
	{username: "Anna"},
	{username: "Jeremy"},
}

func TestGame(t *testing.T) {
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
	// TODO: does this work accidentally?
	t.Run("GET /{game_id}/data/status (no cookie)", func(t *testing.T) {
		path := fmt.Sprintf("/%s/data/status", gameID)
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		dataHandler(w, req)
		require.Equal(t, http.StatusUnauthorized, w.Result().StatusCode)
	})
	// join game
	t.Run("POST /{game_id}/join", func(t *testing.T) {
		for i, user := range users {
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
					users[i].cookie = cookie
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
		values.Set("username", users[0].username)
		u := &url.URL{
			Path:     fmt.Sprintf("/%s/join", gameID),
			RawQuery: values.Encode(),
		}
		req := httptest.NewRequest(http.MethodPost, u.String(), nil)
		w := httptest.NewRecorder()
		joinHandler(w, req)
		require.Equal(t, http.StatusConflict, w.Result().StatusCode,
			"should not be able to join game with duplicate username: %s", users[0].username,
		)
	})

	t.Run("GET /{game_id}/data/status (expect inviting)", func(t *testing.T) {
		path := fmt.Sprintf("/%s/data/status", gameID)
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(users[0].cookie)
		w := httptest.NewRecorder()
		dataHandler(w, req)
		require.Equal(t, http.StatusOK, w.Result().StatusCode)
		status := w.Body.String()
		require.Contains(t, status, "inviting")
	})
	t.Run("GET /{game_id}/data/players", func(t *testing.T) {
		path := fmt.Sprintf("/%s/data/players", gameID)
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(users[0].cookie)
		w := httptest.NewRecorder()
		dataHandler(w, req)
		require.Equal(t, http.StatusOK, w.Result().StatusCode)
	})
	t.Run("GET /{game_id}/data/table", func(t *testing.T) {
		path := fmt.Sprintf("/%s/data/table", gameID)
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(users[0].cookie)
		w := httptest.NewRecorder()
		dataHandler(w, req)
		require.Equal(t, http.StatusOK, w.Result().StatusCode)
	})
	t.Run("POST /{game_id}/action/start", func(t *testing.T) {
		path := fmt.Sprintf("/%s/action/start", gameID)
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.AddCookie(users[0].cookie)
		w := httptest.NewRecorder()
		actionHandler(w, req)
		require.Equal(t, http.StatusOK, w.Result().StatusCode)
	})
	t.Run("GET /{game_id}/data/status (expect=ready)", func(t *testing.T) {
		path := fmt.Sprintf("/%s/data/status", gameID)
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(users[0].cookie)
		w := httptest.NewRecorder()
		dataHandler(w, req)
		require.Equal(t, http.StatusOK, w.Result().StatusCode)
		status := w.Body.String()
		require.Contains(t, status, "turn")
	})
	// build initiative-ordered player list
	players, err := queries.GamePlayerPoints(ctx, gameID)
	require.NoError(t, err)
	cookieByInitiative := make(map[int32]*http.Cookie)
	for _, p := range players {
		for _, u := range users {
			if u.cookie != nil && u.cookie.Value != "" {
				parts := strings.Split(u.cookie.Value, ":")
				if len(parts) == 2 && parts[0] == fmt.Sprintf("%d", p.PlayerID) {
					cookieByInitiative[p.Initiative.Int32] = u.cookie
				}
			}
		}
	}

	// spin through the entire deck, advancing initiative each turn
	t.Run("POST /{game_id}/action/spin (exhaust deck)", func(t *testing.T) {
		state, err := queries.GameState(ctx, gameID)
		require.NoError(t, err)
		current := state.InitiativeCurrent.Int32
		maxInit := int32(0)
		for _, p := range players {
			if p.Initiative.Int32 > maxInit {
				maxInit = p.Initiative.Int32
			}
		}

		for i := 0; ; i++ {
			c := cookieByInitiative[current]
			require.NotNil(t, c, "no cookie for initiative %d", current)

			path := fmt.Sprintf("/%s/action/spin", gameID)
			req := httptest.NewRequest(http.MethodPost, path, nil)
			req.AddCookie(c)
			w := httptest.NewRecorder()
			cache.Delete(gameID)
			actionHandler(w, req)

			if w.Result().StatusCode == http.StatusGone {
				t.Logf("deck exhausted after %d spins", i)
				break
			}
			require.Equal(t, http.StatusOK, w.Result().StatusCode,
				"spin %d failed", i,
			)

			// check if we entered pending state (modifier drawn)
			cache.Delete(gameID)
			gs, err := queries.GameState(ctx, gameID)
			require.NoError(t, err)
			if gs.StateID == 4 {
				lastSpin, err := queries.SpinLogPendingModifier(ctx, gameID)
				require.NoError(t, err)
				effect := lastSpin.ModifierEffect.String
				t.Logf("spin %d: modifier=%s, state=pending", i, effect)

				// find a card the current player holds to target
				cards, err := queries.GameCardsPlayerView(ctx, gameID)
				require.NoError(t, err)
				var targetCard int32
				for _, card := range cards {
					if card.PlayerID.Int32 == lastSpin.PlayerID.Int32 &&
						card.Type == "rule" {
						targetCard = card.ID
						break
					}
				}

				// pick a different player as target for clone/transfer
				var targetPlayer int32
				for _, p := range players {
					if p.PlayerID != lastSpin.PlayerID.Int32 {
						targetPlayer = p.PlayerID
						break
					}
				}

				var actionPath string
				switch effect {
				case modFlip:
					actionPath = fmt.Sprintf("/%s/action/flip?card_id=%d",
						gameID, targetCard,
					)
				case modShred:
					actionPath = fmt.Sprintf("/%s/action/shred?card_id=%d",
						gameID, targetCard,
					)
				case modClone:
					actionPath = fmt.Sprintf(
						"/%s/action/clone?card_id=%d&target_player_id=%d",
						gameID, targetCard, targetPlayer,
					)
				case modTransfer:
					actionPath = fmt.Sprintf(
						"/%s/action/transfer?card_id=%d&target_player_id=%d",
						gameID, targetCard, targetPlayer,
					)
				}

				if targetCard == 0 {
					// no rule card to target, just reset state
					t.Logf("spin %d: no rule card to target, skipping", i)
					err = queries.GameUpdate(ctx, sqlc.GameUpdateParams{
						ID:      gameID,
						StateID: 3,
						InitiativeCurrent: pgtype.Int4{
							Int32: gs.InitiativeCurrent.Int32,
							Valid: true,
						},
					})
					require.NoError(t, err)
				} else {
					modReq := httptest.NewRequest(http.MethodPost, actionPath, nil)
					modReq.AddCookie(c)
					modW := httptest.NewRecorder()
					cache.Delete(gameID)
					actionHandler(modW, modReq)
					require.Equal(t, http.StatusOK, modW.Result().StatusCode,
						"spin %d: %s action failed", i, effect,
					)
					t.Logf("spin %d: %s resolved", i, effect)
					// modifier auto-advances initiative, skip manual next
					current = (current % maxInit) + 1
					continue
				}
			}

			// host advances initiative via action handler
			cache.Delete(gameID)
			nextPath := fmt.Sprintf("/%s/action/next", gameID)
			nextReq := httptest.NewRequest(http.MethodPost, nextPath, nil)
			nextReq.AddCookie(cookieByInitiative[0]) // host
			nextW := httptest.NewRecorder()
			actionHandler(nextW, nextReq)
			require.Equal(t, http.StatusOK, nextW.Result().StatusCode,
				"next failed on spin %d", i,
			)
			current = (current % maxInit) + 1
		}
	})

	// - accuse, decide
	//   - absolve
	//   - convict
	// - consequences
	// - end game
	t.Run("POST /{game_id}/action/end", func(t *testing.T) {
		// ensure game is in a playable state first
		err := queries.GameUpdate(ctx, sqlc.GameUpdateParams{
			ID:      gameID,
			StateID: 3,
			InitiativeCurrent: pgtype.Int4{
				Int32: 1, Valid: true,
			},
		})
		require.NoError(t, err)
		path := fmt.Sprintf("/%s/action/end", gameID)
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.AddCookie(cookieByInitiative[0]) // host
		w := httptest.NewRecorder()
		cache.Delete(gameID)
		actionHandler(w, req)
		require.Equal(t, http.StatusGone, w.Result().StatusCode)
	})
}
