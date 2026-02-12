-- name: InitiativeSet :exec
UPDATE game_players
SET initiative = $1
WHERE game_id = $2
    AND player_id = $3
;

-- name: InitiativeAdvance :exec
UPDATE games
SET initiative_current = (
    CASE 
        WHEN initiative_current = (
            SELECT MAX(game_players.initiative)
            FROM game_players
            WHERE game_players.game_id = $1
        ) THEN 1
        ELSE initiative_current + 1
    END
)
WHERE games.id = $1;
