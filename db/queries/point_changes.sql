-- name: PointChangeCreate :one
-- Records a points delta. infraction_id is the cause when the change came from
-- an affirmed accusation; NULL for a direct host adjustment.
INSERT INTO point_changes (game_id, player_id, delta, infraction_id)
VALUES ($1, $2, $3, $4)
RETURNING id;
