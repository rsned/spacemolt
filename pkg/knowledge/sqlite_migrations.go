package knowledge

import (
	"database/sql"
	_ "embed"
	"fmt"

	_ "modernc.org/sqlite"
)

// Migration represents a database schema migration.
type Migration struct {
	version int
	name    string
	sql     string
}

// collapseFloor is the ledger version a database must have reached to be
// opened by this build. The 1..59 migration chain was collapsed on
// 2026-09-08 into initial_schema.sql, recorded as version 59 so a database
// that came through the old chain (live is exactly at 59) skips it while a
// fresh database gets the whole schema in one statement batch. A database
// with a ledger above 0 but below the floor would silently miss the
// collapsed migrations, so runMigrations refuses it instead.
const collapseFloor = 59

// collapseUpgradeCommit is the last build that still carries the full
// 1..59 chain; it can bring a pre-floor database up to 59.
const collapseUpgradeCommit = "d4350708"

// initialSchemaSQL is the complete schema as of migration 60, dumped from a
// fresh database. Future schema changes are new migration entries; never
// edit this file or an already-applied migration in place.
//
//go:embed initial_schema.sql
var initialSchemaSQL string

// migrations returns all migrations in order.
func migrations() []Migration {
	return []Migration{
		{
			version: collapseFloor,
			name:    "baseline_2026_09_08",
			sql:     initialSchemaSQL,
		},
		{
			// Reconciles the LIVE database with what a fresh one gets.
			// Commit fff8e9cb (2026-05-20) changed the faction_orders /
			// faction_missions primary keys by editing migration 35 in
			// place after the live DB had already applied it, so live kept
			// the old two-column keys; base_market on live still has the
			// pre-collapse column defaults. Audit 2026-09-08: these three
			// tables were the only column-level drift. Each table is
			// rebuilt by copy so any rows present survive (all three were
			// empty on live at audit time).
			version: 60,
			name:    "reconcile_live_shapes",
			sql: `
				-- Hand-built pre-collapse fixtures may lack these tables entirely;
				-- create them in their final shape first so the copy below
				-- always has a source.
				CREATE TABLE IF NOT EXISTS faction_orders (
					faction_id TEXT NOT NULL,
					base_id TEXT NOT NULL,
					order_id TEXT NOT NULL,
					side TEXT,
					item_id TEXT,
					item_name TEXT,
					price_each REAL NOT NULL DEFAULT 0,
					quantity REAL NOT NULL DEFAULT 0,
					captured_utc TEXT NOT NULL,
					PRIMARY KEY (faction_id, base_id, order_id)
				);
				CREATE TABLE IF NOT EXISTS faction_missions (
					faction_id TEXT NOT NULL,
					base_id TEXT NOT NULL,
					mission_id TEXT NOT NULL,
					title TEXT,
					type TEXT,
					description TEXT,
					giver_name TEXT,
					rewards_json TEXT,
					objectives_json TEXT,
					assigned_player_id TEXT,
					expiration_utc TEXT,
					captured_utc TEXT NOT NULL,
					PRIMARY KEY (faction_id, base_id, mission_id)
				);
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

				CREATE TABLE faction_orders_v60_copy AS SELECT * FROM faction_orders;
				DROP TABLE faction_orders;
				CREATE TABLE faction_orders (
					faction_id TEXT NOT NULL,
					base_id TEXT NOT NULL,
					order_id TEXT NOT NULL,
					side TEXT,
					item_id TEXT,
					item_name TEXT,
					price_each REAL NOT NULL DEFAULT 0,
					quantity REAL NOT NULL DEFAULT 0,
					captured_utc TEXT NOT NULL,
					PRIMARY KEY (faction_id, base_id, order_id)
				);
				INSERT OR IGNORE INTO faction_orders
					SELECT faction_id, base_id, order_id, side, item_id, item_name, price_each, quantity, captured_utc
					FROM faction_orders_v60_copy;
				DROP TABLE faction_orders_v60_copy;
				CREATE INDEX IF NOT EXISTS faction_orders_faction ON faction_orders(faction_id);

				CREATE TABLE faction_missions_v60_copy AS SELECT * FROM faction_missions;
				DROP TABLE faction_missions;
				CREATE TABLE faction_missions (
					faction_id TEXT NOT NULL,
					base_id TEXT NOT NULL,
					mission_id TEXT NOT NULL,
					title TEXT,
					type TEXT,
					description TEXT,
					giver_name TEXT,
					rewards_json TEXT,
					objectives_json TEXT,
					assigned_player_id TEXT,
					expiration_utc TEXT,
					captured_utc TEXT NOT NULL,
					PRIMARY KEY (faction_id, base_id, mission_id)
				);
				INSERT OR IGNORE INTO faction_missions
					SELECT faction_id, base_id, mission_id, title, type, description, giver_name, rewards_json, objectives_json, assigned_player_id, expiration_utc, captured_utc
					FROM faction_missions_v60_copy;
				DROP TABLE faction_missions_v60_copy;
				CREATE INDEX IF NOT EXISTS faction_missions_faction ON faction_missions(faction_id);

				CREATE TABLE base_market_v60_copy AS SELECT * FROM base_market;
				DROP TABLE base_market;
				CREATE TABLE base_market (
					id TEXT PRIMARY KEY,
					base_id TEXT NOT NULL,
					item_id TEXT NOT NULL,
					price_each REAL NOT NULL,
					quantity INTEGER NOT NULL,
					is_npc BOOLEAN DEFAULT 1,
					last_updated_tick INTEGER DEFAULT 0,
					FOREIGN KEY (base_id) REFERENCES bases(id) ON DELETE CASCADE
				);
				INSERT OR IGNORE INTO base_market
					SELECT id, base_id, item_id, price_each, COALESCE(quantity, 0), COALESCE(is_npc, 0), last_updated_tick
					FROM base_market_v60_copy;
				DROP TABLE base_market_v60_copy;
				CREATE INDEX IF NOT EXISTS idx_base_market_base_id ON base_market(base_id);
				CREATE INDEX IF NOT EXISTS idx_base_market_item_id ON base_market(item_id);
			`,
		},
	}
}

// runMigrations brings db to the current schema version. Each migration
// runs in its own transaction and is recorded in schema_migrations.
func runMigrations(db *sql.DB) error {
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at TEXT NOT NULL
		)
	`); err != nil {
		return fmt.Errorf("failed to create migrations table: %w", err)
	}

	var currentVersion int
	if err := db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&currentVersion); err != nil {
		return fmt.Errorf("failed to get current migration version: %w", err)
	}
	if currentVersion > 0 && currentVersion < collapseFloor {
		return fmt.Errorf("database schema version %d predates the migration collapse (floor %d): "+
			"upgrade it with a build at or before commit %s, then retry",
			currentVersion, collapseFloor, collapseUpgradeCommit)
	}

	for _, m := range migrations() {
		if m.version <= currentVersion {
			continue
		}
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("failed to begin transaction for migration %d: %w", m.version, err)
		}
		if _, err := tx.Exec(m.sql); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("failed to apply migration %d (%s): %w", m.version, m.name, err)
		}
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
