-- TODO: unused?
-- name: InitiativeSet :exec
UPDATE game_players
SET initiative = $1
WHERE game_id = $2
    AND player_id = $3
;

-- name: InitiativeAdvance :exec
-- TODO: % MAX()
WITH initiative_current AS (
    SELECT initiative_current
    FROM games
    WHERE id = $1
),
initiative_max AS (
    SELECT MAX(game_players.initiative) AS highest
    FROM game_players
    WHERE game_players.game_id = $1
)
UPDATE games
SET initiative_current = (
    initiative_current.initiative_current %
    initiative_max.highest
) + 1
WHERE games.id = $1;
