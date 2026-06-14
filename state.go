package main

import (
	"fmt"
	"net/http"
	"sort"
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
	Infractions  []sqlc.Infractions            // infraction history
	CallerID     int                           // init empty, copies populated by callerInfo()
	CallerName   string                        // init empty, copies populated by callerInfo()
	// AwaitingAck is true when the current-turn player has spun a rule card
	// they must acknowledge before the turn advances.
	AwaitingAck bool
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
	var inGame bool
	for _, player := range s.Players {
		if player.SessionKey.String == cookieKey {
			inGame = true
			if player.Initiative.Int32 == int32(0) {
				return true
			}
		}
	}
	log.Debug("player not host", "in_game", inGame)
	return false
}

// nonHostPlayers returns the count of players who can take turns,
// i.e. everyone except the host (initiative 0).
func (s *state) nonHostPlayers() int {
	var count int
	for _, player := range s.Players {
		if player.Initiative.Int32 != int32(0) {
			count++
		}
	}
	return count
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
	log.Warn("player not current turn", "in_game", inGame)
	return false
}

func (s *state) callerID(cookieKey string) (int, error) {
	for _, player := range s.Players {
		if player.SessionKey.String == cookieKey {
			return int(player.PlayerID), nil
		}
	}
	return 0, fmt.Errorf("player not in game")
}

func (s *state) callerName(cookieKey string) (string, error) {
	for _, player := range s.Players {
		if player.SessionKey.String == cookieKey {
			return player.Name, nil
		}
	}
	return "", fmt.Errorf("player not in game")
}

func (s *state) callerInfo(cookieKey string) error {
	callerID, err := s.callerID(cookieKey)
	if err != nil {
		return fmt.Errorf("determine caller id: %w", err)
	}
	s.CallerID = callerID

	callerName, err := s.callerName(cookieKey)
	if err != nil {
		return fmt.Errorf("determine caller name: %w", err)
	}
	s.CallerName = callerName
	return nil
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
		log.Warn("invalid session cookie format",
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
	case stateTurn, statePending, stateChallenge:
		return true
	default:
		return false
	}
}

// hasPendingModifier reports whether the player whose turn it is still holds
// an unresolved modifier card. Resolving a modifier shreds the card (and the
// card view excludes shredded cards), so a modifier still in hand means the
// choice is owed. Used to restore the pending state after a challenge
// interrupts it.
func (s *state) hasPendingModifier() bool {
	for _, player := range s.Players {
		if player.Initiative.Int32 != s.Game.InitiativeCurrent.Int32 {
			continue
		}
		for _, card := range s.CardsPlayers {
			if card.PlayerID.Int32 == player.PlayerID &&
				card.Type == "modifier" {
				return true
			}
		}
	}
	return false
}

// Standings returns the non-host players ranked by points, highest first.
// Value receiver so templates can call it on the by-value state data.
func (s state) Standings() []sqlc.GamePlayerPointsRow {
	ranked := make([]sqlc.GamePlayerPointsRow, 0, len(s.Players))
	for _, p := range s.Players {
		if p.Initiative.Int32 == 0 {
			continue // skip the host
		}
		ranked = append(ranked, p)
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		return ranked[i].Points.Int32 > ranked[j].Points.Int32
	})
	return ranked
}

// Winners returns the player(s) with the most points, including ties.
func (s state) Winners() []sqlc.GamePlayerPointsRow {
	ranked := s.Standings()
	if len(ranked) == 0 {
		return nil
	}
	most := ranked[0].Points.Int32
	var winners []sqlc.GamePlayerPointsRow
	for _, p := range ranked {
		if p.Points.Int32 == most {
			winners = append(winners, p)
		}
	}
	return winners
}
