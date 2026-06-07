-- name: EventCreate :one
-- Appends one event to the log. Detail FKs (spin_id, infraction_id,
-- game_card_id, point_change_id) are set per event_type; the rest are NULL.
INSERT INTO event_log (
    game_id, event_type, actor_id, target_id,
    spin_id, infraction_id, game_card_id, point_change_id
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING id;

-- name: EventListSince :many
-- Events for a game newer than a given id, oldest first. Clients track the
-- highest id they have seen and poll for the rest.
SELECT id, game_id, event_type, actor_id, target_id,
    spin_id, infraction_id, game_card_id, point_change_id, ts
FROM event_log
WHERE game_id = $1
    AND id > $2
ORDER BY id;
