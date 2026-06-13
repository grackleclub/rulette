-- name: SuggestionCreate :one
INSERT INTO suggestions (front, back)
VALUES ($1, $2)
RETURNING id;

-- name: SuggestionGet :one
SELECT * FROM suggestions
WHERE id = $1;

-- name: SuggestionsNew :many
SELECT * FROM suggestions
WHERE status = 'new'
ORDER BY created;

-- name: SuggestionSetStatus :exec
UPDATE suggestions
SET status = $2,
    issue_url = $3,
    notes = $4
WHERE id = $1;

-- name: SuggestionDelete :exec
DELETE FROM suggestions
WHERE id = $1;
