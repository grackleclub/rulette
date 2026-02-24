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
(1, 'inviting', 'at least one player has joined'), --  TODO: useless?
(2, 'ready', 'joining is closed, ready to start (or paused)'),
(3, 'turn', 'player is mid-turn, spinning wheel or responding'),
(4, 'challenge', ''), -- pause for points adjustment |  TODO: useless?
(5, 'end', 'game over');

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
	name TEXT NOT NULL UNIQUE
);
INSERT INTO card_types (name) 
VALUES
	('rule'),
	('modifier'),
	('prompt');

CREATE TABLE IF NOT EXISTS cards (
	id SERIAL PRIMARY KEY,
	type TEXT NOT NULL,
	front TEXT NOT NULL,
	back TEXT,
	creator INTEGER,
	created TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	generic BOOLEAN DEFAULT FALSE,
	FOREIGN KEY (type) REFERENCES card_types(name) ON DELETE CASCADE
);

INSERT INTO cards (type, front, back, creator, created, generic)
VALUES
	('rule', 'wearing a giant hat', 'wearing a tiny hat', 0, CURRENT_TIMESTAMP, TRUE),
	('rule', 'while doing your best Robert Dinero impersonation', 'wearing a tiny hat', 0, CURRENT_TIMESTAMP, TRUE),
  ('rule', 'a', '1', 0, CURRENT_TIMESTAMP, TRUE),
  ('rule', 'b', '2', 0, CURRENT_TIMESTAMP, TRUE),
  ('rule', 'c', '3', 0, CURRENT_TIMESTAMP, TRUE),
  ('rule', 'd', '4', 0, CURRENT_TIMESTAMP, TRUE),
  ('rule', 'e', '5', 0, CURRENT_TIMESTAMP, TRUE),
  ('rule', 'f', '6', 0, CURRENT_TIMESTAMP, TRUE),
  ('rule', 'g', '7', 0, CURRENT_TIMESTAMP, TRUE),
  ('rule', 'h', '8', 0, CURRENT_TIMESTAMP, TRUE),
  ('rule', 'i', '9', 0, CURRENT_TIMESTAMP, TRUE),
  ('rule', 'j', '10', 0, CURRENT_TIMESTAMP, TRUE),
  ('rule', 'k', '11', 0, CURRENT_TIMESTAMP, TRUE);

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
