-- name: PlayerCreate :exec
INSERT INTO players (name) VALUES (:name) RETURNING id;

-- name: Player :one
SELECT * FROM players WHERE id = :id;

-- name: PlayerDelete :exec
DELETE FROM players WHERE id = :id;

-- name: CardCreate :exec
INSERT INTO cards (type, front, back, creator)
VALUES (:type, :front, :back, :creator)
RETURNING id;

-- name: Card :one
SELECT * FROM cards WHERE id = :id;
-- name: CardDelete :exec
DELETE FROM cards WHERE id = :id;

-- name: GameCreate :exec
INSERT INTO games (name, id, owner_id)
VALUES (:name, :id, :owner_id) 
RETURNING id;
-- name: Games :many
SELECT * FROM games WHERE code = (
	SELECT game_id 
	FROM game_players
	WHERE player_id = :player_id
);
-- name: GameDelete :exec
DELETE FROM games WHERE id = :id;

-- GameState  TODO: maybe deliver state in several queries?

-- GameCardCreate

-- -- SpinThatWheel :exec
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
SET player_id = :player_id
WHERE game_id = :game_id
	AND card_id = :card_id
	AND id = (
		SELECT id
		FROM game_cards
		WHERE game_id = :game_id AND card_id = :card_id
		LIMIT 1
	)	
;
-- name: GameCardFlip :exec
UPDATE game_cards
SET flipped = NOT flipped
WHERE game_id = :game_id
	AND card_id = :card_id
	AND id = (
		SELECT id
		FROM game_cards
		WHERE game_id = :game_id AND card_id = :card_id
		LIMIT 1
	)
;

-- GameCardClone

-- name: GameCardShred :exec
UPDATE game_cards
SET shredded = true
WHERE game_id = :game_id
	AND card_id = :card_id
	AND id = (
		SELECT id
		FROM game_cards
		WHERE game_id = :game_id AND card_id = :card_id
		LIMIT 1
	)
;

-- name: GamePlayerCreate :exec
INSERT INTO game_players (game_id, player_id)
VALUES (:game_id, :player_id);

-- name: GamePlayerDelete :exec
DELETE FROM game_players 
WHERE game_id = :game_id 
	AND player_id = :player_id;

-- name: GamePlayerPoints :many
SELECT (
	SELECT name FROM players WHERE id=player_id
	), 
	points,
	turn_active
FROM game_players 
WHERE game_id = :game_id;

-- name: TurnOrderInit :exec
UPDATE game_players
SET turn_active = true
WHERE game_id = :game_id
	AND player_id = (
		SELECT id 
		FROM players 
		ORDER BY joined ASC
		LIMIT 1
	)
;

-- name: TurnOrderAdvance :exec
-- WITH players AS (
-- 	SELECT COUNT(player_id) AS count
-- 	FROM game_players
-- 	WHERE is_host = false 
-- ),
-- TODO: fix this
;

-- TurnOrderDelete
-- TurnOrderNext

-- InfractionAccuse
-- InfractionConvict
-- InfractionAbsolve
-- InfractionDelete
-- InfractionPlayer

