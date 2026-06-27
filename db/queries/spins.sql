-- name: SpinPendingModifier :one
-- Returns the most recent spin for a game, with the drawn card's type, face,
-- effect, and the spin time. A non-NULL modifier_effect means the spin landed
-- on a modifier; type 'prompt' means it landed on a prompt challenge (ts gates
-- how soon the host may rule it failed).
-- Only returns a spin that occurred after the most recent "turn" event,
-- so stale spins from before a continue/advance don't look pending.
SELECT
    spins.id,
    spins.player_id,
    spins.card_id,
    spins.ts,
    cards.type,
    cards.front,
    cards.modifier_effect
FROM spins
JOIN cards ON cards.id = spins.card_id
WHERE spins.game_id = $1
  AND spins.ts >= COALESCE(
      (SELECT MAX(ts) FROM event_log
       WHERE game_id = $1 AND event_type = 'turn'),
      '1970-01-01'
  )
ORDER BY spins.ts DESC
LIMIT 1;

-- name: SpinLatestElapsedSeconds :one
-- Whole seconds elapsed on the database clock since the most recent spin in a
-- game. Used to time the active prompt challenge (the latest spin is the
-- prompt spin while the game sits in the prompt state).
SELECT FLOOR(EXTRACT(EPOCH FROM (now() - ts)))::int AS seconds
FROM spins
WHERE game_id = $1
ORDER BY ts DESC
LIMIT 1;
