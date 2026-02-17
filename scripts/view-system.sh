#!/bin/bash
# View comprehensive information about a system in the knowledge database
# Usage: ./scripts/view-system.sh <system_id>
# Example: ./scripts/view-system.sh sys_0137

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DB_PATH="${SCRIPT_DIR}/../data/spacemolt-knowledge.db"

if [ -z "$1" ]; then
    echo "Usage: $0 <system_id>"
    echo ""
    echo "Examples:"
    echo "  $0 sys_0137    # View Gemma system"
    echo "  $0 sys_0001    # View Sol system"
    echo ""
    echo "Find system IDs:"
    echo "  sqlite3 data/spacemolt-knowledge.db 'SELECT id, name FROM systems ORDER BY name;'"
    exit 1
fi

SYSTEM_ID="$1"

# Check if database exists
if [ ! -f "$DB_PATH" ]; then
    echo "Error: Database not found at $DB_PATH"
    exit 1
fi

# Check if system exists
SYSTEM_EXISTS=$(sqlite3 "$DB_PATH" "SELECT COUNT(*) FROM systems WHERE id = '$SYSTEM_ID';")
if [ "$SYSTEM_EXISTS" = "0" ]; then
    echo "Error: System '$SYSTEM_ID' not found in database"
    echo ""
    echo "Available systems:"
    sqlite3 "$DB_PATH" "SELECT '  ' || id || ' - ' || name FROM systems ORDER BY name LIMIT 20;"
    exit 1
fi

# Run the SQL script with the system_id parameter
sqlite3 "$DB_PATH" ".param set :system_id $SYSTEM_ID" ".read ${SCRIPT_DIR}/sql/view_system.sql"
