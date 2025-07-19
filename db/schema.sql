CREATE TABLE IF NOT EXISTS players (
	id SERIAL PRIMARY KEY,
	name TEXT NOT NULL,
	created TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS games (
	name TEXT NOT NULL,
	id VARCHAR(6) PRIMARY KEY,
	created TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	owner_id INT NOT NULL,
	state TEXT NOT NULL DEFAULT 'created',
	initiative_current INT DEFAULT 0, 
	FOREIGN KEY (owner_id) REFERENCES players(id) ON DELETE CASCADE,
	FOREIGN KEY (state) REFERENCES game_states(name) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS game_states (
	name TEXT NOT NULL PRIMARY KEY
);

INSERT INTO game_states (name)
VALUES
	('created'),
	('inviting'),
	('ready'), -- not yet started
	('turn'), --player turn, spin wheel
	('challenge'), -- pause for points adjustment
	('end')
;


CREATE TABLE IF NOT EXISTS game_players (
	game_id INT NOT NULL,
	player_id INT NOT NULL,
	points INT DEFAULT 20,
	-- is_host BOOLEAN DEFAULT FALSE, -- TODO: maybe just say that host is initiative 0?
	joined TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	initiative INT,
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
	FOREIGN KEY (type) REFERENCES card_types(name) ON DELETE CASCADE,
	FOREIGN KEY (creator) REFERENCES players(id)
);

-- no primary key because cloning a card is possible
CREATE TABLE IF NOT EXISTS game_cards (
	id SERIAL PRIMARY KEY, -- to distinguish between clones
	game_id VARCHAR(6) NOT NULL,
	card_id INT NOT NULL,
	slot INT NOT NULL, -- 1-indexed number of wheel slots
	stack INT NOT NULL, -- 0 bottom, 1 middle, 2 top
	player_id INT, -- only populated when revealed=true
	revealed BOOLEAN DEFAULT FALSE, -- on the wheel
	flipped BOOLEAN DEFAULT FALSE,
	shredded BOOLEAN DEFAULT FALSE,
	from_clone BOOLEAN DEFAULT FALSE,
	FOREIGN KEY (game_id) REFERENCES games(id) ON DELETE CASCADE,
	FOREIGN KEY (card_id) REFERENCES cards(id) ON DELETE CASCADE,
	FOREIGN KEY (player_id) REFERENCES players(id)
);

CREATE TABLE IF NOT EXISTS infractions (
	id SERIAL PRIMARY KEY,
	game_id VARCHAR(6) NOT NULL,
	accused INT NOT NULL,
	accuser INT NOT NULL,
	created TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	active BOOLEAN DEFAULT TRUE, -- active until applied or denied
	convicted BOOLEAN DEFAULT FALSE,
	FOREIGN KEY (game_id) REFERENCES games(id) ON DELETE CASCADE,
	FOREIGN KEY (accused) REFERENCES players(id) ON DELETE CASCADE,
	FOREIGN KEY (accuser) REFERENCES players(id) ON DELETE CASCADE
);

