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
	// source_units: the book's (item, from_station) source best-ask depth, shared
	// across that book's destination rows. Drives the hauler's per-book concurrency
	// cap and status label.
	if err := ensureColumn(db, "arbitrage_opportunities", "source_units", "REAL NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	// reason: machine-readable cause slug for abandoned/failed mission outcomes
	// (empty for completed) — the abandon-reason catalog's queryable substrate.
	if err := ensureColumn(db, "mission_results", "reason", "TEXT DEFAULT ''"); err != nil {
		return err
	}
	// expiry_budget_ticks: the mission's expires_in_ticks AT ACCEPT. Without it
	// a result row cannot answer whether a delivery landed inside its window,
	// which is the open question about reward: two couriers accepted on the
	// same tick for the same route and the same advertised 2000 paid 2000 and
	// 0. Budgets vary per instance (330-1409 on one route), so elapsed alone
	// explains nothing — the ratio elapsed/budget is the thing to test.
	if err := ensureColumn(db, "mission_results", "expiry_budget_ticks", "INTEGER DEFAULT 0"); err != nil {
		return err
	}
	// Station fuel-desk reserves measured from get_base (see schema.sql for
	// the -1/0 semantics) plus the faction fuel bunker at the same base.
	for _, col := range []struct{ name, typ string }{
		{"fuel_reserve", "INTEGER NOT NULL DEFAULT -1"},
		{"fuel_capacity", "INTEGER NOT NULL DEFAULT -1"},
		{"faction_fuel_reserve", "INTEGER NOT NULL DEFAULT -1"},
		{"faction_fuel_capacity", "INTEGER NOT NULL DEFAULT -1"},
		{"faction_id", "TEXT NOT NULL DEFAULT ''"},
		{"reserve_observed_at", "TEXT NOT NULL DEFAULT ''"},
	} {
		if err := ensureColumn(db, "station_fuel_prices", col.name, col.typ); err != nil {
			return err
		}
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
