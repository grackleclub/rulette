-- name: SpinLogPendingModifier :one
-- Returns the most recent spin for a game, with modifier info.
-- A non-NULL modifier_effect means the spin landed on a modifier.
SELECT
    spin_log.player_id,
    spin_log.card_id,
    cards.modifier_effect
FROM spin_log
JOIN cards ON cards.id = spin_log.card_id
WHERE spin_log.game_id = $1
ORDER BY spin_log.ts DESC
LIMIT 1;
