#!/bin/bash
# Query enhanced analytics from the knowledge base
# Usage: ./scripts/query-analytics.sh [command]

DB_FILE="${DB_FILE:-spacemolt-knowledge.db}"

if [ ! -f "$DB_FILE" ]; then
    echo "Error: Database file not found: $DB_FILE"
    exit 1
fi

command="${1:-help}"

case "$command" in
    depleting)
        threshold="${2:-50000}"
        echo "=== Resources Depleting Below $threshold ==="
        sqlite3 "$DB_FILE" <<EOF
.mode column
.headers on
WITH latest AS (
    SELECT poi_id, resource_id, remaining, game_tick,
           ROW_NUMBER() OVER (PARTITION BY poi_id, resource_id ORDER BY game_tick DESC) as rn
    FROM resource_history
),
previous AS (
    SELECT poi_id, resource_id, remaining, game_tick,
           ROW_NUMBER() OVER (PARTITION BY poi_id, resource_id ORDER BY game_tick DESC) as rn
    FROM resource_history
)
SELECT
    p.name as POI,
    s.name as System,
    l.resource_id as Resource,
    ROUND(l.remaining, 0) as Remaining,
    ROUND(COALESCE(prev.remaining, l.remaining) - l.remaining, 2) as Depleted,
    CASE
        WHEN l.game_tick > COALESCE(prev.game_tick, l.game_tick) AND (COALESCE(prev.remaining, l.remaining) - l.remaining) > 0
        THEN ROUND((COALESCE(prev.remaining, l.remaining) - l.remaining) / (l.game_tick - COALESCE(prev.game_tick, l.game_tick)), 4)
        ELSE 0
    END as 'Rate/Tick'
FROM latest l
JOIN pois p ON l.poi_id = p.id
JOIN systems s ON p.system_id = s.id
LEFT JOIN previous prev ON l.poi_id = prev.poi_id AND l.resource_id = prev.resource_id AND prev.rn = 2
WHERE l.rn = 1 AND l.remaining < $threshold AND l.remaining > 0
ORDER BY l.remaining ASC
LIMIT 20;
EOF
        ;;

    resource-history)
        poi_id="${2}"
        resource_id="${3}"
        if [ -z "$poi_id" ] || [ -z "$resource_id" ]; then
            echo "Usage: $0 resource-history <poi-id> <resource-id>"
            exit 1
        fi
        echo "=== Resource History: $resource_id at $poi_id ==="
        sqlite3 "$DB_FILE" <<EOF
.mode column
.headers on
SELECT
    game_tick as Tick,
    ROUND(richness, 2) as Richness,
    ROUND(remaining, 0) as Remaining,
    recorded_at as 'Recorded At',
    agent_id as Agent
FROM resource_history
WHERE poi_id = '$poi_id' AND resource_id = '$resource_id'
ORDER BY game_tick DESC
LIMIT 50;
EOF
        ;;

    routes)
        echo "=== Known Routes (Optimized) ==="
        sqlite3 "$DB_FILE" <<EOF
.mode column
.headers on
SELECT
    from_system || ' → ' || to_system as Route,
    travel_count as Travels,
    ROUND(avg_fuel_cost, 1) as 'Avg Fuel',
    ROUND(avg_travel_time, 1) as 'Avg Time',
    last_traveled as 'Last Used'
FROM connection_metrics
ORDER BY travel_count DESC, avg_fuel_cost ASC
LIMIT 30;
EOF
        ;;

    route)
        from="${2}"
        to="${3}"
        if [ -z "$from" ] || [ -z "$to" ]; then
            echo "Usage: $0 route <from-system> <to-system>"
            exit 1
        fi
        echo "=== Route: $from → $to ==="
        sqlite3 "$DB_FILE" <<EOF
.mode column
.headers on
SELECT
    from_system as From,
    to_system as To,
    travel_count as 'Times Traveled',
    ROUND(avg_fuel_cost, 2) as 'Avg Fuel Cost',
    ROUND(avg_travel_time, 1) as 'Avg Time (ticks)',
    last_traveled as 'Last Traveled',
    traveled_by as 'Last Agent'
FROM connection_metrics
WHERE from_system = '$from' AND to_system = '$to';
EOF
        ;;

    anomalies)
        type="${2}"
        if [ -z "$type" ]; then
            echo "=== All Active Anomalies ==="
            sqlite3 "$DB_FILE" <<EOF
.mode column
.headers on
SELECT
    type as Type,
    severity as Severity,
    s.name as System,
    p.name as POI,
    description as Description,
    detected_at as 'Detected At'
FROM anomalies a
LEFT JOIN systems s ON a.system_id = s.id
LEFT JOIN pois p ON a.poi_id = p.id
WHERE a.status = 'active'
ORDER BY
    CASE severity
        WHEN 'critical' THEN 1
        WHEN 'warning' THEN 2
        WHEN 'opportunity' THEN 3
        WHEN 'info' THEN 4
    END,
    detected_at DESC
LIMIT 50;
EOF
        else
            echo "=== Active Anomalies: $type ==="
            sqlite3 "$DB_FILE" <<EOF
.mode column
.headers on
SELECT
    severity as Severity,
    s.name as System,
    p.name as POI,
    description as Description,
    detected_at as 'Detected At',
    detected_by as Agent
FROM anomalies a
LEFT JOIN systems s ON a.system_id = s.id
LEFT JOIN pois p ON a.poi_id = p.id
WHERE a.type = '$type' AND a.status = 'active'
ORDER BY detected_at DESC
LIMIT 50;
EOF
        fi
        ;;

    anomalies-severity)
        severity="${2:-opportunity}"
        echo "=== Anomalies by Severity: $severity ==="
        sqlite3 "$DB_FILE" <<EOF
.mode column
.headers on
SELECT
    type as Type,
    s.name as System,
    p.name as POI,
    description as Description,
    detected_at as 'Detected At'
FROM anomalies a
LEFT JOIN systems s ON a.system_id = s.id
LEFT JOIN pois p ON a.poi_id = p.id
WHERE a.severity = '$severity' AND a.status = 'active'
ORDER BY detected_at DESC
LIMIT 50;
EOF
        ;;

    price-trends)
        item_id="${2}"
        station_id="${3}"
        if [ -z "$item_id" ] || [ -z "$station_id" ]; then
            echo "Usage: $0 price-trends <item-id> <station-id>"
            exit 1
        fi
        echo "=== Price Trends: $item_id at $station_id ==="
        sqlite3 "$DB_FILE" <<EOF
.mode column
.headers on
SELECT
    listing_type as Type,
    ROUND(avg_price, 2) as 'Avg Price',
    ROUND(min_price, 2) as Min,
    ROUND(max_price, 2) as Max,
    sample_count as Samples,
    window_start as 'Period Start',
    window_end as 'Period End'
FROM price_trends
WHERE item_id = '$item_id' AND station_id = '$station_id'
ORDER BY window_end DESC
LIMIT 10;
EOF
        ;;

    best-prices)
        item_id="${2}"
        listing_type="${3:-sell}"
        limit="${4:-10}"
        echo "=== Best $listing_type Prices for $item_id ==="
        sqlite3 "$DB_FILE" <<EOF
.mode column
.headers on
SELECT DISTINCT
    ms.station_name as Station,
    ms.system_name as System,
    ROUND(ml.price_per_unit, 2) as Price,
    ROUND(ml.quantity, 0) as Quantity,
    ms.captured_at as 'Last Seen'
FROM market_listings ml
JOIN market_snapshots ms ON ml.snapshot_id = ms.id
WHERE ml.item_id = '$item_id' AND ml.listing_type = '$listing_type'
ORDER BY
    CASE WHEN '$listing_type' = 'buy' THEN ml.price_per_unit END DESC,
    CASE WHEN '$listing_type' = 'sell' THEN ml.price_per_unit END ASC,
    ms.captured_at DESC
LIMIT $limit;
EOF
        ;;

    danger-zones)
        min_level="${2:-3}"
        echo "=== Danger Zones (Level >= $min_level) ==="
        sqlite3 "$DB_FILE" <<EOF
.mode column
.headers on
SELECT
    s.name as System,
    dz.danger_level as 'Danger Level',
    dz.hostile_encounters as Hostiles,
    dz.player_kills as 'Player Kills',
    dz.npc_kills as 'NPC Kills',
    dz.last_incident as 'Last Incident'
FROM danger_zones dz
JOIN systems s ON dz.system_id = s.id
WHERE dz.danger_level >= $min_level
ORDER BY dz.danger_level DESC, dz.hostile_encounters DESC;
EOF
        ;;

    system-danger)
        system_id="${2}"
        if [ -z "$system_id" ]; then
            echo "Usage: $0 system-danger <system-id>"
            exit 1
        fi
        echo "=== Danger Assessment: $system_id ==="
        sqlite3 "$DB_FILE" <<EOF
.mode column
.headers on
SELECT
    s.name as System,
    COALESCE(dz.danger_level, 0) as 'Danger Level',
    COALESCE(dz.hostile_encounters, 0) as 'Hostile Encounters',
    COALESCE(dz.player_kills, 0) as 'Player Kills',
    COALESCE(dz.npc_kills, 0) as 'NPC Kills',
    COALESCE(dz.last_incident, 'Never') as 'Last Incident',
    CASE
        WHEN COALESCE(dz.danger_level, 0) = 0 THEN 'Safe'
        WHEN dz.danger_level <= 2 THEN 'Low Risk'
        WHEN dz.danger_level <= 5 THEN 'Moderate Risk'
        WHEN dz.danger_level <= 8 THEN 'High Risk'
        ELSE 'Extreme Risk'
    END as Assessment
FROM systems s
LEFT JOIN danger_zones dz ON s.id = dz.system_id
WHERE s.id = '$system_id';
EOF
        ;;

    exports)
        echo "=== Knowledge Exports ==="
        sqlite3 "$DB_FILE" <<EOF
.mode column
.headers on
SELECT
    id as 'Export ID',
    created_at as 'Created',
    created_by as 'Created By',
    systems_count as Systems,
    pois_count as POIs,
    experiences_count as Experiences,
    description as Description
FROM knowledge_exports
ORDER BY created_at DESC
LIMIT 20;
EOF
        ;;

    rich-deposits)
        min_richness="${2:-0.85}"
        echo "=== Rich Resource Deposits (Richness >= $min_richness) ==="
        sqlite3 "$DB_FILE" <<EOF
.mode column
.headers on
WITH latest_resources AS (
    SELECT poi_id, resource_id, richness, remaining,
           ROW_NUMBER() OVER (PARTITION BY poi_id, resource_id ORDER BY game_tick DESC) as rn
    FROM resource_history
)
SELECT
    p.name as POI,
    s.name as System,
    lr.resource_id as Resource,
    ROUND(lr.richness, 2) as Richness,
    ROUND(lr.remaining, 0) as Remaining
FROM latest_resources lr
JOIN pois p ON lr.poi_id = p.id
JOIN systems s ON p.system_id = s.id
WHERE lr.rn = 1 AND lr.richness >= $min_richness AND lr.remaining > 0
ORDER BY lr.richness DESC, lr.remaining DESC
LIMIT 30;
EOF
        ;;

    analytics-stats)
        echo "=== Enhanced Analytics Statistics ==="
        sqlite3 "$DB_FILE" <<EOF
SELECT 'Resource History Records' as Metric, COUNT(*) as Count FROM resource_history
UNION ALL
SELECT 'Optimized Routes', COUNT(*) FROM connection_metrics
UNION ALL
SELECT 'Active Anomalies', COUNT(*) FROM anomalies WHERE status = 'active'
UNION ALL
SELECT 'Price Trend Records', COUNT(*) FROM price_trends
UNION ALL
SELECT 'Danger Zones', COUNT(*) FROM danger_zones WHERE danger_level > 0
UNION ALL
SELECT 'Knowledge Exports', COUNT(*) FROM knowledge_exports;
EOF
        ;;

    help|*)
        cat <<EOF
Enhanced Analytics Query Tool
Usage: $0 <command> [args]

Resource Tracking:
  depleting [threshold]          - Show depleting resources (default: 50000)
  resource-history <poi> <res>   - View resource history
  rich-deposits [min-richness]   - Find rich deposits (default: 0.85)

Route Optimization:
  routes                         - List all known optimized routes
  route <from> <to>              - Show specific route metrics

Anomaly Detection:
  anomalies [type]               - View anomalies (all or by type)
  anomalies-severity <severity>  - Filter by severity (opportunity, warning, critical, info)

Market Analytics:
  price-trends <item> <station>  - Analyze price trends
  best-prices <item> <type>      - Find best buy/sell prices

Danger Zones:
  danger-zones [min-level]       - List dangerous systems (default: 3)
  system-danger <system-id>      - Check specific system danger

Knowledge Management:
  exports                        - List knowledge exports
  analytics-stats                - Show analytics statistics

Environment:
  DB_FILE                        - Path to SQLite database (default: spacemolt-knowledge.db)

Examples:
  $0 depleting 30000
  $0 resource-history sol-belt-1 iron
  $0 route Sol "Alpha Centauri"
  $0 anomalies rich_deposit
  $0 best-prices iron sell
  $0 danger-zones 5
  $0 rich-deposits 0.90

EOF
        ;;
esac
