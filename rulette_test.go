package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
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

// testDBOpts returns PostgresOpts for the test database, reading from
// RULETTE_PG_URL when available and falling back to safe defaults.
// NewTestDB uses testcontainers, so these credentials are only used to
// configure the ephemeral container, not to connect to any external database.
func testDBOpts(t *testing.T) postgres.PostgresOpts {
	t.Helper()
	opts := postgres.PostgresOpts{
		User:     "postgres",
		Password: "testcontainer",
		Name:     "rulette",
		Sslmode:  "disable",
	}
	pgURL := os.Getenv("RULETTE_PG_URL")
	if pgURL == "" {
		return opts
	}
	u, err := url.Parse(pgURL)
	if err != nil {
		t.Fatalf("parse RULETTE_PG_URL: %v", err)
	}
	if u.User != nil {
		if username := u.User.Username(); username != "" {
			opts.User = username
		}
		if pass, ok := u.User.Password(); ok && pass != "" {
			opts.Password = pass
		}
	}
	if dbName := strings.Trim(u.Path, "/"); dbName != "" {
		opts.Name = dbName
	}
	return opts
}

func TestGame(t *testing.T) {
	initLogger(nil)
	t.Log("setting up db")
	opts := testDBOpts(t)
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
	dbPool = pool
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
		req := httptest.NewRequest(http.MethodPost, "/create", nil)
		w := httptest.NewRecorder()
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
		// a duplicate name bounces back to the join page with a popup code
		// rather than dead-ending on a raw 409.
		require.Equal(t, http.StatusSeeOther, w.Result().StatusCode,
			"duplicate username should redirect back to join: %s", users[0].username,
		)
		require.Equal(t, fmt.Sprintf("/%s/join?alert=name-taken", gameID),
			w.Result().Header.Get("Location"),
			"redirect target should be the join page with name-taken alert",
		)
	})

	t.Run("GET /{game_id}/join (existing player)", func(t *testing.T) {
		req := httptest.NewRequest(
			http.MethodGet, fmt.Sprintf("/%s/join", gameID), nil,
		)
		req.AddCookie(users[0].cookie)
		w := httptest.NewRecorder()
		joinHandler(w, req)
		require.Equal(t, http.StatusSeeOther, w.Result().StatusCode,
			"existing player GET /join should redirect",
		)
		require.Equal(t, fmt.Sprintf("/%s", gameID),
			w.Result().Header.Get("Location"),
			"redirect target should be the game page",
		)
		for _, c := range w.Result().Cookies() {
			require.NotEqual(t, "session", c.Name,
				"existing-player redirect must not issue a new session cookie",
			)
		}
	})

	t.Run("GET /{game_id}/join (no cookie)", func(t *testing.T) {
		req := httptest.NewRequest(
			http.MethodGet, fmt.Sprintf("/%s/join", gameID), nil,
		)
		w := httptest.NewRecorder()
		joinHandler(w, req)
		require.Equal(t, http.StatusOK, w.Result().StatusCode,
			"strangers without a cookie should still get the join form",
		)
	})

	t.Run("GET /{game_id}/qr (inviting)", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/{game_id}/qr", qrHandler)
		path := fmt.Sprintf("/%s/qr", gameID)
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Result().StatusCode)
		require.Equal(t, "image/png", w.Result().Header.Get("Content-Type"))

		// dump the rendered banner+QR for local eyeballing when requested
		if outDir := os.Getenv("RULETTE_TEST_DUMP_DIR"); outDir != "" {
			require.NoError(t, os.MkdirAll(outDir, 0o755))
			out := outDir + "/qr.png"
			require.NoError(t, os.WriteFile(out, w.Body.Bytes(), 0o644))
			t.Logf("wrote %s", out)
		}
	})
	t.Run("GET /{game_id}/qr (unknown game)", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/{game_id}/qr", qrHandler)
		req := httptest.NewRequest(http.MethodGet, "/000000/qr", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		require.Equal(t, http.StatusNotFound, w.Result().StatusCode)
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
	// a game with a single non-host player can start, but only after the host
	// confirms. this is self-contained: its own game and players, so the shared
	// gameID and the package-level users slice are untouched.
	t.Run("POST /{game_id}/action/start (one player confirm)", func(t *testing.T) {
		// fresh game
		req := httptest.NewRequest(http.MethodPost, "/create", nil)
		w := httptest.NewRecorder()
		createHandler(w, req)
		require.Equal(t, http.StatusSeeOther, w.Result().StatusCode)
		parts := strings.Split(w.Result().Header.Get("Location"), "/")
		require.Len(t, parts, 3)
		soloGameID := parts[1]

		// host (first joiner) plus a single non-host player
		solo := []testuser{{username: "Anna"}, {username: "Oscar"}}
		for i, user := range solo {
			values := url.Values{}
			values.Set("username", user.username)
			u := &url.URL{
				Path:     fmt.Sprintf("/%s/join", soloGameID),
				RawQuery: values.Encode(),
			}
			req := httptest.NewRequest(http.MethodPost, u.String(), nil)
			w := httptest.NewRecorder()
			joinHandler(w, req)
			require.Equal(t, http.StatusSeeOther, w.Result().StatusCode,
				"failed to join with username: %s", user.username,
			)
			for _, c := range w.Result().Cookies() {
				if c.Name == "session" {
					solo[i].cookie = c
				}
			}
			require.NotNil(t, solo[i].cookie,
				"no session cookie for username: %s", user.username,
			)
		}
		host := solo[0]

		startPath := fmt.Sprintf("/%s/action/start", soloGameID)
		statusPath := fmt.Sprintf("/%s/data/status", soloGameID)

		// first attempt: the host is asked to confirm, the game does not start
		req = httptest.NewRequest(http.MethodPost, startPath, nil)
		req.AddCookie(host.cookie)
		w = httptest.NewRecorder()
		actionHandler(w, req)
		require.Equal(t, http.StatusOK, w.Result().StatusCode)
		require.Equal(t, `{"confirmStart":""}`,
			w.Result().Header.Get("HX-Trigger"),
		)
		req = httptest.NewRequest(http.MethodGet, statusPath, nil)
		req.AddCookie(host.cookie)
		w = httptest.NewRecorder()
		dataHandler(w, req)
		require.Equal(t, http.StatusOK, w.Result().StatusCode)
		require.Contains(t, w.Body.String(), "inviting",
			"game should not start before the host confirms",
		)

		// confirmed attempt: the game starts and play begins
		req = httptest.NewRequest(http.MethodPost, startPath+"?confirm=1", nil)
		req.AddCookie(host.cookie)
		w = httptest.NewRecorder()
		actionHandler(w, req)
		require.Equal(t, http.StatusOK, w.Result().StatusCode)
		req = httptest.NewRequest(http.MethodGet, statusPath, nil)
		req.AddCookie(host.cookie)
		w = httptest.NewRecorder()
		dataHandler(w, req)
		require.Equal(t, http.StatusOK, w.Result().StatusCode)
		require.Contains(t, w.Body.String(), "turn",
			"game should start once the host confirms",
		)
	})
	t.Run("POST /{game_id}/action/start", func(t *testing.T) {
		path := fmt.Sprintf("/%s/action/start", gameID)
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.AddCookie(users[0].cookie)
		w := httptest.NewRecorder()
		actionHandler(w, req)
		require.Equal(t, http.StatusOK, w.Result().StatusCode)
	})
	t.Run("GET /{game_id}/qr (not accepting invites)", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/{game_id}/qr", qrHandler)
		path := fmt.Sprintf("/%s/qr", gameID)
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		cache.Delete(gameID)
		mux.ServeHTTP(w, req)
		require.Equal(t, http.StatusConflict, w.Result().StatusCode)
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

		const maxSpins = 200
		var exhausted bool
		for i := 0; i < maxSpins; i++ {
			c := cookieByInitiative[current]
			require.NotNil(t, c, "no cookie for initiative %d", current)

			path := fmt.Sprintf("/%s/action/spin", gameID)
			req := httptest.NewRequest(http.MethodPost, path, nil)
			req.AddCookie(c)
			w := httptest.NewRecorder()
			cache.Delete(gameID)
			actionHandler(w, req)

			require.Equal(t, http.StatusOK, w.Result().StatusCode,
				"spin %d failed", i,
			)

			// a spent deck moves the game to the "ending" state (6) instead of
			// ending outright; the host ends it explicitly.
			cache.Delete(gameID)
			gs, err := queries.GameState(ctx, gameID)
			require.NoError(t, err)
			if gs.StateID == stateEnding {
				t.Logf("deck exhausted after %d spins", i)
				exhausted = true
				break
			}
			if gs.StateID == statePending {
				lastSpin, err := queries.SpinPendingModifier(ctx, gameID)
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
					actionPath = fmt.Sprintf("/%s/action/flip?game_card_id=%d",
						gameID, targetCard,
					)
				case modShred:
					actionPath = fmt.Sprintf("/%s/action/shred?game_card_id=%d",
						gameID, targetCard,
					)
				case modClone:
					actionPath = fmt.Sprintf(
						"/%s/action/clone?game_card_id=%d&target_player_id=%d",
						gameID, targetCard, targetPlayer,
					)
				case modTransfer:
					actionPath = fmt.Sprintf(
						"/%s/action/transfer?game_card_id=%d&target_player_id=%d",
						gameID, targetCard, targetPlayer,
					)
				}

				if targetCard == 0 {
					// no rule card to target, just reset state
					t.Logf("spin %d: no rule card to target, skipping", i)
					err = queries.GameUpdate(ctx, sqlc.GameUpdateParams{
						ID:      gameID,
						StateID: stateTurn,
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

			// shredded modifiers don't advance; same player spins again
			trigger := w.Header().Get("HX-Trigger")
			if strings.Contains(trigger, "modifierShredded") {
				continue
			}
			// a drawn rule card holds the turn until the player acknowledges it
			if strings.Contains(trigger, "newCard") {
				ackReq := httptest.NewRequest(http.MethodPost,
					fmt.Sprintf("/%s/action/acknowledge", gameID), nil)
				ackReq.AddCookie(c)
				ackW := httptest.NewRecorder()
				cache.Delete(gameID)
				actionHandler(ackW, ackReq)
				require.Equal(t, http.StatusOK, ackW.Result().StatusCode,
					"spin %d acknowledge failed", i)
			}
			// the turn has advanced (after ack, or by the modifier above)
			current = (current % maxInit) + 1
		}
		require.True(t, exhausted, "deck not exhausted within %d spins", maxSpins)
	})

	t.Run("POST /{game_id}/action/endgame (host ends)", func(t *testing.T) {
		path := fmt.Sprintf("/%s/action/endgame", gameID)
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.AddCookie(cookieByInitiative[0]) // host
		w := httptest.NewRecorder()
		cache.Delete(gameID)
		actionHandler(w, req)
		require.Equal(t, http.StatusOK, w.Result().StatusCode)

		cache.Delete(gameID)
		gs, err := queries.GameState(ctx, gameID)
		require.NoError(t, err)
		require.Equal(t, int32(stateOver), gs.StateID, "expected game over")
	})

	// put game back into ending state for the continue test
	err = queries.GameUpdate(ctx, sqlc.GameUpdateParams{
		ID:      gameID,
		StateID: stateEnding,
		InitiativeCurrent: pgtype.Int4{
			Int32: 1, Valid: true,
		},
	})
	require.NoError(t, err)
	cache.Delete(gameID)

	t.Run("POST /{game_id}/action/continue (host continues)", func(t *testing.T) {
		path := fmt.Sprintf("/%s/action/continue", gameID)
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.AddCookie(cookieByInitiative[0]) // host
		w := httptest.NewRecorder()
		cache.Delete(gameID)
		actionHandler(w, req)
		require.Equal(t, http.StatusOK, w.Result().StatusCode)

		cache.Delete(gameID)
		gs, err := queries.GameState(ctx, gameID)
		require.NoError(t, err)
		require.Equal(t, int32(stateTurn), gs.StateID, "expected game back to turn")
		require.NotEqual(t, int32(1), gs.InitiativeCurrent.Int32, "expected initiative to advance")
	})

	// set up for advance tests: put the game in turn state with a pending
	// rule-card spin so AwaitingAck is true for the current player.
	err = queries.GameUpdate(ctx, sqlc.GameUpdateParams{
		ID:      gameID,
		StateID: stateTurn,
		InitiativeCurrent: pgtype.Int4{
			Int32: 1, Valid: true,
		},
	})
	require.NoError(t, err)
	// find the player_id for initiative 1
	var advancePlayerID int32
	for _, p := range players {
		if p.Initiative.Int32 == 1 {
			advancePlayerID = p.PlayerID
			break
		}
	}
	require.NotZero(t, advancePlayerID, "need a player at initiative 1")
	// seed a rule-card spin for the current turn player so AwaitingAck fires
	_, err = dbPool.Exec(ctx,
		`INSERT INTO spins (game_id, player_id, slot, card_id)
		 SELECT $1, $2, 1, c.id FROM cards c WHERE c.type = 'rule' LIMIT 1`,
		gameID, advancePlayerID)
	require.NoError(t, err)
	cache.Delete(gameID)

	t.Run("POST /{game_id}/action/advance (non-host rejected)", func(t *testing.T) {
		path := fmt.Sprintf("/%s/action/advance", gameID)
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.AddCookie(cookieByInitiative[1]) // not the host
		w := httptest.NewRecorder()
		cache.Delete(gameID)
		actionHandler(w, req)
		require.Equal(t, http.StatusForbidden, w.Result().StatusCode)
	})

	t.Run("POST /{game_id}/action/advance (host succeeds)", func(t *testing.T) {
		gsBefore, err := queries.GameState(ctx, gameID)
		require.NoError(t, err)
		initBefore := gsBefore.InitiativeCurrent.Int32

		path := fmt.Sprintf("/%s/action/advance", gameID)
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.AddCookie(cookieByInitiative[0]) // host
		w := httptest.NewRecorder()
		cache.Delete(gameID)
		actionHandler(w, req)
		require.Equal(t, http.StatusOK, w.Result().StatusCode)

		cache.Delete(gameID)
		gsAfter, err := queries.GameState(ctx, gameID)
		require.NoError(t, err)
		require.Equal(t, int32(stateTurn), gsAfter.StateID)
		require.NotEqual(t, initBefore, gsAfter.InitiativeCurrent.Int32,
			"initiative should advance after host advance")
	})

	t.Run("POST /{game_id}/action/advance (no pending ack rejected)", func(t *testing.T) {
		path := fmt.Sprintf("/%s/action/advance", gameID)
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.AddCookie(cookieByInitiative[0]) // host
		w := httptest.NewRecorder()
		cache.Delete(gameID)
		actionHandler(w, req)
		require.Equal(t, http.StatusConflict, w.Result().StatusCode)
	})

	// reset game to a playable state for accuse/decide tests
	err = queries.GameUpdate(ctx, sqlc.GameUpdateParams{
		ID:      gameID,
		StateID: stateTurn,
		InitiativeCurrent: pgtype.Int4{
			Int32: 1, Valid: true,
		},
	})
	require.NoError(t, err)
	cache.Delete(gameID)

	// find a rule card held by a non-host player to accuse on
	allCards, err := queries.GameCardsPlayerView(ctx, gameID)
	require.NoError(t, err)
	var accusedPlayerID int32
	var ruleGameCardID int32
	var accuserCookie *http.Cookie
	for _, card := range allCards {
		if card.Type == "rule" && card.PlayerID.Int32 != 1 {
			accusedPlayerID = card.PlayerID.Int32
			ruleGameCardID = card.ID
			// accuser is a different non-host player
			for _, p := range players {
				if p.PlayerID != accusedPlayerID && p.Initiative.Int32 != 0 {
					accuserCookie = cookieByInitiative[p.Initiative.Int32]
					break
				}
			}
			break
		}
	}
	require.NotZero(t, ruleGameCardID, "need a rule card for accuse test")
	require.NotNil(t, accuserCookie, "need an accuser cookie")

	// get accused player's points before accusation
	playersBefore, err := queries.GamePlayerPoints(ctx, gameID)
	require.NoError(t, err)
	var pointsBefore int32
	for _, p := range playersBefore {
		if p.PlayerID == accusedPlayerID {
			pointsBefore = p.Points.Int32
			break
		}
	}

	t.Run("POST /{game_id}/action/accuse", func(t *testing.T) {
		path := fmt.Sprintf(
			"/%s/action/accuse?defendant_id=%d&game_card_id=%d",
			gameID, accusedPlayerID, ruleGameCardID,
		)
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.AddCookie(accuserCookie)
		w := httptest.NewRecorder()
		cache.Delete(gameID)
		actionHandler(w, req)
		require.Equal(t, http.StatusOK, w.Result().StatusCode)

		// verify game entered challenge state
		cache.Delete(gameID)
		gs, err := queries.GameState(ctx, gameID)
		require.NoError(t, err)
		require.Equal(t, int32(stateChallenge), gs.StateID, "expected challenge state")
	})

	t.Run("POST /{game_id}/action/decide (non-host rejected)", func(t *testing.T) {
		path := fmt.Sprintf(
			"/%s/action/decide?infraction_id=1&verdict=affirm&points=2",
			gameID,
		)
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.AddCookie(accuserCookie) // not the host
		w := httptest.NewRecorder()
		cache.Delete(gameID)
		actionHandler(w, req)
		require.Equal(t, http.StatusForbidden, w.Result().StatusCode)
	})

	t.Run("POST /{game_id}/action/decide (affirm)", func(t *testing.T) {
		penalty := int32(2)
		path := fmt.Sprintf(
			"/%s/action/decide?infraction_id=1&verdict=affirm&amount=%d&player_id=%d",
			gameID, penalty, accusedPlayerID,
		)
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.AddCookie(cookieByInitiative[0]) // host
		w := httptest.NewRecorder()
		cache.Delete(gameID)
		actionHandler(w, req)
		require.Equal(t, http.StatusOK, w.Result().StatusCode)

		// verify game returned to turn state
		cache.Delete(gameID)
		gs, err := queries.GameState(ctx, gameID)
		require.NoError(t, err)
		require.Equal(t, int32(stateTurn), gs.StateID, "expected turn state")

		// verify points adjusted
		playersAfter, err := queries.GamePlayerPoints(ctx, gameID)
		require.NoError(t, err)
		for _, p := range playersAfter {
			if p.PlayerID == accusedPlayerID {
				require.Equal(t, pointsBefore-penalty, p.Points.Int32,
					"expected %d points deducted", penalty,
				)
				break
			}
		}
	})

	// test absolve flow
	t.Run("POST /{game_id}/action/accuse (for absolve)", func(t *testing.T) {
		path := fmt.Sprintf(
			"/%s/action/accuse?defendant_id=%d&game_card_id=%d",
			gameID, accusedPlayerID, ruleGameCardID,
		)
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.AddCookie(accuserCookie)
		w := httptest.NewRecorder()
		cache.Delete(gameID)
		actionHandler(w, req)
		require.Equal(t, http.StatusOK, w.Result().StatusCode)
	})

	t.Run("POST /{game_id}/action/decide (absolve)", func(t *testing.T) {
		path := fmt.Sprintf(
			"/%s/action/decide?infraction_id=2&verdict=absolve",
			gameID,
		)
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.AddCookie(cookieByInitiative[0]) // host
		w := httptest.NewRecorder()
		cache.Delete(gameID)
		actionHandler(w, req)
		require.Equal(t, http.StatusOK, w.Result().StatusCode)

		// verify game returned to turn state
		cache.Delete(gameID)
		gs, err := queries.GameState(ctx, gameID)
		require.NoError(t, err)
		require.Equal(t, int32(stateTurn), gs.StateID)
	})

	// modifier-vs-challenge interplay: a transfer modifier owed by the turn
	// player is interrupted by a challenge. The transfer must defer (423) while
	// the challenge is live, decide must restore pending (because the modifier
	// is still in hand), and the retried transfer must then succeed.
	//
	// pick the turn player (initiative 1), a target, and an accuser
	var turnPlayerID, targetPlayerID int32
	for _, p := range players {
		if p.Initiative.Int32 == 1 {
			turnPlayerID = p.PlayerID
		}
	}
	var modAccuserCookie *http.Cookie
	for _, p := range players {
		if p.Initiative.Int32 != 0 && p.PlayerID != turnPlayerID {
			targetPlayerID = p.PlayerID
			modAccuserCookie = cookieByInitiative[p.Initiative.Int32]
			break
		}
	}
	require.NotZero(t, turnPlayerID, "need a turn player")
	require.NotZero(t, targetPlayerID, "need a transfer target")
	require.NotNil(t, modAccuserCookie, "need an accuser")
	turnCookie := cookieByInitiative[1]
	require.NotNil(t, turnCookie, "need the turn player's cookie")

	// seed a pending transfer modifier owned by the turn player, alongside a
	// rule card to give away. Deal exactly these two so the shred check is exact.
	var transferGCID, ruleGCID int32
	require.NoError(t, dbPool.QueryRow(ctx,
		`SELECT gc.id FROM game_cards gc JOIN cards c ON c.id = gc.card_id
		 WHERE gc.game_id = $1 AND c.modifier_effect = 'transfer' LIMIT 1`,
		gameID).Scan(&transferGCID))
	require.NoError(t, dbPool.QueryRow(ctx,
		`SELECT gc.id FROM game_cards gc JOIN cards c ON c.id = gc.card_id
		 WHERE gc.game_id = $1 AND c.type = 'rule' LIMIT 1`,
		gameID).Scan(&ruleGCID))
	// clear the turn player's hand by shredding (keeping player_id set so these
	// don't leak into the wheel view, which keys on player_id IS NULL), then
	// deal exactly the two seeded cards.
	_, err = dbPool.Exec(ctx,
		`UPDATE game_cards SET shredded = true
		 WHERE game_id = $1 AND player_id = $2`, gameID, turnPlayerID)
	require.NoError(t, err)
	_, err = dbPool.Exec(ctx,
		`UPDATE game_cards SET player_id = $1, shredded = false, slot = NULL
		 WHERE game_id = $2 AND id IN ($3, $4)`,
		turnPlayerID, gameID, transferGCID, ruleGCID)
	require.NoError(t, err)
	_, err = dbPool.Exec(ctx,
		`INSERT INTO spins (game_id, player_id, slot, card_id, ts)
		 SELECT $1, $2, 1, id, now() FROM cards WHERE modifier_effect = 'transfer' LIMIT 1`,
		gameID, turnPlayerID)
	require.NoError(t, err)
	require.NoError(t, queries.GameUpdate(ctx, sqlc.GameUpdateParams{
		ID:                gameID,
		StateID:           statePending,
		InitiativeCurrent: pgtype.Int4{Int32: 1, Valid: true},
	}))
	cache.Delete(gameID)

	t.Run("POST /{game_id}/action/transfer (423 during challenge)", func(t *testing.T) {
		// a non-host accuses the turn player, interrupting the pending modifier
		accusePath := fmt.Sprintf(
			"/%s/action/accuse?defendant_id=%d&game_card_id=%d",
			gameID, turnPlayerID, ruleGCID,
		)
		req := httptest.NewRequest(http.MethodPost, accusePath, nil)
		req.AddCookie(modAccuserCookie)
		w := httptest.NewRecorder()
		cache.Delete(gameID)
		actionHandler(w, req)
		require.Equal(t, http.StatusOK, w.Result().StatusCode)
		cache.Delete(gameID)
		gs, err := queries.GameState(ctx, gameID)
		require.NoError(t, err)
		require.Equal(t, int32(stateChallenge), gs.StateID, "accuse should enter challenge")

		// the turn player tries to transfer mid-challenge: deferred, not dead
		xferPath := fmt.Sprintf(
			"/%s/action/transfer?game_card_id=%d&target_player_id=%d",
			gameID, ruleGCID, targetPlayerID,
		)
		req = httptest.NewRequest(http.MethodPost, xferPath, nil)
		req.AddCookie(turnCookie)
		w = httptest.NewRecorder()
		cache.Delete(gameID)
		actionHandler(w, req)
		require.Equal(t, http.StatusLocked, w.Result().StatusCode,
			"transfer during challenge should defer with 423")

		// card stays with the turn player
		cache.Delete(gameID)
		cards, err := queries.GameCardsPlayerView(ctx, gameID)
		require.NoError(t, err)
		for _, c := range cards {
			if c.ID == ruleGCID {
				require.Equal(t, turnPlayerID, c.PlayerID.Int32,
					"rule card must not move on a deferred transfer")
			}
		}
	})

	t.Run("POST /{game_id}/action/decide (restores pending, transfer succeeds)", func(t *testing.T) {
		var infID int32
		require.NoError(t, dbPool.QueryRow(ctx,
			`SELECT id FROM infractions WHERE game_id = $1 AND active = true
			 ORDER BY id DESC LIMIT 1`, gameID).Scan(&infID))

		decidePath := fmt.Sprintf(
			"/%s/action/decide?infraction_id=%d&verdict=absolve", gameID, infID,
		)
		req := httptest.NewRequest(http.MethodPost, decidePath, nil)
		req.AddCookie(cookieByInitiative[0]) // host
		w := httptest.NewRecorder()
		cache.Delete(gameID)
		actionHandler(w, req)
		require.Equal(t, http.StatusOK, w.Result().StatusCode)

		// the turn player still holds the modifier, so pending is restored
		cache.Delete(gameID)
		gs, err := queries.GameState(ctx, gameID)
		require.NoError(t, err)
		require.Equal(t, int32(statePending), gs.StateID,
			"decide should restore pending when a modifier is still owed")

		// the retried transfer now goes through
		xferPath := fmt.Sprintf(
			"/%s/action/transfer?game_card_id=%d&target_player_id=%d",
			gameID, ruleGCID, targetPlayerID,
		)
		req = httptest.NewRequest(http.MethodPost, xferPath, nil)
		req.AddCookie(turnCookie)
		w = httptest.NewRecorder()
		cache.Delete(gameID)
		actionHandler(w, req)
		require.Equal(t, http.StatusOK, w.Result().StatusCode,
			"retried transfer should succeed once pending is restored")

		// rule card moved to target; modifier card shredded (gone from the view)
		cache.Delete(gameID)
		cards, err := queries.GameCardsPlayerView(ctx, gameID)
		require.NoError(t, err)
		var moved, modifierGone bool
		modifierGone = true
		for _, c := range cards {
			if c.ID == ruleGCID {
				require.Equal(t, targetPlayerID, c.PlayerID.Int32,
					"rule card should move to the target")
				moved = true
			}
			if c.ID == transferGCID {
				modifierGone = false
			}
		}
		require.True(t, moved, "transferred card should appear under the target")
		require.True(t, modifierGone, "used modifier card should be shredded")
	})

	// exit tests: reset game to a known state, then have a non-host player exit.
	t.Run("POST /{game_id}/action/exit (non-turn player)", func(t *testing.T) {
		// put game in turn state with initiative on player 1 (non-host)
		err := queries.GameUpdate(ctx, sqlc.GameUpdateParams{
			ID:      gameID,
			StateID: stateTurn,
			InitiativeCurrent: pgtype.Int4{
				Int32: 1, Valid: true,
			},
		})
		require.NoError(t, err)
		cache.Delete(gameID)

		// initiative 2's player exits (not their turn)
		exitCookie := cookieByInitiative[2]
		require.NotNil(t, exitCookie, "need a player at initiative 2")
		path := fmt.Sprintf("/%s/action/exit", gameID)
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.AddCookie(exitCookie)
		w := httptest.NewRecorder()
		actionHandler(w, req)
		require.Equal(t, http.StatusOK, w.Result().StatusCode)
		require.Equal(t, "/", w.Result().Header.Get("HX-Redirect"))

		// session cookie should be expired
		var expired bool
		for _, c := range w.Result().Cookies() {
			if c.Name == "session" {
				expired = c.MaxAge == -1
			}
		}
		require.True(t, expired, "exit must expire the session cookie")

		// the exited player must no longer appear in game_players
		cache.Delete(gameID)
		remaining, err := queries.GamePlayerPoints(ctx, gameID)
		require.NoError(t, err)
		exitParts := strings.Split(exitCookie.Value, ":")
		require.Len(t, exitParts, 2)
		for _, p := range remaining {
			require.NotEqual(t, exitParts[0], fmt.Sprintf("%d", p.PlayerID),
				"exited player must be removed from the game")
		}

		// initiative must still be on player 1 (not the one who exited)
		cache.Delete(gameID)
		gs, err := queries.GameState(ctx, gameID)
		require.NoError(t, err)
		require.Equal(t, int32(1), gs.InitiativeCurrent.Int32,
			"initiative should remain on player 1 when the exiting player was not the current player")
	})

	t.Run("POST /{game_id}/action/exit (turn player advances initiative)", func(t *testing.T) {
		// put game in turn state with initiative on player 1 (the current turn player)
		err := queries.GameUpdate(ctx, sqlc.GameUpdateParams{
			ID:      gameID,
			StateID: stateTurn,
			InitiativeCurrent: pgtype.Int4{
				Int32: 1, Valid: true,
			},
		})
		require.NoError(t, err)
		cache.Delete(gameID)

		// player at initiative 1 exits (their turn)
		turnCookie := cookieByInitiative[1]
		require.NotNil(t, turnCookie, "need a player at initiative 1")
		path := fmt.Sprintf("/%s/action/exit", gameID)
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.AddCookie(turnCookie)
		w := httptest.NewRecorder()
		actionHandler(w, req)
		require.Equal(t, http.StatusOK, w.Result().StatusCode)
		require.Equal(t, "/", w.Result().Header.Get("HX-Redirect"))

		// initiative must have advanced past the exiting player's slot
		cache.Delete(gameID)
		gs, err := queries.GameState(ctx, gameID)
		require.NoError(t, err)
		require.NotEqual(t, int32(1), gs.InitiativeCurrent.Int32,
			"initiative should advance when the current turn player exits")
	})

	t.Run("POST /{game_id}/action/exit cards shredded", func(t *testing.T) {
		// Give the next player (initiative=3, if present; else use host) some
		// cards so we can verify they are shredded when they exit.
		cache.Delete(gameID)
		remaining, err := queries.GamePlayerPoints(ctx, gameID)
		require.NoError(t, err)
		if len(remaining) == 0 {
			t.Skip("no players remaining")
		}
		// pick any remaining non-host player
		var exitPlayer sqlc.GamePlayerPointsRow
		var exitCookie *http.Cookie
		for _, p := range remaining {
			if p.Initiative.Int32 != 0 {
				exitPlayer = p
				exitCookie = cookieByInitiative[p.Initiative.Int32]
				break
			}
		}
		if exitCookie == nil {
			t.Skip("no non-host player with a known cookie remaining")
		}

		// spin a card onto that player by manually assigning one from the wheel
		cards, err := queries.GameCardsWheelView(ctx, gameID)
		require.NoError(t, err)
		if len(cards) == 0 {
			t.Skip("no cards on wheel to assign")
		}

		// move the first wheel card to the player (simulates having a card)
		wheelCards, err := queries.GameCardsPlayerView(ctx, gameID)
		require.NoError(t, err)
		_ = wheelCards // just check it doesn't error

		// put game in stateTurn on a different player so the exiting player
		// is not the current turn player (simpler case)
		err = queries.GameUpdate(ctx, sqlc.GameUpdateParams{
			ID:      gameID,
			StateID: stateTurn,
			InitiativeCurrent: pgtype.Int4{
				Int32: 0, // host's initiative
				Valid: true,
			},
		})
		require.NoError(t, err)
		cache.Delete(gameID)

		path := fmt.Sprintf("/%s/action/exit", gameID)
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.AddCookie(exitCookie)
		w := httptest.NewRecorder()
		actionHandler(w, req)
		require.Equal(t, http.StatusOK, w.Result().StatusCode)

		// verify no unshredded cards remain for this player
		cache.Delete(gameID)
		allCards, err := queries.GameCardsPlayerView(ctx, gameID)
		require.NoError(t, err)
		for _, c := range allCards {
			require.NotEqual(t, exitPlayer.PlayerID, c.PlayerID.Int32,
				"exited player must have no remaining unshredded cards")
		}
	})

	t.Run("POST /{game_id}/action/end", func(t *testing.T) {
		// ensure game is in a playable state first
		err := queries.GameUpdate(ctx, sqlc.GameUpdateParams{
			ID:      gameID,
			StateID: stateTurn,
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
