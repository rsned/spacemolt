package assets

import (
	"database/sql"
	_ "embed"
	"fmt"
)

//go:embed schema.sql
var schemaSQL string

// runMigrations creates all tables and indexes. Idempotent — every worker
// calls Open on startup.
func runMigrations(db *sql.DB) error {
	if _, err := db.Exec(schemaSQL); err != nil {
		return fmt.Errorf("assets: run schema: %w", err)
	}

	// schema.sql uses CREATE TABLE IF NOT EXISTS, so columns added to an
	// existing table never reach a database created before they existed. The
	// live assets.db predates v0.605.0.
	for _, c := range []struct{ table, column, colType string }{
		{"agent_tax", "outstanding_bounty_total", "INTEGER NOT NULL DEFAULT 0"},
		{"agent_tax", "inactivity_exempt", "INTEGER NOT NULL DEFAULT 0"},
	} {
		if err := ensureColumn(db, c.table, c.column, c.colType); err != nil {
			return err
		}
	}

	return nil
}

// ensureColumn adds column (with the given type) to table if not already
// present. SQLite has no "ADD COLUMN IF NOT EXISTS", so existence is checked
// via PRAGMA table_info first. Idempotent. schema.sql uses CREATE TABLE IF NOT
// EXISTS, so a column added to an existing table does not apply to databases
// created before that column existed — add those here.
func ensureColumn(db *sql.DB, table, column, colType string) error {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return fmt.Errorf("assets: table_info(%s): %w", table, err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var (
			cid        int
			name, typ  string
			notNull    int
			dflt       sql.NullString
			primaryKey int
		)
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &primaryKey); err != nil {
			return fmt.Errorf("assets: scan table_info(%s): %w", table, err)
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("assets: iterate table_info(%s): %w", table, err)
	}
	if _, err := db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, colType)); err != nil {
		return fmt.Errorf("assets: add column %s.%s: %w", table, column, err)
	}

	return nil
}
