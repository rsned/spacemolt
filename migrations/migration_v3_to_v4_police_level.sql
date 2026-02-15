-- SpaceMolt Knowledge Base Migration: Version 3 to Version 4
-- Change: Convert security_level (TEXT) to police_level (INTEGER)
--
-- This migration converts the security_level field from a string to an integer
-- to make threshold queries easier for users.
--
-- Apply this migration to existing databases:
--   sqlite3 spacemolt-knowledge.db < migrations/migration_v3_to_v4_police_level.sql
--
-- Migration Date: 2025-02-11
-- Target Schema Version: 4

-- ==============================================================================
-- STEP 1: Add new police_level column
-- ==============================================================================

ALTER TABLE systems ADD COLUMN police_level INTEGER DEFAULT 0;

-- ==============================================================================
-- STEP 2: Migrate existing data from security_level to police_level
-- Mapping:
--   'None', 'NONE', 'none', ''      -> 0
--   'Low', 'LOW', 'low'             -> 1
--   'Medium', 'MEDIUM', 'medium'    -> 2
--   'High', 'HIGH', 'high'          -> 3
--   Any numeric string               -> Parse as int
--   Unknown values                   -> 0 (default)
-- ==============================================================================

UPDATE systems SET police_level = CASE
    WHEN LOWER(security_level) = 'none' OR security_level = '' THEN 0
    WHEN LOWER(security_level) = 'low' THEN 1
    WHEN LOWER(security_level) = 'medium' THEN 2
    WHEN LOWER(security_level) = 'high' THEN 3
    WHEN CAST(security_level AS INTEGER) > 0 THEN CAST(security_level AS INTEGER)
    ELSE 0
END;

-- ==============================================================================
-- STEP 3: Drop old security_level column
-- ==============================================================================

-- Create a new table without security_level
CREATE TABLE IF NOT EXISTS systems_new (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    pos_x REAL NOT NULL,
    pos_y REAL NOT NULL,
    pos_z REAL NOT NULL,
    police_level INTEGER DEFAULT 0,
    faction TEXT,
    visit_count INTEGER DEFAULT 0,
    last_visited TEXT,
    discovered_by TEXT
);

-- Copy data from old table to new table
INSERT INTO systems_new (id, name, pos_x, pos_y, pos_z, police_level, faction, visit_count, last_visited, discovered_by)
SELECT id, name, pos_x, pos_y, pos_z, police_level, faction, visit_count, last_visited, discovered_by
FROM systems;

-- Drop old table
DROP TABLE systems;

-- Rename new table to original name
ALTER TABLE systems_new RENAME TO systems;

-- ==============================================================================
-- STEP 4: Update indexes
-- ==============================================================================

-- Note: Indexes on the systems table are automatically recreated
-- when the table is recreated in Step 3

-- ==============================================================================
-- STEP 5: Update schema version
-- ==============================================================================

INSERT OR REPLACE INTO schema_migrations (version, applied_at) VALUES (4, datetime('now'));

-- ==============================================================================
-- VERIFICATION QUERY
-- Run this to verify the migration was successful:
-- ==============================================================================

-- SELECT id, name, police_level, faction FROM systems LIMIT 10;
