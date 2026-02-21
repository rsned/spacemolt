#!/bin/bash

# Script to show record counts for all tables in spacemolt-knowledge.db

# Default database path
DEFAULT_DB_PATH="data/spacemolt-knowledge.db"

# Use command line argument if provided, otherwise use default
DB_PATH="${1:-$DEFAULT_DB_PATH}"

# Check if database exists
if [ ! -f "$DB_PATH" ]; then
    echo "Database not found: $DB_PATH"
    echo ""
    echo "Usage: $0 [database_path]"
    echo "Default: data/spacemolt-knowledge.db"
    exit 1
fi

echo "======================================================================"
echo "Table Record Counts for: $DB_PATH"
echo "======================================================================"
echo ""

# Get all table names
sqlite3 "$DB_PATH" << 'EOF'
.mode box
SELECT name as table_name
FROM sqlite_master
WHERE type='table'
AND name NOT LIKE 'sqlite_%'
ORDER BY name;
EOF

echo ""
echo "======================================================================"
echo "Record Counts"
echo "======================================================================"

# Get all table names from the database dynamically
TABLES=$(sqlite3 "$DB_PATH" "SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name;")

# Print header
printf "%-35s %15s\n" "Table Name" "Record Count"
echo "----------------------------------------------------------------------"

# Count records in each table
for table in $TABLES; do
    count=$(sqlite3 "$DB_PATH" "SELECT COUNT(*) FROM $table;")
    printf "%-35s %15s\n" "$table" "$count"
done

echo ""
echo "======================================================================"
echo "Total Database Size: $(du -h "$DB_PATH" | cut -f1)"
echo "======================================================================"
