CREATE TABLE IF NOT EXISTS players (
	id SERIAL PRIMARY KEY,
	name TEXT NOT NULL,
	created TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS game_states (
	id INTEGER PRIMARY KEY,
	name TEXT NOT NULL,
	description TEXT
);

INSERT INTO game_states (id, name, description)
VALUES
(0, 'created', 'game created, but no members have joined'),
(1, 'inviting', 'at least one player has joined'),
(2, 'ready', 'joining is closed, ready to start (or paused)'),
(3, 'turn', 'player is mid-turn, spinning wheel or responding'),
(4, 'pending', 'rule modifier choice is pending'),
(5, 'challenge', 'a points challenge is pending'),
(6, 'end', 'game over');

CREATE TABLE IF NOT EXISTS games (
	id VARCHAR(6) PRIMARY KEY,
	name TEXT NOT NULL,
	created TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	owner_id INTEGER,
	state_id INTEGER NOT NULL DEFAULT 0,
	wheel_slots INTEGER NOT NULL DEFAULT 2, -- number of wheel possibilities -- TODO: fill out
	card_count INTEGER NOT NULL DEFAULT 10, -- number of cards in the wheel deck -- TODO: fill out
	initiative_timer INTEGER NOT NULL DEFAULT 30, -- seconds per turn before auto-advance
	initiative_current INTEGER DEFAULT 0, -- TODO: is this used?
	FOREIGN KEY (owner_id) REFERENCES players(id) ON DELETE CASCADE,
	FOREIGN KEY (state_id) REFERENCES game_states(id)
);

CREATE TABLE IF NOT EXISTS game_players (
	game_id VARCHAR(6) NOT NULL,
	player_id INTEGER NOT NULL,
	points INTEGER DEFAULT 20,
	session_key TEXT,
	-- is_host BOOLEAN DEFAULT FALSE, -- NOTE: host has initiative zero
	joined TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	initiative INTEGER,
	PRIMARY KEY (game_id, player_id),
	FOREIGN KEY (game_id) REFERENCES games(id) ON DELETE CASCADE,
	FOREIGN KEY (player_id) REFERENCES players(id) ON DELETE CASCADE
);

-- TODO: should is_host be a card?
CREATE TABLE IF NOT EXISTS card_types (
	name TEXT NOT NULL UNIQUE,
	description TEXT
);
INSERT INTO card_types (name, description)
VALUES
	('rule', 'persistent rule that applies to a single player'),
	('modifier', 'one-time effect applied to a chosen card'),
	('prompt', 'single challenge to be immediately completed');

CREATE TABLE IF NOT EXISTS modifier_effects (
	name TEXT PRIMARY KEY,
	description TEXT
);
INSERT INTO modifier_effects (name, description)
VALUES
	('flip', 'flip a card to reveal its back side'),
	('shred', 'permanently remove a card from play'),
	('clone', 'duplicate a card and give the copy to another player'),
	('transfer', 'transfer a card to another player');

CREATE TABLE IF NOT EXISTS cards (
	id SERIAL PRIMARY KEY,
	type TEXT NOT NULL,
	front TEXT NOT NULL,
	back TEXT,
	creator INTEGER,
	created TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	generic BOOLEAN DEFAULT FALSE,
	modifier_effect TEXT,
	FOREIGN KEY (type) REFERENCES card_types(name) ON DELETE CASCADE,
	FOREIGN KEY (modifier_effect) REFERENCES modifier_effects(name) ON DELETE SET NULL,
	CONSTRAINT chk_modifier_effect CHECK (
		(type = 'modifier' AND modifier_effect IS NOT NULL)
		OR (type != 'modifier' AND modifier_effect IS NULL)
	)
);

INSERT INTO cards (type, front, back, creator, created, generic, modifier_effect)
VALUES
	('rule', 'wearing a giant hat', 'wearing a tiny hat', 0, CURRENT_TIMESTAMP, TRUE, NULL),
	('rule', 'while doing your best Robert De Nero impersonation', 'wearing a tiny hat', 0, CURRENT_TIMESTAMP, TRUE, NULL),
	('modifier', 'flip any of your own cards', '', 0, CURRENT_TIMESTAMP, TRUE, 'flip'),
	('modifier', 'shred any of your own cards', '', 0, CURRENT_TIMESTAMP, TRUE, 'shred'),
	('modifier', 'clone any of your own cards, and give to someone else', '', 0, CURRENT_TIMESTAMP, TRUE, 'clone'),
	('modifier', 'transfer any of your own cards to another player', '', 0, CURRENT_TIMESTAMP, TRUE, 'transfer'),
	('rule', 'a', '1', 0, CURRENT_TIMESTAMP, TRUE, NULL),
	('rule', 'b', '2', 0, CURRENT_TIMESTAMP, TRUE, NULL),
	('rule', 'c', '3', 0, CURRENT_TIMESTAMP, TRUE, NULL),
	('rule', 'd', '4', 0, CURRENT_TIMESTAMP, TRUE, NULL),
	('rule', 'e', '5', 0, CURRENT_TIMESTAMP, TRUE, NULL),
	('rule', 'f', '6', 0, CURRENT_TIMESTAMP, TRUE, NULL),
	('rule', 'g', '7', 0, CURRENT_TIMESTAMP, TRUE, NULL),
	('rule', 'h', '8', 0, CURRENT_TIMESTAMP, TRUE, NULL),
	('rule', 'i', '9', 0, CURRENT_TIMESTAMP, TRUE, NULL),
	('rule', 'j', '10', 0, CURRENT_TIMESTAMP, TRUE, NULL),
	('rule', 'k', '11', 0, CURRENT_TIMESTAMP, TRUE, NULL);

-- card_id lacks primary key to allow cloning within a game,
CREATE TABLE IF NOT EXISTS game_cards (
	id SERIAL PRIMARY KEY, -- to distinguish between clones
	game_id VARCHAR(6) NOT NULL,
	card_id INTEGER NOT NULL,
	slot INTEGER, -- 1-indexed number of wheel slots (MAX=game.wheel_size, NULL=revealed)
	stack INTEGER, -- 1-indexed ascending up the stack (NULL=unshuffled)
	player_id INTEGER, -- (NULL=on the wheel)
	flipped BOOLEAN DEFAULT FALSE,
	shredded BOOLEAN DEFAULT FALSE,
	from_clone BOOLEAN DEFAULT FALSE, -- TODO: this can be inferred, why put it here?
	updated TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	FOREIGN KEY (game_id) REFERENCES games(id) ON DELETE CASCADE,
	FOREIGN KEY (card_id) REFERENCES cards(id) ON DELETE CASCADE, -- allows global deletion, admin moderation
	FOREIGN KEY (player_id) REFERENCES players(id) ON DELETE CASCADE -- a leaving player takes their game cards with them
);

-- TODO: make log viewer route?
CREATE TABLE IF NOT EXISTS spin_log (
	id SERIAL PRIMARY KEY,
	game_id VARCHAR(6) NOT NULL,
	player_id INTEGER, -- (NULL=system,deleted)
	slot INTEGER NOT NULL, -- not referentially enforced, oh well
	card_id INTEGER, -- (NULL=miss,?)
	ts TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	FOREIGN KEY (game_id) REFERENCES games(id) ON DELETE CASCADE,
	FOREIGN KEY (player_id) REFERENCES players(id) ON DELETE SET NULL,
	FOREIGN KEY (card_id) REFERENCES cards(id) ON DELETE SET NULL
);

CREATE TABLE IF NOT EXISTS infractions (
	id SERIAL PRIMARY KEY,
	game_id VARCHAR(6) NOT NULL,
	accused INTEGER NOT NULL,
	accuser INTEGER NOT NULL,
	created TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	active BOOLEAN DEFAULT TRUE, -- active until applied or denied
	convicted BOOLEAN DEFAULT FALSE,
	FOREIGN KEY (game_id) REFERENCES games(id) ON DELETE CASCADE,
	FOREIGN KEY (accused) REFERENCES players(id) ON DELETE CASCADE,
	FOREIGN KEY (accuser) REFERENCES players(id) ON DELETE CASCADE
);

CREATE UNLOGGED TABLE IF NOT EXISTS game_cache (
	game_id VARCHAR(6) PRIMARY KEY,
	value JSONB,
	expires TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP + INTERVAL '1 seconds' -- TODO: make var?
);
