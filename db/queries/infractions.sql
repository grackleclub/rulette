-- name: InfractionCreate :one
INSERT INTO infractions (game_id, game_card_id, accused, accuser)
VALUES ($1, $2, $3, $4)
RETURNING id;

-- name: InfractionDecide :one
UPDATE infractions
SET active = FALSE,
    affirmed = $2,
    points = $3
WHERE id = $1
    AND active = TRUE
RETURNING id;

-- name: InfractionGet :one
SELECT * FROM infractions
WHERE id = $1;

-- name: InfractionUpdatePoints :exec
UPDATE game_players
SET points = points - $1
WHERE game_id = $2
    AND player_id = $3;
