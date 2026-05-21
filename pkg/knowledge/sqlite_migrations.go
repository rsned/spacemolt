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
			// Numbered 31 (not 3) so it runs on DBs that predate the
			// 2026-04-15 migration collapse — those DBs have
			// schema_migrations rows for the old versions 2–30, which
			// would cause any new migration numbered ≤ 30 to be skipped.
			version: 31,
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
		{
			// Backfill historical engineering login XP rows to use
			// source='passive_skill'. See spec
			// docs/superpowers/specs/2026-04-29-passive-xp-detection-design.md §4.
			// Scope is intentionally limited to skill_id='engineering' because
			// that is the only confirmed passive skill. Forward-going code in
			// XPTracker.onXPChange labels all future get_skills/login deltas
			// as passive_skill regardless of skill, so this narrowness only
			// applies to historical data.
			version: 32,
			name:    "relabel_engineering_login_xp_as_passive_skill",
			sql: `
				UPDATE xp_observations
				SET source = 'passive_skill'
				WHERE source = 'action'
				  AND action = 'login'
				  AND skill_id = 'engineering';
			`,
		},
		{
			// Purge xp_observations rows produced by the buggy first
			// implementation of the parseSkillsData get_skills callback path
			// (commit 4c4cd20, merged 2026-04-29). That path did not seed
			// xpLastXP for skills missing from the post-reconnect baseline,
			// so the first comprehensive get_skills snapshot reported each
			// untracked skill's cumulative XP as a spurious delta. Before
			// 4c4cd20 no observations with action='get_skills' existed, so
			// every such row in the DB was produced by the bug.
			//
			// The fix in this branch (parseSkillsData baseline pre-seeding)
			// prevents the bug going forward. Real passive XP will be
			// re-captured on the next 20-min poll interval after baseline
			// is established.
			version: 33,
			name:    "purge_buggy_get_skills_observations",
			sql: `
				DELETE FROM xp_observations
				WHERE action = 'get_skills'
				  AND source = 'passive_skill';
			`,
		},
		{
			version: 34,
			name:    "add_seen_players_tables",
			sql: `
				CREATE TABLE seen_players (
					player_id        TEXT PRIMARY KEY,
					username         TEXT NOT NULL,
					faction_id       TEXT,
					faction_tag      TEXT,
					clan_tag         TEXT,
					primary_color    TEXT,
					secondary_color  TEXT,
					status_message   TEXT,
					anonymous        INTEGER NOT NULL DEFAULT 0,
					first_seen_utc   TEXT NOT NULL,
					last_seen_utc    TEXT NOT NULL,
					sighting_count   INTEGER NOT NULL DEFAULT 1
				);
				CREATE INDEX seen_players_username  ON seen_players(username);
				CREATE INDEX seen_players_faction   ON seen_players(faction_id);
				CREATE INDEX seen_players_last_seen ON seen_players(last_seen_utc);

				CREATE TABLE seen_player_ships (
					player_id       TEXT NOT NULL,
					ship_class      TEXT NOT NULL,
					first_seen_utc  TEXT NOT NULL,
					last_seen_utc   TEXT NOT NULL,
					sighting_count  INTEGER NOT NULL DEFAULT 1,
					PRIMARY KEY (player_id, ship_class)
				);
				CREATE INDEX seen_player_ships_class ON seen_player_ships(ship_class);

				CREATE TABLE seen_player_sightings (
					player_id         TEXT NOT NULL,
					system_id         TEXT NOT NULL,
					poi_id            TEXT NOT NULL DEFAULT '',
					bucket_hour_utc   TEXT NOT NULL,
					ship_class        TEXT,
					source            TEXT NOT NULL,
					in_combat         INTEGER NOT NULL DEFAULT 0,
					first_seen_utc    TEXT NOT NULL,
					last_seen_utc     TEXT NOT NULL,
					observation_count INTEGER NOT NULL DEFAULT 1,
					PRIMARY KEY (player_id, system_id, poi_id, bucket_hour_utc)
				);
				CREATE INDEX seen_sightings_system ON seen_player_sightings(system_id, bucket_hour_utc);
				CREATE INDEX seen_sightings_last   ON seen_player_sightings(last_seen_utc);
			`,
		},
		{
			version: 35,
			name:    "add_faction_dashboard_tables",
			sql: `
				CREATE TABLE factions (
					faction_id      TEXT PRIMARY KEY,
					name            TEXT,
					tag             TEXT,
					leader_id       TEXT,
					leader_username TEXT,
					treasury        INTEGER,
					member_count    INTEGER,
					owned_bases     INTEGER,
					description     TEXT,
					charter         TEXT,
					emblem          TEXT,
					primary_color   TEXT,
					secondary_color TEXT,
					founded_utc     TEXT,
					intel_systems   INTEGER,
					intel_trade     INTEGER,
					captured_utc    TEXT NOT NULL
				);

				CREATE TABLE faction_members (
					faction_id    TEXT NOT NULL,
					player_id     TEXT NOT NULL,
					username      TEXT,
					role          TEXT,
					joined_utc    TEXT,
					last_seen_utc TEXT,
					is_online     INTEGER NOT NULL DEFAULT 0,
					captured_utc  TEXT NOT NULL,
					PRIMARY KEY (faction_id, player_id)
				);

				CREATE TABLE faction_relations (
					faction_id        TEXT NOT NULL,
					target_faction_id TEXT NOT NULL,
					target_name       TEXT,
					target_tag        TEXT,
					kind              TEXT NOT NULL,
					reason            TEXT,
					terms             TEXT,
					our_kills         INTEGER NOT NULL DEFAULT 0,
					their_kills       INTEGER NOT NULL DEFAULT 0,
					started_utc       TEXT,
					captured_utc      TEXT NOT NULL,
					PRIMARY KEY (faction_id, target_faction_id, kind)
				);

				CREATE TABLE faction_bases (
					faction_id    TEXT NOT NULL,
					base_id       TEXT NOT NULL,
					base_name     TEXT,
					system_id     TEXT,
					system_name   TEXT,
					poi_id        TEXT,
					services_json TEXT,
					captured_utc  TEXT NOT NULL,
					PRIMARY KEY (faction_id, base_id)
				);

				CREATE TABLE faction_facilities (
					faction_id    TEXT NOT NULL,
					base_id       TEXT NOT NULL,
					facility_id   TEXT NOT NULL,
					facility_type TEXT,
					category      TEXT,
					level         INTEGER NOT NULL DEFAULT 0,
					status        TEXT,
					recipe_id     TEXT,
					details_json  TEXT,
					captured_utc  TEXT NOT NULL,
					PRIMARY KEY (faction_id, base_id, facility_id)
				);

				CREATE TABLE faction_storage (
					faction_id   TEXT NOT NULL,
					base_id      TEXT NOT NULL,
					credits      INTEGER NOT NULL DEFAULT 0,
					item_count   INTEGER NOT NULL DEFAULT 0,
					captured_utc TEXT NOT NULL,
					PRIMARY KEY (faction_id, base_id)
				);

				CREATE TABLE faction_storage_items (
					faction_id   TEXT NOT NULL,
					base_id      TEXT NOT NULL,
					item_id      TEXT NOT NULL,
					name         TEXT,
					quantity     REAL NOT NULL DEFAULT 0,
					size         INTEGER NOT NULL DEFAULT 0,
					captured_utc TEXT NOT NULL,
					PRIMARY KEY (faction_id, base_id, item_id)
				);

				CREATE TABLE faction_orders (
					faction_id   TEXT NOT NULL,
					base_id      TEXT NOT NULL,
					order_id     TEXT NOT NULL,
					side         TEXT,
					item_id      TEXT,
					item_name    TEXT,
					price_each   REAL NOT NULL DEFAULT 0,
					quantity     REAL NOT NULL DEFAULT 0,
					captured_utc TEXT NOT NULL,
					PRIMARY KEY (faction_id, base_id, order_id)
				);

				CREATE TABLE faction_missions (
					faction_id         TEXT NOT NULL,
					base_id            TEXT NOT NULL,
					mission_id         TEXT NOT NULL,
					title              TEXT,
					type               TEXT,
					description        TEXT,
					giver_name         TEXT,
					rewards_json       TEXT,
					objectives_json    TEXT,
					assigned_player_id TEXT,
					expiration_utc     TEXT,
					captured_utc       TEXT NOT NULL,
					PRIMARY KEY (faction_id, base_id, mission_id)
				);

				CREATE TABLE faction_rooms (
					faction_id   TEXT NOT NULL,
					base_id      TEXT NOT NULL,
					room_id      TEXT NOT NULL,
					name         TEXT,
					access       TEXT,
					description  TEXT,
					captured_utc TEXT NOT NULL,
					PRIMARY KEY (faction_id, base_id, room_id)
				);

				CREATE INDEX faction_members_faction   ON faction_members(faction_id);
				CREATE INDEX faction_relations_faction  ON faction_relations(faction_id);
				CREATE INDEX faction_facilities_faction ON faction_facilities(faction_id);
				CREATE INDEX faction_storage_faction    ON faction_storage(faction_id);
				CREATE INDEX faction_orders_faction     ON faction_orders(faction_id);
				CREATE INDEX faction_missions_faction   ON faction_missions(faction_id);
				CREATE INDEX faction_rooms_faction      ON faction_rooms(faction_id);
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

		// Special case for migration 31: skip if column already exists.
		// (The ensureColumn call above normally adds it; this branch handles
		// the case where a fresh DB already has the column via initial_schema.)
		if m.version == 31 {
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

	// Self-healing safeguard: some DBs predate the 2026-04-15 migration
	// collapse and have schema_migrations rows for the old versions 2–30.
	// Those rows cause currentVersion to be >= 30, which makes the lower-
	// numbered migrations above be skipped even if their effects were never
	// applied. Run AFTER the migration loop so fresh DBs (which got
	// everything via migration 1 = initial_schema) are no-ops, while
	// pre-collapse DBs get the missing column/tables filled in.
	if err := ensureSystemsLastVisitedTickColumn(db); err != nil {
		return fmt.Errorf("ensure last_visited_tick column: %w", err)
	}
	if err := ensureCollapseMissingTables(db); err != nil {
		return fmt.Errorf("ensure collapse-missing tables: %w", err)
	}

	return nil
}

// ensureSystemsLastVisitedTickColumn adds the last_visited_tick column to the
// systems table and backfills it from poi_resources if the column is missing.
// This is idempotent and runs outside the schema_migrations version check, so
// it fixes DBs that predate the 2026-04-15 migration collapse (which left
// schema_migrations rows for versions 2–30, causing currentVersion >= 30 and
// new lower-numbered migrations to be skipped).
func ensureSystemsLastVisitedTickColumn(db *sql.DB) error {
	// The systems table may not exist yet on a fresh DB where migration 1
	// (initial_schema) has not run. In that case the safeguard has nothing
	// to do — the freshly-created table will include the column via
	// initial_schema.sql.
	var tableCount int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='systems'`,
	).Scan(&tableCount); err != nil {
		return fmt.Errorf("check systems table: %w", err)
	}
	if tableCount == 0 {
		return nil
	}

	var colCount int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('systems') WHERE name='last_visited_tick'`,
	).Scan(&colCount); err != nil {
		return fmt.Errorf("check last_visited_tick column: %w", err)
	}
	if colCount > 0 {
		return nil
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(
		`ALTER TABLE systems ADD COLUMN last_visited_tick INTEGER NOT NULL DEFAULT 0`,
	); err != nil {
		return fmt.Errorf("add last_visited_tick column: %w", err)
	}

	if _, err := tx.Exec(`
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
		)
	`); err != nil {
		return fmt.Errorf("backfill last_visited_tick: %w", err)
	}

	return tx.Commit()
}

// ensureCollapseMissingTables creates tables that are declared in
// initial_schema.sql but may be absent from DBs that predate the
// 2026-04-15 migration collapse. All DDL uses CREATE TABLE IF NOT EXISTS
// / CREATE INDEX IF NOT EXISTS so the pass is idempotent and a no-op on
// DBs that already have them.
//
// The tables covered here are ones confirmed to be actively written by the
// current code:
//   - agent_ships       (pkg/knowledge/sqlite_player.go)
//   - ships             (pkg/knowledge/sqlite_catalog.go — catalog import)
//   - ship_build_materials (pkg/knowledge/sqlite_catalog.go — catalog import)
//   - base_facilities   (pkg/knowledge/sqlite.go — RememberBase)
func ensureCollapseMissingTables(db *sql.DB) error {
	const ddl = `
CREATE TABLE IF NOT EXISTS "ships" (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    class TEXT,
    category TEXT,
    description TEXT,
    lore TEXT,
    faction TEXT,
    tier INTEGER DEFAULT 0,
    scale INTEGER DEFAULT 1,
    price INTEGER DEFAULT 0,
    base_hull INTEGER DEFAULT 0,
    base_shield INTEGER DEFAULT 0,
    base_shield_recharge INTEGER DEFAULT 0,
    base_armor INTEGER DEFAULT 0,
    base_speed INTEGER DEFAULT 0,
    base_fuel INTEGER DEFAULT 0,
    cargo_capacity INTEGER DEFAULT 0,
    cpu_capacity INTEGER DEFAULT 0,
    power_capacity INTEGER DEFAULT 0,
    weapon_slots INTEGER DEFAULT 0,
    defense_slots INTEGER DEFAULT 0,
    utility_slots INTEGER DEFAULT 0,
    build_time INTEGER DEFAULT 0,
    shipyard_tier INTEGER DEFAULT 0,
    starter_ship BOOLEAN DEFAULT 0,
    tow_speed_bonus INTEGER DEFAULT 0,
    required_skills TEXT DEFAULT '{}',
    default_modules TEXT DEFAULT '[]',
    flavor_tags TEXT DEFAULT '[]',
    last_updated_tick INTEGER DEFAULT 0,
    passive_recipes TEXT DEFAULT '[]'
);

CREATE INDEX IF NOT EXISTS idx_ships_class ON ships(class);
CREATE INDEX IF NOT EXISTS idx_ships_faction ON ships(faction);

CREATE TABLE IF NOT EXISTS "agent_ships" (
    id TEXT PRIMARY KEY,
    owner_id TEXT NOT NULL,
    class_id TEXT NOT NULL,
    name TEXT,
    hull REAL DEFAULT 0,
    max_hull REAL DEFAULT 0,
    shield REAL DEFAULT 0,
    max_shield REAL DEFAULT 0,
    shield_recharge REAL DEFAULT 0,
    armor REAL DEFAULT 0,
    speed REAL DEFAULT 0,
    fuel REAL DEFAULT 0,
    max_fuel REAL DEFAULT 0,
    cargo_used REAL DEFAULT 0,
    cargo_capacity REAL DEFAULT 0,
    cpu_used REAL DEFAULT 0,
    cpu_capacity REAL DEFAULT 0,
    power_used REAL DEFAULT 0,
    power_capacity REAL DEFAULT 0,
    weapon_slots INTEGER DEFAULT 0,
    defense_slots INTEGER DEFAULT 0,
    utility_slots INTEGER DEFAULT 0,
    docked_at_base TEXT,
    last_updated_tick INTEGER DEFAULT 0,
    FOREIGN KEY (owner_id) REFERENCES players(id) ON DELETE CASCADE,
    FOREIGN KEY (class_id) REFERENCES "ships"(id)
);

CREATE INDEX IF NOT EXISTS idx_agent_ships_class ON agent_ships(class_id);
CREATE INDEX IF NOT EXISTS idx_agent_ships_owner ON agent_ships(owner_id);

CREATE TABLE IF NOT EXISTS "ship_build_materials" (
    ship_class_id TEXT NOT NULL,
    item_id TEXT NOT NULL,
    quantity INTEGER NOT NULL,
    PRIMARY KEY (ship_class_id, item_id),
    FOREIGN KEY (ship_class_id) REFERENCES "ships"(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS "base_facilities" (
    base_id TEXT NOT NULL,
    facility_name TEXT NOT NULL,
    instance_id TEXT NOT NULL DEFAULT '',
    description TEXT DEFAULT '',
    category TEXT DEFAULT 'unknown',
    level INTEGER DEFAULT 0,
    active BOOLEAN DEFAULT 1,
    maintenance_satisfied BOOLEAN DEFAULT 1,
    service TEXT DEFAULT '',
    recipe_id TEXT DEFAULT '',
    last_updated_tick INTEGER DEFAULT 0,
    PRIMARY KEY (base_id, instance_id),
    FOREIGN KEY (base_id) REFERENCES bases(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_base_facilities_category ON base_facilities(category);
CREATE INDEX IF NOT EXISTS idx_base_facilities_recipe ON base_facilities(recipe_id);
`
	if _, err := db.Exec(ddl); err != nil {
		return fmt.Errorf("create missing tables: %w", err)
	}
	return nil
}
