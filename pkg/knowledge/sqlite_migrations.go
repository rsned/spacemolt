package knowledge

import (
	"database/sql"
	_ "embed"
	"fmt"

	_ "modernc.org/sqlite"
)

// Migration represents a database schema migration
type Migration struct {
	version int
	name    string
	sql     string
}

// initialSchemaSQL holds the complete schema produced by the original
// 30-migration chain (versions 1–30), collapsed into a single migration
// for faster test startup. Future schema changes should be added as new
// migration entries (version: 2, 3, ...) rather than edited into this file.
//
//go:embed initial_schema.sql
var initialSchemaSQL string

// migrations returns all migrations in order.
//
// This used to contain 30 individual migration entries (one per historical
// schema change); they were collapsed into a single initial_schema migration
// on 2026-04-15 because 74 TestSQLiteKB_* tests were paying ~1.5s each in
// migration overhead under -race, pushing the suite past the pre-commit
// hook's 120s timeout. An existing DB with schema_migrations rows for any
// of versions 1–30 will skip the collapsed entry (since 1 ≤ max_version).
func migrations() []Migration {
	return []Migration{
		{
			version: 1,
			name:    "initial_schema",
			sql:     initialSchemaSQL,
		},
		{
			version: 2,
			name:    "add_quantity_to_xp_observations",
			sql: `
				-- Only add column if it doesn't already exist
				ALTER TABLE xp_observations ADD COLUMN quantity INTEGER NOT NULL DEFAULT 1;
			`,
		},
		{
			version: 3,
			name:    "add_last_visited_tick_to_systems",
			sql: `
				ALTER TABLE systems ADD COLUMN last_visited_tick INTEGER NOT NULL DEFAULT 0;

				UPDATE systems
				SET last_visited_tick = (
					SELECT MAX(pr.last_updated_tick)
					FROM poi_resources pr
					JOIN pois p ON pr.poi_id = p.id
					WHERE p.system_id = systems.id
				)
				WHERE EXISTS (
					SELECT 1
					FROM pois p
					JOIN poi_resources pr ON pr.poi_id = p.id
					WHERE p.system_id = systems.id
				);
			`,
		},
	}
}

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

		// Special case for migration 2: check if column already exists
		if m.version == 2 {
			var colCount int
			err := db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('xp_observations') WHERE name='quantity'").Scan(&colCount)
			if err == nil && colCount > 0 {
				// Column already exists, just record the migration as applied
				if _, err := db.Exec(
					"INSERT INTO schema_migrations (version, applied_at) VALUES (?, datetime('now'))",
					m.version,
				); err != nil {
					return fmt.Errorf("failed to record migration %d: %w", m.version, err)
				}
				continue
			}
		}

		// Special case for migration 3: skip if column already exists.
		if m.version == 3 {
			var colCount int
			err := db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('systems') WHERE name='last_visited_tick'").Scan(&colCount)
			if err == nil && colCount > 0 {
				// Column already exists, just record the migration as applied
				if _, err := db.Exec(
					"INSERT INTO schema_migrations (version, applied_at) VALUES (?, datetime('now'))",
					m.version,
				); err != nil {
					return fmt.Errorf("failed to record migration %d: %w", m.version, err)
				}
				continue
			}
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
