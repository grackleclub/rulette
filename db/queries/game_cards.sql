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
    ((ROW_NUMBER() OVER ()) % (SELECT wheel_slots FROM games WHERE games.id = $1)) + 1,
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
    player_id,
    from_clone,
    flipped,
    shredded,
    updated,
    slot,
    stack,
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
    (
        SELECT modifier_effect FROM cards
        WHERE cards.id = game_cards.card_id
    ) AS modifier_effect
FROM game_cards
WHERE game_id = $1
    AND shredded IS FALSE
    AND player_id IS NOT NULL;

-- Public view of the unrevealed wheel.
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
),
spin_log AS (
    INSERT INTO spin_log (game_id, player_id, slot, card_id)
    VALUES (
        $1,
        $2,
        (SELECT random_slot FROM spin),
        (SELECT card_id FROM game_cards WHERE id = (
                SELECT id FROM resultant_card)
        )
    )
)
UPDATE game_cards
SET player_id = $2, slot = NULL, stack = NULL, updated = CURRENT_TIMESTAMP
WHERE game_cards.id = (SELECT id FROM resultant_card)
RETURNING id;

-- GameCardCreate :exec
-- TODO: after MVP, implement card creation phase

-- name: GameCardMove :exec
UPDATE game_cards
SET player_id = $2
WHERE id = $1;

-- name: GameCardFlip :exec
UPDATE game_cards
SET flipped = NOT flipped
WHERE id = $1;

-- name: GameCardClone :exec
INSERT INTO game_cards (game_id, card_id, player_id, from_clone)
SELECT game_id, card_id, $2, TRUE
FROM game_cards
WHERE game_cards.id = $1
    AND player_id IS NOT NULL
    AND shredded = FALSE;

-- name: GameCardShred :exec
UPDATE game_cards
SET shredded = TRUE
WHERE id = $1;
