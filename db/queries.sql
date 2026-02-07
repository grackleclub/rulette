------------------------
-------- PLAYER --------
------------------------

-- name: PlayerCreate :one
INSERT INTO players (name) VALUES ($1) RETURNING id;

-- name: Player :one
SELECT * FROM players WHERE id = $1;

-- name: PlayerDelete :exec
DELETE FROM players WHERE id = $1;

------------------------
--------- CARD ---------
------------------------

-- name: CardCreate :exec
INSERT INTO cards (type, front, back, creator)
VALUES ($1, $2, $3, $4)
RETURNING id;

-- name: Card :one
SELECT * FROM cards WHERE id = $1;
-- name: CardDelete :exec
DELETE FROM cards WHERE id = $1;

------------------------
--------- GAME ---------
------------------------

-- name: GameCreate :exec
INSERT INTO games (name, id, owner_id)
VALUES ($1, $2, $3)
RETURNING id;

-- name: GameUpdate :exec
UPDATE games
SET state_id = $1, initiative_current = $2
WHERE id = $3;

-- name: Games :many
SELECT * FROM games WHERE id = (
	SELECT game_id 
	FROM game_players
	WHERE player_id = $1
);

-- name: GameDelete :exec
DELETE FROM games WHERE id = $1;

-- name: GameState :one
SELECT
    id,
    name,
    owner_id,
    state_id,
    initiative_current,
    (
        SELECT name
        FROM game_states
        WHERE game_states.id = state_id
    ) AS state_name,
    (
        SELECT COUNT(player_id)
        FROM game_players
        WHERE game_players.game_id = games.id
    ) AS player_count
FROM games WHERE games.id = $1;

------------------------
------ GAME CARDS ------
------------------------

-- name: GameCardsInitGeneric :exec
INSERT INTO game_cards (
    game_id, 
    card_id, 
    slot, 
    stack, 
    player_id
) SELECT
    $1::text,
    id, 
    (ROW_NUMBER() OVER ()) % (SELECT wheel_slots FROM games WHERE games.id = $1),
    NULL, -- unshuffled
    NULL -- unrevealed
FROM cards 
WHERE generic IS TRUE LIMIT (SELECT card_count FROM games WHERE games.id = $1);

-- shuffles stacks within a slot, leaving slot assignments unchanged
-- name: GameCardsShuffle :exec
WITH ordered AS (
 SELECT
        card_id,
        slot,
        ROW_NUMBER() OVER (PARTITION BY slot ORDER BY RANDOM()) AS stack 
    FROM game_cards
    WHERE game_id = $1
)
UPDATE game_cards
SET stack = ordered.stack
FROM ordered
WHERE game_cards.game_id = $1
    AND game_cards.card_id = ordered.card_id
    AND game_cards.slot = ordered.slot;

-- name: GameCardsPlayerView :many
SELECT
    id,
    (
        SELECT
            CASE
                WHEN flipped THEN back
                ELSE front
            END
        FROM cards 
        WHERE cards.id = game_cards.card_id
    ) AS content,
    (
        SELECT type FROM cards 
        WHERE cards.id = game_cards.card_id
    ) AS type,
    (
        SELECT generic FROM cards 
        WHERE cards.id = game_cards.card_id
    ) AS generic,
    flipped
FROM game_cards
WHERE game_id = $1
    AND shredded IS FALSE
    AND player_id IS NOT NULL;

-- name: GameCardsWheelView :many
SELECT
    slot,
    (
        SELECT COUNT(id) 
        FROM game_cards 
        WHERE game_cards.game_id = $1 AND slot = game_cards.slot AND shredded IS FALSE
    ) AS stack_size,
    (
        SELECT type
        FROM cards 
        WHERE cards.id = (
            SELECT card_id 
            FROM game_cards 
            WHERE game_cards.game_id = $1 
                AND game_cards.slot = game_cards.slot 
                AND game_cards.shredded IS FALSE
            ORDER BY stack DESC LIMIT 1
        )
    ) AS top_card_type
FROM game_cards
WHERE game_id = $1 AND player_id IS NULL
ORDER BY slot ASC;

-- sql.ErrNoRows = end of game
-- name: GameCardsWheelSpin :one
WITH spin AS (
    SELECT floor(random() * wheel_slots)::int + 1 AS random_slot
    FROM games
    WHERE games.id = $1
),
resultant_card AS (
    SELECT (
        SELECT game_cards.id
        FROM game_cards
        WHERE game_id = $1 
            AND slot = (SELECT random_slot FROM spin)
        ORDER BY stack DESC
        LIMIT 1
    ) AS id
)
UPDATE game_cards
SET player_id = $2, slot = NULL, stack = NULL, updated = CURRENT_TIMESTAMP
WHERE game_cards.id = (SELECT id FROM resultant_card)
RETURNING id;

-- GameCardCreate :exec
-- TODO: after MVP, implement card creation phase

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
    );

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
    );

-- GameCardClone
-- TODO: how to impelement?

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

------------------------
----- GAME PLAYER ------
------------------------

-- session_key is expected to be valid for the duration of the game
-- name: GamePlayerCreate :exec
INSERT INTO game_players (game_id, player_id, session_key, initiative)
VALUES ($1, $2, $3, $4);

-- name: GamePlayerDelete :exec
DELETE FROM game_players 
WHERE game_id = $1 
	AND player_id = $2;

-- name: GamePlayerPoints :many
-- TODO: is id=player_id correct?
SELECT 
    player_id,
    (SELECT name FROM players WHERE players.id=game_players.player_id) AS name, 
    points,
    session_key,
    initiative
FROM game_players 
WHERE game_id = $1
ORDER BY initiative ASC;

-- -- NOTE: this may not be needed
-- -- name: GameCards :many
-- SELECT
--     game_cards.id, 
--     cards.type, 
--     cards.front, 
--     cards.back,
--     game_cards.stack, 
--     game_cards.slot, 
--     game_cards.player_id, 
--     game_cards.flipped, 
--     game_cards.shredded, 
--     game_cards.from_clone
-- FROM game_cards
-- JOIN cards ON cards.id = game_cards.card_id
-- WHERE game_cards.game_id = $1;

------------------------
------ INITATIVE -------
------------------------

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

------------------------
----- INFRACTIONS ------
------------------------

-- InfractionAccuse
-- InfractionConvict
-- InfractionAbsolve
-- InfractionDelete
-- InfractionPlayer

