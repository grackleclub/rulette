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
(6, 'ending', 'deck exhausted, waiting on host to end the game'),
(7, 'end', 'game over')
ON CONFLICT DO NOTHING;

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
	('prompt', 'single challenge to be immediately completed')
ON CONFLICT DO NOTHING;

CREATE TABLE IF NOT EXISTS modifier_effects (
	name TEXT PRIMARY KEY,
	description TEXT
);
INSERT INTO modifier_effects (name, description)
VALUES
	('flip', 'flip a card to reveal its back side'),
	('shred', 'permanently remove a card from play'),
	('clone', 'duplicate a card and give the copy to another player'),
	('transfer', 'transfer a card to another player')
ON CONFLICT DO NOTHING;

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
DELETE FROM cards a USING cards b
WHERE a.id > b.id AND a.front = b.front;
CREATE UNIQUE INDEX IF NOT EXISTS cards_front_unique ON cards (front);

INSERT INTO cards (type, front, back, creator, created, generic, modifier_effect)
VALUES
	('modifier', 'flip any of your own cards', '', 0, CURRENT_TIMESTAMP, TRUE, 'flip'),
	('modifier', 'shred any of your own cards', '', 0, CURRENT_TIMESTAMP, TRUE, 'shred'),
	('modifier', 'clone any of your own cards, and give to someone else', '', 0, CURRENT_TIMESTAMP, TRUE, 'clone'),
	('modifier', 'transfer any of your own cards to another player', '', 0, CURRENT_TIMESTAMP, TRUE, 'transfer'),
	('rule', 'in a whisper', 'a little too loudly', 0, CURRENT_TIMESTAMP, TRUE, NULL),
	('rule', 'doing your best Robert De Nero impersonation', 'doing your worst Robin Williams impersonation', 0, CURRENT_TIMESTAMP, TRUE, NULL),
	('rule', 'not using any pronouns', 'using only pronouns', 0, CURRENT_TIMESTAMP, TRUE, NULL),
	('rule', 'referring to yourself only in the third person', 'referring to yourself in the second person', 0, CURRENT_TIMESTAMP, TRUE, NULL),
	('rule', 'in a transatlantic accent', 'in a valley girl accent', 0, CURRENT_TIMESTAMP, TRUE, NULL),
	('rule', 'while singing', 'in a monotone', 0, CURRENT_TIMESTAMP, TRUE, NULL),
	('rule', 'in your best Shakespearian english', 'using all the contemporary slang you can', 0, CURRENT_TIMESTAMP, TRUE, NULL),
	('rule', 'never saying "um"', 'saying "um" every other word', 0, CURRENT_TIMESTAMP, TRUE, NULL),
	('rule', 'doing your best Matthew McConaughey impersonation', 'doing your worst Jack Nicholson impersonation', 0, CURRENT_TIMESTAMP, TRUE, NULL),
	('rule', 'as if everything is juicy gossip', 'as if everything is really boring', 0, CURRENT_TIMESTAMP, TRUE, NULL),
	('rule', 'while trying to incite a revolution', 'while trying to calm everyone down', 0, CURRENT_TIMESTAMP, TRUE, NULL),
	('rule', 'like your mouth is full of marshmallows', 'like you have horrible cottonmouth', 0, CURRENT_TIMESTAMP, TRUE, NULL),
	('rule', 'name dropping every sentence', 'not using any names', 0, CURRENT_TIMESTAMP, TRUE, NULL),
	('rule', 'speaking only in haiku', 'rhyming every sentence', 0, CURRENT_TIMESTAMP, TRUE, NULL),
	('rule', 'always starting with a compliment', 'always self-aggrandizing', 0, CURRENT_TIMESTAMP, TRUE, NULL),
	('rule', 'starting every sentence with the next letter of the alphabet', 'starting every word with the next letter of the alphabet', 0, CURRENT_TIMESTAMP, TRUE, NULL),
	('rule', 'starting with a fun fact', 'ending with a fun fact', 0, CURRENT_TIMESTAMP, TRUE, NULL),
	('rule', 'assigning superlatives to the other players', 'assigning superlatives to yourself', 0, CURRENT_TIMESTAMP, TRUE, NULL),
	('rule', 'as if everything is a question', 'as if everything is a definite answer', 0, CURRENT_TIMESTAMP, TRUE, NULL),
	('rule', 'with vocal fry', 'over-enunciating', 0, CURRENT_TIMESTAMP, TRUE, NULL)
ON CONFLICT (front) DO UPDATE SET
	back = EXCLUDED.back,
	type = EXCLUDED.type,
	generic = EXCLUDED.generic,
	modifier_effect = EXCLUDED.modifier_effect;

-- this list must match the INSERT VALUES above
DELETE FROM cards WHERE generic = TRUE AND front NOT IN (
	'flip any of your own cards',
	'shred any of your own cards',
	'clone any of your own cards, and give to someone else',
	'transfer any of your own cards to another player',
	'in a whisper',
	'doing your best Robert De Nero impersonation',
	'not using any pronouns',
	'referring to yourself only in the third person',
	'in a transatlantic accent',
	'while singing',
	'in your best Shakespearian english',
	'never saying "um"',
	'doing your best Matthew McConaughey impersonation',
	'as if everything is juicy gossip',
	'while trying to incite a revolution',
	'like your mouth is full of marshmallows',
	'name dropping every sentence',
	'speaking only in haiku',
	'always starting with a compliment',
	'starting every sentence with the next letter of the alphabet',
	'starting with a fun fact',
	'assigning superlatives to the other players',
	'as if everything is a question',
	'with vocal fry'
);

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

-- spins: per-spin detail (one row per wheel spin). a detail table referenced
-- by event_log; not the player-facing log itself.
CREATE TABLE IF NOT EXISTS spins (
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
	game_card_id INTEGER NOT NULL, -- which rule was allegedly violated
	accused INTEGER NOT NULL,
	accuser INTEGER NOT NULL,
	created TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	active BOOLEAN DEFAULT TRUE, -- active until decided
	affirmed BOOLEAN DEFAULT FALSE,
	-- points changes are recorded in point_changes, not here
	FOREIGN KEY (game_id) REFERENCES games(id) ON DELETE CASCADE,
	FOREIGN KEY (game_card_id) REFERENCES game_cards(id) ON DELETE CASCADE,
	FOREIGN KEY (accused) REFERENCES players(id) ON DELETE CASCADE,
	FOREIGN KEY (accuser) REFERENCES players(id) ON DELETE CASCADE
);

CREATE UNLOGGED TABLE IF NOT EXISTS game_cache (
	game_id VARCHAR(6) PRIMARY KEY,
	value JSONB,
	expires TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP + INTERVAL '1 seconds' -- TODO: make var?
);

CREATE TABLE IF NOT EXISTS event_types (
	name TEXT PRIMARY KEY,
	description TEXT
);
INSERT INTO event_types (name, description)
VALUES
	('start', 'host started the game'),
	('rolled-end', 'a player spun the end of the deck'),
	('end', 'game ended'),
	('pause', 'host paused the game'),
	('resume', 'host resumed the game'),
	('turn', 'initiative passed to a player'),
	('spin', 'a player spun the wheel'),
	('points', 'points adjusted for a player'),
	('accuse', 'a player accused another of an infraction'),
	('decide', 'host decided an infraction'),
	('flip', 'a card was flipped'),
	('shred', 'a card was shredded'),
	('clone', 'a card was cloned'),
	('transfer', 'a card was transferred')
ON CONFLICT DO NOTHING;

-- point_changes: a record of every points change. infraction_id says what
-- caused it: set means an affirmed accusation, NULL means a direct host
-- adjustment.
CREATE TABLE IF NOT EXISTS point_changes (
	id SERIAL PRIMARY KEY,
	game_id VARCHAR(6) NOT NULL,
	player_id INTEGER, -- whose balance changed (NULL=deleted)
	delta INTEGER NOT NULL, -- signed
	infraction_id INTEGER, -- cause; NULL = direct host adjustment
	ts TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	FOREIGN KEY (game_id) REFERENCES games(id) ON DELETE CASCADE,
	FOREIGN KEY (player_id) REFERENCES players(id) ON DELETE SET NULL,
	FOREIGN KEY (infraction_id) REFERENCES infractions(id) ON DELETE SET NULL
);

-- event_log: an ordered, player-visible feed of game events. each row points
-- at the table holding that event's detail (spins, infractions, game_cards,
-- point_changes) instead of copying it. actor_id, target_id, and ts are set
-- when the event happens and never change.
CREATE TABLE IF NOT EXISTS event_log (
	id SERIAL PRIMARY KEY,
	game_id VARCHAR(6) NOT NULL,
	event_type TEXT NOT NULL,
	actor_id INTEGER, -- who caused it (NULL=system/host)
	target_id INTEGER, -- who it's about
	spin_id INTEGER, -- spin events
	infraction_id INTEGER, -- accuse/decide events
	game_card_id INTEGER, -- flip/shred/clone/transfer events
	point_change_id INTEGER, -- points events
	ts TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	FOREIGN KEY (game_id) REFERENCES games(id) ON DELETE CASCADE,
	FOREIGN KEY (event_type) REFERENCES event_types(name),
	FOREIGN KEY (actor_id) REFERENCES players(id) ON DELETE SET NULL,
	FOREIGN KEY (target_id) REFERENCES players(id) ON DELETE SET NULL,
	FOREIGN KEY (spin_id) REFERENCES spins(id) ON DELETE SET NULL,
	FOREIGN KEY (infraction_id) REFERENCES infractions(id) ON DELETE SET NULL,
	FOREIGN KEY (game_card_id) REFERENCES game_cards(id) ON DELETE SET NULL,
	FOREIGN KEY (point_change_id) REFERENCES point_changes(id) ON DELETE SET NULL
);
CREATE INDEX IF NOT EXISTS event_log_game_id_idx ON event_log (game_id, id);
