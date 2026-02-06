#!/bin/bash
# Query the agent knowledge base
# Usage: ./scripts/query-knowledge.sh [command]

DB_FILE="${DB_FILE:-spacemolt-knowledge.db}"

if [ ! -f "$DB_FILE" ]; then
    echo "Error: Database file not found: $DB_FILE"
    echo "Set DB_FILE environment variable to specify a different path"
    exit 1
fi

command="${1:-help}"

case "$command" in
    systems)
        echo "=== Discovered Systems ==="
        sqlite3 "$DB_FILE" <<EOF
.mode column
.headers on
SELECT
    name as System,
    security_level as Security,
    faction as Faction,
    visit_count as Visits,
    discovered_by as Discoverer
FROM systems
ORDER BY visit_count DESC;
EOF
        ;;

    pois)
        system="${2}"
        if [ -z "$system" ]; then
            echo "=== All Points of Interest ==="
            sqlite3 "$DB_FILE" <<EOF
.mode column
.headers on
SELECT
    p.name as POI,
    p.type as Type,
    p.system_id as System,
    COUNT(pr.resource_id) as Resources
FROM pois p
LEFT JOIN poi_resources pr ON p.id = pr.poi_id
GROUP BY p.id
ORDER BY p.system_id, p.name;
EOF
        else
            echo "=== POIs in $system ==="
            sqlite3 "$DB_FILE" <<EOF
.mode column
.headers on
SELECT
    p.name as POI,
    p.type as Type,
    GROUP_CONCAT(pr.resource_id, ', ') as Resources
FROM pois p
LEFT JOIN poi_resources pr ON p.id = pr.poi_id
WHERE p.system_id = '$system'
GROUP BY p.id
ORDER BY p.name;
EOF
        fi
        ;;

    resources)
        echo "=== Resource-Rich Locations ==="
        sqlite3 "$DB_FILE" <<EOF
.mode column
.headers on
SELECT
    p.name as Location,
    p.system_id as System,
    p.type as Type,
    pr.resource_id as Resource,
    ROUND(pr.richness, 2) as Richness,
    ROUND(pr.remaining, 0) as Remaining
FROM pois p
JOIN poi_resources pr ON p.id = pr.poi_id
WHERE pr.remaining > 0
ORDER BY pr.richness DESC, pr.remaining DESC
LIMIT 20;
EOF
        ;;

    mining)
        resource="${2:-iron}"
        echo "=== Best Mining Locations for $resource ==="
        sqlite3 "$DB_FILE" <<EOF
.mode column
.headers on
SELECT
    p.name as Location,
    p.system_id as System,
    s.security_level as Security,
    pr.resource_id as Resource,
    ROUND(pr.richness, 2) as Richness,
    ROUND(pr.remaining, 0) as Remaining
FROM pois p
JOIN poi_resources pr ON p.id = pr.poi_id
JOIN systems s ON p.system_id = s.id
WHERE pr.resource_id LIKE '%$resource%'
  AND pr.remaining > 0
  AND (p.type LIKE '%asteroid%' OR p.type LIKE '%belt%')
ORDER BY pr.richness DESC, pr.remaining DESC
LIMIT 10;
EOF
        ;;

    connections)
        system="${2}"
        if [ -z "$system" ]; then
            echo "Usage: $0 connections <system-id>"
            exit 1
        fi
        echo "=== Connections from $system ==="
        sqlite3 "$DB_FILE" <<EOF
.mode column
.headers on
SELECT
    c.to_system as Destination,
    s.name as Name,
    s.security_level as Security,
    CASE
        WHEN s.visit_count > 0 THEN 'Visited (' || s.visit_count || 'x)'
        ELSE 'Unexplored'
    END as Status
FROM connections c
LEFT JOIN systems s ON c.to_system = s.id
WHERE c.from_system = '$system'
ORDER BY Status DESC, Destination;
EOF
        ;;

    experiences)
        agent="${2}"
        limit="${3:-20}"
        if [ -z "$agent" ]; then
            echo "=== Recent Experiences (All Agents) ==="
            sqlite3 "$DB_FILE" <<EOF
.mode column
.headers on
SELECT
    agent_id as Agent,
    time as Tick,
    type as Type,
    description as Action,
    outcome as Result
FROM experiences
ORDER BY id DESC
LIMIT $limit;
EOF
        else
            echo "=== Recent Experiences for $agent ==="
            sqlite3 "$DB_FILE" <<EOF
.mode column
.headers on
SELECT
    time as Tick,
    type as Type,
    description as Action,
    outcome as Result,
    location as Location
FROM experiences
WHERE agent_id = '$agent'
ORDER BY id DESC
LIMIT $limit;
EOF
        fi
        ;;

    agents)
        echo "=== Registered Agents ==="
        sqlite3 "$DB_FILE" <<EOF
.mode column
.headers on
SELECT
    id as ID,
    name as Name,
    role as Role,
    faction as Faction,
    status as Status
FROM agents
ORDER BY role, name;
EOF
        ;;

    stats)
        echo "=== Knowledge Base Statistics ==="
        sqlite3 "$DB_FILE" <<EOF
SELECT 'Systems Discovered' as Metric, COUNT(*) as Count FROM systems
UNION ALL
SELECT 'POIs Discovered', COUNT(*) FROM pois
UNION ALL
SELECT 'Resources Found', COUNT(*) FROM poi_resources
UNION ALL
SELECT 'Hyperspace Routes', COUNT(*) FROM connections
UNION ALL
SELECT 'Agent Experiences', COUNT(*) FROM experiences
UNION ALL
SELECT 'Registered Agents', COUNT(*) FROM agents;
EOF
        ;;

    unexplored)
        echo "=== Unexplored Systems ==="
        sqlite3 "$DB_FILE" <<EOF
.mode column
.headers on
SELECT DISTINCT
    c.to_system as System,
    c.from_system as 'Reachable From'
FROM connections c
LEFT JOIN systems s ON c.to_system = s.id
WHERE s.visit_count IS NULL OR s.visit_count = 0
ORDER BY c.from_system, c.to_system;
EOF
        ;;

    help|*)
        cat <<EOF
Usage: $0 <command> [args]

Commands:
  systems              - List all discovered systems
  pois [system-id]     - List POIs (all or for specific system)
  resources            - List top 20 resource-rich locations
  mining [resource]    - Find best mining spots (default: iron)
  connections <system> - Show hyperspace connections from a system
  experiences [agent] [limit] - Show recent experiences
  agents               - List all registered agents
  stats                - Show knowledge base statistics
  unexplored           - List unexplored systems
  help                 - Show this help message

Environment:
  DB_FILE              - Path to SQLite database (default: spacemolt-knowledge.db)

Examples:
  $0 systems
  $0 pois Sol
  $0 mining copper
  $0 connections Sol
  $0 experiences miner-1 10
  $0 unexplored

EOF
        ;;
esac
