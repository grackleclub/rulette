-- name: PlayerCreate :exec
INSERT INTO players (name) VALUES (:name) RETURNING id;

-- Player
-- PlayerDelete

-- CardCreate
-- Card
-- CardEdit
-- CardDelete


-- name: GameCreate :exec
INSERT INTO games (name, code, owner)
VALUES (:name, :code, :owner) 
RETURNING code;
-- Game
-- GameEdit
-- GameDelete
-- GameActiveCount
-- GameState  TODO: maybe deliver state in several queries?

-- GameCardCreate
-- SpinThatWheel
-- GameCardMove
-- GameCardReveal
-- GameCardFlip
-- GameCardClone
-- GameCardShred
-- GameCardDelete

-- GamePlayerCreate
-- GamePlayer
-- GamePlayerEdit
-- GamePlayerDelete
-- GamePlayerPoints

-- TurnOrderCreate
-- TurnOrderEdit
-- TurnOrderDelete
-- TurnOrderNext

-- InfractionAccuse
-- InfractionConvict
-- InfractionAbsolve
-- InfractionDelete
-- InfractionPlayer

