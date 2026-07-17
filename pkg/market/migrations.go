package market

import (
	"database/sql"
	_ "embed"
	"fmt"
)

//go:embed schema.sql
var schemaSQL string

// runMigrations creates all tables and indexes. Idempotent.
func runMigrations(db *sql.DB) error {
	if _, err := db.Exec(schemaSQL); err != nil {
		return fmt.Errorf("failed to run schema: %w", err)
	}
	// schema.sql uses CREATE TABLE IF NOT EXISTS, so columns added to an
	// existing table do not apply to databases created before the column
	// existed. Add those idempotently here.
	if err := ensureColumn(db, "arbitrage_opportunities", "completed_at", "TEXT"); err != nil {
		return err
	}
	// cycles_seen: consecutive scan cycles a route (item, from, to) has appeared in.
	// 1 = first sighting (fragile); higher = durable supply/demand. Carried forward by
	// ScanArbitrage and used as a ranking boost by the hauler.
	if err := ensureColumn(db, "arbitrage_opportunities", "cycles_seen", "INTEGER DEFAULT 1"); err != nil {
		return err
	}
	// reason: machine-readable cause slug for abandoned/failed mission outcomes
	// (empty for completed) — the abandon-reason catalog's queryable substrate.
	if err := ensureColumn(db, "mission_results", "reason", "TEXT DEFAULT ''"); err != nil {
		return err
	}
	return nil
}

// ensureColumn adds column (with the given type) to table if it is not already
// present. SQLite has no "ADD COLUMN IF NOT EXISTS", so existence is checked via
// PRAGMA table_info first. Idempotent.
func ensureColumn(db *sql.DB, table, column, colType string) error {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return fmt.Errorf("table_info(%s): %w", table, err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var cid, notnull, pk int
		var name, ctype string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return fmt.Errorf("scan table_info(%s): %w", table, err)
		}
		if name == column {
			return rows.Err() // already present
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("table_info(%s): %w", table, err)
	}
	if _, err := db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, colType)); err != nil {
		return fmt.Errorf("add column %s.%s: %w", table, column, err)
	}
	return nil
}
