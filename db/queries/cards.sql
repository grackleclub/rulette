-- name: CardCreate :exec
INSERT INTO cards (type, front, back, creator)
VALUES ($1, $2, $3, $4)
RETURNING id;

-- name: Card :one
SELECT * FROM cards WHERE id = $1;

-- name: CardDelete :exec
DELETE FROM cards WHERE id = $1;

