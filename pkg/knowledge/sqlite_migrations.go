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
-- Add last_updated column to track data freshness
-- Note: This will be renamed to last_updated_tick in migration v9
ALTER TABLE systems ADD COLUMN last_updated INTEGER DEFAULT 0;
ALTER TABLE connections ADD COLUMN last_updated INTEGER DEFAULT 0;
ALTER TABLE pois ADD COLUMN last_updated INTEGER DEFAULT 0;
ALTER TABLE poi_resources ADD COLUMN last_updated INTEGER DEFAULT 0;
ALTER TABLE experiences ADD COLUMN last_updated INTEGER DEFAULT 0;
ALTER TABLE agents ADD COLUMN last_updated INTEGER DEFAULT 0;
ALTER TABLE market_snapshots ADD COLUMN last_updated INTEGER DEFAULT 0;
ALTER TABLE market_listings ADD COLUMN last_updated INTEGER DEFAULT 0;
ALTER TABLE ship_listings ADD COLUMN last_updated INTEGER DEFAULT 0;
ALTER TABLE resource_history ADD COLUMN last_updated INTEGER DEFAULT 0;
ALTER TABLE connection_metrics ADD COLUMN last_updated INTEGER DEFAULT 0;
ALTER TABLE anomalies ADD COLUMN last_updated INTEGER DEFAULT 0;
ALTER TABLE price_trends ADD COLUMN last_updated INTEGER DEFAULT 0;
ALTER TABLE knowledge_exports ADD COLUMN last_updated INTEGER DEFAULT 0;
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
-- Remove exploration tracking columns that are no longer needed
-- These were used for tracking agent exploration but are causing issues

-- Drop columns from systems table
ALTER TABLE systems DROP COLUMN visit_count;
ALTER TABLE systems DROP COLUMN last_visited;
ALTER TABLE systems DROP COLUMN discovered_by;

-- Drop discovered_by from pois table
ALTER TABLE pois DROP COLUMN discovered_by;

-- Drop discovered_by from bases table
ALTER TABLE bases DROP COLUMN discovered_by;
`,
		},
		{
			version: 9,
			name:    "schema_alignment",
			sql: `
-- Align database schema with game server API structure
-- This migration renames columns to match server terminology and structure

-- Rename position columns to match server API (position.x/y)
ALTER TABLE systems RENAME COLUMN pos_x TO position_x;
ALTER TABLE systems RENAME COLUMN pos_y TO position_y;
ALTER TABLE systems DROP COLUMN pos_z;

ALTER TABLE pois RENAME COLUMN pos_x TO position_x;
ALTER TABLE pois RENAME COLUMN pos_y TO position_y;

-- Rename faction to empire for consistency with server API and bases table
ALTER TABLE systems RENAME COLUMN faction TO empire;
ALTER TABLE agents RENAME COLUMN faction TO empire;

-- Add description to systems table (present in server API)
ALTER TABLE systems ADD COLUMN description TEXT;

-- Rename last_updated to last_updated_tick for clarity (game tick, not timestamp)
ALTER TABLE systems RENAME COLUMN last_updated TO last_updated_tick;
ALTER TABLE connections RENAME COLUMN last_updated TO last_updated_tick;
ALTER TABLE pois RENAME COLUMN last_updated TO last_updated_tick;
ALTER TABLE poi_resources RENAME COLUMN last_updated TO last_updated_tick;
ALTER TABLE experiences RENAME COLUMN last_updated TO last_updated_tick;
ALTER TABLE agents RENAME COLUMN last_updated TO last_updated_tick;
ALTER TABLE market_snapshots RENAME COLUMN last_updated TO last_updated_tick;
ALTER TABLE market_listings RENAME COLUMN last_updated TO last_updated_tick;
ALTER TABLE ship_listings RENAME COLUMN last_updated TO last_updated_tick;
ALTER TABLE resource_history RENAME COLUMN last_updated TO last_updated_tick;
ALTER TABLE connection_metrics RENAME COLUMN last_updated TO last_updated_tick;
ALTER TABLE anomalies RENAME COLUMN last_updated TO last_updated_tick;
ALTER TABLE price_trends RENAME COLUMN last_updated TO last_updated_tick;
ALTER TABLE knowledge_exports RENAME COLUMN last_updated TO last_updated_tick;
ALTER TABLE bases RENAME COLUMN last_updated TO last_updated_tick;
ALTER TABLE base_facilities RENAME COLUMN last_updated TO last_updated_tick;
ALTER TABLE base_market RENAME COLUMN last_updated TO last_updated_tick;

-- Note: danger_zones.last_updated remains as TEXT (timestamp) not INTEGER (tick)
-- This tracks "last incident time" not "when we updated this record"
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
