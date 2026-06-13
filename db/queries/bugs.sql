-- name: BugCreate :one
INSERT INTO bugs (game_url, os, browser, version, description)
VALUES ($1, $2, $3, $4, $5)
RETURNING id;

-- name: BugGet :one
SELECT * FROM bugs
WHERE id = $1;

-- name: BugsNew :many
SELECT * FROM bugs
WHERE status = 'new'
ORDER BY created;

-- name: BugSetStatus :exec
UPDATE bugs
SET status = $2,
    issue_url = $3,
    notes = $4
WHERE id = $1;

-- name: BugDelete :exec
DELETE FROM bugs
WHERE id = $1;
