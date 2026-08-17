-- Agent asset + capability ledger. Keyed on the server's stable hex player_id.
-- Tables mirror the wire shapes: maps become narrow tables, structs become wide
-- ones, so a new skill or faction is a new ROW and needs no migration.
--
-- No foreign keys to the item/ship catalogs on purpose: the legacy agent_ships
-- table declares FOREIGN KEY(class_id) REFERENCES ships(id) and 96% of observed
-- class_id values do not resolve (prospector/prospect, excavator/excavation).
-- Store class_id verbatim and resolve at read time so a mismatch is visible
-- rather than fatal.

CREATE TABLE IF NOT EXISTS agents (
    player_id  TEXT PRIMARY KEY,
    agent_id   TEXT NOT NULL DEFAULT '',
    username   TEXT NOT NULL DEFAULT '',
    first_seen TEXT NOT NULL DEFAULT '',
    last_seen  TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_agents_agent_id ON agents(agent_id);

-- captured_at is PER TABLE, not per agent. The sources refresh on different
-- cadences (carrier profile is two free queries; a storage sweep is N calls),
-- and one agent-level timestamp would make a 20-minute-old skill level and a
-- 6-day-old holding indistinguishable.
-- STATION IDS ARE DUAL-NAMED. Every station can appear under a base id or a
-- poi id, and for 15 of them the two differ: the five empire capitals
-- (confederacy_central_command = sol_central, central_nexus = the_core,
-- frontier_station = mobile_capital, grand_exchange_station = grand_exchange,
-- crimson_war_citadel = war_citadel) are legacy names the server kept live and
-- uses interchangeably, the seven pirate strongholds carry a mechanical
-- "_station" suffix, and three player bases are hex-id pairs.
--
-- This table stores BOTH forms: home_base and docked_at_base are base ids,
-- current_poi is a poi id. agent_hulls.location_base_id is a base id.
--
-- So any reader joining these columns against pois.id silently drops all five
-- capitals and all seven pirate strongholds. There are no FKs here by design,
-- so nothing will error -- it will just under-report. The authoritative map is
-- bases(id, poi_id) in spacemolt-knowledge.db; resolve through it rather than
-- stripping "_station", which only covers the pirate cases and none of the
-- genuine renames. Verified against databot 2026-08-01.
CREATE TABLE IF NOT EXISTS agent_profile (
    player_id      TEXT PRIMARY KEY,
    username       TEXT NOT NULL DEFAULT '',
    empire         TEXT NOT NULL DEFAULT '',
    credits        REAL NOT NULL DEFAULT 0,
    home_base      TEXT NOT NULL DEFAULT '',
    docked_at_base TEXT NOT NULL DEFAULT '',
    current_system TEXT NOT NULL DEFAULT '',
    current_poi    TEXT NOT NULL DEFAULT '',
    active_ship_id TEXT NOT NULL DEFAULT '',
    faction_id     TEXT NOT NULL DEFAULT '',
    faction_rank   TEXT NOT NULL DEFAULT '',
    experience     INTEGER NOT NULL DEFAULT 0,
    captured_at    TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_agent_profile_faction ON agent_profile(faction_id);

CREATE TABLE IF NOT EXISTS agent_skills (
    player_id   TEXT NOT NULL,
    skill       TEXT NOT NULL,
    level       INTEGER NOT NULL DEFAULT 0,
    xp          REAL NOT NULL DEFAULT 0,
    captured_at TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (player_id, skill)
);
CREATE INDEX IF NOT EXISTS idx_agent_skills_skill ON agent_skills(skill, level);

-- baseline is the load-bearing column, not reputation: reputation floats above
-- baseline from missions and decays back toward it when idle, so baseline is
-- what makes an unlock permanent (an_introduction raises the pirate baseline
-- from -30 to 10, which is what makes stronghold docking stick).
CREATE TABLE IF NOT EXISTS agent_standings (
    player_id          TEXT NOT NULL,
    faction            TEXT NOT NULL,
    reputation         INTEGER NOT NULL DEFAULT 0,
    baseline           INTEGER NOT NULL DEFAULT 0,
    outstanding_bounty INTEGER NOT NULL DEFAULT 0,
    jailed_until       TEXT NOT NULL DEFAULT '',
    captured_at        TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (player_id, faction)
);
CREATE INDEX IF NOT EXISTS idx_agent_standings_faction ON agent_standings(faction, baseline);

-- NO foreign key on class_id: see the header note. Also no agent_storage_ships
-- table — list_ships already reports every owned hull AND its location, and
-- OwnedShip is a strict superset of StorageShip, so view_storage's ships array
-- is a cross-check rather than a second source.
CREATE TABLE IF NOT EXISTS agent_hulls (
    player_id        TEXT NOT NULL,
    ship_id          TEXT NOT NULL,
    class_id         TEXT NOT NULL DEFAULT '',
    class_name       TEXT NOT NULL DEFAULT '',
    is_active        INTEGER NOT NULL DEFAULT 0,
    hull_current     INTEGER NOT NULL DEFAULT 0,
    hull_max         INTEGER NOT NULL DEFAULT 0,
    hull_raw         TEXT NOT NULL DEFAULT '',
    fuel_current     INTEGER NOT NULL DEFAULT 0,
    fuel_max         INTEGER NOT NULL DEFAULT 0,
    fuel_raw         TEXT NOT NULL DEFAULT '',
    cargo_used       INTEGER NOT NULL DEFAULT 0,
    location         TEXT NOT NULL DEFAULT '',
    location_base_id TEXT NOT NULL DEFAULT '',
    modules          INTEGER NOT NULL DEFAULT 0,
    listing_id       TEXT NOT NULL DEFAULT '',
    listing_price    INTEGER NOT NULL DEFAULT 0,
    listing_base_id  TEXT NOT NULL DEFAULT '',
    captured_at      TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (player_id, ship_id)
);
CREATE INDEX IF NOT EXISTS idx_agent_hulls_base ON agent_hulls(location_base_id);
CREATE INDEX IF NOT EXISTS idx_agent_hulls_class ON agent_hulls(class_id);

-- CarrierProfile + CarrierCapacity + CarrierTierProgress flattened into one
-- row. The remaining_* columns are what make "who is still in probation and
-- how far off are they" a query instead of a worker-log sweep.
CREATE TABLE IF NOT EXISTS agent_carrier (
    player_id                       TEXT PRIMARY KEY,
    tier                            TEXT NOT NULL DEFAULT '',
    successful_deliveries           INTEGER NOT NULL DEFAULT 0,
    delivered_value                 INTEGER NOT NULL DEFAULT 0,
    priority_deliveries             INTEGER NOT NULL DEFAULT 0,
    returns                         INTEGER NOT NULL DEFAULT 0,
    breaches                        INTEGER NOT NULL DEFAULT 0,
    defaults                        INTEGER NOT NULL DEFAULT 0,
    active_contracts                INTEGER NOT NULL DEFAULT 0,
    active_liability                INTEGER NOT NULL DEFAULT 0,
    outstanding_debt                INTEGER NOT NULL DEFAULT 0,
    debt_blocks_acceptance          INTEGER NOT NULL DEFAULT 0,
    next_tier                       TEXT NOT NULL DEFAULT '',
    at_maximum_tier                 INTEGER NOT NULL DEFAULT 0,
    required_successful_deliveries  INTEGER NOT NULL DEFAULT 0,
    remaining_successful_deliveries INTEGER NOT NULL DEFAULT 0,
    required_delivered_value        INTEGER NOT NULL DEFAULT 0,
    remaining_delivered_value       INTEGER NOT NULL DEFAULT 0,
    active_contract_limit           INTEGER NOT NULL DEFAULT 0,
    active_contracts_unlimited      INTEGER NOT NULL DEFAULT 0,
    aggregate_liability_limit       INTEGER NOT NULL DEFAULT 0,
    remaining_aggregate_liability   INTEGER NOT NULL DEFAULT 0,
    single_package_liability_limit  INTEGER NOT NULL DEFAULT 0,
    liability_unlimited             INTEGER NOT NULL DEFAULT 0,
    captured_at                     TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_agent_carrier_tier ON agent_carrier(tier);

-- Derived, never hand-written: materialised by pkg/assets rules on every
-- capture. A TABLE rather than a SQL view because the rules live in Go;
-- expressing them in SQL too would fork the definitions and let them drift.
--
-- This is a SCREENING FILTER, not a promise. The workers' own gates
-- (buildMissionCandidate, freightCandidate, haulGate) are per-pass and
-- live-priced and remain authoritative at accept time.
CREATE TABLE IF NOT EXISTS agent_capability (
    player_id       TEXT NOT NULL,
    capability      TEXT NOT NULL,
    eligible        INTEGER NOT NULL DEFAULT 0,
    blocking_reason TEXT NOT NULL DEFAULT '',
    as_of           TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (player_id, capability)
);
CREATE INDEX IF NOT EXISTS idx_agent_capability_lookup ON agent_capability(capability, eligible);

-- Per-base storage holdings. base_id is the WIRE value (base-id form, e.g.
-- confederacy_central_command), never normalised at write time: keeping it
-- verbatim is what lets a capture be diffed field-for-field against the raw
-- frame. Readers resolve through assets.BaseResolver -- see the dual-named
-- station note above agent_profile.
--
-- Discovered from view_storage's prose hint, which enumerates every base
-- holding items and is AGENT-GLOBAL: one call from anywhere returns the full
-- list, even when queried against a base with no holdings.
CREATE TABLE IF NOT EXISTS agent_storage (
    player_id   TEXT NOT NULL,
    base_id     TEXT NOT NULL,
    credits     INTEGER NOT NULL DEFAULT 0,
    captured_at TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (player_id, base_id)
);
CREATE INDEX IF NOT EXISTS idx_agent_storage_base ON agent_storage(base_id);

-- quantity is REAL, not INTEGER. CargoItem.Quantity is float64 on the wire and
-- bill_of_materials already made the INTEGER mistake, where an INTEGER column
-- silently ceils fractional quantities.
CREATE TABLE IF NOT EXISTS agent_storage_items (
    player_id   TEXT NOT NULL,
    base_id     TEXT NOT NULL,
    item_id     TEXT NOT NULL,
    name        TEXT NOT NULL DEFAULT '',
    quantity    REAL NOT NULL DEFAULT 0,
    size        INTEGER NOT NULL DEFAULT 0,
    captured_at TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (player_id, base_id, item_id)
);
CREATE INDEX IF NOT EXISTS idx_agent_storage_items_item ON agent_storage_items(item_id);

-- Faction assets get their OWN tables rather than a holder_type discriminator
-- on the agent ones: player_id and faction_id are different id spaces, faction
-- storage carries fuel-bunker columns agents do not have, and no reader can
-- forget a WHERE holder_type filter that does not exist.
CREATE TABLE IF NOT EXISTS faction_profile (
    faction_id   TEXT PRIMARY KEY,
    name         TEXT NOT NULL DEFAULT '',
    tag          TEXT NOT NULL DEFAULT '',
    leader_id    TEXT NOT NULL DEFAULT '',
    treasury     INTEGER NOT NULL DEFAULT 0,
    member_count INTEGER NOT NULL DEFAULT 0,
    owned_bases  INTEGER NOT NULL DEFAULT 0,
    captured_at  TEXT NOT NULL DEFAULT ''
);

-- fuel_reserve/fuel_capacity are the faction's bunker at that base. They ride
-- the view_faction_storage response, so they are columns here rather than a
-- separate faction_fuel_bunkers table.
CREATE TABLE IF NOT EXISTS faction_storage (
    faction_id    TEXT NOT NULL,
    base_id       TEXT NOT NULL,
    credits       INTEGER NOT NULL DEFAULT 0,
    fuel_reserve  INTEGER NOT NULL DEFAULT 0,
    fuel_capacity INTEGER NOT NULL DEFAULT 0,
    captured_at   TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (faction_id, base_id)
);
CREATE INDEX IF NOT EXISTS idx_faction_storage_base ON faction_storage(base_id);

CREATE TABLE IF NOT EXISTS faction_storage_items (
    faction_id  TEXT NOT NULL,
    base_id     TEXT NOT NULL,
    item_id     TEXT NOT NULL,
    name        TEXT NOT NULL DEFAULT '',
    quantity    REAL NOT NULL DEFAULT 0,
    size        INTEGER NOT NULL DEFAULT 0,
    captured_at TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (faction_id, base_id, item_id)
);
CREATE INDEX IF NOT EXISTS idx_faction_storage_items_item ON faction_storage_items(item_id);

-- Lifetime death and loss counters, straight off get_status. APPEND-ON-CHANGE:
-- a row is written only when a counter differs from the agent's latest row, so
-- the table is a series of events rather than one hourly row per agent.
--
-- This is deliberately NOT the replaceSet pattern the other asset tables use.
-- agent_hulls deletes and reinserts, which makes it impossible to tell that a
-- hull ever existed — and a ship loss is exactly the thing we went looking for
-- and could not find. Counters only ever ratchet upward, so appending on change
-- is both cheap and the whole point.
CREATE TABLE IF NOT EXISTS agent_stats (
    player_id                  TEXT NOT NULL,
    captured_at                TEXT NOT NULL,
    ships_lost                 INTEGER NOT NULL DEFAULT 0,
    ships_destroyed            INTEGER NOT NULL DEFAULT 0,
    deaths_by_pirate           INTEGER NOT NULL DEFAULT 0,
    deaths_by_player           INTEGER NOT NULL DEFAULT 0,
    deaths_by_self_destruct    INTEGER NOT NULL DEFAULT 0,
    deaths_by_wildlife         INTEGER NOT NULL DEFAULT 0,
    insurance_claims_made      INTEGER NOT NULL DEFAULT 0,
    insurance_payouts_received INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (player_id, captured_at)
);
CREATE INDEX IF NOT EXISTS idx_agent_stats_player ON agent_stats(player_id, captured_at DESC);
