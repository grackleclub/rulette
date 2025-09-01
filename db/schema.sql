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

CREATE TABLE IF NOT EXISTS games (
	id VARCHAR(6) PRIMARY KEY,
	name TEXT NOT NULL,
	created TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	owner_id INTEGER,
	state_id INTEGER NOT NULL DEFAULT 0,
	initiative_current INTEGER DEFAULT 0, -- TODO: is this used?
	FOREIGN KEY (owner_id) REFERENCES players(id) ON DELETE CASCADE,
	FOREIGN KEY (state_id) REFERENCES game_states(id)
);

INSERT INTO game_states (id, name, description)
VALUES
	(0, 'created', 'game created, but no members have joined'),
	(1, 'inviting', 'at least one player has joined'), --  TODO: useless?
	(2, 'ready', 'joining is closed, ready to start (or paused)'),
	(3, 'turn', 'player is mid-turn, spinning wheel or responding'),
	(4, 'challenge', ''), -- pause for points adjustment |  TODO: useless?
	(5, 'end', 'game over')
;

CREATE TABLE IF NOT EXISTS game_players (
	game_id VARCHAR(6) NOT NULL,
	player_id INTEGER NOT NULL,
	points INTEGER DEFAULT 20,
	session_key TEXT,
	-- is_host BOOLEAN DEFAULT FALSE, -- TODO: maybe just say that host is initiative 0?
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
	('rule', 'while doing your best Robert Dinero impersonation', 'wearing a tiny hat', 0, CURRENT_TIMESTAMP, TRUE);

-- no primary key because cloning a card is possible
CREATE TABLE IF NOT EXISTS game_cards (
	id SERIAL PRIMARY KEY, -- to distinguish between clones
	game_id VARCHAR(6) NOT NULL,
	card_id INTEGER NOT NULL,
	slot INTEGER NOT NULL, -- 1-indexed number of wheel slots
	stack INTEGER NOT NULL, -- 0 bottom, 1 middle, 2 top; irrelevant if revealed
	player_id INTEGER, -- only populated when revealed=true
	revealed BOOLEAN DEFAULT FALSE, -- on the wheel
	flipped BOOLEAN DEFAULT FALSE,
	shredded BOOLEAN DEFAULT FALSE,
	from_clone BOOLEAN DEFAULT FALSE,
	FOREIGN KEY (game_id) REFERENCES games(id) ON DELETE CASCADE,
	FOREIGN KEY (card_id) REFERENCES cards(id) ON DELETE CASCADE
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

