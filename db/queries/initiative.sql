-- TODO: unused?
-- name: InitiativeSet :exec
UPDATE game_players
SET initiative = $1
WHERE game_id = $2
    AND player_id = $3
;

-- name: InitiativeAdvance :exec
-- Move initiative to the next non-host player, skipping empty slots left by
-- players who exited, and wrapping back to the lowest when past the top.
WITH cur AS (
  SELECT initiative_current FROM games WHERE id = $1
)
UPDATE games
SET initiative_current = COALESCE(
  (
    SELECT MIN(game_players.initiative)
    FROM game_players, cur
    WHERE game_players.game_id = $1
      AND game_players.initiative > 0
      AND game_players.initiative > cur.initiative_current
  ),
  (
    SELECT MIN(game_players.initiative)
    FROM game_players
    WHERE game_players.game_id = $1
      AND game_players.initiative > 0
  )
)
WHERE games.id = $1;

-- name: InitiativeCurrentPlayer :one
-- The player whose turn it is now (initiative matches the game's current).
SELECT game_players.player_id
FROM game_players
JOIN games ON games.id = game_players.game_id
WHERE game_players.game_id = $1
    AND game_players.initiative = games.initiative_current;
