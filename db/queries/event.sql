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
-- Events for a game newer than a given id, oldest first, with the names and
-- details the feed text and sounds need. Clients track the highest id seen.
SELECT
    e.id,
    e.event_type,
    actor.name AS actor_name,
    target.name AS target_name,
    pc.delta AS points_delta,
    inf.affirmed AS infraction_affirmed
FROM event_log e
LEFT JOIN players actor ON actor.id = e.actor_id
LEFT JOIN players target ON target.id = e.target_id
LEFT JOIN point_changes pc ON pc.id = e.point_change_id
LEFT JOIN infractions inf ON inf.id = e.infraction_id
WHERE e.game_id = $1
    AND e.id > $2
ORDER BY e.id;
