-- name: GameCreate :exec
INSERT INTO games (name, id, owner_id)
VALUES ($1, $2, $3)
RETURNING id;

-- name: GameUpdate :exec
UPDATE games
SET state_id = $1, initiative_current = $2
WHERE id = $3;

-- name: Games :many
SELECT * FROM games WHERE id = (
	SELECT game_id 
	FROM game_players
	WHERE player_id = $1
);

-- name: GameDelete :exec
DELETE FROM games WHERE id = $1;

-- name: GameState :one
SELECT
    id,
    name,
    owner_id,
    state_id,
    initiative_current,
    (
        SELECT name
        FROM game_states
        WHERE game_states.id = state_id
    ) AS state_name,
    (
        SELECT COUNT(player_id)
        FROM game_players
        WHERE game_players.game_id = games.id
    ) AS player_count
FROM games WHERE games.id = $1;
