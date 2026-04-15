#!/bin/bash
# Script to apply the quantity column migration to existing knowledge databases

DB_PATH="${1:-spacemolt-knowledge.db}"

if [ ! -f "$DB_PATH" ]; then
    echo "Database file not found: $DB_PATH"
    echo "Usage: $0 [path_to_database.db]"
    exit 1
fi

echo "Applying quantity column migration to $DB_PATH..."

# Check if column already exists
COLUMN_EXISTS=$(sqlite3 "$DB_PATH" "SELECT COUNT(*) FROM pragma_table_info('xp_observations') WHERE name='quantity'")

if [ "$COLUMN_EXISTS" -gt 0 ]; then
    echo "✓ Quantity column already exists in database"
    exit 0
fi

# Apply the migration
sqlite3 "$DB_PATH" <<EOF
ALTER TABLE xp_observations ADD COLUMN quantity INTEGER NOT NULL DEFAULT 1;
EOF

if [ $? -eq 0 ]; then
    echo "✓ Successfully added quantity column to xp_observations table"

    # Update the schema_migrations table to mark migration 2 as applied
    sqlite3 "$DB_PATH" "INSERT INTO schema_migrations (version, applied_at) VALUES (2, datetime('now'));"
    echo "✓ Marked migration 2 as applied"
else
    echo "✗ Failed to apply migration"
    exit 1
fi
