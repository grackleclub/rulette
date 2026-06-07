-- name: SpinPendingModifier :one
-- Returns the most recent spin for a game, with modifier info.
-- A non-NULL modifier_effect means the spin landed on a modifier.
SELECT
    spins.id,
    spins.player_id,
    spins.card_id,
    cards.modifier_effect
FROM spins
JOIN cards ON cards.id = spins.card_id
WHERE spins.game_id = $1
ORDER BY spins.ts DESC
LIMIT 1;
