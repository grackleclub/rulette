-- name: InfractionCreate :one
INSERT INTO infractions (game_id, game_card_id, accused, accuser)
VALUES ($1, $2, $3, $4)
RETURNING id;

-- name: InfractionDecide :one
UPDATE infractions
SET active = FALSE,
    affirmed = $2
WHERE id = $1
    AND active = TRUE
RETURNING id;

-- name: InfractionGet :one
SELECT * FROM infractions
WHERE id = $1;

-- name: InfractionsByGame :many
SELECT id, game_id, game_card_id, accused, accuser, created, active, affirmed
FROM infractions
WHERE game_id = $1
ORDER BY created DESC;

-- name: InfractionsActiveCount :one
SELECT COUNT(*) FROM infractions
WHERE game_id = $1
    AND active = TRUE;
