-- name: PlayerCreate :one
INSERT INTO players (name) VALUES ($1) RETURNING id;

-- name: Player :one
SELECT * FROM players WHERE id = $1;

-- name: PlayerDelete :exec
DELETE FROM players WHERE id = $1;

-- session_key is expected to be valid for the duration of the game
-- name: GamePlayerCreate :exec
INSERT INTO game_players (game_id, player_id, session_key, initiative)
VALUES ($1, $2, $3, $4);

-- name: GamePlayerDelete :exec
DELETE FROM game_players 
WHERE game_id = $1 
	AND player_id = $2;

-- name: GamePlayerPoints :many
-- TODO: is id=player_id correct?
SELECT 
    player_id,
    (SELECT name FROM players WHERE players.id=game_players.player_id) AS name, 
    points,
    session_key,
    initiative
FROM game_players 
WHERE game_id = $1
ORDER BY initiative ASC;
