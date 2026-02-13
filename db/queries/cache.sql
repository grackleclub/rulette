-- name: CacheSet :exec
INSERT INTO game_cache (game_id, value, expires)
VALUES ($1, $2, CURRENT_TIMESTAMP + INTERVAL '1 second') -- TODO: make var?
ON CONFLICT (game_id) DO UPDATE SET 
    value = EXCLUDED.value, 
    expires = EXCLUDED.expires;

-- name: CacheGet :one
SELECT value FROM game_cache
WHERE game_id = $1 AND expires > CURRENT_TIMESTAMP;

-- name: CacheClean :exec
DELETE FROM game_cache
WHERE expires <= CURRENT_TIMESTAMP;

