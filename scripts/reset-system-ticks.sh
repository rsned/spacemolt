#!/bin/bash
# Reset last_updated_tick to 0 for a system to force a full rescan
# Usage: ./scripts/reset-system-ticks.sh <system_id>
# Example: ./scripts/reset-system-ticks.sh sys_0137

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DB_PATH="${SCRIPT_DIR}/../data/spacemolt-knowledge.db"

if [ -z "$1" ]; then
    echo "Usage: $0 <system_id>"
    echo ""
    echo "Resets last_updated_tick to 0 for all data related to a system."
    echo "This will cause agents to perform a full rescan next time they visit."
    echo ""
    echo "Examples:"
    echo "  $0 sys_0137    # Reset Gemma system"
    echo "  $0 sys_0001    # Reset Sol system"
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

# Show current system info
echo "=================================================="
echo "System: $SYSTEM_ID"
SYSTEM_NAME=$(sqlite3 "$DB_PATH" "SELECT name FROM systems WHERE id = '$SYSTEM_ID';")
echo "Name: $SYSTEM_NAME"
echo "=================================================="
echo ""

# Show counts before reset
echo "Records before reset:"
echo "  Systems: $(sqlite3 "$DB_PATH" "SELECT COUNT(*) FROM systems WHERE id = '$SYSTEM_ID' AND last_updated_tick > 0;")"
echo "  POIs: $(sqlite3 "$DB_PATH" "SELECT COUNT(*) FROM pois WHERE system_id = '$SYSTEM_ID' AND last_updated_tick > 0;")"
echo "  POI Resources: $(sqlite3 "$DB_PATH" "SELECT COUNT(*) FROM poi_resources pr JOIN pois p ON pr.poi_id = p.id WHERE p.system_id = '$SYSTEM_ID' AND pr.last_updated_tick > 0;")"
echo "  Bases: $(sqlite3 "$DB_PATH" "SELECT COUNT(*) FROM bases b JOIN pois p ON b.poi_id = p.id WHERE p.system_id = '$SYSTEM_ID' AND b.last_updated_tick > 0;")"
echo "  Base Services: $(sqlite3 "$DB_PATH" "SELECT COUNT(*) FROM base_services bs JOIN bases b ON bs.base_id = b.id JOIN pois p ON b.poi_id = p.id WHERE p.system_id = '$SYSTEM_ID';")"
echo "  Base Facilities: $(sqlite3 "$DB_PATH" "SELECT COUNT(*) FROM base_facilities bf JOIN bases b ON bf.base_id = b.id JOIN pois p ON b.poi_id = p.id WHERE p.system_id = '$SYSTEM_ID';")"
echo ""

# Confirm with user
read -p "Reset all last_updated_tick values to 0 for this system? (y/N) " -n 1 -r
echo
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    echo "Cancelled."
    exit 0
fi

echo ""
echo "Resetting ticks..."

# Reset systems table
SYSTEMS_UPDATED=$(sqlite3 "$DB_PATH" "UPDATE systems SET last_updated_tick = 0 WHERE id = '$SYSTEM_ID'; SELECT changes();")
echo "  ✓ Reset $SYSTEMS_UPDATED system record(s)"

# Reset pois table
POIS_UPDATED=$(sqlite3 "$DB_PATH" "UPDATE pois SET last_updated_tick = 0 WHERE system_id = '$SYSTEM_ID'; SELECT changes();")
echo "  ✓ Reset $POIS_UPDATED POI record(s)"

# Reset poi_resources table (need to join through pois)
RESOURCES_UPDATED=$(sqlite3 "$DB_PATH" "UPDATE poi_resources SET last_updated_tick = 0 WHERE poi_id IN (SELECT id FROM pois WHERE system_id = '$SYSTEM_ID'); SELECT changes();")
echo "  ✓ Reset $RESOURCES_UPDATED POI resource record(s)"

# Reset bases table (need to join through pois)
BASES_UPDATED=$(sqlite3 "$DB_PATH" "UPDATE bases SET last_updated_tick = 0 WHERE poi_id IN (SELECT id FROM pois WHERE system_id = '$SYSTEM_ID'); SELECT changes();")
echo "  ✓ Reset $BASES_UPDATED base record(s)"

# Note: base_services and base_facilities don't have last_updated_tick columns
SERVICES_COUNT=$(sqlite3 "$DB_PATH" "SELECT COUNT(*) FROM base_services bs JOIN bases b ON bs.base_id = b.id JOIN pois p ON b.poi_id = p.id WHERE p.system_id = '$SYSTEM_ID';")
FACILITIES_COUNT=$(sqlite3 "$DB_PATH" "SELECT COUNT(*) FROM base_facilities bf JOIN bases b ON bf.base_id = b.id JOIN pois p ON b.poi_id = p.id WHERE p.system_id = '$SYSTEM_ID';")
echo "  ℹ Note: base_services ($SERVICES_COUNT records) and base_facilities ($FACILITIES_COUNT records) will be refreshed via base rescans"

echo ""
echo "=================================================="
echo "Reset complete! Agents will perform a full rescan"
echo "of system $SYSTEM_ID ($SYSTEM_NAME) on next visit."
echo "=================================================="
