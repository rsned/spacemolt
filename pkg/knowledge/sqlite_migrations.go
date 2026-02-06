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
	pos_x REAL NOT NULL,
	pos_y REAL NOT NULL,
	pos_z REAL NOT NULL,
	security_level TEXT,
	faction TEXT,
	visit_count INTEGER DEFAULT 0,
	last_visited TEXT,
	discovered_by TEXT
);

-- Connections table: stores system-to-system connections
CREATE TABLE IF NOT EXISTS connections (
	from_system TEXT NOT NULL,
	to_system TEXT NOT NULL,
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
	pos_x REAL NOT NULL,
	pos_y REAL NOT NULL,
	discovered_by TEXT,
	base_id TEXT,
	FOREIGN KEY (system_id) REFERENCES systems(id) ON DELETE CASCADE
);

-- POI resources table: stores resource information for POIs
CREATE TABLE IF NOT EXISTS poi_resources (
	poi_id TEXT NOT NULL,
	resource_id TEXT NOT NULL,
	richness REAL NOT NULL,
	remaining REAL NOT NULL,
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
	location TEXT
);

-- Agents table: stores agent metadata
CREATE TABLE IF NOT EXISTS agents (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	role TEXT NOT NULL,
	faction TEXT,
	status TEXT DEFAULT 'active'
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
	FOREIGN KEY (snapshot_id) REFERENCES market_snapshots(id) ON DELETE CASCADE
);

-- Indexes for efficient queries
CREATE INDEX IF NOT EXISTS idx_market_snapshots_system_station ON market_snapshots(system_id, station_id, captured_at DESC);
CREATE INDEX IF NOT EXISTS idx_market_snapshots_captured_at ON market_snapshots(captured_at DESC);
CREATE INDEX IF NOT EXISTS idx_market_listings_snapshot_id ON market_listings(snapshot_id);
CREATE INDEX IF NOT EXISTS idx_market_listings_item_id ON market_listings(item_id);
CREATE INDEX IF NOT EXISTS idx_market_listings_item_type ON market_listings(item_type);
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
