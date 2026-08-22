package main

import (
	"fmt"
	"net/http"

	sqlc "github.com/grackleclub/rulette/db/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

// actionStart moves a pregame lobby into play: seed and shuffle the deck,
// then hand initiative to the first non-host player. With too few players
// it answers with a notice or a confirm prompt instead of starting.
func actionStart(a *actionRequest) error {
	ctx := a.r.Context()
	// require a minimum of non-host players, otherwise no player holds
	// the starting initiative and the game would soft-lock. surface a
	// notice via HX-Trigger (200, no swap) rather than a raw http error
	if a.state.nonHostPlayers() < minimumPlayers {
		a.log.Warn("host attempted to start game without enough players",
			"non_host_players", a.state.nonHostPlayers(),
			"minimum", minimumPlayers,
		)
		a.w.Header().Set("HX-Trigger", fmt.Sprintf(
			`{"notice":"Need at least %d non-host player to start the game."}`,
			minimumPlayers,
		))
		a.w.WriteHeader(http.StatusOK)
		return nil
	}
	// the game plays best with two or more, but a single non-host
	// player is allowed once the host confirms via the start dialog.
	// without confirmation, ask first instead of starting.
	if a.state.nonHostPlayers() < recommendedPlayers &&
		a.r.URL.Query().Get("confirm") != "1" {
		a.log.Info("prompting host to start with deficit of players",
			"non_host_players", a.state.nonHostPlayers(),
			"recommended", recommendedPlayers,
		)
		a.w.Header().Set("HX-Trigger", `{"confirmStart":""}`)
		a.w.WriteHeader(http.StatusOK)
		return nil
	}
	// populate and shuffle the deck
	if err := queries.GameCardsInitGeneric(ctx, a.gameID); err != nil {
		return serverErr("init deck", err)
	}
	if err := queries.GameCardsShuffle(ctx, a.gameID); err != nil {
		return serverErr("shuffle deck", err)
	}
	a.log.Info("deck initialized and shuffled")

	// set game to in-progress
	if err := queries.GameUpdate(ctx, sqlc.GameUpdateParams{
		ID:                a.gameID,
		StateID:           stateReady, // in progress
		InitiativeCurrent: pgtype.Int4{Int32: 0, Valid: true},
	}); err != nil {
		return serverErr("start game", err)
	}
	a.log.Info("game started")

	// start initiative with first non-host player
	if err := queries.GameUpdate(ctx, sqlc.GameUpdateParams{
		ID:                a.gameID,
		StateID:           stateTurn,
		InitiativeCurrent: pgtype.Int4{Int32: 1, Valid: true},
	}); err != nil {
		return serverErr("update initiative", err)
	}
	a.log.Info("initiative initiated", "state", "ready", "initiative", 1)

	// log the start, and the first player's turn so they hear the ding.
	// state is already committed, so these are best-effort: a failure
	// shouldn't 500 a game that has already started.
	if err := recordEvent(ctx, a.log, queries, sqlc.EventCreateParams{
		GameID:    a.gameID,
		EventType: "start",
	}); err != nil {
		a.log.Error("log start event", "error", err)
	}
	firstPlayer, err := queries.InitiativeCurrentPlayer(ctx, a.gameID)
	if err != nil {
		a.log.Error("find first turn player", "error", err)
	} else if err := recordEvent(ctx, a.log, queries, sqlc.EventCreateParams{
		GameID:    a.gameID,
		EventType: "turn",
		TargetID:  pgInt(firstPlayer),
	}); err != nil {
		a.log.Error("log first turn event", "error", err)
	}
	a.refresh("refreshTable")
	return nil
}

// actionPause suspends play by parking the game in the ready state.
func actionPause(a *actionRequest) error {
	ctx := a.r.Context()
	if err := setGameState(ctx, queries, a.state, a.gameID, stateReady); err != nil {
		return serverErr("pause game", err)
	}
	a.log.Info("game paused")
	if err := recordEvent(ctx, a.log, queries, sqlc.EventCreateParams{
		GameID:    a.gameID,
		EventType: "pause",
	}); err != nil {
		return serverErr("record pause event", err)
	}
	a.refresh("refreshTable")
	return nil
}

// actionResume returns a paused game to turn play.
func actionResume(a *actionRequest) error {
	ctx := a.r.Context()
	if err := setGameState(ctx, queries, a.state, a.gameID, stateTurn); err != nil {
		return serverErr("resume game", err)
	}
	a.log.Info("game resumed")
	if err := recordEvent(ctx, a.log, queries, sqlc.EventCreateParams{
		GameID:    a.gameID,
		EventType: "resume",
	}); err != nil {
		return serverErr("record resume event", err)
	}
	a.refresh("refreshTable")
	return nil
}

// actionEndgame finalizes a game whose deck has run out. Only valid in
// the "ending" state, which a spent deck puts the game into.
func actionEndgame(a *actionRequest) error {
	ctx := a.r.Context()
	if err := queries.GameUpdate(ctx, sqlc.GameUpdateParams{
		ID:      a.gameID,
		StateID: stateOver,
	}); err != nil {
		return serverErr("end game", err)
	}
	a.log.Info("game ended by host")
	if err := recordEvent(ctx, a.log, queries, sqlc.EventCreateParams{
		GameID:    a.gameID,
		EventType: "end",
	}); err != nil {
		return serverErr("record end event", err)
	}
	a.refresh("refreshTable")
	return nil
}

// actionContinue keeps playing past a spent deck: back to turn play and
// pass initiative on.
func actionContinue(a *actionRequest) error {
	ctx := a.r.Context()
	if err := setGameState(ctx, queries, a.state, a.gameID, stateTurn); err != nil {
		return serverErr("continue game", err)
	}
	a.log.Info("game continued by host")
	if err := recordEvent(ctx, a.log, queries, sqlc.EventCreateParams{
		GameID:    a.gameID,
		EventType: "continue",
	}); err != nil {
		return serverErr("record continue event", err)
	}
	if err := advanceTurn(ctx, a.log, queries, a.gameID); err != nil {
		return serverErr("advance initiative after continue", err)
	}
	a.refresh("refreshTable")
	return nil
}

// actionEnd lets the host end the game outright from any in-progress
// state.
func actionEnd(a *actionRequest) error {
	ctx := a.r.Context()
	if err := queries.GameUpdate(ctx, sqlc.GameUpdateParams{
		ID:                a.gameID,
		StateID:           stateOver, // game over
		InitiativeCurrent: pgtype.Int4{Int32: 0, Valid: true},
	}); err != nil {
		return serverErr("end game", err)
	}
	if err := recordEvent(ctx, a.log, queries, sqlc.EventCreateParams{
		GameID:    a.gameID,
		EventType: "end",
	}); err != nil {
		return serverErr("record end event", err)
	}
	cache.Delete(a.gameID)
	a.log.Info("game ended")
	a.w.WriteHeader(http.StatusGone)
	return nil
}
