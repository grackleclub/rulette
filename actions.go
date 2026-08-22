package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"

	sqlc "github.com/grackleclub/rulette/db/sqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"go.opentelemetry.io/otel/trace"
)

const (
	// minimumPlayers is the number of non-host players required to start, so
	// that someone holds the starting initiative and the game can't soft-lock.
	minimumPlayers = 1
	// recommendedPlayers is how many non-host players the game plays best with.
	// fewer than this still starts, but only after the host confirms.
	recommendedPlayers = 2
	// promptSeconds is how long the spinner has to complete a prompt challenge.
	// The spinner's countdown runs locally; the host may rule it complete at
	// any time but can only rule it failed once this has elapsed.
	promptSeconds = 60
	// promptGraceSeconds is the server-side allowance before the host may rule
	// a prompt failed: a couple seconds past promptSeconds to absorb the latency
	// between the spinner's local clock and the server.
	promptGraceSeconds = promptSeconds + 2
)

// actionRequest carries what every action handler needs: the request, a
// logger already tagged with the request fields, and the cached game state
// with caller info populated. Handlers return failures as errors for
// actionHandler to write; they only write the success response themselves,
// usually via refresh.
type actionRequest struct {
	w      http.ResponseWriter
	r      *http.Request
	log    *slog.Logger
	gameID string
	action string
	state  *state
}

// caller returns the acting player's id, resolved from the session key.
func (a *actionRequest) caller() int32 {
	return int32(a.state.CallerID)
}

// refresh invalidates the game's cached state and answers the HTMX request.
// trigger is the HX-Trigger value: a bare event name or a JSON object
// carrying several events.
func (a *actionRequest) refresh(trigger string) {
	cache.Delete(a.gameID)
	a.w.Header().Set("HX-Trigger", trigger)
	a.w.WriteHeader(http.StatusOK)
}

// queryInt reads a required integer query parameter.
func (a *actionRequest) queryInt(name string) (int32, error) {
	s := a.r.URL.Query().Get(name)
	if s == "" {
		return 0, failf(http.StatusBadRequest, "missing %s", name)
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, failf(http.StatusBadRequest, "invalid %s", name)
	}
	return int32(v), nil
}

// formInt reads a required integer form value.
func (a *actionRequest) formInt(name string) (int32, error) {
	s := a.r.FormValue(name)
	if s == "" {
		return 0, failf(http.StatusBadRequest, "missing %s", name)
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, failf(http.StatusBadRequest, "invalid %s", name)
	}
	return int32(v), nil
}

// begin opens a transaction and returns queries bound to it. The caller
// must defer a rollback and commit on success.
func (a *actionRequest) begin() (pgx.Tx, *sqlc.Queries, error) {
	tx, err := dbPool.Begin(a.r.Context())
	if err != nil {
		return nil, nil, serverErr("begin transaction", err)
	}
	return tx, queries.WithTx(tx), nil
}

// actionError is a failure to report to the client: status and msg go on
// the wire; err, when set, is the underlying cause and is only logged.
type actionError struct {
	status int
	msg    string
	err    error
}

func (e *actionError) Error() string {
	if e.err != nil {
		return fmt.Sprintf("%s: %v", e.msg, e.err)
	}
	return e.msg
}

func (e *actionError) Unwrap() error {
	return e.err
}

// failf builds a client-fault error (4xx); the formatted message is sent
// to the client verbatim.
func failf(status int, format string, args ...any) error {
	return &actionError{status: status, msg: fmt.Sprintf(format, args...)}
}

// serverErr wraps an unexpected failure as a 500; the client sees only
// "server error" while op and the cause go to the log.
func serverErr(op string, err error) error {
	return &actionError{
		status: http.StatusInternalServerError,
		msg:    "server error",
		err:    fmt.Errorf("%s: %w", op, err),
	}
}

// writeActionError logs a failed action and writes its HTTP answer. 5xx
// log as errors; 423 and 425 log as debug, because clients poll into
// those routinely (a queued modifier retry, a too-early prompt fail) and
// the noise would drown real warnings; other 4xx log as warnings.
func writeActionError(w http.ResponseWriter, log *slog.Logger, err error) {
	var ae *actionError
	if !errors.As(err, &ae) {
		ae = &actionError{
			status: http.StatusInternalServerError,
			msg:    "server error",
			err:    err,
		}
	}
	switch {
	case ae.status >= http.StatusInternalServerError:
		log.Error(ae.msg, "error", ae.err)
	case ae.status == http.StatusLocked || ae.status == http.StatusTooEarly:
		log.Debug(ae.Error())
	default:
		log.Warn(ae.Error())
	}
	http.Error(w, ae.msg, ae.status)
}

// actionSpec declares one action's policy: when in the game's life it may
// run, who may call it, and which states allow it. actionHandler enforces
// the policy so the handlers hold only game logic.
type actionSpec struct {
	// pregame actions run before the game starts; all others require a
	// game in progress.
	pregame  bool
	hostOnly bool
	turnOnly bool
	// states lists the in-progress states where the action is legal (nil
	// allows all); denyMsg is the 409 answer when the game is elsewhere.
	states  []int32
	denyMsg string
	// pendingModifier marks the modifier choices, which answer 423 (retry
	// shortly) while a challenge or prompt interrupts the pending state.
	pendingModifier bool
	fn              func(*actionRequest) error
}

var actionSpecs = map[string]actionSpec{
	"start": {pregame: true, hostOnly: true, fn: actionStart},
	"spin": {turnOnly: true,
		states:  []int32{stateTurn},
		denyMsg: "cannot spin in current state",
		fn:      actionSpin},
	"acknowledge": {turnOnly: true,
		states:  []int32{stateTurn},
		denyMsg: "cannot acknowledge in current state",
		fn:      actionAcknowledge},
	"advance": {hostOnly: true,
		states:  []int32{stateTurn, statePromptShred, stateAccusationTransfer},
		denyMsg: "cannot advance in current state",
		fn:      actionAdvance},
	"pause": {hostOnly: true,
		states:  []int32{stateTurn},
		denyMsg: "cannot pause in current state",
		fn:      actionPause},
	"resume": {hostOnly: true,
		states:  []int32{stateReady},
		denyMsg: "cannot resume in current state",
		fn:      actionResume},
	"endgame": {hostOnly: true,
		states:  []int32{stateEnding},
		denyMsg: "cannot end game in current state",
		fn:      actionEndgame},
	"continue": {hostOnly: true,
		states:  []int32{stateEnding},
		denyMsg: "cannot continue in current state",
		fn:      actionContinue},
	"flip":     {turnOnly: true, pendingModifier: true, fn: actionFlip},
	"shred":    {turnOnly: true, pendingModifier: true, fn: actionShred},
	"clone":    {turnOnly: true, pendingModifier: true, fn: actionClone},
	"transfer": {turnOnly: true, pendingModifier: true, fn: actionTransfer},
	"accuse": {
		states:  []int32{stateTurn, statePending, stateChallenge},
		denyMsg: "cannot accuse in current state",
		fn:      actionAccuse},
	"decide": {hostOnly: true,
		states:  []int32{stateChallenge},
		denyMsg: "no active challenge",
		fn:      actionDecide},
	"succeed": {hostOnly: true,
		states:  []int32{statePrompt},
		denyMsg: "no active prompt",
		fn:      actionPromptRuling},
	"fail": {hostOnly: true,
		states:  []int32{statePrompt},
		denyMsg: "no active prompt",
		fn:      actionPromptRuling},
	"prompt-shred": {turnOnly: true,
		states:  []int32{statePromptShred},
		denyMsg: "no prompt shred pending",
		fn:      actionPromptShred},
	"accusation-transfer": {
		states:  []int32{stateAccusationTransfer},
		denyMsg: "no transfer pending",
		fn:      actionAccusationTransfer},
	"end": {hostOnly: true, fn: actionEnd},
}

// checkActionPolicy runs the declared guards: caller role first, then
// state legality.
func checkActionPolicy(a *actionRequest, spec actionSpec, cookieKey string) error {
	if spec.hostOnly && !a.state.isHost(cookieKey) {
		return failf(http.StatusForbidden, "only the host can %s", a.action)
	}
	if spec.turnOnly && !a.state.isPlayerTurn(cookieKey) {
		return failf(http.StatusForbidden, "not your turn")
	}
	if spec.states != nil &&
		!slices.Contains(spec.states, a.state.Game.StateID) {
		return failf(http.StatusConflict, "%s", spec.denyMsg)
	}
	if spec.pendingModifier {
		switch a.state.Game.StateID {
		case statePending:
		case stateChallenge, statePrompt:
			// a challenge interrupting a pending modifier is only
			// temporary: the decide handler restores pending once the
			// challenge clears, so answer 423 (Locked) to signal "retry
			// shortly" rather than a dead end. the client retries on
			// every table poll, so this logs at debug to avoid spam.
			return failf(http.StatusLocked, "interruption in progress")
		default:
			// unexpected: a modifier action with no modifier owed.
			return failf(http.StatusConflict, "no pending modifier")
		}
	}
	return nil
}

func actionHandler(w http.ResponseWriter, r *http.Request) {
	pathLong := strings.TrimPrefix(r.URL.Path, "/")
	parts := strings.Split(pathLong, "/")
	if len(parts) != 3 {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	gameID := parts[0]
	action := parts[2]
	span := trace.SpanFromContext(r.Context())
	span.SetAttributes(
		attrGameID.String(gameID),
		attrAction.String(action),
	)
	log := log.With(
		"handler", "actionHandler",
		"game_id", gameID,
		"action", action,
	)
	log.Debug("actionHandler called")
	cookieID, cookieKey, err := cookie(r)
	if err != nil {
		setCookieErr(w, err)
		return
	}
	span.SetAttributes(attrPlayerID.String(cookieID))
	log = log.With("cookie_id", cookieID)
	state, err := stateFromCacheOrDB(r.Context(), &cache, gameID)
	if err != nil {
		if err == ErrStateNoGame {
			log.Warn(ErrStateNoGame.Error(), "game_id", gameID)
			http.Error(w, "game not found", http.StatusNotFound)
			return
		}
		log.Error("unexpected error getting state", "error", err, "game_id", gameID)
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	if !state.isPlayerInGame(cookieKey) {
		log.Warn("prohibiting unauthorized player access")
		http.Error(w, "player not in game", http.StatusForbidden)
		return
	}
	err = state.callerInfo(cookieKey)
	if err != nil {
		log.Error("populate caller info", "error", err)
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	span.SetAttributes(
		attrStateID.Int(int(state.Game.StateID)),
		attrCallerName.String(state.CallerName),
	)
	if state.Game.StateID == stateOver {
		log.Warn("request to ended game", "game_id", gameID)
		http.Error(w, "game over", http.StatusGone)
		return
	}
	spec, known := actionSpecs[action]
	pregame := state.Game.StateID == stateInviting ||
		state.Game.StateID == stateCreated
	if pregame && (!known || !spec.pregame) {
		log.Warn(ErrActionInvalid.Error())
		http.Error(w, ErrActionInvalid.Error(), http.StatusTooEarly)
		return
	}
	if !pregame && (!known || spec.pregame) {
		log.Warn("unsupported action requested")
		http.Error(w, "unsupported action", http.StatusNotImplemented)
		return
	}
	a := &actionRequest{
		w:      w,
		r:      r,
		log:    log,
		gameID: gameID,
		action: action,
		state:  &state,
	}
	if err := checkActionPolicy(a, spec, cookieKey); err != nil {
		writeActionError(w, log, err)
		return
	}
	if err := spec.fn(a); err != nil {
		writeActionError(w, log, err)
	}
}

// setGameState moves the game to stateID, preserving the current
// initiative. q may be transaction-bound.
func setGameState(
	ctx context.Context,
	q *sqlc.Queries,
	s *state,
	gameID string,
	stateID int32,
) error {
	return q.GameUpdate(ctx, sqlc.GameUpdateParams{
		ID:      gameID,
		StateID: stateID,
		InitiativeCurrent: pgtype.Int4{
			Int32: s.Game.InitiativeCurrent.Int32,
			Valid: true,
		},
	})
}

// resumeAfterChallenge moves the game out of a resolved challenge and updates
// the game state: back to challenge if more infractions are queued (so the host
// keeps getting prompted), to pending if this challenge interrupted a modifier
// choice, otherwise to normal turn play. Shared by the decide handler and the
// post-affirm card transfer so they pick the next state the same way.
func resumeAfterChallenge(
	ctx context.Context,
	q *sqlc.Queries,
	s *state,
	gameID string,
) error {
	remaining, err := q.InfractionsActiveCount(ctx, gameID)
	if err != nil {
		return fmt.Errorf("count active infractions: %w", err)
	}
	nextState := int32(stateTurn)
	if remaining > 0 {
		nextState = stateChallenge
	} else if s.hasPendingModifier() {
		nextState = statePending
	}
	if err := setGameState(ctx, q, s, gameID, nextState); err != nil {
		return fmt.Errorf("transition state after challenge: %w", err)
	}
	return nil
}
