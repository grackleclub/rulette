-- name: SpinPendingModifier :one
-- Returns the most recent spin for a game, with modifier info.
-- A non-NULL modifier_effect means the spin landed on a modifier.
-- Only returns a spin that occurred after the most recent "turn" event,
-- so stale spins from before a continue/advance don't look pending.
SELECT
    spins.id,
    spins.player_id,
    spins.card_id,
    cards.modifier_effect
FROM spins
JOIN cards ON cards.id = spins.card_id
WHERE spins.game_id = $1
  AND spins.ts > COALESCE(
      (SELECT MAX(ts) FROM event_log
       WHERE game_id = $1 AND event_type = 'turn'),
      '1970-01-01'
  )
ORDER BY spins.ts DESC
LIMIT 1;
