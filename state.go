package main

import (
	"net/http"
	"strings"
	"time"

	sqlc "github.com/grackleclub/rulette/db/sqlc"
)

// state is a struct to populate the global game cache.
type state struct {
	Updated      time.Time
	Game         sqlc.GameStateRow
	Players      []sqlc.GamePlayerPointsRow
	CardsWheel   []sqlc.GameCardsWheelViewRow  // hidden cards on the wheel
	CardsPlayers []sqlc.GameCardsPlayerViewRow // revealed cards held by players
	Config       map[string]string             // generic baggage (e.g. frontend refresh rate)
	Infractions  []sqlc.Infractions             // infraction history
}

// isPlayerInGame returns true when cookieKey exists in game_players.
func (s *state) isPlayerInGame(cookieKey string) bool {
	for _, player := range s.Players {
		if player.SessionKey.String == cookieKey {
			return true
		}
	}
	return false
}

// isHost verifies that the player is host
// by checking that they are initiative 0 for the game.
func (s *state) isHost(cookieKey string) bool {
	for _, player := range s.Players {
		if player.SessionKey.String == cookieKey &&
			player.Initiative.Int32 == int32(0) {
			return true
		}
	}
	return false
}

func (s *state) isPlayerTurn(cookieKey string) bool {
	var inGame bool
	for _, player := range s.Players {
		if player.SessionKey.String == cookieKey {
			inGame = true
			if player.Initiative.Int32 == s.Game.InitiativeCurrent.Int32 {
				return true
			}
		}
	}
	log.Info("player not current turn", "in_game", inGame)
	return false
}

// cookie inspects the request for cookie and returns
// the player ID and session key, or any error.
//
// Cookie format is: {player_id}:{session_key}
func cookie(r *http.Request) (string, string, error) {
	var cookieID string
	var cookieKey string
	cookie, err := r.Cookie("session")
	if err != nil {
		return "", "", ErrCookieMissing
	}
	parts := strings.Split(cookie.Value, ":")
	if len(parts) != 2 {
		log.Info("invalid session cookie format",
			"cookie_value", cookie.Value,
		)
		return "", "", ErrCookieInvalid
	}
	cookieID = parts[0]
	cookieKey = parts[1]
	return cookieID, cookieKey, nil
}

// isGameActive returns true when when game is active
// and players can accuse each other.
func (s *state) isGameActive() bool {
	switch s.Game.StateID {
	case 3, 4, 5:
		return true
	default:
		return false
	}
}
