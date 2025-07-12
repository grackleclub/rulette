CREATE TABLE IF NOT EXISTS players (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL,
	created TIMESTAMP DEFAULT CURRENT_TIMESTAMP
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
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	type TEXT NOT NULL,
	front TEXT NOT NULL,
	back TEXT,
	created TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	creator INTEGER,
	default BOOLEAN DEFAULT FALSE,
	FOREIGN KEY (type) REFERENCES card_types(name) ON DELETE CASCADE,
	FOREIGN KEY (creator) REFERENCES players(id)
);

CREATE TABLE IF NOT EXISTS games (
	name TEXT NOT NULL,
	code VARCHAR(6) PRIMARY KEY,
	created TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	owner INT NOT NULL,
	FOREIGN KEY (owner) REFERENCES players(id) ON DELETE CASCADE
);

-- no primary key because cloning a card is possible
CREATE TABLE IF NOT EXISTS game_cards (
	game_id VARCHAR(6) NOT NULL,
	card_id INT NOT NULL,
	slot INT NOT NULL, -- 1-indexed number of wheel slots
	stack INT NOT NULL, -- 0 bottom, 1 middle, 2 top
	player_id INT, -- only populated when revealed=true
	revealed BOOLEAN DEFAULT FALSE, -- on the wheel
	flipped BOOLEAN DEFAULT FALSE,
	shredded BOOLEAN DEFAULT FALSE,
	FOREIGN KEY (game_id) REFERENCES games(code) ON DELETE CASCADE,
	FOREIGN KEY (card_id) REFERENCES cards(id) ON DELETE CASCADE,
	FOREIGN KEY (player_id) REFERENCES players(id)
);

CREATE TABLE IF NOT EXISTS game_players (
	game_id INT NOT NULL,
	player_id INT NOT NULL,
	points INT DEFAULT 20,
	is_host BOOLEAN DEFAULT FALSE,
	PRIMARY KEY (game_id, player_id),
	FOREIGN KEY (game_id) REFERENCES games(id) ON DELETE CASCADE,
	FOREIGN KEY (player_id) REFERENCES players(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS turn_order (
	game_id INT NOT NULL,
	player_id INT NOT NULL,
	position INT NOT NULL,
	active BOOLEAN DEFAULT FALSE,
	PRIMARY KEY (game_id, player_id),
	FOREIGN KEY (game_id) REFERENCES games(code) ON DELETE CASCADE,
	FOREIGN KEY (player_id) REFERENCES players(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS infractions (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	game_id VARCHAR(6) NOT NULL,
	accused INT NOT NULL,
	accuser INT NOT NULL,
	created TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	active BOOLEAN DEFAULT TRUE, -- active until applied or denied
	convicted BOOLEAN DEFAULT FALSE,
	FOREIGN KEY (game_id) REFERENCES games(code) ON DELETE CASCADE,
	FOREIGN KEY (accused) REFERENCES players(id) ON DELETE CASCADE,
	FOREIGN KEY (accuser) REFERENCES players(id) ON DELETE CASCADE,
	PRIMARY KEY (game_id, accused, accuser)
);

