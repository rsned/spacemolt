-- Migration: Add quantity column to xp_observations table
-- Run this script on existing databases to add the quantity column
--
-- Usage: sqlite3 spacemolt-knowledge.db < migrate_add_quantity.sql
--
-- This will add the quantity column with default value 1 to all existing records

ALTER TABLE xp_observations ADD COLUMN quantity INTEGER NOT NULL DEFAULT 1;
