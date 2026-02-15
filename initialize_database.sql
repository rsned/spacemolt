-- SpaceMolt Knowledge Base Database Schema
-- SQLite 3 Compatible
--
-- This file contains the complete database schema for the SpaceMolt agent knowledge base.
-- Use this to initialize a fresh database:
--   sqlite3 spacemolt-knowledge.db < initialize_database.sql
--
-- Schema Version: 4
-- Last Updated: 2025-02-11
--
-- Migration v3->v4: Changed security_level (TEXT) to police_level (INTEGER)

-- ============================================================================
-- CORE TABLES
-- ============================================================================

-- Systems table: stores solar system information
CREATE TABLE IF NOT EXISTS systems (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	pos_x REAL NOT NULL,
	pos_y REAL NOT NULL,
	pos_z REAL NOT NULL,
	police_level INTEGER DEFAULT 0,
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

-- POIs table: stores points of interest (stations, asteroid belts, planets, etc.)
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

-- Experiences table: stores agent experiences for learning
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

-- ============================================================================
-- MARKET DATA TABLES
-- ============================================================================

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
	FOREIGN KEY (station_id) REFERENCES pois(id) ON DELETE CASCADE
);

-- ============================================================================
-- ANALYTICS TABLES
-- ============================================================================

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
	export_data TEXT -- JSON snapshot
);

-- Schema migrations tracking
CREATE TABLE IF NOT EXISTS schema_migrations (
	version INTEGER PRIMARY KEY,
	applied_at TEXT NOT NULL
);

-- ============================================================================
-- INDEXES FOR PERFORMANCE
-- ============================================================================

-- Core tables indexes
CREATE INDEX IF NOT EXISTS idx_experiences_agent_id ON experiences(agent_id, time DESC);
CREATE INDEX IF NOT EXISTS idx_pois_system_id ON pois(system_id);
CREATE INDEX IF NOT EXISTS idx_connections_from ON connections(from_system);
CREATE INDEX IF NOT EXISTS idx_connections_to ON connections(to_system);

-- Market data indexes
CREATE INDEX IF NOT EXISTS idx_market_snapshots_system_station ON market_snapshots(system_id, station_id, captured_at DESC);
CREATE INDEX IF NOT EXISTS idx_market_snapshots_captured_at ON market_snapshots(captured_at DESC);
CREATE INDEX IF NOT EXISTS idx_market_listings_snapshot_id ON market_listings(snapshot_id);
CREATE INDEX IF NOT EXISTS idx_market_listings_item_id ON market_listings(item_id);
CREATE INDEX IF NOT EXISTS idx_market_listings_item_type ON market_listings(item_type);

-- Ship listings indexes
CREATE INDEX IF NOT EXISTS idx_ship_listings_system_station ON ship_listings(system_id, station_id, captured_at DESC);
CREATE INDEX IF NOT EXISTS idx_ship_listings_captured_at ON ship_listings(captured_at DESC);

-- Analytics indexes
CREATE INDEX IF NOT EXISTS idx_resource_history_poi_resource ON resource_history(poi_id, resource_id, game_tick DESC);
CREATE INDEX IF NOT EXISTS idx_resource_history_tick ON resource_history(game_tick DESC);
CREATE INDEX IF NOT EXISTS idx_anomalies_type_severity ON anomalies(type, severity, detected_at DESC);
CREATE INDEX IF NOT EXISTS idx_anomalies_system ON anomalies(system_id, status);
CREATE INDEX IF NOT EXISTS idx_anomalies_poi ON anomalies(poi_id, status);
CREATE INDEX IF NOT EXISTS idx_price_trends_item_station ON price_trends(item_id, station_id, window_end DESC);
CREATE INDEX IF NOT EXISTS idx_connection_metrics_from ON connection_metrics(from_system, avg_fuel_cost ASC);
CREATE INDEX IF NOT EXISTS idx_danger_zones_level ON danger_zones(danger_level DESC);

-- ============================================================================
-- INITIAL MIGRATION RECORD
-- ============================================================================

-- Record that schema version 4 has been applied
INSERT OR IGNORE INTO schema_migrations (version, applied_at) VALUES (4, datetime('now'));
