-- SpaceMolt Knowledge Base Database Schema
-- SQLite 3 Compatible
--
-- This file is AUTO-GENERATED from the Go migration runner in
-- pkg/knowledge/sqlite_migrations.go. Do not edit by hand — instead add or
-- modify a migration in sqlite_migrations.go and re-run:
--
--   ./scripts/sql/regenerate_initialize_database.sh
--
-- Use this to initialize a fresh database outside the application:
--
--   sqlite3 spacemolt-knowledge.db < scripts/sql/initialize_database.sql
--
-- Migrations applied: 14
-- Last Regenerated: 2026-06-07

-- ============================================================================
-- TABLES
-- ============================================================================

CREATE TABLE "agent_ships" (
    id TEXT PRIMARY KEY,
    owner_id TEXT NOT NULL,
    class_id TEXT NOT NULL,
    name TEXT,
    hull REAL DEFAULT 0,
    max_hull REAL DEFAULT 0,
    shield REAL DEFAULT 0,
    max_shield REAL DEFAULT 0,
    shield_recharge REAL DEFAULT 0,
    armor REAL DEFAULT 0,
    speed REAL DEFAULT 0,
    fuel REAL DEFAULT 0,
    max_fuel REAL DEFAULT 0,
    cargo_used REAL DEFAULT 0,
    cargo_capacity REAL DEFAULT 0,
    cpu_used REAL DEFAULT 0,
    cpu_capacity REAL DEFAULT 0,
    power_used REAL DEFAULT 0,
    power_capacity REAL DEFAULT 0,
    weapon_slots INTEGER DEFAULT 0,
    defense_slots INTEGER DEFAULT 0,
    utility_slots INTEGER DEFAULT 0,
    docked_at_base TEXT,
    last_updated_tick INTEGER DEFAULT 0,
    FOREIGN KEY (owner_id) REFERENCES players(id) ON DELETE CASCADE,
    FOREIGN KEY (class_id) REFERENCES "ships"(id)
);


CREATE TABLE agents (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	role TEXT NOT NULL,
	empire TEXT,
	status TEXT DEFAULT 'active',
	last_updated_tick INTEGER DEFAULT 0
, wallet_credits INTEGER NOT NULL DEFAULT 0, wallet_updated_at TEXT);


CREATE TABLE anomalies (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	type TEXT NOT NULL, -- 'rich_deposit', 'hostile_zone', 'rare_resource', 'empty_station', etc.
	severity TEXT NOT NULL, -- 'info', 'warning', 'critical', 'opportunity'
	system_id TEXT,
	poi_id TEXT,
	description TEXT NOT NULL,
	details TEXT, -- JSON with additional data
	detected_at TEXT NOT NULL,
	detected_by TEXT,
	status TEXT DEFAULT 'active', -- 'active', 'resolved', 'obsolete'
	last_updated_tick INTEGER DEFAULT 0,
	FOREIGN KEY (system_id) REFERENCES systems(id) ON DELETE CASCADE,
	FOREIGN KEY (poi_id) REFERENCES pois(id) ON DELETE CASCADE
);


CREATE TABLE "base_facilities" (
	base_id TEXT NOT NULL,
	facility_name TEXT NOT NULL,
	instance_id TEXT NOT NULL DEFAULT '',
	description TEXT DEFAULT '',
	category TEXT DEFAULT 'unknown',
	level INTEGER DEFAULT 0,
	active BOOLEAN DEFAULT 1,
	maintenance_satisfied BOOLEAN DEFAULT 1,
	service TEXT DEFAULT '',
	recipe_id TEXT DEFAULT '',
	last_updated_tick INTEGER DEFAULT 0,
	PRIMARY KEY (base_id, instance_id),
	FOREIGN KEY (base_id) REFERENCES bases(id) ON DELETE CASCADE
);


CREATE TABLE base_market (
	id TEXT PRIMARY KEY,
	base_id TEXT NOT NULL,
	item_id TEXT NOT NULL,
	price_each REAL NOT NULL,
	quantity INTEGER NOT NULL,
	is_npc BOOLEAN DEFAULT 1,
	last_updated_tick INTEGER DEFAULT 0,
	FOREIGN KEY (base_id) REFERENCES bases(id) ON DELETE CASCADE
);


CREATE TABLE base_services (
	base_id TEXT NOT NULL,
	service_name TEXT NOT NULL,
	available BOOLEAN DEFAULT 1,
	PRIMARY KEY (base_id, service_name),
	FOREIGN KEY (base_id) REFERENCES bases(id) ON DELETE CASCADE
);


CREATE TABLE bases (
	id TEXT PRIMARY KEY,
	poi_id TEXT NOT NULL,
	name TEXT NOT NULL,
	description TEXT,
	empire TEXT,
	defense_level INTEGER DEFAULT 0,
	has_drones BOOLEAN DEFAULT 0,
	public_access BOOLEAN DEFAULT 1,
	last_updated_tick INTEGER DEFAULT 0, story TEXT DEFAULT '', pirate_rep_required INTEGER DEFAULT 0, condition TEXT DEFAULT '', condition_text TEXT DEFAULT '', satisfaction_pct INTEGER DEFAULT 0, satisfied_count INTEGER DEFAULT 0, total_service_infra INTEGER DEFAULT 0,
	FOREIGN KEY (poi_id) REFERENCES pois(id) ON DELETE CASCADE
);


CREATE TABLE change_snapshots (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	entity_type TEXT NOT NULL,   -- 'system', 'poi', 'base'
	entity_id TEXT NOT NULL,
	system_id TEXT NOT NULL,
	change_summary TEXT NOT NULL, -- human-readable description of changes
	old_data TEXT NOT NULL,       -- JSON snapshot of previous values
	detected_by TEXT NOT NULL,    -- agent ID that detected the change
	detected_at_tick INTEGER NOT NULL,
	detected_at TEXT NOT NULL
);


CREATE TABLE connection_metrics (
	from_system TEXT NOT NULL,
	to_system TEXT NOT NULL,
	travel_count INTEGER DEFAULT 0,
	avg_fuel_cost REAL,
	avg_travel_time REAL, -- in game ticks
	last_traveled TEXT,
	traveled_by TEXT,
	last_updated_tick INTEGER DEFAULT 0,
	PRIMARY KEY (from_system, to_system),
	FOREIGN KEY (from_system) REFERENCES systems(id) ON DELETE CASCADE,
	FOREIGN KEY (to_system) REFERENCES systems(id) ON DELETE CASCADE
);


CREATE TABLE connections (
	from_system TEXT NOT NULL,
	to_system TEXT NOT NULL,
	last_updated_tick INTEGER DEFAULT 0, distance INTEGER DEFAULT 0,
	PRIMARY KEY (from_system, to_system),
	FOREIGN KEY (from_system) REFERENCES systems(id) ON DELETE CASCADE,
	FOREIGN KEY (to_system) REFERENCES systems(id) ON DELETE CASCADE
);


CREATE TABLE danger_zones (
	system_id TEXT PRIMARY KEY,
	danger_level INTEGER DEFAULT 0, -- 0-10 scale
	hostile_encounters INTEGER DEFAULT 0,
	player_kills INTEGER DEFAULT 0,
	npc_kills INTEGER DEFAULT 0,
	last_incident TEXT,
	last_updated TEXT NOT NULL,
	FOREIGN KEY (system_id) REFERENCES systems(id) ON DELETE CASCADE
);


CREATE TABLE experiences (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	agent_id TEXT NOT NULL,
	time TEXT NOT NULL,
	type TEXT NOT NULL,
	description TEXT NOT NULL,
	outcome TEXT,
	location TEXT,
	last_updated_tick INTEGER DEFAULT 0
);


CREATE TABLE faction_bases (
					faction_id    TEXT NOT NULL,
					base_id       TEXT NOT NULL,
					base_name     TEXT,
					system_id     TEXT,
					system_name   TEXT,
					poi_id        TEXT,
					services_json TEXT,
					captured_utc  TEXT NOT NULL,
					PRIMARY KEY (faction_id, base_id)
				);


CREATE TABLE faction_facilities (
					faction_id    TEXT NOT NULL,
					base_id       TEXT NOT NULL,
					facility_id   TEXT NOT NULL,
					facility_type TEXT,
					category      TEXT,
					level         INTEGER NOT NULL DEFAULT 0,
					status        TEXT,
					recipe_id     TEXT,
					details_json  TEXT,
					captured_utc  TEXT NOT NULL,
					PRIMARY KEY (faction_id, base_id, facility_id)
				);


CREATE TABLE faction_fuel_bunkers (
					faction_id    TEXT NOT NULL,
					base_id       TEXT NOT NULL,
					base_name     TEXT,
					fuel_reserve  INTEGER NOT NULL DEFAULT 0,
					fuel_capacity INTEGER NOT NULL DEFAULT 0,
					captured_utc  TEXT NOT NULL,
					PRIMARY KEY (faction_id, base_id)
				);


CREATE TABLE faction_members (
					faction_id    TEXT NOT NULL,
					player_id     TEXT NOT NULL,
					username      TEXT,
					role          TEXT,
					joined_utc    TEXT,
					last_seen_utc TEXT,
					is_online     INTEGER NOT NULL DEFAULT 0,
					captured_utc  TEXT NOT NULL,
					PRIMARY KEY (faction_id, player_id)
				);


CREATE TABLE faction_missions (
					faction_id         TEXT NOT NULL,
					base_id            TEXT NOT NULL,
					mission_id         TEXT NOT NULL,
					title              TEXT,
					type               TEXT,
					description        TEXT,
					giver_name         TEXT,
					rewards_json       TEXT,
					objectives_json    TEXT,
					assigned_player_id TEXT,
					expiration_utc     TEXT,
					captured_utc       TEXT NOT NULL,
					PRIMARY KEY (faction_id, base_id, mission_id)
				);


CREATE TABLE faction_orders (
					faction_id   TEXT NOT NULL,
					base_id      TEXT NOT NULL,
					order_id     TEXT NOT NULL,
					side         TEXT,
					item_id      TEXT,
					item_name    TEXT,
					price_each   REAL NOT NULL DEFAULT 0,
					quantity     REAL NOT NULL DEFAULT 0,
					captured_utc TEXT NOT NULL,
					PRIMARY KEY (faction_id, base_id, order_id)
				);


CREATE TABLE faction_relations (
					faction_id        TEXT NOT NULL,
					target_faction_id TEXT NOT NULL,
					target_name       TEXT,
					target_tag        TEXT,
					kind              TEXT NOT NULL,
					reason            TEXT,
					terms             TEXT,
					our_kills         INTEGER NOT NULL DEFAULT 0,
					their_kills       INTEGER NOT NULL DEFAULT 0,
					started_utc       TEXT,
					captured_utc      TEXT NOT NULL,
					PRIMARY KEY (faction_id, target_faction_id, kind)
				);


CREATE TABLE faction_rooms (
					faction_id   TEXT NOT NULL,
					base_id      TEXT NOT NULL,
					room_id      TEXT NOT NULL,
					name         TEXT,
					access       TEXT,
					description  TEXT,
					captured_utc TEXT NOT NULL,
					PRIMARY KEY (faction_id, base_id, room_id)
				);


CREATE TABLE faction_storage (
					faction_id   TEXT NOT NULL,
					base_id      TEXT NOT NULL,
					credits      INTEGER NOT NULL DEFAULT 0,
					item_count   INTEGER NOT NULL DEFAULT 0,
					captured_utc TEXT NOT NULL,
					PRIMARY KEY (faction_id, base_id)
				);


CREATE TABLE faction_storage_items (
					faction_id   TEXT NOT NULL,
					base_id      TEXT NOT NULL,
					item_id      TEXT NOT NULL,
					name         TEXT,
					quantity     REAL NOT NULL DEFAULT 0,
					size         INTEGER NOT NULL DEFAULT 0,
					captured_utc TEXT NOT NULL,
					PRIMARY KEY (faction_id, base_id, item_id)
				);


CREATE TABLE factions (
					faction_id      TEXT PRIMARY KEY,
					name            TEXT,
					tag             TEXT,
					leader_id       TEXT,
					leader_username TEXT,
					treasury        INTEGER,
					member_count    INTEGER,
					owned_bases     INTEGER,
					description     TEXT,
					charter         TEXT,
					emblem          TEXT,
					primary_color   TEXT,
					secondary_color TEXT,
					founded_utc     TEXT,
					intel_systems   INTEGER,
					intel_trade     INTEGER,
					captured_utc    TEXT NOT NULL
				);


CREATE TABLE item_ammo (
    item_id    TEXT PRIMARY KEY REFERENCES items(id),
    ammo_type  TEXT NOT NULL
, modifiers TEXT);


CREATE TABLE item_consumable_effects (
    item_id     TEXT PRIMARY KEY REFERENCES items(id),
    effect_type TEXT NOT NULL,
    subtype     TEXT,
    amount      INTEGER,
    duration    INTEGER,
    stat        TEXT
);


CREATE TABLE item_defenses (
    item_id             TEXT PRIMARY KEY REFERENCES item_modules(item_id),
    armor_bonus         INTEGER,
    hull_bonus          INTEGER,
    shield_bonus        INTEGER,
    shield_recharge_bonus INTEGER,
    armor_repair_rate   INTEGER,
    resistance_bonus    TEXT,
    damage_reduction    REAL,
    cloak_strength      INTEGER,
    cooldown            INTEGER,
    damage              INTEGER,
    damage_type         TEXT,
    range               INTEGER
);


CREATE TABLE item_mining (
    item_id      TEXT PRIMARY KEY REFERENCES item_modules(item_id),
    mining_power INTEGER NOT NULL DEFAULT 0,
    mining_range INTEGER NOT NULL DEFAULT 0
);


CREATE TABLE item_modules (
    item_id     TEXT PRIMARY KEY REFERENCES items(id),
    type        TEXT NOT NULL,
    type_id     TEXT NOT NULL,
    cpu_usage   INTEGER NOT NULL DEFAULT 0,
    power_usage INTEGER NOT NULL DEFAULT 0,
    hidden      BOOLEAN NOT NULL DEFAULT 0,
    special     TEXT
, slot TEXT);


CREATE TABLE item_utilities (
    item_id          TEXT PRIMARY KEY REFERENCES item_modules(item_id),
    speed_bonus      INTEGER,
    cargo_bonus      INTEGER,
    cloak_strength   INTEGER,
    scanner_power    INTEGER,
    accuracy_bonus   INTEGER,
    tracking_bonus   INTEGER,
    signature_bonus  INTEGER,
    fuel_efficiency  REAL,
    drone_bandwidth  INTEGER,
    drone_capacity   INTEGER,
    harvest_power    INTEGER,
    harvest_range    INTEGER,
    survey_power     INTEGER,
    survey_range     INTEGER,
    tow_speed_penalty INTEGER,
    cooldown         INTEGER
, cpu_bonus INTEGER, max_fuel_bonus INTEGER, hull_penalty INTEGER, speed_penalty INTEGER);


CREATE TABLE item_weapons (
    item_id     TEXT PRIMARY KEY REFERENCES item_modules(item_id),
    damage      INTEGER NOT NULL DEFAULT 0,
    damage_type TEXT NOT NULL,
    range       INTEGER,
    reach       INTEGER,
    cooldown    INTEGER NOT NULL DEFAULT 0,
    ammo_type   TEXT,
    magazine_size INTEGER
, armor_bypass_bonus REAL, shield_bypass_bonus REAL);


CREATE TABLE items (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT,
    category TEXT,
    rarity TEXT,
    size INTEGER DEFAULT 1,
    base_value INTEGER DEFAULT 0,
    stackable BOOLEAN DEFAULT 0,
    tradeable BOOLEAN DEFAULT 0,
    last_updated_tick INTEGER DEFAULT 0
, hazardous BOOLEAN DEFAULT 0, power_bonus INTEGER DEFAULT 0, quest_item BOOLEAN NOT NULL DEFAULT 0, extracted_by TEXT, required_skills TEXT, region_lock TEXT, passenger_economy_berths INTEGER NOT NULL DEFAULT 0, passenger_business_berths INTEGER NOT NULL DEFAULT 0, passenger_first_berths INTEGER NOT NULL DEFAULT 0);


CREATE TABLE knowledge_exports (
	id TEXT PRIMARY KEY,
	created_at TEXT NOT NULL,
	created_by TEXT,
	description TEXT,
	systems_count INTEGER DEFAULT 0,
	pois_count INTEGER DEFAULT 0,
	experiences_count INTEGER DEFAULT 0,
	export_data TEXT, -- JSON snapshot
	last_updated_tick INTEGER DEFAULT 0
);


CREATE TABLE market_analyses (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	system_id TEXT NOT NULL,
	system_name TEXT NOT NULL,
	station_id TEXT NOT NULL,
	station_name TEXT NOT NULL,
	game_tick INTEGER NOT NULL,
	captured_at TEXT NOT NULL,
	agent_id TEXT NOT NULL,
	last_updated_tick INTEGER NOT NULL,

	-- Analysis content
	mode TEXT,
	skill_level INTEGER,
	scanning_range TEXT,
	stations_in_range INTEGER,
	items_scanned INTEGER,
	top_insights TEXT, -- JSON array of insights
	total_items INTEGER,
	total_pages INTEGER,
	page INTEGER,
	hint TEXT,
	xp_gained TEXT, -- JSON object
	analysis TEXT -- JSON object
);


CREATE TABLE market_buy_orders (
					station_id    TEXT NOT NULL,
					system_id     TEXT,
					item_id       TEXT NOT NULL,
					item_name     TEXT,
					price_each    REAL NOT NULL DEFAULT 0,
					quantity      REAL NOT NULL DEFAULT 0,
					source        TEXT,
					captured_utc  TEXT NOT NULL
				, my_quantity REAL NOT NULL DEFAULT 0);


CREATE TABLE market_demand_history (
					station_id     TEXT NOT NULL,
					system_id      TEXT,
					item_id        TEXT NOT NULL,
					item_name      TEXT,
					bucket_utc     TEXT NOT NULL,
					captured_utc   TEXT NOT NULL,
					best_price     REAL NOT NULL DEFAULT 0,
					total_qty      REAL NOT NULL DEFAULT 0,
					sm_best_price  REAL NOT NULL DEFAULT 0,
					sm_qty         REAL NOT NULL DEFAULT 0,
					order_count    INTEGER NOT NULL DEFAULT 0,
					PRIMARY KEY (station_id, item_id, bucket_utc)
				);


CREATE TABLE market_listings (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	snapshot_id INTEGER NOT NULL,
	item_id TEXT NOT NULL,
	item_type TEXT NOT NULL,
	quantity REAL NOT NULL,
	price_per_unit REAL NOT NULL,
	total_price REAL NOT NULL,
	listing_type TEXT NOT NULL,
	listed_by TEXT,
	last_updated_tick INTEGER DEFAULT 0,
	FOREIGN KEY (snapshot_id) REFERENCES market_snapshots(id) ON DELETE CASCADE
);


CREATE TABLE market_snapshots (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	system_id TEXT NOT NULL,
	system_name TEXT NOT NULL,
	station_id TEXT NOT NULL,
	station_name TEXT NOT NULL,
	game_tick INTEGER NOT NULL,
	captured_at TEXT NOT NULL,
	agent_id TEXT,
	last_updated_tick INTEGER DEFAULT 0,
	FOREIGN KEY (station_id) REFERENCES pois(id) ON DELETE CASCADE
);


CREATE TABLE mission_objectives (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    mission_id        TEXT NOT NULL,
    sort_order        INTEGER NOT NULL DEFAULT 0,
    type              TEXT NOT NULL,
    description       TEXT,
    item_id           TEXT,
    quantity          INTEGER DEFAULT 0,
    system_id         TEXT,
    system_name       TEXT,
    target_base_id    TEXT,
    target_base_name  TEXT,
    FOREIGN KEY (mission_id) REFERENCES mission_templates(id) ON DELETE CASCADE
);


CREATE TABLE mission_template_locations (
    mission_id       TEXT NOT NULL,
    base_id          TEXT NOT NULL,
    system_id        TEXT,
    first_seen_tick  INTEGER DEFAULT 0,
    last_seen_tick   INTEGER DEFAULT 0,
    first_seen_at    TEXT,
    last_seen_at     TEXT,
    PRIMARY KEY (mission_id, base_id),
    FOREIGN KEY (mission_id) REFERENCES mission_templates(id) ON DELETE CASCADE
);


CREATE TABLE mission_templates (
    id                  TEXT PRIMARY KEY,
    title               TEXT NOT NULL,
    description         TEXT,
    type                TEXT,
    difficulty          INTEGER DEFAULT 0,
    giver_name          TEXT,
    giver_title         TEXT,
    faction_id          TEXT,
    faction_name        TEXT,
    dialog_offer        TEXT,
    dialog_accept       TEXT,
    dialog_decline      TEXT,
    dialog_complete     TEXT,
    chain_next          TEXT,
    repeatable          INTEGER DEFAULT 0,
    expires_in_ticks    INTEGER DEFAULT 0,
    rewards_credits     INTEGER DEFAULT 0,
    rewards_skill_xp    TEXT DEFAULT '{}',
    rewards_items       TEXT DEFAULT '{}',
    requirements        TEXT DEFAULT '{}',
    required_modules    TEXT DEFAULT '[]',
    provided_items      TEXT DEFAULT '{}',
    first_seen_tick     INTEGER DEFAULT 0,
    last_seen_tick      INTEGER DEFAULT 0,
    first_seen_at       TEXT,
    last_seen_at        TEXT
);


CREATE TABLE player_skills (
    player_id TEXT NOT NULL,
    skill_id TEXT NOT NULL,
    level INTEGER DEFAULT 0,
    current_xp REAL DEFAULT 0,
    PRIMARY KEY (player_id, skill_id),
    FOREIGN KEY (player_id) REFERENCES players(id) ON DELETE CASCADE
);


CREATE TABLE player_stats (
    player_id TEXT PRIMARY KEY,
    ships_destroyed INTEGER DEFAULT 0,
    times_destroyed INTEGER DEFAULT 0,
    ore_mined REAL DEFAULT 0,
    credits_earned REAL DEFAULT 0,
    credits_spent REAL DEFAULT 0,
    trades_completed INTEGER DEFAULT 0,
    systems_discovered INTEGER DEFAULT 0,
    items_crafted INTEGER DEFAULT 0,
    missions_completed INTEGER DEFAULT 0,
    bases_destroyed INTEGER DEFAULT 0,
    distance_traveled INTEGER DEFAULT 0,
    pirates_destroyed INTEGER DEFAULT 0,
    ships_lost INTEGER DEFAULT 0,
    time_played INTEGER DEFAULT 0,
    last_updated_tick INTEGER DEFAULT 0,
    FOREIGN KEY (player_id) REFERENCES players(id) ON DELETE CASCADE
);


CREATE TABLE players (
    id TEXT PRIMARY KEY,
    username TEXT NOT NULL,
    empire TEXT,
    credits REAL DEFAULT 0,
    current_system TEXT,
    current_poi TEXT,
    current_ship_id TEXT,
    home_base TEXT,
    docked_at_base TEXT,
    faction_id TEXT,
    faction_rank TEXT,
    experience INTEGER DEFAULT 0,
    last_updated_tick INTEGER DEFAULT 0
);


CREATE TABLE poi_resources (
	poi_id TEXT NOT NULL,
	resource_id TEXT NOT NULL,
	richness REAL NOT NULL,
	remaining REAL NOT NULL,
	last_updated_tick INTEGER DEFAULT 0, detected_by TEXT,
	PRIMARY KEY (poi_id, resource_id),
	FOREIGN KEY (poi_id) REFERENCES pois(id) ON DELETE CASCADE
);


CREATE TABLE pois (
	id TEXT PRIMARY KEY,
	system_id TEXT NOT NULL,
	name TEXT NOT NULL,
	type TEXT NOT NULL,
	description TEXT,
	position_x REAL NOT NULL,
	position_y REAL NOT NULL,
	base_id TEXT,
	last_updated_tick INTEGER DEFAULT 0, class TEXT, hidden BOOLEAN NOT NULL DEFAULT 0, reveal_difficulty INTEGER NOT NULL DEFAULT 0, expires_at TEXT, detected_by TEXT,
	FOREIGN KEY (system_id) REFERENCES systems(id) ON DELETE CASCADE
);


CREATE TABLE price_trends (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	item_id TEXT NOT NULL,
	item_type TEXT NOT NULL,
	station_id TEXT NOT NULL,
	system_id TEXT NOT NULL,
	listing_type TEXT NOT NULL, -- 'buy' or 'sell'
	avg_price REAL NOT NULL,
	min_price REAL NOT NULL,
	max_price REAL NOT NULL,
	sample_count INTEGER NOT NULL,
	window_start TEXT NOT NULL,
	window_end TEXT NOT NULL,
	last_updated_tick INTEGER DEFAULT 0,
	FOREIGN KEY (station_id) REFERENCES pois(id) ON DELETE CASCADE
);


CREATE TABLE recipe_inputs (
    recipe_id TEXT NOT NULL,
    item_id TEXT NOT NULL,
    quantity INTEGER NOT NULL,
    PRIMARY KEY (recipe_id, item_id),
    FOREIGN KEY (recipe_id) REFERENCES recipes(id) ON DELETE CASCADE
);


CREATE TABLE recipe_outputs (
    recipe_id TEXT NOT NULL,
    item_id TEXT NOT NULL,
    quantity INTEGER NOT NULL,
    quality_mod BOOLEAN DEFAULT 0,
    PRIMARY KEY (recipe_id, item_id),
    FOREIGN KEY (recipe_id) REFERENCES recipes(id) ON DELETE CASCADE
);


CREATE TABLE recipes (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT,
    category TEXT,
    crafting_time INTEGER DEFAULT 0,
    base_quality INTEGER DEFAULT 0,
    skill_quality_mod INTEGER DEFAULT 0,
    required_skills TEXT DEFAULT '{}',
    last_updated_tick INTEGER DEFAULT 0
, hidden BOOLEAN DEFAULT 0, facility_only BOOLEAN NOT NULL DEFAULT 0, no_recycle BOOLEAN NOT NULL DEFAULT 0, fuel_output INTEGER NOT NULL DEFAULT 0);


CREATE TABLE resource_history (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	poi_id TEXT NOT NULL,
	resource_id TEXT NOT NULL,
	richness REAL NOT NULL,
	remaining REAL NOT NULL,
	game_tick INTEGER NOT NULL,
	recorded_at TEXT NOT NULL,
	agent_id TEXT,
	last_updated_tick INTEGER DEFAULT 0,
	FOREIGN KEY (poi_id) REFERENCES pois(id) ON DELETE CASCADE
);


CREATE TABLE schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at TEXT NOT NULL
		);


CREATE TABLE seen_player_ships (
					player_id       TEXT NOT NULL,
					ship_class      TEXT NOT NULL,
					first_seen_utc  TEXT NOT NULL,
					last_seen_utc   TEXT NOT NULL,
					sighting_count  INTEGER NOT NULL DEFAULT 1,
					PRIMARY KEY (player_id, ship_class)
				);


CREATE TABLE seen_player_sightings (
					player_id         TEXT NOT NULL,
					system_id         TEXT NOT NULL,
					poi_id            TEXT NOT NULL DEFAULT '',
					bucket_hour_utc   TEXT NOT NULL,
					ship_class        TEXT,
					source            TEXT NOT NULL,
					in_combat         INTEGER NOT NULL DEFAULT 0,
					first_seen_utc    TEXT NOT NULL,
					last_seen_utc     TEXT NOT NULL,
					observation_count INTEGER NOT NULL DEFAULT 1,
					PRIMARY KEY (player_id, system_id, poi_id, bucket_hour_utc)
				);


CREATE TABLE seen_players (
					player_id        TEXT PRIMARY KEY,
					username         TEXT NOT NULL,
					faction_id       TEXT,
					faction_tag      TEXT,
					clan_tag         TEXT,
					primary_color    TEXT,
					secondary_color  TEXT,
					status_message   TEXT,
					anonymous        INTEGER NOT NULL DEFAULT 0,
					first_seen_utc   TEXT NOT NULL,
					last_seen_utc    TEXT NOT NULL,
					sighting_count   INTEGER NOT NULL DEFAULT 1
				);


CREATE TABLE "ship_build_materials" (
    ship_class_id TEXT NOT NULL,
    item_id TEXT NOT NULL,
    quantity INTEGER NOT NULL,
    PRIMARY KEY (ship_class_id, item_id),
    FOREIGN KEY (ship_class_id) REFERENCES "ships"(id) ON DELETE CASCADE
);


CREATE TABLE ship_cargo (
    ship_id TEXT NOT NULL,
    item_id TEXT NOT NULL,
    quantity REAL NOT NULL,
    PRIMARY KEY (ship_id, item_id),
    FOREIGN KEY (ship_id) REFERENCES "agent_ships"(id) ON DELETE CASCADE
);


CREATE TABLE ship_listings (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	system_id TEXT NOT NULL,
	system_name TEXT NOT NULL,
	station_id TEXT NOT NULL,
	station_name TEXT NOT NULL,
	ship_class TEXT NOT NULL,
	ship_name TEXT NOT NULL,
	base_price REAL NOT NULL,
	description TEXT,
	cargo_space INTEGER,
	module_slots INTEGER,
	utility_slots INTEGER,
	weapon_slots INTEGER,
	game_tick INTEGER NOT NULL,
	captured_at TEXT NOT NULL,
	agent_id TEXT,
	last_updated_tick INTEGER DEFAULT 0,
	FOREIGN KEY (station_id) REFERENCES pois(id) ON DELETE CASCADE
);


CREATE TABLE ship_modules (
    id TEXT PRIMARY KEY,
    ship_id TEXT NOT NULL,
    type_id TEXT NOT NULL,
    name TEXT,
    type TEXT,
    cpu_usage INTEGER DEFAULT 0,
    power_usage INTEGER DEFAULT 0,
    quality REAL DEFAULT 1,
    quality_grade TEXT,
    wear REAL DEFAULT 0,
    wear_status TEXT,
    last_updated_tick INTEGER DEFAULT 0,
    FOREIGN KEY (ship_id) REFERENCES "agent_ships"(id) ON DELETE CASCADE
);


CREATE TABLE "ships" (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    class TEXT,
    category TEXT,
    description TEXT,
    lore TEXT,
    faction TEXT,
    tier INTEGER DEFAULT 0,
    scale INTEGER DEFAULT 1,
    price INTEGER DEFAULT 0,
    base_hull INTEGER DEFAULT 0,
    base_shield INTEGER DEFAULT 0,
    base_shield_recharge INTEGER DEFAULT 0,
    base_armor INTEGER DEFAULT 0,
    base_speed INTEGER DEFAULT 0,
    base_fuel INTEGER DEFAULT 0,
    cargo_capacity INTEGER DEFAULT 0,
    cpu_capacity INTEGER DEFAULT 0,
    power_capacity INTEGER DEFAULT 0,
    weapon_slots INTEGER DEFAULT 0,
    defense_slots INTEGER DEFAULT 0,
    utility_slots INTEGER DEFAULT 0,
    build_time INTEGER DEFAULT 0,
    shipyard_tier INTEGER DEFAULT 0,
    starter_ship BOOLEAN DEFAULT 0,
    tow_speed_bonus INTEGER DEFAULT 0,
    required_skills TEXT DEFAULT '{}',
    default_modules TEXT DEFAULT '[]',
    flavor_tags TEXT DEFAULT '[]',
    last_updated_tick INTEGER DEFAULT 0
, passive_recipes TEXT DEFAULT '[]', based_on TEXT, npc_role TEXT, special TEXT, required_reputation INTEGER NOT NULL DEFAULT 0, piloting_required INTEGER NOT NULL DEFAULT 0, inherent_capabilities TEXT);


CREATE TABLE skills (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT,
    category TEXT,
    max_level INTEGER DEFAULT 10,
    training_source TEXT,
    xp_per_level TEXT DEFAULT '[]',
    bonus_per_level TEXT DEFAULT '{}',
    required_skills TEXT DEFAULT '{}',
    last_updated_tick INTEGER DEFAULT 0
, empire_restriction TEXT DEFAULT '');


CREATE TABLE storage_snapshot_items (
	snapshot_id INTEGER NOT NULL,
	item_id TEXT NOT NULL,
	name TEXT NOT NULL DEFAULT '',
	quantity REAL NOT NULL DEFAULT 0,
	size INTEGER NOT NULL DEFAULT 0,
	FOREIGN KEY (snapshot_id) REFERENCES storage_snapshots(id) ON DELETE CASCADE
);


CREATE TABLE storage_snapshot_ships (
	snapshot_id INTEGER NOT NULL,
	ship_id TEXT NOT NULL,
	class_id TEXT NOT NULL DEFAULT '',
	class_name TEXT NOT NULL DEFAULT '',
	cargo_used INTEGER NOT NULL DEFAULT 0,
	FOREIGN KEY (snapshot_id) REFERENCES storage_snapshots(id) ON DELETE CASCADE
);


CREATE TABLE storage_snapshots (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	agent_id TEXT NOT NULL,
	base_id TEXT NOT NULL,
	credits INTEGER NOT NULL DEFAULT 0,
	captured_at TEXT NOT NULL,
	UNIQUE(agent_id, base_id)
);


CREATE TABLE systems (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	position_x REAL NOT NULL,
	position_y REAL NOT NULL,
	police_level INTEGER DEFAULT 0,
	empire TEXT,
	description TEXT,
	last_updated_tick INTEGER DEFAULT 0
, is_stronghold BOOLEAN DEFAULT 0, security_status TEXT DEFAULT '', last_visited_tick INTEGER NOT NULL DEFAULT 0);


CREATE TABLE xp_observations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    agent_id TEXT NOT NULL,
    action TEXT NOT NULL,
    target TEXT NOT NULL DEFAULT '',
    source TEXT NOT NULL DEFAULT 'action',
    skill_id TEXT NOT NULL,
    xp_delta REAL NOT NULL,
    level_delta INTEGER NOT NULL DEFAULT 0,
    level_before INTEGER NOT NULL,
    level_after INTEGER NOT NULL,
    game_tick INTEGER NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    mission_id TEXT DEFAULT NULL,
    quantity INTEGER NOT NULL DEFAULT 1
);


-- ============================================================================
-- INDEXES
-- ============================================================================

CREATE INDEX faction_facilities_faction ON faction_facilities(faction_id);

CREATE INDEX faction_members_faction   ON faction_members(faction_id);

CREATE INDEX faction_missions_faction   ON faction_missions(faction_id);

CREATE INDEX faction_orders_faction     ON faction_orders(faction_id);

CREATE INDEX faction_relations_faction  ON faction_relations(faction_id);

CREATE INDEX faction_rooms_faction      ON faction_rooms(faction_id);

CREATE INDEX faction_storage_faction    ON faction_storage(faction_id);

CREATE INDEX idx_agent_ships_class ON agent_ships(class_id);

CREATE INDEX idx_agent_ships_owner ON agent_ships(owner_id);

CREATE INDEX idx_anomalies_poi ON anomalies(poi_id, status);

CREATE INDEX idx_anomalies_system ON anomalies(system_id, status);

CREATE INDEX idx_anomalies_type_severity ON anomalies(type, severity, detected_at DESC);

CREATE INDEX idx_base_facilities_category ON base_facilities(category);

CREATE INDEX idx_base_facilities_recipe ON base_facilities(recipe_id);

CREATE INDEX idx_base_market_base_id ON base_market(base_id);

CREATE INDEX idx_base_market_item_id ON base_market(item_id);

CREATE INDEX idx_bases_poi_id ON bases(poi_id);

CREATE INDEX idx_change_snapshots_agent ON change_snapshots(detected_by, detected_at DESC);

CREATE INDEX idx_change_snapshots_entity ON change_snapshots(entity_type, entity_id, detected_at DESC);

CREATE INDEX idx_change_snapshots_system ON change_snapshots(system_id, detected_at DESC);

CREATE INDEX idx_connection_metrics_from ON connection_metrics(from_system, avg_fuel_cost ASC);

CREATE INDEX idx_connections_from ON connections(from_system);

CREATE INDEX idx_connections_to ON connections(to_system);

CREATE INDEX idx_danger_zones_level ON danger_zones(danger_level DESC);

CREATE INDEX idx_experiences_agent_id ON experiences(agent_id, time DESC);

CREATE INDEX idx_item_ammo_type ON item_ammo(ammo_type);

CREATE INDEX idx_item_consumable_effects_type ON item_consumable_effects(effect_type);

CREATE INDEX idx_item_modules_type ON item_modules(type);

CREATE INDEX idx_item_modules_type_id ON item_modules(type_id);

CREATE INDEX idx_item_weapons_ammo_type ON item_weapons(ammo_type);

CREATE INDEX idx_item_weapons_damage_type ON item_weapons(damage_type);

CREATE INDEX idx_items_category ON items(category);

CREATE INDEX idx_market_analyses_captured ON market_analyses(captured_at DESC);

CREATE INDEX idx_market_analyses_system_station ON market_analyses(system_id, station_id, captured_at DESC);

CREATE INDEX idx_market_listings_item_id ON market_listings(item_id);

CREATE INDEX idx_market_listings_item_type ON market_listings(item_type);

CREATE INDEX idx_market_listings_snapshot_id ON market_listings(snapshot_id);

CREATE INDEX idx_market_snapshots_captured_at ON market_snapshots(captured_at DESC);

CREATE INDEX idx_market_snapshots_system_station ON market_snapshots(system_id, station_id, captured_at DESC);

CREATE INDEX idx_mission_locations_base    ON mission_template_locations(base_id);

CREATE INDEX idx_mission_objectives_mission ON mission_objectives(mission_id);

CREATE INDEX idx_mission_templates_faction ON mission_templates(faction_id);

CREATE INDEX idx_mission_templates_type    ON mission_templates(type);

CREATE INDEX idx_players_empire ON players(empire);

CREATE INDEX idx_pois_expires_at ON pois(expires_at);

CREATE INDEX idx_pois_system_id ON pois(system_id);

CREATE INDEX idx_price_trends_item_station ON price_trends(item_id, station_id, window_end DESC);

CREATE INDEX idx_recipes_category ON recipes(category);

CREATE INDEX idx_resource_history_poi_resource ON resource_history(poi_id, resource_id, game_tick DESC);

CREATE INDEX idx_resource_history_tick ON resource_history(game_tick DESC);

CREATE INDEX idx_ship_listings_captured_at ON ship_listings(captured_at DESC);

CREATE INDEX idx_ship_listings_system_station ON ship_listings(system_id, station_id, captured_at DESC);

CREATE INDEX idx_ship_modules_ship ON ship_modules(ship_id);

CREATE INDEX idx_ships_class ON ships(class);

CREATE INDEX idx_ships_faction ON ships(faction);

CREATE INDEX idx_skills_category ON skills(category);

CREATE INDEX idx_storage_snapshot_items_snapshot ON storage_snapshot_items(snapshot_id);

CREATE INDEX idx_storage_snapshot_ships_snapshot ON storage_snapshot_ships(snapshot_id);

CREATE INDEX idx_storage_snapshots_agent ON storage_snapshots(agent_id);

CREATE INDEX idx_xp_obs_action ON xp_observations(action);

CREATE INDEX idx_xp_obs_agent ON xp_observations(agent_id);

CREATE INDEX idx_xp_obs_skill ON xp_observations(skill_id);

CREATE INDEX idx_xp_obs_source ON xp_observations(source);

CREATE INDEX market_buy_orders_item ON market_buy_orders(item_id);

CREATE INDEX market_buy_orders_station_item ON market_buy_orders(station_id, item_id);

CREATE INDEX market_demand_history_item ON market_demand_history(item_id, bucket_utc);

CREATE INDEX seen_player_ships_class ON seen_player_ships(ship_class);

CREATE INDEX seen_players_faction   ON seen_players(faction_id);

CREATE INDEX seen_players_last_seen ON seen_players(last_seen_utc);

CREATE INDEX seen_players_username  ON seen_players(username);

CREATE INDEX seen_sightings_last   ON seen_player_sightings(last_seen_utc);

CREATE INDEX seen_sightings_system ON seen_player_sightings(system_id, bucket_hour_utc);


-- ============================================================================
-- MIGRATION VERSION RECORDS
-- ============================================================================

INSERT OR IGNORE INTO schema_migrations (version, applied_at) VALUES (1, datetime('now'));
INSERT OR IGNORE INTO schema_migrations (version, applied_at) VALUES (2, datetime('now'));
INSERT OR IGNORE INTO schema_migrations (version, applied_at) VALUES (31, datetime('now'));
INSERT OR IGNORE INTO schema_migrations (version, applied_at) VALUES (32, datetime('now'));
INSERT OR IGNORE INTO schema_migrations (version, applied_at) VALUES (33, datetime('now'));
INSERT OR IGNORE INTO schema_migrations (version, applied_at) VALUES (34, datetime('now'));
INSERT OR IGNORE INTO schema_migrations (version, applied_at) VALUES (35, datetime('now'));
INSERT OR IGNORE INTO schema_migrations (version, applied_at) VALUES (36, datetime('now'));
INSERT OR IGNORE INTO schema_migrations (version, applied_at) VALUES (37, datetime('now'));
INSERT OR IGNORE INTO schema_migrations (version, applied_at) VALUES (38, datetime('now'));
INSERT OR IGNORE INTO schema_migrations (version, applied_at) VALUES (39, datetime('now'));
INSERT OR IGNORE INTO schema_migrations (version, applied_at) VALUES (40, datetime('now'));
INSERT OR IGNORE INTO schema_migrations (version, applied_at) VALUES (41, datetime('now'));
INSERT OR IGNORE INTO schema_migrations (version, applied_at) VALUES (42, datetime('now'));

