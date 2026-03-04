-- Item Module & Consumable Detail Tables
-- SQLite 3 Compatible
--
-- These tables extend the base `items` table with type-specific attributes
-- for ship modules, weapons, consumables, and ammunition. Each row references
-- an item by its `item_id` (FK to items.id).
--
-- The base `items` table stores the universal fields shared by all items
-- (id, name, description, category, rarity, size, base_value, stackable,
-- tradeable). These tables capture the additional structured fields that
-- only apply to specific item types.
--
-- These schemas are expected to evolve as the game server adds new module
-- types and attributes. Track changes here rather than in the main
-- initialize_database.sql.
--
-- Usage:
--   sqlite3 spacemolt-knowledge.db < scripts/sql/schema_item_modules.sql

-- ============================================================================
-- SHIP MODULES (common base for all fitted modules)
-- ============================================================================

-- All ship modules share cpu_usage, power_usage, type, and type_id.
-- Type-specific attributes live in the detail tables below. A module will
-- have one row here plus one row in the appropriate detail table.
CREATE TABLE IF NOT EXISTS item_modules (
    item_id     TEXT PRIMARY KEY REFERENCES items(id),
    type        TEXT NOT NULL,       -- 'weapon', 'defense', 'mining', 'utility'
    type_id     TEXT NOT NULL,       -- server-assigned module identifier
    cpu_usage   INTEGER NOT NULL DEFAULT 0,
    power_usage INTEGER NOT NULL DEFAULT 0,
    hidden      BOOLEAN NOT NULL DEFAULT 0,
    special     TEXT                 -- comma-separated special ability tags (nullable)
);

CREATE INDEX IF NOT EXISTS idx_item_modules_type ON item_modules(type);
CREATE INDEX IF NOT EXISTS idx_item_modules_type_id ON item_modules(type_id);

-- ============================================================================
-- WEAPONS
-- ============================================================================

CREATE TABLE IF NOT EXISTS item_weapons (
    item_id     TEXT PRIMARY KEY REFERENCES item_modules(item_id),
    damage      INTEGER NOT NULL DEFAULT 0,
    damage_type TEXT NOT NULL,        -- 'kinetic', 'energy', 'em', 'explosive', 'void', 'thermal'
    range       INTEGER,              -- engagement range (nullable, a few weapons omit this)
    reach       INTEGER,              -- max targeting reach (nullable)
    cooldown    INTEGER NOT NULL DEFAULT 0,
    ammo_type   TEXT,                 -- NULL for energy weapons; e.g. 'autocannon', 'missile', 'railgun', 'plasma', 'em_charge', 'torpedo', 'mine', 'void_core'
    magazine_size INTEGER             -- NULL when ammo_type is NULL
);

CREATE INDEX IF NOT EXISTS idx_item_weapons_damage_type ON item_weapons(damage_type);
CREATE INDEX IF NOT EXISTS idx_item_weapons_ammo_type ON item_weapons(ammo_type);

-- ============================================================================
-- DEFENSE MODULES
-- ============================================================================

CREATE TABLE IF NOT EXISTS item_defenses (
    item_id             TEXT PRIMARY KEY REFERENCES item_modules(item_id),
    armor_bonus         INTEGER,      -- flat armor HP added
    hull_bonus          INTEGER,      -- flat hull HP added
    shield_bonus        INTEGER,      -- flat shield HP added
    shield_recharge_bonus INTEGER,    -- shield recharge rate increase
    armor_repair_rate   INTEGER,      -- passive armor repair per tick
    resistance_bonus    TEXT,         -- JSON object mapping damage type to bonus, e.g. '{"em": 30}'
    damage_reduction    REAL,         -- flat damage reduction per hit
    cloak_strength      INTEGER,      -- cloaking effectiveness
    -- Some defense modules have active abilities (e.g. force fields)
    cooldown            INTEGER,      -- activation cooldown in ticks (nullable)
    damage              INTEGER,      -- reactive damage (e.g. damage fields) (nullable)
    damage_type         TEXT,         -- reactive damage type (nullable)
    range               INTEGER       -- active ability range (nullable)
);

-- ============================================================================
-- MINING MODULES
-- ============================================================================

CREATE TABLE IF NOT EXISTS item_mining (
    item_id      TEXT PRIMARY KEY REFERENCES item_modules(item_id),
    mining_power INTEGER NOT NULL DEFAULT 0,
    mining_range INTEGER NOT NULL DEFAULT 0
);

-- ============================================================================
-- UTILITY MODULES
-- ============================================================================

-- Utility modules are the most heterogeneous category. Each row stores the
-- union of all known utility-specific fields; unused columns are NULL.
CREATE TABLE IF NOT EXISTS item_utilities (
    item_id          TEXT PRIMARY KEY REFERENCES item_modules(item_id),
    speed_bonus      INTEGER,
    cargo_bonus      INTEGER,
    cloak_strength   INTEGER,
    scanner_power    INTEGER,
    accuracy_bonus   INTEGER,
    tracking_bonus   INTEGER,
    signature_bonus  INTEGER,
    fuel_efficiency  REAL,
    drone_bandwidth  INTEGER,
    drone_capacity   INTEGER,
    harvest_power    INTEGER,
    harvest_range    INTEGER,
    survey_power     INTEGER,
    survey_range     INTEGER,
    tow_speed_penalty INTEGER,
    cooldown         INTEGER
);

-- ============================================================================
-- CONSUMABLES WITH EFFECTS
-- ============================================================================

-- Consumable items that produce an in-game effect when used. Covers buffs,
-- repairs, countermeasures, probes, fuel, shields, and emergency jumps.
CREATE TABLE IF NOT EXISTS item_consumable_effects (
    item_id   TEXT PRIMARY KEY REFERENCES items(id),
    effect_type TEXT NOT NULL,         -- 'buff', 'repair', 'fuel', 'shield', 'power', 'probe', 'countermeasure', 'beacon', 'emergency_jump'
    subtype   TEXT,                    -- further classification (e.g. 'ecm', 'chaff', 'flare', 'decoy' for countermeasures)
    amount    INTEGER,                 -- magnitude (heal amount, fuel amount, buff percentage, etc.)
    duration  INTEGER,                 -- duration in ticks (nullable for instant effects)
    stat      TEXT                     -- which stat is affected (for buffs: 'weapon_damage', 'speed', 'all_combat', 'cloak_strength', 'ecm_immunity', etc.)
);

CREATE INDEX IF NOT EXISTS idx_item_consumable_effects_type ON item_consumable_effects(effect_type);

-- ============================================================================
-- AMMUNITION TYPES
-- ============================================================================

-- Ammo items that are loaded into weapons. Separated from other consumables
-- because their effect structure is purely {type: "ammo", subtype: <weapon_class>}
-- and they pair with the ammo_type field on weapons.
CREATE TABLE IF NOT EXISTS item_ammo (
    item_id    TEXT PRIMARY KEY REFERENCES items(id),
    ammo_type  TEXT NOT NULL           -- matches item_weapons.ammo_type: 'autocannon', 'missile', 'railgun', 'plasma', 'em_charge', 'torpedo', 'mine', 'void_core'
);

CREATE INDEX IF NOT EXISTS idx_item_ammo_type ON item_ammo(ammo_type);
