package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"path"
	"strconv"
	"strings"

	sqlc "github.com/grackleclub/rulette/db/sqlc"
	semconv "go.opentelemetry.io/otel/semconv/v1.32.0"
	"go.opentelemetry.io/otel/trace"
)

// renderEvents writes the event feed for a game. ?since=<id> returns only newer
// events (clamped to a valid id; bad values fall back to the whole game). The
// status lets a finished game serve the feed with 286 so polling stops but the
// history still loads.
func renderEvents(w http.ResponseWriter, r *http.Request, gameID string, status int) {
	var since int
	if s := r.URL.Query().Get("since"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n >= 0 {
			if n > math.MaxInt32 {
				n = math.MaxInt32
			}
			since = n
		}
	}
	events, err := queries.EventListSince(r.Context(), sqlc.EventListSinceParams{
		GameID: gameID,
		ID:     int32(since),
	})
	if err != nil {
		log.Error("list events", "error", err, "game_id", gameID)
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	if status != http.StatusOK {
		w.WriteHeader(status)
	}
	filepath := path.Join("static", "html", "tmpl.events.html")
	if err := renderTemplate(r.Context(), w, filepath, events); err != nil {
		log.Error("render events", "error", err, "template", filepath)
	}
}

// gameHandler handles the '/{game_id}' endpoint
// This endpoint serves as a lobby pregame, and for primary play.
func gameHandler(w http.ResponseWriter, r *http.Request) {
	gameID := strings.Replace(r.URL.Path, "/", "", 1)
	span := trace.SpanFromContext(r.Context())
	span.SetAttributes(attrGameID.String(gameID))
	log := log.With("handler", "gameHandler", "game_id", gameID)

	if r.Method != http.MethodGet {
		log.Debug("unsupported method", "method", r.Method)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cookieID, cookieKey, err := cookie(r)
	if err != nil {
		switch err {
		case ErrCookieMissing:
			// visitor simply hasn't joined yet: send them to the join page.
			log.Debug("no session for game page, redirecting to join")
		case ErrCookieInvalid:
			// a malformed or tampered cookie is a user mistake or abuse path.
			log.Warn("redirecting visitor to join, invalid session cookie", "error", err)
		default:
			log.Error("unexpected error getting cookie", "error", err)
			redirectAlert(w, r, alertError)
			return
		}
		span.SetAttributes(
			attrAlert.String(alertNoSession),
			semconv.ErrorMessage(err.Error()),
		)
		http.Redirect(w, r, fmt.Sprintf("/%s/join", gameID), http.StatusSeeOther)
		return
	}
	span.SetAttributes(attrPlayerID.String(cookieID))

	state, err := stateFromCacheOrDB(r.Context(), &cache, gameID)
	if err != nil {
		if err == ErrStateNoGame {
			log.Warn("game not found", "game_id", gameID)
			redirectAlert(w, r, alertNotFound)
			return
		}
		log.Error("unexpected error fetching game state", "error", err)
		redirectAlert(w, r, alertError)
		return
	}
	if !state.isPlayerInGame(cookieKey) {
		log.Warn("prohibiting unauthorized player access",
			"cookie_id", cookieID,
		)
		span.SetAttributes(attrAlert.String(alertNotMember))
		http.Redirect(w, r, fmt.Sprintf("/%s/join", gameID), http.StatusSeeOther)
		return
	}
	err = state.callerInfo(cookieKey)
	if err != nil {
		log.Error("populate caller info", "error", err)
		redirectAlert(w, r, alertError)
		return
	}
	span.SetAttributes(
		attrStateID.Int(int(state.Game.StateID)),
		attrCallerName.String(state.CallerName),
	)

	filepath := path.Join("static", "html", "tmpl.game.html")
	if err := renderTemplate(r.Context(), w, filepath, state); err != nil {
		log.Error("render template",
			"error", err,
			"template", filepath,
			"game_id", gameID,
		)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
}

// dataHandler returns game data or html elements.
// (e.g. player cards, table data)
func dataHandler(w http.ResponseWriter, r *http.Request) {
	pathLong := strings.TrimPrefix(r.URL.Path, "/")
	parts := strings.Split(pathLong, "/")
	if len(parts) != 3 {
		http.Error(w, "invalid path", http.StatusBadRequest)
	}
	gameID := parts[0]
	topic := parts[2]
	span := trace.SpanFromContext(r.Context())
	span.SetAttributes(
		attrGameID.String(gameID),
		attrTopic.String(topic),
	)
	log := log.With("handler", "dataHandler", "game_id", gameID, "topic", topic)
	log.Debug("dataHandler called")
	if r.Method != http.MethodGet {
		log.Debug("unsupported method", "method", r.Method)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	cookieID, cookieKey, err := cookie(r)
	if err != nil {
		setCookieErr(w, err)
		return
	}
	span.SetAttributes(attrPlayerID.String(cookieID))
	state, err := stateFromCacheOrDB(r.Context(), &cache, gameID)
	if err != nil {
		if err == ErrStateNoGame {
			log.Warn("game not found", "game_id", gameID)
			http.Error(w, "game not found", http.StatusNotFound)
			return
		}
		log.Error("unexpected error getting state", "error", err, "game_id", gameID)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !state.isPlayerInGame(cookieKey) {
		log.Warn(
			"prohibiting unauthorized player access",
			"cookie_id", cookieID,
		)
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

	switch state.Game.StateID {
	case stateOver: // game over
		// htmx stops a polling trigger when it sees status 286, so the
		// polled sections settle instead of erroring forever. The table
		// shows the final screen; the others just clear and stop.
		const stopPolling = 286
		switch topic {
		case "players":
			w.WriteHeader(stopPolling)
			filepath := path.Join("static", "html", "tmpl.gameover.html")
			if err := renderTemplate(r.Context(), w, filepath, state); err != nil {
				log.Error("render gameover", "error", err, "template", filepath)
			}
		case "events":
			// still serve the log so the final events and the history
			// modal work, but with 286 so the feed stops polling
			renderEvents(w, r, gameID, stopPolling)
		case "status", "table", "infraction":
			w.WriteHeader(stopPolling)
		default:
			http.Error(w, "game over", http.StatusGone)
		}
		return
	case stateEnding, stateChallenge, statePending, stateTurn, stateReady, stateInviting, stateCreated: // in progress (6 = deck spent, host to end)
		switch topic {
		case "players":
			filepath := path.Join("static", "html", "tmpl.players.html")
			if err := renderTemplate(r.Context(), w, filepath, state); err != nil {
				log.Error("render template", "error", err, "template", filepath)
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
			return
		case "table":
			filepath := path.Join("static", "html", "tmpl.table.html")
			if err := renderTemplate(r.Context(), w, filepath, state); err != nil {
				log.Error("render template", "error", err, "template", filepath)
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
			return
		case "status":
			filepath := path.Join("static", "html", "tmpl.status.html")
			if err := renderTemplate(r.Context(), w, filepath, state); err != nil {
				log.Error("render template", "error", err, "template", filepath)
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
			return
		case "events":
			// the feed and the sound engine both read this
			renderEvents(w, r, gameID, http.StatusOK)
			return
		case "state": // NOTE: debug endpoint
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			err := json.NewEncoder(w).Encode(state)
			if err != nil {
				log.Error("encode state response", "error", err)
				http.Error(w, "server error", http.StatusInternalServerError)
				return
			}
		case "points":
			filepath := path.Join("static", "html", "tmpl.points.html")
			if err := renderTemplate(r.Context(), w, filepath, state); err != nil {
				log.Error("render template", "error", err, "template", filepath)
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
			return
		case "modifier":
			filepath := path.Join("static", "html", "tmpl.modifier.html")
			if err := renderTemplate(r.Context(), w, filepath, state); err != nil {
				log.Error("render template", "error", err, "template", filepath)
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
			return
		case "accuse":
			filepath := path.Join("static", "html", "tmpl.accuse_dialog.html")
			if err := renderTemplate(r.Context(), w, filepath, state); err != nil {
				log.Error("render template", "error", err, "template", filepath)
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
			return
		case "infraction":
			if state.Game.StateID != stateChallenge {
				log.Debug("infraction poll outside challenge state", "state_id", state.Game.StateID)
				w.WriteHeader(http.StatusNoContent)
				return
			}
			if !state.isHost(cookieKey) {
				log.Debug("infraction poll by non-host")
				w.WriteHeader(http.StatusNoContent)
				return
			}
			for _, inf := range state.Infractions {
				if inf.Active.Bool {
					log.Debug("serving infraction to host", "infraction_id", inf.ID)
					accusedName := ""
					for _, p := range state.Players {
						if p.PlayerID == inf.Accused {
							accusedName = p.Name
							break
						}
					}
					ruleContent := ""
					for _, c := range state.CardsPlayers {
						if c.ID == inf.GameCardID {
							if s, ok := c.Content.(string); ok {
								ruleContent = s
							}
							break
						}
					}
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(map[string]any{
						"id":      inf.ID,
						"accused": accusedName,
						"rule":    ruleContent,
					})
					return
				}
			}
			w.WriteHeader(http.StatusNoContent)
			return
		default:
			log.Warn(ErrTopicInvalid.Error())
			http.Error(w, ErrTopicInvalid.Error(), http.StatusBadRequest)
			return
		}

	}
}
