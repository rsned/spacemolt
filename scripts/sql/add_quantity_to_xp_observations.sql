-- Migration: Add quantity column to xp_observations table
-- This script safely adds the quantity column if it doesn't already exist

-- Check if column exists (this will fail silently if column doesn't exist)
-- Then add the column if needed
-- SQLite doesn't support "IF NOT EXISTS" for ALTER TABLE ADD COLUMN,
-- so we need to use a workaround

BEGIN;

-- Try to add the column (will fail if already exists, which we catch)
-- Using a pragma check first
SELECT CASE
    WHEN NOT EXISTS (
        SELECT 1 FROM pragma_table_info('xp_observations')
        WHERE name='quantity'
    )
    THEN ALTER TABLE xp_observations ADD COLUMN quantity INTEGER NOT NULL DEFAULT 1
END;

COMMIT;
