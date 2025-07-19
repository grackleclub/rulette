-- name: PlayerCreate :one
INSERT INTO players (name) VALUES ($1) RETURNING id;

-- name: Player :one
SELECT * FROM players WHERE id = $1;

-- name: PlayerDelete :exec
DELETE FROM players WHERE id = $1;


-- name: CardCreate :exec
INSERT INTO cards (type, front, back, creator)
VALUES ($1, $2, $3, $4)
RETURNING id;

-- name: Card :one
SELECT * FROM cards WHERE id = $1;
-- name: CardDelete :exec
DELETE FROM cards WHERE id = $1;


-- name: GameCreate :exec
INSERT INTO games (name, id, owner_id)
VALUES ($1, $2, $3)
RETURNING id;

-- name: Games :many
SELECT * FROM games WHERE id = (
	SELECT game_id 
	FROM game_players
	WHERE player_id = $1
);

-- name: GameDelete :exec
DELETE FROM games WHERE id = $1;

-- name: GameState :one
SELECT * FROM games WHERE id = $1;

-- GameCardCreate

-- WITH slots AS (
-- 	SELECT MAX(slot) 
-- 	FROM game_cards 
-- 	WHERE game_id = :game_id
-- ),
-- spin AS (
-- 	SELECT card_id
-- 	FROM game_cards
-- 	WHERE game_id = :game_id
-- 		AND revealed = false
-- 		AND slot = RANDOM() % (SELECT * FROM slots)
-- 	ORDER BY stack DESC LIMIT 1
-- )
-- UPDATE game_cards
-- SET	
-- 	revealed = true, 
-- 	player_id = :player_id
-- WHERE id = (SELECT card_id FROM spin);

-- Moves a single card of matching id to the new player_id provided.
-- name: GameCardMove :exec
UPDATE game_cards
SET player_id = $1
WHERE game_cards.game_id = $2
    AND game_cards.card_id = $3
    AND game_cards.id = (
        SELECT game_cards.id
        FROM game_cards
        WHERE game_cards.game_id = $2 AND game_cards.card_id = $3
        LIMIT 1
    )
;
-- name: GameCardFlip :exec
UPDATE game_cards
SET flipped = NOT flipped
WHERE game_cards.game_id = $1
    AND game_cards.card_id = $2
    AND game_cards.id = (
        SELECT game_cards.id
        FROM game_cards
        WHERE game_cards.game_id = $1 AND game_cards.card_id = $2
        LIMIT 1
    )
;

-- GameCardClone

-- name: GameCardShred :exec
WITH cte AS (
    SELECT id
    FROM game_cards
    WHERE game_cards.game_id = $1
      AND game_cards.card_id = $2
    LIMIT 1
)
UPDATE game_cards
SET shredded = true
WHERE id IN (SELECT id FROM cte);


-- name: GamePlayerCreate :exec
INSERT INTO game_players (game_id, player_id)
VALUES ($1, $2);

-- name: GamePlayerDelete :exec
DELETE FROM game_players 
WHERE game_id = $1 
	AND player_id = $2;

-- name: GamePlayerPoints :many
-- TODO: is id=player_id correct?
SELECT 
	(SELECT name FROM players WHERE id=player_id) AS name, 
	points,
	initiative
FROM game_players 
WHERE game_id = $1
ORDER BY initiative ASC;

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

-- InfractionAccuse
-- InfractionConvict
-- InfractionAbsolve
-- InfractionDelete
-- InfractionPlayer

