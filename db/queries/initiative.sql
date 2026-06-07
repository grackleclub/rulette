-- TODO: unused?
-- name: InitiativeSet :exec
UPDATE game_players
SET initiative = $1
WHERE game_id = $2
    AND player_id = $3
;

-- name: InitiativeAdvance :exec
WITH initiative_max AS (
  SELECT MAX(game_players.initiative) AS highest
  FROM game_players
  WHERE game_players.game_id = $1
)
UPDATE games
SET initiative_current = (
  games.initiative_current % initiative_max.highest
) + 1
FROM initiative_max
WHERE games.id = $1;

-- name: InitiativeCurrentPlayer :one
-- The player whose turn it is now (initiative matches the game's current).
SELECT game_players.player_id
FROM game_players
JOIN games ON games.id = game_players.game_id
WHERE game_players.game_id = $1
    AND game_players.initiative = games.initiative_current;
