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
		{
			version: 36,
			name:    "market_buy_demand",
			sql: `
				CREATE TABLE market_buy_demand (
					station_id      TEXT NOT NULL,
					system_id       TEXT,
					item_id         TEXT NOT NULL,
					item_name       TEXT,
					best_buy_price  REAL NOT NULL DEFAULT 0,
					buy_quantity    REAL NOT NULL DEFAULT 0,
					captured_utc    TEXT NOT NULL,
					PRIMARY KEY (station_id, item_id)
				);
				CREATE INDEX market_buy_demand_item ON market_buy_demand(item_id);

				CREATE TABLE market_buy_orders (
					station_id    TEXT NOT NULL,
					system_id     TEXT,
					item_id       TEXT NOT NULL,
					item_name     TEXT,
					price_each    REAL NOT NULL DEFAULT 0,
					quantity      REAL NOT NULL DEFAULT 0,
					source        TEXT,
					captured_utc  TEXT NOT NULL
				);
				CREATE INDEX market_buy_orders_station_item ON market_buy_orders(station_id, item_id);
				CREATE INDEX market_buy_orders_item ON market_buy_orders(item_id);
			`,
		},
		{
			version: 37,
			name:    "drop_market_buy_demand",
			sql:     `DROP TABLE IF EXISTS market_buy_demand;`,
		},
		{
			version: 38,
			name:    "market_demand_history",
			sql: `
				CREATE TABLE market_demand_history (
					station_id     TEXT NOT NULL,
					system_id      TEXT,
					item_id        TEXT NOT NULL,
					item_name      TEXT,
					bucket_utc     TEXT NOT NULL,
					captured_utc   TEXT NOT NULL,
					best_price     REAL NOT NULL DEFAULT 0,
					total_qty      REAL NOT NULL DEFAULT 0,
					sm_best_price  REAL NOT NULL DEFAULT 0,
					sm_qty         REAL NOT NULL DEFAULT 0,
					order_count    INTEGER NOT NULL DEFAULT 0,
					PRIMARY KEY (station_id, item_id, bucket_utc)
				);
				CREATE INDEX market_demand_history_item ON market_demand_history(item_id, bucket_utc);
			`,
		},
		{
			version: 39,
			name:    "market_buy_orders_my_quantity",
			sql:     `ALTER TABLE market_buy_orders ADD COLUMN my_quantity REAL NOT NULL DEFAULT 0;`,
		},
		{
			version: 40,
			name:    "add_detected_by_to_pois_and_resources",
			sql: `
				ALTER TABLE pois ADD COLUMN detected_by TEXT;
				ALTER TABLE poi_resources ADD COLUMN detected_by TEXT;
			`,
		},
		{
			version: 41,
			name:    "add_faction_fuel_bunkers",
			sql: `
				CREATE TABLE faction_fuel_bunkers (
					faction_id    TEXT NOT NULL,
					base_id       TEXT NOT NULL,
					base_name     TEXT,
					fuel_reserve  INTEGER NOT NULL DEFAULT 0,
					fuel_capacity INTEGER NOT NULL DEFAULT 0,
					captured_utc  TEXT NOT NULL,
					PRIMARY KEY (faction_id, base_id)
				);
			`,
		},
		{
			// Catalog field refresh: surface module/combat/economy stats the
			// server added (passenger berths, bypass bonuses, slot, quest/region
			// flags, ship capabilities, recipe facility/fuel flags). Import tools
			// decode these; columns let the agent KB persist and query them.
			version: 42,
			name:    "add_catalog_stat_fields",
			sql: `
				ALTER TABLE items ADD COLUMN quest_item BOOLEAN NOT NULL DEFAULT 0;
				ALTER TABLE items ADD COLUMN extracted_by TEXT;
				ALTER TABLE items ADD COLUMN required_skills TEXT;
				ALTER TABLE items ADD COLUMN region_lock TEXT;
				ALTER TABLE items ADD COLUMN passenger_economy_berths INTEGER NOT NULL DEFAULT 0;
				ALTER TABLE items ADD COLUMN passenger_business_berths INTEGER NOT NULL DEFAULT 0;
				ALTER TABLE items ADD COLUMN passenger_first_berths INTEGER NOT NULL DEFAULT 0;

				ALTER TABLE item_modules ADD COLUMN slot TEXT;

				ALTER TABLE item_weapons ADD COLUMN armor_bypass_bonus REAL;
				ALTER TABLE item_weapons ADD COLUMN shield_bypass_bonus REAL;

				ALTER TABLE item_utilities ADD COLUMN cpu_bonus INTEGER;
				ALTER TABLE item_utilities ADD COLUMN max_fuel_bonus INTEGER;
				ALTER TABLE item_utilities ADD COLUMN hull_penalty INTEGER;
				ALTER TABLE item_utilities ADD COLUMN speed_penalty INTEGER;

				ALTER TABLE item_ammo ADD COLUMN modifiers TEXT;

				ALTER TABLE recipes ADD COLUMN facility_only BOOLEAN NOT NULL DEFAULT 0;
				ALTER TABLE recipes ADD COLUMN no_recycle BOOLEAN NOT NULL DEFAULT 0;
				ALTER TABLE recipes ADD COLUMN fuel_output INTEGER NOT NULL DEFAULT 0;

				ALTER TABLE ships ADD COLUMN based_on TEXT;
				ALTER TABLE ships ADD COLUMN npc_role TEXT;
				ALTER TABLE ships ADD COLUMN special TEXT;
				ALTER TABLE ships ADD COLUMN required_reputation INTEGER NOT NULL DEFAULT 0;
				ALTER TABLE ships ADD COLUMN piloting_required INTEGER NOT NULL DEFAULT 0;
				ALTER TABLE ships ADD COLUMN inherent_capabilities TEXT;
				`,
		},
		{
			// Passenger catalog: identity-only record of citizens seen as
			// ferry passengers across the galaxy (Name, Empire/citizenship,
			// Bio, travel class). Populated passively from passenger-bearing
			// responses; no per-sighting fare/destination tracking.
			version: 43,
			name:    "add_passengers_table",
			sql: `
				CREATE TABLE passengers (
					citizen_id     TEXT PRIMARY KEY,
					name           TEXT NOT NULL,
					citizenship    TEXT,
					bio            TEXT,
					class          TEXT,
					first_seen_utc TEXT NOT NULL,
					last_seen_utc  TEXT NOT NULL,
					sighting_count INTEGER NOT NULL DEFAULT 1
				);
				CREATE INDEX passengers_citizenship ON passengers(citizenship);
				CREATE INDEX passengers_name ON passengers(name);
			`,
		},
		{
			// Sell-side market capture: mirror of the buy-side demand tables
			// (market_buy_orders + market_demand_history). market_sell_orders is
			// the current per-station snapshot (replaced per station each
			// capture); market_supply_history is the hourly time series. Both
			// populated alongside demand from the same view_market read.
			version: 44,
			name:    "add_market_sell_supply_tables",
			sql: `
				CREATE TABLE market_sell_orders (
					station_id    TEXT NOT NULL,
					system_id     TEXT,
					item_id       TEXT NOT NULL,
					item_name     TEXT,
					price_each    REAL NOT NULL DEFAULT 0,
					quantity      REAL NOT NULL DEFAULT 0,
					my_quantity   REAL NOT NULL DEFAULT 0,
					source        TEXT,
					captured_utc  TEXT NOT NULL
				);
				CREATE INDEX market_sell_orders_station_item ON market_sell_orders(station_id, item_id);
				CREATE INDEX market_sell_orders_item ON market_sell_orders(item_id);

				CREATE TABLE market_supply_history (
					station_id     TEXT NOT NULL,
					system_id      TEXT,
					item_id        TEXT NOT NULL,
					item_name      TEXT,
					bucket_utc     TEXT NOT NULL,
					captured_utc   TEXT NOT NULL,
					best_price     REAL NOT NULL DEFAULT 0,
					total_qty      REAL NOT NULL DEFAULT 0,
					sm_best_price  REAL NOT NULL DEFAULT 0,
					sm_qty         REAL NOT NULL DEFAULT 0,
					order_count    INTEGER NOT NULL DEFAULT 0,
					PRIMARY KEY (station_id, item_id, bucket_utc)
				);
				CREATE INDEX market_supply_history_item ON market_supply_history(item_id, bucket_utc);
			`,
		},
		{
			// Server v0.41x switched recipe crafting_time to a fractional value
			// (e.g. 2.76s); the column was created with INTEGER affinity. Rebuild
			// the recipes table to give crafting_time REAL affinity. recipes is a
			// repopulated catalog cache (StoreRecipes DELETEs + re-INSERTs) with no
			// inbound foreign keys, so the rebuild is safe. Guarded in the runner
			// for fixtures that lack the table.
			version: 45,
			name:    "recipes_crafting_time_real",
			sql: `
				CREATE TABLE recipes_new (
					id TEXT PRIMARY KEY,
					name TEXT NOT NULL,
					description TEXT,
					category TEXT,
					crafting_time REAL DEFAULT 0,
					base_quality INTEGER DEFAULT 0,
					skill_quality_mod INTEGER DEFAULT 0,
					required_skills TEXT DEFAULT '{}',
					last_updated_tick INTEGER DEFAULT 0,
					hidden BOOLEAN DEFAULT 0,
					facility_only BOOLEAN NOT NULL DEFAULT 0,
					no_recycle BOOLEAN NOT NULL DEFAULT 0,
					fuel_output INTEGER NOT NULL DEFAULT 0
				);
				INSERT INTO recipes_new (id, name, description, category, crafting_time,
					base_quality, skill_quality_mod, required_skills, last_updated_tick,
					hidden, facility_only, no_recycle, fuel_output)
				SELECT id, name, description, category, crafting_time,
					base_quality, skill_quality_mod, required_skills, last_updated_tick,
					hidden, facility_only, no_recycle, fuel_output
				FROM recipes;
				DROP TABLE recipes;
				ALTER TABLE recipes_new RENAME TO recipes;
				CREATE INDEX idx_recipes_category ON recipes(category);
			`,
		},
		{
			version: 46,
			name:    "drop_market_snapshot_tables",
			sql: `
				DROP TABLE IF EXISTS market_listings;
				DROP TABLE IF EXISTS market_snapshots;
				DROP TABLE IF EXISTS market_analyses;
				DROP TABLE IF EXISTS price_trends;
			`,
		},
		{
			version: 47,
			name:    "ship_listings_v2",
			sql: `
				DROP TABLE IF EXISTS ship_listings;
				CREATE TABLE ship_listings (
					id INTEGER PRIMARY KEY AUTOINCREMENT,
					system_id TEXT NOT NULL,
					system_name TEXT NOT NULL,
					station_id TEXT NOT NULL,
					station_name TEXT NOT NULL,
					listing_id TEXT NOT NULL,
					ship_id TEXT NOT NULL,
					class_id TEXT NOT NULL,
					ship_name TEXT,
					category TEXT,
					tier INTEGER,
					scale INTEGER,
					hull INTEGER,
					max_hull INTEGER,
					shield INTEGER,
					modules_count INTEGER,
					price INTEGER NOT NULL,
					seller TEXT,
					listed_at TEXT,
					game_tick INTEGER NOT NULL,
					captured_at TEXT NOT NULL,
					agent_id TEXT,
					FOREIGN KEY (station_id) REFERENCES pois(id) ON DELETE CASCADE
				);
				CREATE INDEX idx_ship_listings_station ON ship_listings(station_id, captured_at DESC);
				CREATE INDEX idx_ship_listings_class ON ship_listings(class_id, captured_at DESC);
			`,
		},
		{
			version: 48,
			name:    "public_facilities",
			sql: `
				CREATE TABLE IF NOT EXISTS public_facilities (
					station_id     TEXT NOT NULL,
					facility_id    TEXT NOT NULL,
					recipe_id      TEXT NOT NULL DEFAULT '',
					facility_name  TEXT DEFAULT '',
					category       TEXT DEFAULT '',
					level          INTEGER DEFAULT 1,
					rental_fee_per_run INTEGER DEFAULT 0,
					owner_faction  TEXT DEFAULT '',
					public         INTEGER DEFAULT 1,
					details_json   TEXT DEFAULT '',
					last_seen_tick INTEGER DEFAULT 0,
					last_seen_utc  TEXT DEFAULT '',
					PRIMARY KEY (station_id, facility_id)
				);
				CREATE INDEX IF NOT EXISTS idx_public_facilities_recipe ON public_facilities(recipe_id);
			`,
		},
		{
			// The game ships no catalog or codex for its ~45 wildlife species, so
			// every field below is something an agent has to go and see. These
			// four tables are the field guide we build ourselves.
			//
			// No foreign keys to pois/systems on purpose: creatures live at belts
			// and clouds we may never have surveyed, and a hunt must be able to
			// record a sighting at a POI the KB has not heard of. (Nothing in this
			// DB enables PRAGMA foreign_keys anyway.)
			version: 49,
			name:    "wildlife_field_guide",
			sql: `
				CREATE TABLE IF NOT EXISTS wildlife_species (
					species           TEXT PRIMARY KEY,
					name              TEXT NOT NULL DEFAULT '',
					role              TEXT NOT NULL DEFAULT '',
					max_hull          INTEGER NOT NULL DEFAULT 0,
					max_shield        INTEGER NOT NULL DEFAULT 0,
					danger            TEXT NOT NULL DEFAULT '',
					danger_scanned_utc TEXT NOT NULL DEFAULT '',
					habitats          TEXT NOT NULL DEFAULT '',
					first_seen_utc    TEXT NOT NULL DEFAULT '',
					last_seen_utc     TEXT NOT NULL DEFAULT ''
				);

				-- What a species SHOOTS WITH, which is the half of the fight a
				-- resistance fit is chosen against. Keyed per battle so
				-- re-importing a battle log replaces its row instead of
				-- double-counting: the raw shots live on the server, never here,
				-- so these counters ARE the observation, and hit rate is
				-- computed on read from hits/shots.
				CREATE TABLE IF NOT EXISTS wildlife_attacks (
					species      TEXT NOT NULL,
					battle_id    TEXT NOT NULL DEFAULT '',
					weapon_name  TEXT NOT NULL DEFAULT '',
					damage_type  TEXT NOT NULL DEFAULT '',
					shot_kind    TEXT NOT NULL DEFAULT '',
					shots        INTEGER NOT NULL DEFAULT 0,
					hits         INTEGER NOT NULL DEFAULT 0,
					damage_total REAL NOT NULL DEFAULT 0,
					damage_min   REAL NOT NULL DEFAULT 0,
					damage_max   REAL NOT NULL DEFAULT 0,
					observed_utc TEXT NOT NULL DEFAULT '',
					PRIMARY KEY (species, battle_id, weapon_name, damage_type, shot_kind)
				);
				CREATE INDEX IF NOT EXISTS idx_wildlife_attacks_species ON wildlife_attacks(species);
				CREATE INDEX IF NOT EXISTS idx_wildlife_attacks_damage_type ON wildlife_attacks(damage_type);

				CREATE TABLE IF NOT EXISTS wildlife_sightings (
					id              INTEGER PRIMARY KEY AUTOINCREMENT,
					species         TEXT NOT NULL,
					system_id       TEXT NOT NULL DEFAULT '',
					poi_id          TEXT NOT NULL DEFAULT '',
					source          TEXT NOT NULL DEFAULT '',
					observed_count  INTEGER NOT NULL DEFAULT 0,
					abundance       TEXT NOT NULL DEFAULT '',
					ranched         INTEGER NOT NULL DEFAULT 0,
					branded         INTEGER NOT NULL DEFAULT 0,
					in_combat       INTEGER NOT NULL DEFAULT 0,
					bloom_status    TEXT NOT NULL DEFAULT '',
					bloom_intensity REAL NOT NULL DEFAULT 0,
					game_tick       INTEGER NOT NULL DEFAULT 0,
					observed_utc    TEXT NOT NULL DEFAULT '',
					agent_id        TEXT NOT NULL DEFAULT ''
				);
				CREATE INDEX IF NOT EXISTS idx_wildlife_sightings_species ON wildlife_sightings(species, observed_utc DESC);
				CREATE INDEX IF NOT EXISTS idx_wildlife_sightings_place ON wildlife_sightings(system_id, poi_id, observed_utc DESC);

				CREATE TABLE IF NOT EXISTS wildlife_kills (
					creature_id    TEXT NOT NULL,
					game_tick      INTEGER NOT NULL DEFAULT 0,
					species        TEXT NOT NULL DEFAULT '',
					creature_name  TEXT NOT NULL DEFAULT '',
					role           TEXT NOT NULL DEFAULT '',
					max_hull       INTEGER NOT NULL DEFAULT 0,
					system_id      TEXT NOT NULL DEFAULT '',
					poi_id         TEXT NOT NULL DEFAULT '',
					battle_id      TEXT NOT NULL DEFAULT '',
					duration_ticks INTEGER NOT NULL DEFAULT 0,
					damage_dealt   INTEGER NOT NULL DEFAULT 0,
					damage_taken   INTEGER NOT NULL DEFAULT 0,
					wreck_id       TEXT NOT NULL DEFAULT '',
					salvage_value  INTEGER NOT NULL DEFAULT 0,
					carcass_read   INTEGER NOT NULL DEFAULT 0,
					killed_utc     TEXT NOT NULL DEFAULT '',
					agent_id       TEXT NOT NULL DEFAULT '',
					PRIMARY KEY (creature_id, game_tick)
				);
				CREATE INDEX IF NOT EXISTS idx_wildlife_kills_species ON wildlife_kills(species, killed_utc DESC);

				CREATE TABLE IF NOT EXISTS wildlife_kill_drops (
					creature_id TEXT NOT NULL,
					game_tick   INTEGER NOT NULL DEFAULT 0,
					item_id     TEXT NOT NULL,
					quantity    REAL NOT NULL DEFAULT 0,
					PRIMARY KEY (creature_id, game_tick, item_id)
				);
				CREATE INDEX IF NOT EXISTS idx_wildlife_kill_drops_item ON wildlife_kill_drops(item_id);
			`,
		},
		// NOTE: the ship-class prestige/unlock columns added for server v0.495.1
		// are NOT a numbered migration. A plain `ALTER TABLE ships` here fails on
		// pre-collapse DBs, where `ships` does not exist until
		// ensureCollapseMissingTables runs *after* this loop. They are applied by
		// ensureShipClassPrestigeCols instead, which runs once the table is
		// guaranteed to exist.
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

		// Special case for migration 40: the pois/poi_resources tables come from
		// migration 1 (initial_schema). Some narrow migration-test fixtures fake
		// "migration 1 applied" without creating those tables, so guard the
		// ALTERs — if pois is absent there's nothing to alter; record as applied.
		if m.version == 40 {
			var tableCount int
			if err := db.QueryRow(
				`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='pois'`,
			).Scan(&tableCount); err != nil {
				return fmt.Errorf("check pois table: %w", err)
			}
			if tableCount == 0 {
				if _, err := db.Exec(
					"INSERT INTO schema_migrations (version, applied_at) VALUES (?, datetime('now'))",
					m.version,
				); err != nil {
					return fmt.Errorf("failed to record migration %d: %w", m.version, err)
				}
				continue
			}
		}

		// Special case for migration 42: the items/recipes/ships catalog tables
		// come from migration 1 (initial_schema). Narrow migration-test fixtures
		// fake "migration 1 applied" without creating them, so guard the ALTERs —
		// if items is absent there's nothing to alter; record as applied.
		if m.version == 42 {
			var tableCount int
			if err := db.QueryRow(
				`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='items'`,
			).Scan(&tableCount); err != nil {
				return fmt.Errorf("check items table: %w", err)
			}
			if tableCount == 0 {
				if _, err := db.Exec(
					"INSERT INTO schema_migrations (version, applied_at) VALUES (?, datetime('now'))",
					m.version,
				); err != nil {
					return fmt.Errorf("failed to record migration %d: %w", m.version, err)
				}
				continue
			}
		}

		// Special case for migration 45: rebuilds the recipes catalog table.
		// Narrow migration-test fixtures may lack it; if recipes is absent there
		// is nothing to rebuild, so record as applied and skip.
		if m.version == 45 {
			var tableCount int
			if err := db.QueryRow(
				`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='recipes'`,
			).Scan(&tableCount); err != nil {
				return fmt.Errorf("check recipes table: %w", err)
			}
			if tableCount == 0 {
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
	if err := ensurePublicFacilitiesRentalCol(db); err != nil {
		return fmt.Errorf("ensure public_facilities rental_fee_per_run column: %w", err)
	}
	if err := ensureShipClassPrestigeCols(db); err != nil {
		return fmt.Errorf("ensure ships prestige/unlock columns: %w", err)
	}
	if err := ensureMissionTemplatesProceduralCol(db); err != nil {
		return fmt.Errorf("ensure mission_templates procedural column: %w", err)
	}

	return nil
}

// ensureShipClassPrestigeCols adds the ship-class prestige/unlock columns
// introduced by server v0.495.1 (required_achievement, required_faction_
// achievement, required_faction_leader, prestige_lock, default_loadout_version).
//
// This is a self-healing ensure rather than a numbered migration on purpose: a
// migration's `ALTER TABLE ships` runs inside the migration loop, which is
// BEFORE ensureCollapseMissingTables creates `ships` on pre-collapse DBs, so it
// would die with "no such table: ships". Running here — after the table is
// guaranteed to exist — covers every cohort: fresh DBs (ships from
// initial_schema), current DBs, and pre-collapse DBs alike.
func ensureShipClassPrestigeCols(db *sql.DB) error {
	var tableCount int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='ships'`,
	).Scan(&tableCount); err != nil {
		return fmt.Errorf("check ships table: %w", err)
	}
	if tableCount == 0 {
		return nil // table not created yet; nothing to reconcile
	}

	cols := []struct {
		name string
		ddl  string
	}{
		{"required_achievement", `ALTER TABLE ships ADD COLUMN required_achievement TEXT DEFAULT ''`},
		{"required_faction_achievement", `ALTER TABLE ships ADD COLUMN required_faction_achievement TEXT DEFAULT ''`},
		{"required_faction_leader", `ALTER TABLE ships ADD COLUMN required_faction_leader INTEGER NOT NULL DEFAULT 0`},
		{"prestige_lock", `ALTER TABLE ships ADD COLUMN prestige_lock TEXT DEFAULT ''`},
		{"default_loadout_version", `ALTER TABLE ships ADD COLUMN default_loadout_version INTEGER NOT NULL DEFAULT 0`},
	}

	for _, c := range cols {
		var present int
		if err := db.QueryRow(
			`SELECT COUNT(*) FROM pragma_table_info('ships') WHERE name=?`, c.name,
		).Scan(&present); err != nil {
			return fmt.Errorf("check ships.%s column: %w", c.name, err)
		}
		if present > 0 {
			continue
		}
		if _, err := db.Exec(c.ddl); err != nil {
			return fmt.Errorf("add ships.%s column: %w", c.name, err)
		}
	}

	return nil
}

// ensureMissionTemplatesProceduralCol adds the mission_templates.procedural
// column (0 = hand-authored template, 1 = route-generated/procedural) to DBs
// that predate it. Self-healing ensure rather than a numbered migration so it
// runs AFTER ensureCollapseMissingTables guarantees the table exists — mirrors
// ensureShipClassPrestigeCols.
func ensureMissionTemplatesProceduralCol(db *sql.DB) error {
	var tableCount int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='mission_templates'`,
	).Scan(&tableCount); err != nil {
		return fmt.Errorf("check mission_templates table: %w", err)
	}
	if tableCount == 0 {
		return nil // table not created yet; nothing to reconcile
	}
	var present int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('mission_templates') WHERE name='procedural'`,
	).Scan(&present); err != nil {
		return fmt.Errorf("check mission_templates.procedural column: %w", err)
	}
	if present > 0 {
		return nil
	}
	if _, err := db.Exec(`ALTER TABLE mission_templates ADD COLUMN procedural INTEGER DEFAULT 0`); err != nil {
		return fmt.Errorf("add mission_templates.procedural column: %w", err)
	}
	return nil
}

// ensurePublicFacilitiesRentalCol reconciles DBs that applied the original
// migration 48, which created public_facilities with a provisional `labor_cost`
// column. Migration 48 was later corrected in place to `rental_fee_per_run`
// (the real server field production.rental_fee_per_run), but an in-place edit
// never re-runs on a DB that already recorded version 48 — so those DBs keep
// the old column while the code inserts the new one. This idempotent fixup
// renames the column when needed. Fresh DBs (new migration 48) already have
// rental_fee_per_run and are a no-op.
func ensurePublicFacilitiesRentalCol(db *sql.DB) error {
	var tableCount int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='public_facilities'`,
	).Scan(&tableCount); err != nil {
		return fmt.Errorf("check public_facilities table: %w", err)
	}
	if tableCount == 0 {
		return nil // table not created yet; nothing to reconcile
	}

	var newCol int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('public_facilities') WHERE name='rental_fee_per_run'`,
	).Scan(&newCol); err != nil {
		return fmt.Errorf("check rental_fee_per_run column: %w", err)
	}
	if newCol > 0 {
		return nil // already correct
	}

	var oldCol int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('public_facilities') WHERE name='labor_cost'`,
	).Scan(&oldCol); err != nil {
		return fmt.Errorf("check labor_cost column: %w", err)
	}
	if oldCol == 0 {
		return nil // neither column present; leave alone (unexpected shape)
	}

	if _, err := db.Exec(
		`ALTER TABLE public_facilities RENAME COLUMN labor_cost TO rental_fee_per_run`,
	); err != nil {
		return fmt.Errorf("rename labor_cost to rental_fee_per_run: %w", err)
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
    passive_recipes TEXT DEFAULT '[]',
    required_achievement TEXT DEFAULT '',
    required_faction_achievement TEXT DEFAULT '',
    required_faction_leader INTEGER NOT NULL DEFAULT 0,
    prestige_lock TEXT DEFAULT '',
    default_loadout_version INTEGER NOT NULL DEFAULT 0
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
