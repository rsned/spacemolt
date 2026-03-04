package knowledge

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// Migration represents a database schema migration
type Migration struct {
	version int
	name    string
	sql     string
}

// migrations returns all migrations in order
func migrations() []Migration {
	return []Migration{
		{
			version: 1,
			name:    "initial_schema",
			sql: `
-- Systems table: stores solar system information
CREATE TABLE IF NOT EXISTS systems (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	position_x REAL NOT NULL,
	position_y REAL NOT NULL,
	police_level INTEGER DEFAULT 0,
	empire TEXT,
	description TEXT,
	last_updated_tick INTEGER DEFAULT 0
);

-- Connections table: stores system-to-system connections
CREATE TABLE IF NOT EXISTS connections (
	from_system TEXT NOT NULL,
	to_system TEXT NOT NULL,
	last_updated_tick INTEGER DEFAULT 0,
	PRIMARY KEY (from_system, to_system),
	FOREIGN KEY (from_system) REFERENCES systems(id) ON DELETE CASCADE,
	FOREIGN KEY (to_system) REFERENCES systems(id) ON DELETE CASCADE
);

-- POIs table: stores points of interest
CREATE TABLE IF NOT EXISTS pois (
	id TEXT PRIMARY KEY,
	system_id TEXT NOT NULL,
	name TEXT NOT NULL,
	type TEXT NOT NULL,
	description TEXT,
	position_x REAL NOT NULL,
	position_y REAL NOT NULL,
	base_id TEXT,
	last_updated_tick INTEGER DEFAULT 0,
	FOREIGN KEY (system_id) REFERENCES systems(id) ON DELETE CASCADE
);

-- POI resources table: stores resource information for POIs
CREATE TABLE IF NOT EXISTS poi_resources (
	poi_id TEXT NOT NULL,
	resource_id TEXT NOT NULL,
	richness REAL NOT NULL,
	remaining REAL NOT NULL,
	last_updated_tick INTEGER DEFAULT 0,
	PRIMARY KEY (poi_id, resource_id),
	FOREIGN KEY (poi_id) REFERENCES pois(id) ON DELETE CASCADE
);

-- Experiences table: stores agent experiences
CREATE TABLE IF NOT EXISTS experiences (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	agent_id TEXT NOT NULL,
	time TEXT NOT NULL,
	type TEXT NOT NULL,
	description TEXT NOT NULL,
	outcome TEXT,
	location TEXT,
	last_updated_tick INTEGER DEFAULT 0
);

-- Agents table: stores agent metadata
CREATE TABLE IF NOT EXISTS agents (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	role TEXT NOT NULL,
	empire TEXT,
	status TEXT DEFAULT 'active',
	last_updated_tick INTEGER DEFAULT 0
);

-- Create indexes for better query performance
CREATE INDEX IF NOT EXISTS idx_experiences_agent_id ON experiences(agent_id, time DESC);
CREATE INDEX IF NOT EXISTS idx_pois_system_id ON pois(system_id);
CREATE INDEX IF NOT EXISTS idx_connections_from ON connections(from_system);
CREATE INDEX IF NOT EXISTS idx_connections_to ON connections(to_system);
`,
		},
		{
			version: 2,
			name:    "market_data",
			sql: `
-- Market snapshots table: stores captured market state
CREATE TABLE IF NOT EXISTS market_snapshots (
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

-- Market listings table: individual listings in a snapshot
CREATE TABLE IF NOT EXISTS market_listings (
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

-- Indexes for efficient queries
CREATE INDEX IF NOT EXISTS idx_market_snapshots_system_station ON market_snapshots(system_id, station_id, captured_at DESC);
CREATE INDEX IF NOT EXISTS idx_market_snapshots_captured_at ON market_snapshots(captured_at DESC);
CREATE INDEX IF NOT EXISTS idx_market_listings_snapshot_id ON market_listings(snapshot_id);
CREATE INDEX IF NOT EXISTS idx_market_listings_item_id ON market_listings(item_id);
CREATE INDEX IF NOT EXISTS idx_market_listings_item_type ON market_listings(item_type);

-- Ship listings table: stores available ships at stations
CREATE TABLE IF NOT EXISTS ship_listings (
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

-- Indexes for ship listings
CREATE INDEX IF NOT EXISTS idx_ship_listings_system_station ON ship_listings(system_id, station_id, captured_at DESC);
CREATE INDEX IF NOT EXISTS idx_ship_listings_captured_at ON ship_listings(captured_at DESC);
`,
		},
		{
			version: 3,
			name:    "enhanced_analytics",
			sql: `
-- Resource depletion tracking: monitors resource changes over time
CREATE TABLE IF NOT EXISTS resource_history (
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

-- Connection metadata: for route optimization
CREATE TABLE IF NOT EXISTS connection_metrics (
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

-- Anomalies: unusual discoveries worth noting
CREATE TABLE IF NOT EXISTS anomalies (
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

-- Market price trends: aggregate price data for analysis
CREATE TABLE IF NOT EXISTS price_trends (
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

-- System danger levels: track hostile encounters
CREATE TABLE IF NOT EXISTS danger_zones (
	system_id TEXT PRIMARY KEY,
	danger_level INTEGER DEFAULT 0, -- 0-10 scale
	hostile_encounters INTEGER DEFAULT 0,
	player_kills INTEGER DEFAULT 0,
	npc_kills INTEGER DEFAULT 0,
	last_incident TEXT,
	last_updated TEXT NOT NULL,
	FOREIGN KEY (system_id) REFERENCES systems(id) ON DELETE CASCADE
);

-- Knowledge exports: metadata for sharing discoveries
CREATE TABLE IF NOT EXISTS knowledge_exports (
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

-- Indexes for enhanced queries
CREATE INDEX IF NOT EXISTS idx_resource_history_poi_resource ON resource_history(poi_id, resource_id, game_tick DESC);
CREATE INDEX IF NOT EXISTS idx_resource_history_tick ON resource_history(game_tick DESC);
CREATE INDEX IF NOT EXISTS idx_anomalies_type_severity ON anomalies(type, severity, detected_at DESC);
CREATE INDEX IF NOT EXISTS idx_anomalies_system ON anomalies(system_id, status);
CREATE INDEX IF NOT EXISTS idx_anomalies_poi ON anomalies(poi_id, status);
CREATE INDEX IF NOT EXISTS idx_price_trends_item_station ON price_trends(item_id, station_id, window_end DESC);
CREATE INDEX IF NOT EXISTS idx_connection_metrics_from ON connection_metrics(from_system, avg_fuel_cost ASC);
CREATE INDEX IF NOT EXISTS idx_danger_zones_level ON danger_zones(danger_level DESC);
`,
		},
		{
			version: 5,
			name:    "add_last_updated",
			sql: `
-- This migration originally added last_updated columns to all tables.
-- The initial schema (v1) now creates these as last_updated_tick directly,
-- so this is a no-op for fresh installs.
`,
		},
		{
			version: 6,
			name:    "add_bases_table",
			sql: `
-- Bases table: stores space stations, outposts, bases, and fortresses
CREATE TABLE IF NOT EXISTS bases (
	id TEXT PRIMARY KEY,
	poi_id TEXT NOT NULL,
	name TEXT NOT NULL,
	description TEXT,
	empire TEXT,
	defense_level INTEGER DEFAULT 0,
	has_drones BOOLEAN DEFAULT 0,
	public_access BOOLEAN DEFAULT 1,
	last_updated_tick INTEGER DEFAULT 0,
	FOREIGN KEY (poi_id) REFERENCES pois(id) ON DELETE CASCADE
);

-- Base services table: stores available services at bases
CREATE TABLE IF NOT EXISTS base_services (
	base_id TEXT NOT NULL,
	service_name TEXT NOT NULL,
	available BOOLEAN DEFAULT 1,
	PRIMARY KEY (base_id, service_name),
	FOREIGN KEY (base_id) REFERENCES bases(id) ON DELETE CASCADE
);

-- Base facilities table: stores facilities at bases
CREATE TABLE IF NOT EXISTS base_facilities (
	base_id TEXT NOT NULL,
	facility_name TEXT NOT NULL,
	category TEXT DEFAULT 'unknown',
	level INTEGER DEFAULT 0,
	last_updated_tick INTEGER DEFAULT 0,
	PRIMARY KEY (base_id, facility_name),
	FOREIGN KEY (base_id) REFERENCES bases(id) ON DELETE CASCADE
);

-- Base market table: stores market items at bases
CREATE TABLE IF NOT EXISTS base_market (
	id TEXT PRIMARY KEY,
	base_id TEXT NOT NULL,
	item_id TEXT NOT NULL,
	price_each REAL NOT NULL,
	quantity INTEGER NOT NULL,
	is_npc BOOLEAN DEFAULT 1,
	last_updated_tick INTEGER DEFAULT 0,
	FOREIGN KEY (base_id) REFERENCES bases(id) ON DELETE CASCADE
);

-- Indexes for efficient queries
CREATE INDEX IF NOT EXISTS idx_bases_poi_id ON bases(poi_id);
CREATE INDEX IF NOT EXISTS idx_base_market_base_id ON base_market(base_id);
CREATE INDEX IF NOT EXISTS idx_base_market_item_id ON base_market(item_id);
CREATE INDEX IF NOT EXISTS idx_base_facilities_category ON base_facilities(category);
`,
		},
		{
			version: 7,
			name:    "add_facility_category_level",
			sql: `
-- This migration is now a no-op since category, level, and last_updated_tick
-- were added directly in migration v6 for new installs
-- Kept for backward compatibility with databases that ran the old migration v6
`,
		},
		{
			version: 8,
			name:    "remove_exploration_tracking",
			sql: `
-- Remove exploration tracking columns that are no longer needed.
-- These columns were added in a migration (v4) that was later removed,
-- so they may not exist on fresh databases. This migration is now a no-op
-- for fresh installs; existing databases that had v4 applied already had
-- these columns removed when they ran the original v8.
`,
		},
		{
			version: 9,
			name:    "schema_alignment",
			sql: `
-- This migration originally renamed columns (pos_x→position_x, faction→empire,
-- last_updated→last_updated_tick) and added description to systems.
-- The initial schema (v1) now uses the correct names directly, so this is
-- a no-op for fresh installs.
`,
		},
		{
			version: 10,
			name:    "add_system_is_stronghold",
			sql: `
-- Add is_stronghold column to systems table for pirate stronghold tracking
ALTER TABLE systems ADD COLUMN is_stronghold BOOLEAN DEFAULT 0;
`,
		},
		{
			version: 11,
			name:    "add_system_security_status",
			sql: `
-- Add security_status column to systems table (e.g. "high_sec", "low_sec", "null_sec")
ALTER TABLE systems ADD COLUMN security_status TEXT DEFAULT '';
`,
		},
		{
			version: 12,
			name:    "catalog_tables",
			sql: `
-- Items catalog
CREATE TABLE IF NOT EXISTS items (
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
);

-- Ship classes catalog
CREATE TABLE IF NOT EXISTS ship_classes (
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
);

-- Ship class build materials (normalized)
CREATE TABLE IF NOT EXISTS ship_class_build_materials (
    ship_class_id TEXT NOT NULL,
    item_id TEXT NOT NULL,
    quantity INTEGER NOT NULL,
    PRIMARY KEY (ship_class_id, item_id),
    FOREIGN KEY (ship_class_id) REFERENCES ship_classes(id) ON DELETE CASCADE
);

-- Skills catalog
CREATE TABLE IF NOT EXISTS skills (
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
);

-- Recipes catalog
CREATE TABLE IF NOT EXISTS recipes (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT,
    category TEXT,
    crafting_time INTEGER DEFAULT 0,
    base_quality INTEGER DEFAULT 0,
    skill_quality_mod INTEGER DEFAULT 0,
    required_skills TEXT DEFAULT '{}',
    last_updated_tick INTEGER DEFAULT 0
);

-- Recipe inputs (normalized)
CREATE TABLE IF NOT EXISTS recipe_inputs (
    recipe_id TEXT NOT NULL,
    item_id TEXT NOT NULL,
    quantity INTEGER NOT NULL,
    PRIMARY KEY (recipe_id, item_id),
    FOREIGN KEY (recipe_id) REFERENCES recipes(id) ON DELETE CASCADE
);

-- Recipe outputs (normalized)
CREATE TABLE IF NOT EXISTS recipe_outputs (
    recipe_id TEXT NOT NULL,
    item_id TEXT NOT NULL,
    quantity INTEGER NOT NULL,
    quality_mod BOOLEAN DEFAULT 0,
    PRIMARY KEY (recipe_id, item_id),
    FOREIGN KEY (recipe_id) REFERENCES recipes(id) ON DELETE CASCADE
);

-- Add distance to connections
ALTER TABLE connections ADD COLUMN distance INTEGER DEFAULT 0;

-- Indexes
CREATE INDEX IF NOT EXISTS idx_items_category ON items(category);
CREATE INDEX IF NOT EXISTS idx_ship_classes_class ON ship_classes(class);
CREATE INDEX IF NOT EXISTS idx_ship_classes_faction ON ship_classes(faction);
CREATE INDEX IF NOT EXISTS idx_skills_category ON skills(category);
CREATE INDEX IF NOT EXISTS idx_recipes_category ON recipes(category);
`,
		},
		{
			version: 13,
			name:    "player_state_tables",
			sql: `
-- Players table
CREATE TABLE IF NOT EXISTS players (
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

-- Player stats (1:1 with players)
CREATE TABLE IF NOT EXISTS player_stats (
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

-- Player skills (player's progress in each skill)
CREATE TABLE IF NOT EXISTS player_skills (
    player_id TEXT NOT NULL,
    skill_id TEXT NOT NULL,
    level INTEGER DEFAULT 0,
    current_xp REAL DEFAULT 0,
    PRIMARY KEY (player_id, skill_id),
    FOREIGN KEY (player_id) REFERENCES players(id) ON DELETE CASCADE
);

-- Ships (player-owned ships)
CREATE TABLE IF NOT EXISTS ships (
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
    FOREIGN KEY (class_id) REFERENCES ship_classes(id)
);

-- Ship cargo
CREATE TABLE IF NOT EXISTS ship_cargo (
    ship_id TEXT NOT NULL,
    item_id TEXT NOT NULL,
    quantity REAL NOT NULL,
    PRIMARY KEY (ship_id, item_id),
    FOREIGN KEY (ship_id) REFERENCES ships(id) ON DELETE CASCADE
);

-- Ship modules (fitted modules on a ship)
CREATE TABLE IF NOT EXISTS ship_modules (
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
    FOREIGN KEY (ship_id) REFERENCES ships(id) ON DELETE CASCADE
);

-- Mission templates (available missions from mission boards)
CREATE TABLE IF NOT EXISTS mission_templates (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    description TEXT,
    type TEXT,
    difficulty INTEGER DEFAULT 0,
    base_id TEXT,
    giver_name TEXT,
    giver_title TEXT,
    dialog_offer TEXT,
    chain_next TEXT,
    expires_in_ticks INTEGER DEFAULT 0,
    rewards_credits INTEGER DEFAULT 0,
    rewards_skill_xp TEXT DEFAULT '{}',
    requirements TEXT DEFAULT '{}',
    last_updated_tick INTEGER DEFAULT 0
);

-- Mission objectives
CREATE TABLE IF NOT EXISTS mission_objectives (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    mission_id TEXT NOT NULL,
    type TEXT NOT NULL,
    description TEXT,
    sort_order INTEGER DEFAULT 0,
    FOREIGN KEY (mission_id) REFERENCES mission_templates(id) ON DELETE CASCADE
);

-- Indexes
CREATE INDEX IF NOT EXISTS idx_players_empire ON players(empire);
CREATE INDEX IF NOT EXISTS idx_ships_owner ON ships(owner_id);
CREATE INDEX IF NOT EXISTS idx_ships_class ON ships(class_id);
CREATE INDEX IF NOT EXISTS idx_ship_modules_ship ON ship_modules(ship_id);
CREATE INDEX IF NOT EXISTS idx_mission_templates_type ON mission_templates(type);
CREATE INDEX IF NOT EXISTS idx_mission_templates_base ON mission_templates(base_id);
`,
		},
		{
			version: 14,
			name:    "add_market_analyses_table",
			sql: `
-- Market analyses table: stores AI-generated trading insights
CREATE TABLE IF NOT EXISTS market_analyses (
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

CREATE INDEX IF NOT EXISTS idx_market_analyses_system_station ON market_analyses(system_id, station_id, captured_at DESC);
CREATE INDEX IF NOT EXISTS idx_market_analyses_captured ON market_analyses(captured_at DESC);
`,
		},
		{
			version: 15,
			name:    "item_module_detail_tables",
			sql: `
-- Ship modules: common base for all fitted modules
CREATE TABLE IF NOT EXISTS item_modules (
    item_id     TEXT PRIMARY KEY REFERENCES items(id),
    type        TEXT NOT NULL,
    type_id     TEXT NOT NULL,
    cpu_usage   INTEGER NOT NULL DEFAULT 0,
    power_usage INTEGER NOT NULL DEFAULT 0,
    hidden      BOOLEAN NOT NULL DEFAULT 0,
    special     TEXT
);
CREATE INDEX IF NOT EXISTS idx_item_modules_type ON item_modules(type);
CREATE INDEX IF NOT EXISTS idx_item_modules_type_id ON item_modules(type_id);

-- Weapons
CREATE TABLE IF NOT EXISTS item_weapons (
    item_id     TEXT PRIMARY KEY REFERENCES item_modules(item_id),
    damage      INTEGER NOT NULL DEFAULT 0,
    damage_type TEXT NOT NULL,
    range       INTEGER,
    reach       INTEGER,
    cooldown    INTEGER NOT NULL DEFAULT 0,
    ammo_type   TEXT,
    magazine_size INTEGER
);
CREATE INDEX IF NOT EXISTS idx_item_weapons_damage_type ON item_weapons(damage_type);
CREATE INDEX IF NOT EXISTS idx_item_weapons_ammo_type ON item_weapons(ammo_type);

-- Defense modules
CREATE TABLE IF NOT EXISTS item_defenses (
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

-- Mining modules
CREATE TABLE IF NOT EXISTS item_mining (
    item_id      TEXT PRIMARY KEY REFERENCES item_modules(item_id),
    mining_power INTEGER NOT NULL DEFAULT 0,
    mining_range INTEGER NOT NULL DEFAULT 0
);

-- Utility modules
CREATE TABLE IF NOT EXISTS item_utilities (
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
);

-- Consumable effects (non-ammo)
CREATE TABLE IF NOT EXISTS item_consumable_effects (
    item_id     TEXT PRIMARY KEY REFERENCES items(id),
    effect_type TEXT NOT NULL,
    subtype     TEXT,
    amount      INTEGER,
    duration    INTEGER,
    stat        TEXT
);
CREATE INDEX IF NOT EXISTS idx_item_consumable_effects_type ON item_consumable_effects(effect_type);

-- Ammunition types
CREATE TABLE IF NOT EXISTS item_ammo (
    item_id    TEXT PRIMARY KEY REFERENCES items(id),
    ammo_type  TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_item_ammo_type ON item_ammo(ammo_type);
`,
		},
	}
}

// runMigrations executes all pending migrations
func runMigrations(db *sql.DB) error {
	// Create migrations table if it doesn't exist
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at TEXT NOT NULL
		)
	`); err != nil {
		return fmt.Errorf("failed to create migrations table: %w", err)
	}

	// Get current migration version
	var currentVersion int
	row := db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_migrations")
	if err := row.Scan(&currentVersion); err != nil {
		return fmt.Errorf("failed to get current migration version: %w", err)
	}

	// Run pending migrations
	migrations := migrations()
	for _, m := range migrations {
		if m.version <= currentVersion {
			continue // Already applied
		}

		// Run migration in a transaction
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("failed to begin transaction for migration %d: %w", m.version, err)
		}

		// Execute migration SQL
		if _, err := tx.Exec(m.sql); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("failed to apply migration %d (%s): %w", m.version, m.name, err)
		}

		// Record migration
		if _, err := tx.Exec(
			"INSERT INTO schema_migrations (version, applied_at) VALUES (?, datetime('now'))",
			m.version,
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("failed to record migration %d: %w", m.version, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("failed to commit migration %d: %w", m.version, err)
		}
	}

	return nil
}
