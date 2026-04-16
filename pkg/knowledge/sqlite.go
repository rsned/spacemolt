package knowledge

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
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
	// Debug logging to see what tick is being passed in
	if sys.LastUpdatedTick <= 0 {
		log.Printf("[DEBUG] RememberSystem called for system %s (%s) with last_updated_tick=%d",
			sys.ID, sys.Name, sys.LastUpdatedTick)
	}

	tx, err := kb.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Insert or update system
	_, err = tx.ExecContext(ctx, `
		INSERT INTO systems (id, name, description, position_x, position_y, police_level, security_status, empire, is_stronghold, last_updated_tick)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			description = excluded.description,
			position_x = CASE WHEN excluded.position_x != 0 THEN excluded.position_x ELSE systems.position_x END,
			position_y = CASE WHEN excluded.position_y != 0 THEN excluded.position_y ELSE systems.position_y END,
			police_level = excluded.police_level,
			security_status = excluded.security_status,
			empire = excluded.empire,
			is_stronghold = excluded.is_stronghold,
			last_updated_tick = excluded.last_updated_tick
	`, sys.ID, sys.Name, sys.Description, sys.Position.X, sys.Position.Y,
		sys.PoliceLevel, sys.SecurityStatus, sys.Empire, sys.IsStronghold, sys.LastUpdatedTick)
	if err != nil {
		return fmt.Errorf("failed to upsert system: %w", err)
	}

	// Store connections
	for _, conn := range sys.Connections {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO connections (from_system, to_system, distance, last_updated_tick)
			VALUES (?, ?, ?, 0)
			ON CONFLICT(from_system, to_system) DO UPDATE SET distance = excluded.distance
		`, sys.ID, conn.SystemID, conn.Distance); err != nil {
			return fmt.Errorf("failed to store connection %s -> %s: %w", sys.ID, conn.SystemID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// UpsertSystemFromMap inserts or updates a system using map data only.
// Unlike RememberSystem, this preserves richer data (police_level, description,
// last_updated_tick) that may have been collected by explorers. It only updates
// fields that the map data provides: name, position, empire (when non-empty),
// is_stronghold, and connections.
func (kb *SQLiteKB) UpsertSystemFromMap(ctx context.Context, data MapSystemData) error {
	tx, err := kb.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO systems (id, name, position_x, position_y, empire, is_stronghold, police_level, last_updated_tick)
		VALUES (?, ?, ?, ?, ?, ?, 0, 0)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			position_x = excluded.position_x,
			position_y = excluded.position_y,
			empire = CASE WHEN excluded.empire <> '' THEN excluded.empire ELSE systems.empire END,
			is_stronghold = excluded.is_stronghold
	`, data.ID, data.Name, data.PositionX, data.PositionY, data.Empire, data.IsStronghold)
	if err != nil {
		return fmt.Errorf("failed to upsert system from map: %w", err)
	}

	// Store connections (map data has no distance — default to 0, don't overwrite existing)
	for _, connID := range data.Connections {
		if _, err := tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO connections (from_system, to_system, distance, last_updated_tick)
			VALUES (?, ?, 0, 0)
		`, data.ID, connID); err != nil {
			return fmt.Errorf("failed to store connection %s -> %s: %w", data.ID, connID, err)
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

	err := kb.db.QueryRowContext(ctx, `
		SELECT id, name, COALESCE(description, ''), position_x, position_y, police_level, COALESCE(security_status, ''), empire, is_stronghold, last_updated_tick
		FROM systems
		WHERE id = ?
	`, systemID).Scan(
		&sys.ID, &sys.Name, &sys.Description, &sys.Position.X, &sys.Position.Y,
		&sys.PoliceLevel, &sys.SecurityStatus, &sys.Empire, &sys.IsStronghold, &sys.LastUpdatedTick,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil // System not found
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query system: %w", err)
	}

	// Load connections
	rows, err := kb.db.QueryContext(ctx, `
		SELECT to_system, distance FROM connections WHERE from_system = ?
	`, systemID)
	if err != nil {
		return nil, fmt.Errorf("failed to query connections: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var conn SystemConnection
		if err := rows.Scan(&conn.SystemID, &conn.Distance); err != nil {
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
	// Find connections where the destination is not in the systems table (not yet explored)
	rows, err := kb.db.QueryContext(ctx, `
		SELECT c.to_system
		FROM connections c
		LEFT JOIN systems s ON c.to_system = s.id
		WHERE c.from_system = ? AND s.id IS NULL
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
		INSERT OR IGNORE INTO connections (from_system, to_system, last_updated_tick)
		VALUES (?, ?, 0)
	`, fromSystem, toSystem)
	if err != nil {
		return fmt.Errorf("failed to remember connection: %w", err)
	}
	return nil
}

// RememberPOI stores or updates POI knowledge
func (kb *SQLiteKB) RememberPOI(ctx context.Context, poi POI) error {
	// Debug logging to see what tick is being passed in
	if poi.LastUpdatedTick <= 0 {
		log.Printf("[DEBUG] RememberPOI called for POI %s (%s) with last_updated_tick=%d",
			poi.ID, poi.Name, poi.LastUpdatedTick)
	}

	tx, err := kb.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Insert or update POI. Use COALESCE to preserve existing non-empty values
	// when the incoming data has empty fields (e.g., server omitting 'class').
	_, err = tx.ExecContext(ctx, `
		INSERT INTO pois (id, system_id, name, type, class, description, position_x, position_y, hidden, reveal_difficulty, expires_at, last_updated_tick)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			system_id = excluded.system_id,
			name = CASE WHEN excluded.name != '' THEN excluded.name ELSE pois.name END,
			type = CASE WHEN excluded.type != '' THEN excluded.type ELSE pois.type END,
			class = CASE WHEN excluded.class IS NOT NULL AND excluded.class != '' THEN excluded.class ELSE pois.class END,
			description = CASE WHEN excluded.description != '' THEN excluded.description ELSE pois.description END,
			position_x = CASE WHEN excluded.position_x != 0 THEN excluded.position_x ELSE pois.position_x END,
			position_y = CASE WHEN excluded.position_y != 0 THEN excluded.position_y ELSE pois.position_y END,
			hidden = excluded.hidden,
			reveal_difficulty = CASE WHEN excluded.reveal_difficulty != 0 THEN excluded.reveal_difficulty ELSE pois.reveal_difficulty END,
			expires_at = CASE WHEN excluded.expires_at IS NOT NULL AND excluded.expires_at != '' THEN excluded.expires_at ELSE pois.expires_at END,
			last_updated_tick = excluded.last_updated_tick
	`, poi.ID, poi.SystemID, poi.Name, poi.Type, sql.NullString{String: poi.Class, Valid: poi.Class != ""}, poi.Description,
		poi.Position.X, poi.Position.Y, poi.Hidden, poi.RevealDifficulty, sql.NullString{String: poi.ExpiresAt, Valid: poi.ExpiresAt != ""}, poi.LastUpdatedTick)
	if err != nil {
		return fmt.Errorf("failed to upsert POI: %w", err)
	}

	// Only update resources if we have new data — don't wipe existing resources
	// when the incoming POI has none (e.g., from get_system which omits resources).
	if len(poi.Resources) > 0 {
		_, err = tx.ExecContext(ctx, `DELETE FROM poi_resources WHERE poi_id = ?`, poi.ID)
		if err != nil {
			return fmt.Errorf("failed to delete old POI resources: %w", err)
		}
	}

	// Insert resources
	for _, res := range poi.Resources {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO poi_resources (poi_id, resource_id, richness, remaining, last_updated_tick)
			VALUES (?, ?, ?, ?, ?)
		`, poi.ID, res.ResourceID, res.Richness, res.Remaining, poi.LastUpdatedTick)
		if err != nil {
			return fmt.Errorf("failed to insert POI resource: %w", err)
		}
		// Debug logging to verify last_updated_tick is being set
		if poi.LastUpdatedTick <= 0 {
			log.Printf("[DEBUG] RememberPOI: POI %s resource %s has last_updated_tick=%d (poi.LastUpdatedTick=%d)",
				poi.ID, res.ResourceID, poi.LastUpdatedTick, poi.LastUpdatedTick)
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
		SELECT id, system_id, name, type, COALESCE(class, ''), description, position_x, position_y, hidden, reveal_difficulty, last_updated_tick
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
		var class sql.NullString
		err := rows.Scan(
			&poi.ID,
			&poi.SystemID,
			&poi.Name,
			&poi.Type,
			&class,
			&description,
			&poi.Position.X,
			&poi.Position.Y,
			&poi.Hidden,
			&poi.RevealDifficulty,
			&poi.LastUpdatedTick,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan POI: %w", err)
		}
		poi.Class = class.String
		poi.Description = description
		pois = append(pois, poi)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating POIs: %w", err)
	}

	// Load resources for all POIs in this system in a single query.
	resRows, err := kb.db.QueryContext(ctx, `
		SELECT pr.poi_id, pr.resource_id, pr.richness, pr.remaining
		FROM poi_resources pr
		JOIN pois p ON pr.poi_id = p.id
		WHERE p.system_id = ?
	`, systemID)
	if err != nil {
		return nil, fmt.Errorf("failed to query POI resources: %w", err)
	}
	defer func() { _ = resRows.Close() }()

	resMap := make(map[string][]game.POIResource)
	for resRows.Next() {
		var poiID string
		var res game.POIResource
		if err := resRows.Scan(&poiID, &res.ResourceID, &res.Richness, &res.Remaining); err != nil {
			return nil, fmt.Errorf("failed to scan POI resource: %w", err)
		}
		resMap[poiID] = append(resMap[poiID], res)
	}
	if err := resRows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating POI resources: %w", err)
	}

	for i := range pois {
		pois[i].Resources = resMap[pois[i].ID]
	}

	return pois, nil
}

// RememberBase stores or updates space base knowledge
func (kb *SQLiteKB) RememberBase(ctx context.Context, base SpaceBase) error {
	tx, err := kb.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Insert or update base
	_, err = tx.ExecContext(ctx, `
		INSERT INTO bases (id, poi_id, name, description, story, empire, defense_level, has_drones, public_access,
			pirate_rep_required, condition, condition_text, satisfaction_pct, satisfied_count, total_service_infra,
			last_updated_tick)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			poi_id = excluded.poi_id,
			name = excluded.name,
			description = excluded.description,
			story = CASE WHEN excluded.story != '' THEN excluded.story ELSE bases.story END,
			empire = excluded.empire,
			defense_level = excluded.defense_level,
			has_drones = excluded.has_drones,
			public_access = excluded.public_access,
			pirate_rep_required = excluded.pirate_rep_required,
			condition = excluded.condition,
			condition_text = excluded.condition_text,
			satisfaction_pct = excluded.satisfaction_pct,
			satisfied_count = excluded.satisfied_count,
			total_service_infra = excluded.total_service_infra,
			last_updated_tick = excluded.last_updated_tick
	`, base.ID, base.POIID, base.Name, base.Description, base.Story, base.Empire,
		base.DefenseLevel, base.HasDrones, base.PublicAccess,
		base.PirateRepRequired, base.Condition, base.ConditionText, base.SatisfactionPct,
		base.SatisfiedCount, base.TotalServiceInfra,
		base.LastUpdatedTick)
	if err != nil {
		return fmt.Errorf("failed to upsert base: %w", err)
	}

	// Delete existing services for this base
	_, err = tx.ExecContext(ctx, `DELETE FROM base_services WHERE base_id = ?`, base.ID)
	if err != nil {
		return fmt.Errorf("failed to delete old base services: %w", err)
	}

	// Insert services
	for serviceName, available := range base.Services {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO base_services (base_id, service_name, available)
			VALUES (?, ?, ?)
		`, base.ID, serviceName, available)
		if err != nil {
			return fmt.Errorf("failed to insert base service: %w", err)
		}
	}

	// Delete existing facilities for this base
	_, err = tx.ExecContext(ctx, `DELETE FROM base_facilities WHERE base_id = ?`, base.ID)
	if err != nil {
		return fmt.Errorf("failed to delete old base facilities: %w", err)
	}

	// Insert facilities
	for _, facility := range base.Facilities {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO base_facilities (base_id, facility_name, instance_id, description, category, level,
				active, maintenance_satisfied, service, recipe_id, last_updated_tick)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, base.ID, facility.ID, facility.InstanceID, facility.Description, facility.Category, facility.Level,
			facility.Active, facility.MaintenanceSatisfied, facility.Service, facility.RecipeID,
			base.LastUpdatedTick)
		if err != nil {
			log.Printf("[DEBUG] failed to insert facility: base_id=%q, facility_name=%q, instance_id=%q, category=%q, err=%v",
				base.ID, facility.ID, facility.InstanceID, facility.Category, err)
			return fmt.Errorf("failed to insert base facility: %w", err)
		}
	}

	// Delete existing market items for this base
	_, err = tx.ExecContext(ctx, `DELETE FROM base_market WHERE base_id = ?`, base.ID)
	if err != nil {
		return fmt.Errorf("failed to delete old base market items: %w", err)
	}

	// Insert market items
	for _, item := range base.Market {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO base_market (id, base_id, item_id, price_each, quantity, is_npc, last_updated_tick)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, item.ID, base.ID, item.ItemID, item.PriceEach, item.Quantity, item.IsNPC, base.LastUpdatedTick)
		if err != nil {
			return fmt.Errorf("failed to insert base market item: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// GetBase retrieves a base by ID
// loadFacilities reads all facilities for a base from the database.
func (kb *SQLiteKB) loadFacilities(ctx context.Context, baseID string) ([]Facility, error) {
	rows, err := kb.db.QueryContext(ctx, `
		SELECT facility_name, COALESCE(instance_id, ''), COALESCE(description, ''),
			category, level, COALESCE(active, 1), COALESCE(maintenance_satisfied, 1),
			COALESCE(service, ''), COALESCE(recipe_id, ''), last_updated_tick
		FROM base_facilities
		WHERE base_id = ?
		ORDER BY facility_name
	`, baseID)
	if err != nil {
		return nil, fmt.Errorf("failed to query base facilities: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var facilities []Facility
	for rows.Next() {
		var f Facility
		if err := rows.Scan(&f.ID, &f.InstanceID, &f.Description, &f.Category, &f.Level,
			&f.Active, &f.MaintenanceSatisfied, &f.Service, &f.RecipeID, &f.LastUpdated); err != nil {
			return nil, fmt.Errorf("failed to scan base facility: %w", err)
		}
		// Use mapping for display name if available
		if mapped, ok := FacilityCategoryMapping[f.ID]; ok {
			f.Name = mapped.Name
		} else {
			f.Name = f.ID
		}
		facilities = append(facilities, f)
	}
	return facilities, rows.Err()
}

func (kb *SQLiteKB) GetBase(ctx context.Context, baseID string) (*SpaceBase, error) {
	var base SpaceBase
	var description, story sql.NullString

	err := kb.db.QueryRowContext(ctx, `
		SELECT id, poi_id, name, description, COALESCE(story, ''), empire, defense_level, has_drones, public_access,
			COALESCE(pirate_rep_required, 0), COALESCE(condition, ''), COALESCE(condition_text, ''),
			COALESCE(satisfaction_pct, 0), COALESCE(satisfied_count, 0), COALESCE(total_service_infra, 0),
			last_updated_tick
		FROM bases
		WHERE id = ?
	`, baseID).Scan(
		&base.ID,
		&base.POIID,
		&base.Name,
		&description,
		&story,
		&base.Empire,
		&base.DefenseLevel,
		&base.HasDrones,
		&base.PublicAccess,
		&base.PirateRepRequired,
		&base.Condition,
		&base.ConditionText,
		&base.SatisfactionPct,
		&base.SatisfiedCount,
		&base.TotalServiceInfra,
		&base.LastUpdatedTick,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("base not found: %s", baseID)
		}
		return nil, fmt.Errorf("failed to query base: %w", err)
	}

	if description.Valid {
		base.Description = description.String
	}
	if story.Valid {
		base.Story = story.String
	}

	// Load services
	rows, err := kb.db.QueryContext(ctx, `
		SELECT service_name, available
		FROM base_services
		WHERE base_id = ?
	`, baseID)
	if err != nil {
		return nil, fmt.Errorf("failed to query base services: %w", err)
	}
	defer func() { _ = rows.Close() }()

	base.Services = make(map[string]bool)
	for rows.Next() {
		var serviceName string
		var available bool
		if err := rows.Scan(&serviceName, &available); err != nil {
			return nil, fmt.Errorf("failed to scan base service: %w", err)
		}
		base.Services[serviceName] = available
	}
	_ = rows.Close()

	// Load facilities
	facilities, err := kb.loadFacilities(ctx, baseID)
	if err != nil {
		return nil, err
	}
	base.Facilities = facilities

	// Load market items
	rows, err = kb.db.QueryContext(ctx, `
		SELECT id, item_id, price_each, quantity, is_npc
		FROM base_market
		WHERE base_id = ?
		ORDER BY item_id
	`, baseID)
	if err != nil {
		return nil, fmt.Errorf("failed to query base market: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var item BaseMarketItem
		if err := rows.Scan(&item.ID, &item.ItemID, &item.PriceEach, &item.Quantity, &item.IsNPC); err != nil {
			return nil, fmt.Errorf("failed to scan base market item: %w", err)
		}
		base.Market = append(base.Market, item)
	}

	return &base, nil
}

// GetBaseByPOI retrieves a base by its POI ID
func (kb *SQLiteKB) GetBaseByPOI(ctx context.Context, poiID string) (*SpaceBase, error) {
	var base SpaceBase
	var description, story sql.NullString

	err := kb.db.QueryRowContext(ctx, `
		SELECT id, poi_id, name, description, COALESCE(story, ''), empire, defense_level, has_drones, public_access,
			COALESCE(pirate_rep_required, 0), COALESCE(condition, ''), COALESCE(condition_text, ''),
			COALESCE(satisfaction_pct, 0), COALESCE(satisfied_count, 0), COALESCE(total_service_infra, 0),
			last_updated_tick
		FROM bases
		WHERE poi_id = ?
	`, poiID).Scan(
		&base.ID,
		&base.POIID,
		&base.Name,
		&description,
		&story,
		&base.Empire,
		&base.DefenseLevel,
		&base.HasDrones,
		&base.PublicAccess,
		&base.PirateRepRequired,
		&base.Condition,
		&base.ConditionText,
		&base.SatisfactionPct,
		&base.SatisfiedCount,
		&base.TotalServiceInfra,
		&base.LastUpdatedTick,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("no base found at POI: %s", poiID)
		}
		return nil, fmt.Errorf("failed to query base by POI: %w", err)
	}

	if description.Valid {
		base.Description = description.String
	}
	if story.Valid {
		base.Story = story.String
	}

	// Load services (same as GetBase)
	rows, err := kb.db.QueryContext(ctx, `
		SELECT service_name, available
		FROM base_services
		WHERE base_id = ?
	`, base.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to query base services: %w", err)
	}
	defer func() { _ = rows.Close() }()

	base.Services = make(map[string]bool)
	for rows.Next() {
		var serviceName string
		var available bool
		if err := rows.Scan(&serviceName, &available); err != nil {
			return nil, fmt.Errorf("failed to scan base service: %w", err)
		}
		base.Services[serviceName] = available
	}
	_ = rows.Close()

	// Load facilities
	facilities, err := kb.loadFacilities(ctx, base.ID)
	if err != nil {
		return nil, err
	}
	base.Facilities = facilities

	// Load market items (same as GetBase)
	rows, err = kb.db.QueryContext(ctx, `
		SELECT id, item_id, price_each, quantity, is_npc
		FROM base_market
		WHERE base_id = ?
		ORDER BY item_id
	`, base.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to query base market: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var item BaseMarketItem
		if err := rows.Scan(&item.ID, &item.ItemID, &item.PriceEach, &item.Quantity, &item.IsNPC); err != nil {
			return nil, fmt.Errorf("failed to scan base market item: %w", err)
		}
		base.Market = append(base.Market, item)
	}

	return &base, nil
}

// AddExperience logs an agent experience
func (kb *SQLiteKB) AddExperience(ctx context.Context, agentID, expType, description, outcome, location string) error {
	_, err := kb.db.ExecContext(ctx, `
		INSERT INTO experiences (agent_id, time, type, description, outcome, location, last_updated_tick)
		VALUES (?, ?, ?, ?, ?, ?, 0)
	`, agentID, time.Now().Format(time.RFC3339), expType, description, outcome, location)
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
func (kb *SQLiteKB) RegisterAgent(ctx context.Context, agentID, name, role, empire string, personality []byte) error {
	_, err := kb.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO agents (id, name, role, empire, status, last_updated_tick)
		VALUES (?, ?, ?, ?, 'active', 0)
	`, agentID, name, role, empire)
	if err != nil {
		return fmt.Errorf("failed to register agent: %w", err)
	}
	return nil
}

// GetSystems returns all known systems
func (kb *SQLiteKB) GetSystems() []System {
	// Query all systems
	rows, err := kb.db.Query(`
		SELECT id, name, COALESCE(description, ''), position_x, position_y, police_level, COALESCE(security_status, ''), empire, is_stronghold, last_updated_tick
		FROM systems
	`)
	if err != nil {
		return nil
	}
	defer func() { _ = rows.Close() }()

	var systems []System
	for rows.Next() {
		var sys System

		if err := rows.Scan(
			&sys.ID, &sys.Name, &sys.Description, &sys.Position.X, &sys.Position.Y,
			&sys.PoliceLevel, &sys.SecurityStatus, &sys.Empire, &sys.IsStronghold, &sys.LastUpdatedTick,
		); err != nil {
			continue
		}

		systems = append(systems, sys)
	}

	if err := rows.Err(); err != nil {
		return nil
	}

	// Load all connections in a single query and map them
	connRows, err := kb.db.Query(`
		SELECT from_system, to_system, distance FROM connections
	`)
	if err != nil {
		return systems // Return systems without connections if query fails
	}
	defer func() { _ = connRows.Close() }()

	// Create a map of connections
	connMap := make(map[string][]SystemConnection)
	for connRows.Next() {
		var from string
		var conn SystemConnection
		if err := connRows.Scan(&from, &conn.SystemID, &conn.Distance); err != nil {
			continue
		}
		connMap[from] = append(connMap[from], conn)
	}

	// Add connections to systems
	for i := range systems {
		systems[i].Connections = connMap[systems[i].ID]
	}

	return systems
}

// GetSystemsWithContext retrieves all systems from the database (with context for graph building)
func (kb *SQLiteKB) GetSystemsWithContext(ctx context.Context) ([]System, error) {
	query := `SELECT id, name, position_x, position_y, empire, police_level, is_stronghold, last_updated_tick
			  FROM systems`

	rows, err := kb.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query systems: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var systems []System
	for rows.Next() {
		var s System
		err := rows.Scan(&s.ID, &s.Name, &s.Position.X, &s.Position.Y,
			&s.Empire, &s.PoliceLevel, &s.IsStronghold, &s.LastUpdatedTick)
		if err != nil {
			return nil, fmt.Errorf("scan system: %w", err)
		}
		systems = append(systems, s)
	}

	return systems, nil
}

// GetConnections retrieves all system connections (for graph building)
func (kb *SQLiteKB) GetConnections(ctx context.Context) ([]Connection, error) {
	query := `SELECT from_system, to_system, distance FROM connections`

	rows, err := kb.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query connections: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var conns []Connection
	for rows.Next() {
		var c Connection
		err := rows.Scan(&c.FromSystem, &c.ToSystem, &c.Distance)
		if err != nil {
			return nil, fmt.Errorf("scan connection: %w", err)
		}
		conns = append(conns, c)
	}

	return conns, nil
}

// GetConnectionMetrics retrieves connection metrics (for weighted pathfinding)
func (kb *SQLiteKB) GetConnectionMetrics(ctx context.Context) ([]ConnectionMetric, error) {
	query := `SELECT from_system, to_system, avg_fuel_cost, avg_travel_time, last_traveled
			  FROM connection_metrics
			  WHERE avg_fuel_cost IS NOT NULL`

	rows, err := kb.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query connection metrics: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var metrics []ConnectionMetric
	for rows.Next() {
		var m ConnectionMetric
		err := rows.Scan(&m.FromSystem, &m.ToSystem, &m.AvgFuelCost,
			&m.AvgTravelTime, &m.LastTraveled)
		if err != nil {
			return nil, fmt.Errorf("scan connection metric: %w", err)
		}
		metrics = append(metrics, m)
	}

	return metrics, nil
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
		INSERT INTO market_snapshots (system_id, system_name, station_id, station_name, game_tick, captured_at, agent_id, last_updated_tick)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, snapshot.SystemID, snapshot.SystemName, snapshot.StationID, snapshot.StationName, snapshot.GameTick, snapshot.CapturedAt.Format(time.RFC3339), agentID, snapshot.GameTick)
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
			INSERT INTO market_listings (snapshot_id, item_id, item_type, quantity, price_per_unit, total_price, listing_type, listed_by, last_updated_tick)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, snapshotID, listing.ItemID, listing.ItemType, listing.Quantity, listing.PricePerUnit, listing.TotalPrice, listing.Type, listing.ListedBy, snapshot.GameTick)
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

// StoreMarketAnalysis stores a market analysis snapshot
func (kb *SQLiteKB) StoreMarketAnalysis(ctx context.Context, analysis MarketAnalysis, agentID string) error {
	// Serialize complex fields to JSON
	insightsJSON, err := json.Marshal(analysis.TopInsights)
	if err != nil {
		return fmt.Errorf("failed to marshal top_insights: %w", err)
	}
	xpJSON, err := json.Marshal(analysis.XPGained)
	if err != nil {
		return fmt.Errorf("failed to marshal xp_gained: %w", err)
	}
	analysisDataJSON, err := json.Marshal(analysis.AnalysisData)
	if err != nil {
		return fmt.Errorf("failed to marshal analysis: %w", err)
	}

	result, err := kb.db.ExecContext(ctx, `
		INSERT INTO market_analyses (system_id, system_name, station_id, station_name,
			game_tick, captured_at, agent_id, last_updated_tick,
			mode, skill_level, scanning_range, stations_in_range, items_scanned,
			top_insights, total_items, total_pages, page, hint, xp_gained, analysis)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, analysis.SystemID, analysis.SystemName, analysis.StationID, analysis.StationName,
		analysis.GameTick, analysis.CapturedAt.Format(time.RFC3339), agentID, analysis.GameTick,
		analysis.Mode, analysis.SkillLevel, analysis.ScanningRange, analysis.StationsInRange,
		analysis.ItemsScanned, string(insightsJSON), analysis.TotalItems, analysis.TotalPages,
		analysis.Page, analysis.Hint, string(xpJSON), string(analysisDataJSON))
	if err != nil {
		return fmt.Errorf("failed to insert market analysis: %w", err)
	}

	_, err = result.LastInsertId()
	return err
}

// GetLatestMarketAnalysis retrieves the most recent market analysis
func (kb *SQLiteKB) GetLatestMarketAnalysis(ctx context.Context, systemID, stationID string) (*MarketAnalysis, error) {
	query := `
		SELECT id, system_id, system_name, station_id, station_name, game_tick, captured_at,
			mode, skill_level, scanning_range, stations_in_range, items_scanned,
			top_insights, total_items, total_pages, page, hint, xp_gained, analysis
		FROM market_analyses
		WHERE system_id = ? AND station_id = ?
		ORDER BY captured_at DESC
		LIMIT 1
	`

	var analysis MarketAnalysis
	var id int
	var capturedAt string
	var insightsJSON, xpJSON, analysisDataJSON string

	err := kb.db.QueryRowContext(ctx, query, systemID, stationID).Scan(
		&id, &analysis.SystemID, &analysis.SystemName, &analysis.StationID, &analysis.StationName,
		&analysis.GameTick, &capturedAt,
		&analysis.Mode, &analysis.SkillLevel, &analysis.ScanningRange, &analysis.StationsInRange,
		&analysis.ItemsScanned, &insightsJSON, &analysis.TotalItems, &analysis.TotalPages,
		&analysis.Page, &analysis.Hint, &xpJSON, &analysisDataJSON,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil // No analysis found
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query market analysis: %w", err)
	}

	analysis.CapturedAt, err = time.Parse(time.RFC3339, capturedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to parse captured_at: %w", err)
	}

	// Unmarshal JSON fields
	if err := json.Unmarshal([]byte(insightsJSON), &analysis.TopInsights); err != nil {
		return nil, fmt.Errorf("failed to unmarshal top_insights: %w", err)
	}
	if err := json.Unmarshal([]byte(xpJSON), &analysis.XPGained); err != nil {
		return nil, fmt.Errorf("failed to unmarshal xp_gained: %w", err)
	}
	if err := json.Unmarshal([]byte(analysisDataJSON), &analysis.AnalysisData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal analysis: %w", err)
	}

	return &analysis, nil
}

// GetMarketAnalysisHistory retrieves historical market analyses
func (kb *SQLiteKB) GetMarketAnalysisHistory(ctx context.Context, systemID, stationID string, limit int) ([]MarketAnalysis, error) {
	query := `
		SELECT id, system_id, system_name, station_id, station_name, game_tick, captured_at,
			mode, skill_level, scanning_range, stations_in_range, items_scanned,
			top_insights, total_items, total_pages, page, hint, xp_gained, analysis
		FROM market_analyses
		WHERE system_id = ? AND station_id = ?
		ORDER BY captured_at DESC
		LIMIT ?
	`

	rows, err := kb.db.QueryContext(ctx, query, systemID, stationID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query market analyses: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var analyses []MarketAnalysis
	for rows.Next() {
		var analysis MarketAnalysis
		var id int
		var capturedAt string
		var insightsJSON, xpJSON, analysisDataJSON string

		err := rows.Scan(
			&id, &analysis.SystemID, &analysis.SystemName, &analysis.StationID, &analysis.StationName,
			&analysis.GameTick, &capturedAt,
			&analysis.Mode, &analysis.SkillLevel, &analysis.ScanningRange, &analysis.StationsInRange,
			&analysis.ItemsScanned, &insightsJSON, &analysis.TotalItems, &analysis.TotalPages,
			&analysis.Page, &analysis.Hint, &xpJSON, &analysisDataJSON,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan analysis: %w", err)
		}

		analysis.CapturedAt, err = time.Parse(time.RFC3339, capturedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to parse captured_at: %w", err)
		}

		// Unmarshal JSON fields
		if err := json.Unmarshal([]byte(insightsJSON), &analysis.TopInsights); err != nil {
			return nil, fmt.Errorf("failed to unmarshal top_insights: %w", err)
		}
		if err := json.Unmarshal([]byte(xpJSON), &analysis.XPGained); err != nil {
			return nil, fmt.Errorf("failed to unmarshal xp_gained: %w", err)
		}
		if err := json.Unmarshal([]byte(analysisDataJSON), &analysis.AnalysisData); err != nil {
			return nil, fmt.Errorf("failed to unmarshal analysis: %w", err)
		}

		analyses = append(analyses, analysis)
	}

	return analyses, nil
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
				utility_slots, weapon_slots, game_tick, captured_at, agent_id, last_updated_tick)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, listings.SystemID, listings.SystemName, listings.StationID, listings.StationName,
			ship.ShipClass, ship.ShipName, ship.BasePrice, ship.Description, ship.CargoSpace,
			ship.ModuleSlots, ship.UtilitySlots, ship.WeaponSlots, listings.GameTick,
			listings.CapturedAt.Format(time.RFC3339), agentID, listings.GameTick)
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

// UpdateAgentWalletCredits updates the wallet credits for an agent.
func (kb *SQLiteKB) UpdateAgentWalletCredits(ctx context.Context, agentID string, credits int) error {
	_, err := kb.db.ExecContext(ctx, `
		UPDATE agents SET wallet_credits = ?, wallet_updated_at = ? WHERE id = ?`,
		credits, time.Now().Format(time.RFC3339), agentID)
	if err != nil {
		return fmt.Errorf("failed to update wallet credits for %s: %w", agentID, err)
	}
	return nil
}

// StoreStorageSnapshot upserts a storage snapshot for an agent at a base.
func (kb *SQLiteKB) StoreStorageSnapshot(ctx context.Context, snapshot StorageSnapshot) error {
	tx, err := kb.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if snapshot.CapturedAt.IsZero() {
		snapshot.CapturedAt = time.Now()
	}

	// Upsert the snapshot row
	result, err := tx.ExecContext(ctx, `
		INSERT INTO storage_snapshots (agent_id, base_id, credits, captured_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(agent_id, base_id) DO UPDATE SET
			credits = excluded.credits,
			captured_at = excluded.captured_at`,
		snapshot.AgentID, snapshot.BaseID, snapshot.Credits, snapshot.CapturedAt.Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("failed to upsert storage snapshot: %w", err)
	}

	// Get the snapshot ID (either new or existing)
	var snapshotID int64
	snapshotID, err = result.LastInsertId()
	if err != nil || snapshotID == 0 {
		// ON CONFLICT UPDATE doesn't return LastInsertId — query it
		err = tx.QueryRowContext(ctx,
			"SELECT id FROM storage_snapshots WHERE agent_id = ? AND base_id = ?",
			snapshot.AgentID, snapshot.BaseID).Scan(&snapshotID)
		if err != nil {
			return fmt.Errorf("failed to get snapshot id: %w", err)
		}
	}

	// Replace items
	if _, err := tx.ExecContext(ctx, "DELETE FROM storage_snapshot_items WHERE snapshot_id = ?", snapshotID); err != nil {
		return fmt.Errorf("failed to delete old items: %w", err)
	}
	for _, item := range snapshot.Items {
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO storage_snapshot_items (snapshot_id, item_id, name, quantity, size) VALUES (?, ?, ?, ?, ?)",
			snapshotID, item.ItemID, item.Name, item.Quantity, item.Size); err != nil {
			return fmt.Errorf("failed to insert item %s: %w", item.ItemID, err)
		}
	}

	// Replace ships
	if _, err := tx.ExecContext(ctx, "DELETE FROM storage_snapshot_ships WHERE snapshot_id = ?", snapshotID); err != nil {
		return fmt.Errorf("failed to delete old ships: %w", err)
	}
	for _, ship := range snapshot.Ships {
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO storage_snapshot_ships (snapshot_id, ship_id, class_id, class_name, cargo_used) VALUES (?, ?, ?, ?, ?)",
			snapshotID, ship.ShipID, ship.ClassID, ship.ClassName, ship.CargoUsed); err != nil {
			return fmt.Errorf("failed to insert ship %s: %w", ship.ShipID, err)
		}
	}

	return tx.Commit()
}

// GetStorageSnapshot returns the latest storage snapshot for an agent at a base.
func (kb *SQLiteKB) GetStorageSnapshot(ctx context.Context, agentID, baseID string) (*StorageSnapshot, error) {
	var snapshot StorageSnapshot
	var capturedAt string
	var snapshotID int64

	err := kb.db.QueryRowContext(ctx,
		"SELECT id, agent_id, base_id, credits, captured_at FROM storage_snapshots WHERE agent_id = ? AND base_id = ?",
		agentID, baseID).Scan(&snapshotID, &snapshot.AgentID, &snapshot.BaseID, &snapshot.Credits, &capturedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get storage snapshot: %w", err)
	}

	snapshot.CapturedAt, _ = time.Parse(time.RFC3339, capturedAt)

	// Load items
	itemRows, err := kb.db.QueryContext(ctx,
		"SELECT item_id, name, quantity, size FROM storage_snapshot_items WHERE snapshot_id = ?", snapshotID)
	if err != nil {
		return nil, fmt.Errorf("failed to query items: %w", err)
	}
	defer func() { _ = itemRows.Close() }()
	for itemRows.Next() {
		var item StorageSnapshotItem
		if err := itemRows.Scan(&item.ItemID, &item.Name, &item.Quantity, &item.Size); err != nil {
			return nil, fmt.Errorf("failed to scan item: %w", err)
		}
		snapshot.Items = append(snapshot.Items, item)
	}

	// Load ships
	shipRows, err := kb.db.QueryContext(ctx,
		"SELECT ship_id, class_id, class_name, cargo_used FROM storage_snapshot_ships WHERE snapshot_id = ?", snapshotID)
	if err != nil {
		return nil, fmt.Errorf("failed to query ships: %w", err)
	}
	defer func() { _ = shipRows.Close() }()
	for shipRows.Next() {
		var ship StorageSnapshotShip
		if err := shipRows.Scan(&ship.ShipID, &ship.ClassID, &ship.ClassName, &ship.CargoUsed); err != nil {
			return nil, fmt.Errorf("failed to scan ship: %w", err)
		}
		snapshot.Ships = append(snapshot.Ships, ship)
	}

	return &snapshot, nil
}

// GetAllStorageSnapshots returns the latest storage snapshots for all agents.
func (kb *SQLiteKB) GetAllStorageSnapshots(ctx context.Context) ([]StorageSnapshot, error) {
	rows, err := kb.db.QueryContext(ctx,
		"SELECT id, agent_id, base_id, credits, captured_at FROM storage_snapshots ORDER BY agent_id, base_id")
	if err != nil {
		return nil, fmt.Errorf("failed to query storage snapshots: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var snapshots []StorageSnapshot
	for rows.Next() {
		var s StorageSnapshot
		var snapshotID int64
		var capturedAt string
		if err := rows.Scan(&snapshotID, &s.AgentID, &s.BaseID, &s.Credits, &capturedAt); err != nil {
			return nil, fmt.Errorf("failed to scan snapshot: %w", err)
		}
		s.CapturedAt, _ = time.Parse(time.RFC3339, capturedAt)

		// Load items for this snapshot
		itemRows, err := kb.db.QueryContext(ctx,
			"SELECT item_id, name, quantity, size FROM storage_snapshot_items WHERE snapshot_id = ?", snapshotID)
		if err != nil {
			return nil, fmt.Errorf("failed to query items for snapshot %d: %w", snapshotID, err)
		}
		for itemRows.Next() {
			var item StorageSnapshotItem
			if err := itemRows.Scan(&item.ItemID, &item.Name, &item.Quantity, &item.Size); err != nil {
				_ = itemRows.Close()
				return nil, fmt.Errorf("failed to scan item: %w", err)
			}
			s.Items = append(s.Items, item)
		}
		_ = itemRows.Close()

		// Load ships for this snapshot
		shipRows, err := kb.db.QueryContext(ctx,
			"SELECT ship_id, class_id, class_name, cargo_used FROM storage_snapshot_ships WHERE snapshot_id = ?", snapshotID)
		if err != nil {
			return nil, fmt.Errorf("failed to query ships for snapshot %d: %w", snapshotID, err)
		}
		for shipRows.Next() {
			var ship StorageSnapshotShip
			if err := shipRows.Scan(&ship.ShipID, &ship.ClassID, &ship.ClassName, &ship.CargoUsed); err != nil {
				_ = shipRows.Close()
				return nil, fmt.Errorf("failed to scan ship: %w", err)
			}
			s.Ships = append(s.Ships, ship)
		}
		_ = shipRows.Close()

		snapshots = append(snapshots, s)
	}

	return snapshots, nil
}

// RecordChangeSnapshot stores a snapshot of old data when a change is detected.
func (kb *SQLiteKB) RecordChangeSnapshot(ctx context.Context, snapshot ChangeSnapshot) error {
	_, err := kb.db.ExecContext(ctx, `
		INSERT INTO change_snapshots (entity_type, entity_id, system_id, change_summary, old_data, detected_by, detected_at_tick, detected_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'))
	`, snapshot.EntityType, snapshot.EntityID, snapshot.SystemID,
		snapshot.ChangeSummary, snapshot.OldData, snapshot.DetectedBy, snapshot.DetectedAtTick)
	if err != nil {
		return fmt.Errorf("failed to insert change snapshot: %w", err)
	}
	return nil
}

// GetChangeSnapshots retrieves change snapshots for a system, newest first.
func (kb *SQLiteKB) GetChangeSnapshots(ctx context.Context, systemID string, limit int) ([]ChangeSnapshot, error) {
	rows, err := kb.db.QueryContext(ctx, `
		SELECT id, entity_type, entity_id, system_id, change_summary, old_data, detected_by, detected_at_tick, detected_at
		FROM change_snapshots
		WHERE system_id = ?
		ORDER BY detected_at DESC
		LIMIT ?
	`, systemID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query change snapshots: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var snapshots []ChangeSnapshot
	for rows.Next() {
		var s ChangeSnapshot
		var detectedAt string
		if err := rows.Scan(&s.ID, &s.EntityType, &s.EntityID, &s.SystemID,
			&s.ChangeSummary, &s.OldData, &s.DetectedBy, &s.DetectedAtTick, &detectedAt); err != nil {
			return nil, fmt.Errorf("failed to scan change snapshot: %w", err)
		}
		s.DetectedAt, _ = time.Parse("2006-01-02 15:04:05", detectedAt)
		snapshots = append(snapshots, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating change snapshots: %w", err)
	}
	return snapshots, nil
}

// RecordXPObservation inserts a single XP change observation.
func (kb *SQLiteKB) RecordXPObservation(ctx context.Context, obs XPObservation) error {
	_, err := kb.db.ExecContext(ctx, `
		INSERT INTO xp_observations (agent_id, action, target, source, skill_id, xp_delta, level_delta, level_before, level_after, game_tick, mission_id, quantity)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, obs.AgentID, obs.Action, obs.Target, obs.Source, obs.SkillID,
		obs.XPDelta, obs.LevelDelta, obs.LevelBefore, obs.LevelAfter, obs.GameTick, obs.MissionID, obs.Quantity)
	if err != nil {
		return fmt.Errorf("failed to insert xp observation: %w", err)
	}
	return nil
}

// GetXPObservations retrieves observations, optionally filtered by action.
func (kb *SQLiteKB) GetXPObservations(ctx context.Context, action string, limit int) ([]XPObservation, error) {
	var query string
	var args []any
	if action != "" {
		query = `
			SELECT id, agent_id, action, target, source, skill_id, xp_delta, level_delta, level_before, level_after, game_tick, created_at, COALESCE(mission_id, ''), quantity
			FROM xp_observations
			WHERE action = ?
			ORDER BY created_at DESC
			LIMIT ?`
		args = []any{action, limit}
	} else {
		query = `
			SELECT id, agent_id, action, target, source, skill_id, xp_delta, level_delta, level_before, level_after, game_tick, created_at, COALESCE(mission_id, ''), quantity
			FROM xp_observations
			ORDER BY created_at DESC
			LIMIT ?`
		args = []any{limit}
	}

	rows, err := kb.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query xp observations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var observations []XPObservation
	for rows.Next() {
		var o XPObservation
		var createdAt string
		if err := rows.Scan(&o.ID, &o.AgentID, &o.Action, &o.Target, &o.Source,
			&o.SkillID, &o.XPDelta, &o.LevelDelta, &o.LevelBefore, &o.LevelAfter,
			&o.GameTick, &createdAt, &o.MissionID, &o.Quantity); err != nil {
			return nil, fmt.Errorf("failed to scan xp observation: %w", err)
		}
		o.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
		observations = append(observations, o)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating xp observations: %w", err)
	}
	return observations, nil
}

// GetXPSummary aggregates observations by action, skill, and source.
func (kb *SQLiteKB) GetXPSummary(ctx context.Context) ([]XPSummaryRow, error) {
	rows, err := kb.db.QueryContext(ctx, `
		SELECT action, skill_id, source, AVG(xp_delta), COUNT(*)
		FROM xp_observations
		GROUP BY action, skill_id, source
		ORDER BY action, skill_id
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query xp summary: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var summary []XPSummaryRow
	for rows.Next() {
		var r XPSummaryRow
		if err := rows.Scan(&r.Action, &r.SkillID, &r.Source, &r.AvgXPDelta, &r.Count); err != nil {
			return nil, fmt.Errorf("failed to scan xp summary row: %w", err)
		}
		summary = append(summary, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating xp summary: %w", err)
	}
	return summary, nil
}
