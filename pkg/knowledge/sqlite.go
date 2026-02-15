package knowledge

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// Config holds configuration for SQLite knowledge base
type Config struct {
	DBPath       string        // Path to SQLite database file
	WAL          bool          // Enable WAL mode for better concurrency
	MaxOpenConns int           // Maximum open connections
	MaxIdleConns int           // Maximum idle connections
	BusyTimeout  time.Duration // Timeout for busy database
}

// DefaultConfig returns a config with sensible defaults
func DefaultConfig() Config {
	return Config{
		DBPath:       "spacemolt-knowledge.db",
		WAL:          true,
		MaxOpenConns: 25,
		MaxIdleConns: 5,
		BusyTimeout:  5 * time.Second,
	}
}

// SQLiteKB implements Base interface using SQLite backend
type SQLiteKB struct {
	db *sql.DB
}

// NewSQLiteKB creates a new SQLite-backed knowledge base
func NewSQLiteKB(config Config) (*SQLiteKB, error) {
	if config.DBPath == "" {
		config.DBPath = DefaultConfig().DBPath
	}
	if config.MaxOpenConns == 0 {
		config.MaxOpenConns = DefaultConfig().MaxOpenConns
	}
	if config.MaxIdleConns == 0 {
		config.MaxIdleConns = DefaultConfig().MaxIdleConns
	}
	if config.BusyTimeout == 0 {
		config.BusyTimeout = DefaultConfig().BusyTimeout
	}

	// Open database connection
	db, err := sql.Open("sqlite", config.DBPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(config.MaxOpenConns)
	db.SetMaxIdleConns(config.MaxIdleConns)

	// Set busy timeout
	if _, err := db.Exec(fmt.Sprintf("PRAGMA busy_timeout = %d", int(config.BusyTimeout.Milliseconds()))); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to set busy timeout: %w", err)
	}

	// Enable WAL mode for better concurrency
	if config.WAL {
		if _, err := db.Exec("PRAGMA journal_mode = WAL"); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("failed to enable WAL mode: %w", err)
		}
	}

	// Run migrations
	if err := runMigrations(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return &SQLiteKB{db: db}, nil
}

// Close closes the database connection
func (kb *SQLiteKB) Close() error {
	if kb.db != nil {
		return kb.db.Close()
	}
	return nil
}

// RememberSystem stores or updates system knowledge
func (kb *SQLiteKB) RememberSystem(ctx context.Context, sys System) error {
	tx, err := kb.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Insert or update system
	// Use INSERT OR REPLACE to handle both new and existing systems
	_, err = tx.ExecContext(ctx, `
		INSERT INTO systems (id, name, pos_x, pos_y, pos_z, police_level, faction, visit_count, last_visited, discovered_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, COALESCE((SELECT visit_count FROM systems WHERE id = ?), 0) + 1, datetime('now'), ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			pos_x = excluded.pos_x,
			pos_y = excluded.pos_y,
			pos_z = excluded.pos_z,
			police_level = excluded.police_level,
			faction = excluded.faction,
			visit_count = visit_count + 1,
			last_visited = datetime('now'),
			discovered_by = excluded.discovered_by
	`, sys.ID, sys.Name, sys.Position.X, sys.Position.Y, sys.Position.Z,
		sys.PoliceLevel, sys.Faction, sys.ID, sys.DiscoveredBy)
	if err != nil {
		return fmt.Errorf("failed to upsert system: %w", err)
	}

	// Store connections
	for _, connID := range sys.Connections {
		if _, err := tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO connections (from_system, to_system)
			VALUES (?, ?)
		`, sys.ID, connID); err != nil {
			return fmt.Errorf("failed to store connection %s -> %s: %w", sys.ID, connID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// GetSystem retrieves a system by ID
func (kb *SQLiteKB) GetSystem(ctx context.Context, systemID string) (*System, error) {
	var sys System
	var lastVisited sql.NullString

	err := kb.db.QueryRowContext(ctx, `
		SELECT id, name, pos_x, pos_y, pos_z, police_level, faction, visit_count, last_visited, discovered_by
		FROM systems
		WHERE id = ?
	`, systemID).Scan(
		&sys.ID, &sys.Name, &sys.Position.X, &sys.Position.Y, &sys.Position.Z,
		&sys.PoliceLevel, &sys.Faction, &sys.VisitCount, &lastVisited, &sys.DiscoveredBy,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil // System not found
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query system: %w", err)
	}

	if lastVisited.Valid {
		sys.LastVisited = lastVisited.String
	}

	// Load connections
	rows, err := kb.db.QueryContext(ctx, `
		SELECT to_system FROM connections WHERE from_system = ?
	`, systemID)
	if err != nil {
		return nil, fmt.Errorf("failed to query connections: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var conn string
		if err := rows.Scan(&conn); err != nil {
			return nil, fmt.Errorf("failed to scan connection: %w", err)
		}
		sys.Connections = append(sys.Connections, conn)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating connections: %w", err)
	}

	return &sys, nil
}

// GetUnknownConnections finds unexplored connections from a system
func (kb *SQLiteKB) GetUnknownConnections(ctx context.Context, systemID string) ([]string, error) {
	// Find connections where the destination has never been visited (visit_count = 0 or NULL)
	rows, err := kb.db.QueryContext(ctx, `
		SELECT c.to_system
		FROM connections c
		LEFT JOIN systems s ON c.to_system = s.id
		WHERE c.from_system = ? AND (s.visit_count IS NULL OR s.visit_count = 0)
	`, systemID)
	if err != nil {
		return nil, fmt.Errorf("failed to query unknown connections: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var unknown []string
	for rows.Next() {
		var conn string
		if err := rows.Scan(&conn); err != nil {
			return nil, fmt.Errorf("failed to scan connection: %w", err)
		}
		unknown = append(unknown, conn)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating connections: %w", err)
	}

	return unknown, nil
}

// RememberConnection stores a system connection (with deduplication)
func (kb *SQLiteKB) RememberConnection(ctx context.Context, fromSystem, toSystem string) error {
	_, err := kb.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO connections (from_system, to_system)
		VALUES (?, ?)
	`, fromSystem, toSystem)
	if err != nil {
		return fmt.Errorf("failed to remember connection: %w", err)
	}
	return nil
}

// RememberPOI stores or updates POI knowledge
func (kb *SQLiteKB) RememberPOI(ctx context.Context, poi POI) error {
	tx, err := kb.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Insert or update POI (base_id is left as NULL for now since POI struct doesn't have it)
	_, err = tx.ExecContext(ctx, `
		INSERT INTO pois (id, system_id, name, type, description, pos_x, pos_y, discovered_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			system_id = excluded.system_id,
			name = excluded.name,
			type = excluded.type,
			description = excluded.description,
			pos_x = excluded.pos_x,
			pos_y = excluded.pos_y,
			discovered_by = excluded.discovered_by
	`, poi.ID, poi.SystemID, poi.Name, poi.Type, poi.Description,
		poi.Position.X, poi.Position.Y, poi.DiscoveredBy)
	if err != nil {
		return fmt.Errorf("failed to upsert POI: %w", err)
	}

	// Delete existing resources for this POI
	_, err = tx.ExecContext(ctx, `DELETE FROM poi_resources WHERE poi_id = ?`, poi.ID)
	if err != nil {
		return fmt.Errorf("failed to delete old POI resources: %w", err)
	}

	// Insert resources
	for _, res := range poi.Resources {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO poi_resources (poi_id, resource_id, richness, remaining)
			VALUES (?, ?, ?, ?)
		`, poi.ID, res.ResourceID, res.Richness, res.Remaining)
		if err != nil {
			return fmt.Errorf("failed to insert POI resource: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// GetPOIs retrieves all POIs in a system
func (kb *SQLiteKB) GetPOIs(ctx context.Context, systemID string) ([]POI, error) {
	rows, err := kb.db.QueryContext(ctx, `
		SELECT id, system_id, name, type, description, pos_x, pos_y, discovered_by
		FROM pois
		WHERE system_id = ?
		ORDER BY name
	`, systemID)
	if err != nil {
		return nil, fmt.Errorf("failed to query POIs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var pois []POI
	for rows.Next() {
		var poi POI
		var description string
		err := rows.Scan(
			&poi.ID,
			&poi.SystemID,
			&poi.Name,
			&poi.Type,
			&description,
			&poi.Position.X,
			&poi.Position.Y,
			&poi.DiscoveredBy,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan POI: %w", err)
		}
		poi.Description = description
		pois = append(pois, poi)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating POIs: %w", err)
	}

	return pois, nil
}

// AddExperience logs an agent experience
func (kb *SQLiteKB) AddExperience(ctx context.Context, agentID, expType, description, outcome, location string) error {
	_, err := kb.db.ExecContext(ctx, `
		INSERT INTO experiences (agent_id, time, type, description, outcome, location)
		VALUES (?, datetime('now'), ?, ?, ?, ?)
	`, agentID, expType, description, outcome, location)
	if err != nil {
		return fmt.Errorf("failed to add experience: %w", err)
	}

	// Clean up old experiences (keep only last 100 per agent)
	_, err = kb.db.ExecContext(ctx, `
		DELETE FROM experiences
		WHERE id IN (
			SELECT id FROM experiences
			WHERE agent_id = ?
			ORDER BY time DESC
			LIMIT -1 OFFSET 100
		)
	`, agentID)
	if err != nil {
		return fmt.Errorf("failed to clean up old experiences: %w", err)
	}

	return nil
}

// GetRecentExperiences retrieves recent experiences for an agent
func (kb *SQLiteKB) GetRecentExperiences(ctx context.Context, agentID string, limit int) ([]Experience, error) {
	rows, err := kb.db.QueryContext(ctx, `
		SELECT time, type, description, outcome, location
		FROM experiences
		WHERE agent_id = ?
		ORDER BY time DESC
		LIMIT ?
	`, agentID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query experiences: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var experiences []Experience
	for rows.Next() {
		var exp Experience
		if err := rows.Scan(&exp.Time, &exp.Type, &exp.Description, &exp.Outcome, &exp.Location); err != nil {
			return nil, fmt.Errorf("failed to scan experience: %w", err)
		}
		experiences = append(experiences, exp)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating experiences: %w", err)
	}

	return experiences, nil
}

// RegisterAgent registers an agent in the knowledge base
func (kb *SQLiteKB) RegisterAgent(ctx context.Context, agentID, name, role, faction string, personality []byte) error {
	_, err := kb.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO agents (id, name, role, faction, status)
		VALUES (?, ?, ?, ?, 'active')
	`, agentID, name, role, faction)
	if err != nil {
		return fmt.Errorf("failed to register agent: %w", err)
	}
	return nil
}

// GetSystems returns all known systems
func (kb *SQLiteKB) GetSystems() []System {
	// Query all systems
	rows, err := kb.db.Query(`
		SELECT id, name, pos_x, pos_y, pos_z, police_level, faction, visit_count, last_visited, discovered_by
		FROM systems
	`)
	if err != nil {
		return nil
	}
	defer func() { _ = rows.Close() }()

	var systems []System
	for rows.Next() {
		var sys System
		var lastVisited sql.NullString

		if err := rows.Scan(
			&sys.ID, &sys.Name, &sys.Position.X, &sys.Position.Y, &sys.Position.Z,
			&sys.PoliceLevel, &sys.Faction, &sys.VisitCount, &lastVisited, &sys.DiscoveredBy,
		); err != nil {
			continue
		}

		if lastVisited.Valid {
			sys.LastVisited = lastVisited.String
		}

		systems = append(systems, sys)
	}

	if err := rows.Err(); err != nil {
		return nil
	}

	// Load all connections in a single query and map them
	connRows, err := kb.db.Query(`
		SELECT from_system, to_system FROM connections
	`)
	if err != nil {
		return systems // Return systems without connections if query fails
	}
	defer func() { _ = connRows.Close() }()

	// Create a map of connections
	connMap := make(map[string][]string)
	for connRows.Next() {
		var from, to string
		if err := connRows.Scan(&from, &to); err != nil {
			continue
		}
		connMap[from] = append(connMap[from], to)
	}

	// Add connections to systems
	for i := range systems {
		systems[i].Connections = connMap[systems[i].ID]
	}

	return systems
}

// StoreMarketSnapshot stores a market snapshot with its listings
func (kb *SQLiteKB) StoreMarketSnapshot(ctx context.Context, snapshot MarketSnapshot, agentID string) error {
	tx, err := kb.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Insert snapshot
	result, err := tx.ExecContext(ctx, `
		INSERT INTO market_snapshots (system_id, system_name, station_id, station_name, game_tick, captured_at, agent_id)
		VALUES (?, ?, ?, ?, ?, datetime('now'), ?)
	`, snapshot.SystemID, snapshot.SystemName, snapshot.StationID, snapshot.StationName, snapshot.GameTick, agentID)
	if err != nil {
		return fmt.Errorf("failed to insert market snapshot: %w", err)
	}

	snapshotID, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get snapshot ID: %w", err)
	}

	// Insert listings
	for _, listing := range snapshot.Listings {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO market_listings (snapshot_id, item_id, item_type, quantity, price_per_unit, total_price, listing_type, listed_by)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`, snapshotID, listing.ItemID, listing.ItemType, listing.Quantity, listing.PricePerUnit, listing.TotalPrice, listing.Type, listing.ListedBy)
		if err != nil {
			return fmt.Errorf("failed to insert market listing: %w", err)
		}
	}

	return tx.Commit()
}

// GetMarketSnapshots retrieves historical market snapshots
func (kb *SQLiteKB) GetMarketSnapshots(ctx context.Context, systemID, stationID string, limit int) ([]MarketSnapshot, error) {
	query := `
		SELECT id, system_id, system_name, station_id, station_name, game_tick, captured_at, agent_id
		FROM market_snapshots
		WHERE system_id = ? AND station_id = ?
		ORDER BY captured_at DESC
		LIMIT ?
	`

	rows, err := kb.db.QueryContext(ctx, query, systemID, stationID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query market snapshots: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var snapshots []MarketSnapshot
	for rows.Next() {
		var snap MarketSnapshot
		var id int
		var capturedAt string
		var agentID sql.NullString

		err := rows.Scan(&id, &snap.SystemID, &snap.SystemName, &snap.StationID, &snap.StationName, &snap.GameTick, &capturedAt, &agentID)
		if err != nil {
			return nil, fmt.Errorf("failed to scan snapshot: %w", err)
		}

		snap.CapturedAt, err = time.Parse(time.RFC3339, capturedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to parse captured_at: %w", err)
		}

		// Get listings for this snapshot
		listings, err := kb.getMarketListings(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("failed to get listings for snapshot %d: %w", id, err)
		}
		snap.Listings = listings

		snapshots = append(snapshots, snap)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating snapshots: %w", err)
	}

	return snapshots, nil
}

// GetLatestMarketSnapshot retrieves the most recent market snapshot
func (kb *SQLiteKB) GetLatestMarketSnapshot(ctx context.Context, systemID, stationID string) (*MarketSnapshot, error) {
	query := `
		SELECT id, system_id, system_name, station_id, station_name, game_tick, captured_at, agent_id
		FROM market_snapshots
		WHERE system_id = ? AND station_id = ?
		ORDER BY captured_at DESC
		LIMIT 1
	`

	var snap MarketSnapshot
	var id int
	var capturedAt string
	var agentID sql.NullString

	err := kb.db.QueryRowContext(ctx, query, systemID, stationID).Scan(
		&id, &snap.SystemID, &snap.SystemName, &snap.StationID, &snap.StationName, &snap.GameTick, &capturedAt, &agentID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil // No snapshot found
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query latest snapshot: %w", err)
	}

	snap.CapturedAt, err = time.Parse(time.RFC3339, capturedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to parse captured_at: %w", err)
	}

	// Get listings for this snapshot
	listings, err := kb.getMarketListings(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get listings for snapshot %d: %w", id, err)
	}
	snap.Listings = listings

	return &snap, nil
}

// GetMarketItems retrieves unique item IDs optionally filtered by type
func (kb *SQLiteKB) GetMarketItems(ctx context.Context, itemType string) ([]string, error) {
	query := `
		SELECT DISTINCT item_id
		FROM market_listings
	`
	args := []any{}

	if itemType != "" {
		query += " WHERE item_type = ?"
		args = append(args, itemType)
	}

	query += " ORDER BY item_id"

	rows, err := kb.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query market items: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var items []string
	for rows.Next() {
		var itemID string
		if err := rows.Scan(&itemID); err != nil {
			return nil, fmt.Errorf("failed to scan item: %w", err)
		}
		items = append(items, itemID)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating items: %w", err)
	}

	return items, nil
}

// getMarketListings retrieves all listings for a snapshot
func (kb *SQLiteKB) getMarketListings(ctx context.Context, snapshotID int) ([]MarketListing, error) {
	query := `
		SELECT item_id, item_type, quantity, price_per_unit, total_price, listing_type, listed_by
		FROM market_listings
		WHERE snapshot_id = ?
		ORDER BY item_type, item_id
	`

	rows, err := kb.db.QueryContext(ctx, query, snapshotID)
	if err != nil {
		return nil, fmt.Errorf("failed to query listings: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var listings []MarketListing
	for rows.Next() {
		var listing MarketListing
		var listedBy sql.NullString

		err := rows.Scan(&listing.ItemID, &listing.ItemType, &listing.Quantity, &listing.PricePerUnit, &listing.TotalPrice, &listing.Type, &listedBy)
		if err != nil {
			return nil, fmt.Errorf("failed to scan listing: %w", err)
		}

		if listedBy.Valid {
			listing.ListedBy = listedBy.String
		}

		listings = append(listings, listing)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating listings: %w", err)
	}

	return listings, nil
}

// HasMarketSnapshotToday checks if a market snapshot was captured today for a station
func (kb *SQLiteKB) HasMarketSnapshotToday(ctx context.Context, systemID, stationID string) (bool, error) {
	var count int
	err := kb.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM market_snapshots
		WHERE system_id = ? AND station_id = ?
		AND DATE(captured_at) = DATE('now')
	`, systemID, stationID).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check for today's snapshot: %w", err)
	}
	return count > 0, nil
}

// StoreShipListings stores ship listings at a station
func (kb *SQLiteKB) StoreShipListings(ctx context.Context, listings ShipListings, agentID string) error {
	tx, err := kb.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, ship := range listings.Listings {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO ship_listings (system_id, system_name, station_id, station_name,
				ship_class, ship_name, base_price, description, cargo_space, module_slots,
				utility_slots, weapon_slots, game_tick, captured_at, agent_id)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'), ?)
		`, listings.SystemID, listings.SystemName, listings.StationID, listings.StationName,
			ship.ShipClass, ship.ShipName, ship.BasePrice, ship.Description, ship.CargoSpace,
			ship.ModuleSlots, ship.UtilitySlots, ship.WeaponSlots, listings.GameTick, agentID)
		if err != nil {
			return fmt.Errorf("failed to insert ship listing: %w", err)
		}
	}

	return tx.Commit()
}

// GetShipListings retrieves historical ship listings
func (kb *SQLiteKB) GetShipListings(ctx context.Context, systemID, stationID string, limit int) ([]ShipListings, error) {
	query := `
		SELECT DISTINCT system_id, system_name, station_id, station_name, game_tick, captured_at
		FROM ship_listings
		WHERE system_id = ? AND station_id = ?
		ORDER BY captured_at DESC
		LIMIT ?
	`

	rows, err := kb.db.QueryContext(ctx, query, systemID, stationID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query ship listings: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var snapshots []ShipListings
	for rows.Next() {
		var snap ShipListings
		var capturedAt string

		err := rows.Scan(&snap.SystemID, &snap.SystemName, &snap.StationID, &snap.StationName, &snap.GameTick, &capturedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan snapshot: %w", err)
		}

		snap.CapturedAt, err = time.Parse(time.RFC3339, capturedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to parse captured_at: %w", err)
		}

		// Get listings for this snapshot
		listings, err := kb.getShipListingsForSnapshot(ctx, snap.SystemID, snap.StationID, snap.CapturedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to get ship listings: %w", err)
		}
		snap.Listings = listings

		snapshots = append(snapshots, snap)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating snapshots: %w", err)
	}

	return snapshots, nil
}

// GetLatestShipListings retrieves the most recent ship listings
func (kb *SQLiteKB) GetLatestShipListings(ctx context.Context, systemID, stationID string) (*ShipListings, error) {
	query := `
		SELECT system_id, system_name, station_id, station_name, game_tick, MAX(captured_at) as captured_at
		FROM ship_listings
		WHERE system_id = ? AND station_id = ?
		GROUP BY system_id, station_id
		LIMIT 1
	`

	var snap ShipListings
	var capturedAt string

	err := kb.db.QueryRowContext(ctx, query, systemID, stationID).Scan(
		&snap.SystemID, &snap.SystemName, &snap.StationID, &snap.StationName, &snap.GameTick, &capturedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil // No listings found
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query latest listings: %w", err)
	}

	snap.CapturedAt, err = time.Parse(time.RFC3339, capturedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to parse captured_at: %w", err)
	}

	// Get listings for this snapshot
	listings, err := kb.getShipListingsForSnapshot(ctx, snap.SystemID, snap.StationID, snap.CapturedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to get ship listings: %w", err)
	}
	snap.Listings = listings

	return &snap, nil
}

// HasShipListingsToday checks if ship listings were captured today for a station
func (kb *SQLiteKB) HasShipListingsToday(ctx context.Context, systemID, stationID string) (bool, error) {
	var count int
	err := kb.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM ship_listings
		WHERE system_id = ? AND station_id = ?
		AND DATE(captured_at) = DATE('now')
	`, systemID, stationID).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check for today's ship listings: %w", err)
	}
	return count > 0, nil
}

// getShipListingsForSnapshot retrieves all ships for a specific snapshot time
func (kb *SQLiteKB) getShipListingsForSnapshot(ctx context.Context, systemID, stationID string, capturedAt time.Time) ([]ShipListing, error) {
	query := `
		SELECT ship_class, ship_name, base_price, description, cargo_space, module_slots, utility_slots, weapon_slots
		FROM ship_listings
		WHERE system_id = ? AND station_id = ? AND captured_at = ?
		ORDER BY ship_class
	`

	rows, err := kb.db.QueryContext(ctx, query, systemID, stationID, capturedAt.Format(time.RFC3339))
	if err != nil {
		return nil, fmt.Errorf("failed to query ship listings: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var listings []ShipListing
	for rows.Next() {
		var ship ShipListing
		var description sql.NullString

		err := rows.Scan(&ship.ShipClass, &ship.ShipName, &ship.BasePrice, &description,
			&ship.CargoSpace, &ship.ModuleSlots, &ship.UtilitySlots, &ship.WeaponSlots)
		if err != nil {
			return nil, fmt.Errorf("failed to scan ship listing: %w", err)
		}

		if description.Valid {
			ship.Description = description.String
		}

		listings = append(listings, ship)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating ship listings: %w", err)
	}

	return listings, nil
}
