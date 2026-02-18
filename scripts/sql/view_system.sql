-- ========================================================================
-- VIEW COMPREHENSIVE SYSTEM INFORMATION
-- ========================================================================
-- Usage examples:
--
-- Method 1: Using sed to replace the system_id variable
--   sed 's/:system_id/sys_0137/g' scripts/sql/view_system.sql | sqlite3 data/spacemolt-knowledge.db
--
-- Method 2: Interactive mode
--   sqlite3 data/spacemolt-knowledge.db
--   > .parameter init
--   > .param set :system_id sys_0137
--   > .read scripts/sql/view_system.sql
--
-- Method 3: Command line with variable
--   sqlite3 data/spacemolt-knowledge.db ".param set :system_id sys_0137" ".read scripts/sql/view_system.sql"
--
-- Method 4: Create a helper script
--   echo "SELECT * FROM system_view('sys_0137');" | sqlite3 data/spacemolt-knowledge.db
-- ========================================================================

.mode box
.headers on
.nullvalue 'N/A'

-- ========================================================================
-- SYSTEM OVERVIEW
-- ========================================================================
SELECT '=== SYSTEM OVERVIEW ===' AS "==================================================";

SELECT
    'System ID' AS "Field",
    id AS "Value"
FROM systems
WHERE id = :system_id

UNION ALL

SELECT
    'Name',
    name
FROM systems
WHERE id = :system_id

UNION ALL

SELECT
    'Position',
    'X: ' || CAST(ROUND(position_x, 2) AS TEXT) ||
    ', Y: ' || CAST(ROUND(position_y, 2) AS TEXT)
FROM systems
WHERE id = :system_id

UNION ALL

SELECT
    'Police Level',
    CAST(police_level AS TEXT) || ' (' ||
    CASE
        WHEN police_level = 0 THEN 'Lawless'
        WHEN police_level < 30 THEN 'Low Security'
        WHEN police_level < 70 THEN 'Medium Security'
        WHEN police_level >= 70 THEN 'High Security'
        ELSE 'Unknown'
    END || ')'
FROM systems
WHERE id = :system_id

UNION ALL

SELECT
    'Empire',
    COALESCE(empire, 'None')
FROM systems
WHERE id = :system_id

UNION ALL

SELECT
    'Stronghold',
    CASE WHEN is_stronghold = 1 THEN 'Yes' ELSE 'No' END
FROM systems
WHERE id = :system_id

UNION ALL

SELECT
    'Description',
    COALESCE(description, 'None')
FROM systems
WHERE id = :system_id

UNION ALL

SELECT
    'Last Updated Tick',
    CAST(last_updated_tick AS TEXT)
FROM systems
WHERE id = :system_id;

-- ========================================================================
-- CONNECTED SYSTEMS
-- ========================================================================
SELECT '=== CONNECTED SYSTEMS (' || COUNT(*) || ') ===' AS "=================================================="
FROM connections
WHERE from_system = :system_id;

SELECT
    c.to_system AS "System ID",
    s.name AS "Name",
    ROUND(s.position_x, 2) AS "X",
    ROUND(s.position_y, 2) AS "Y",
    CASE s.police_level
        WHEN 0 THEN 'Lawless'
        WHEN s.police_level < 30 THEN 'Low'
        WHEN s.police_level < 70 THEN 'Medium'
        WHEN s.police_level >= 70 THEN 'High'
        ELSE 'Unknown'
    END AS "Security",
    COALESCE(s.empire, 'None') AS "Empire"
FROM connections c
LEFT JOIN systems s ON c.to_system = s.id
WHERE c.from_system = :system_id
ORDER BY s.name;

-- ========================================================================
-- POINTS OF INTEREST
-- ========================================================================
SELECT '=== POINTS OF INTEREST (' || COUNT(*) || ') ===' AS "=================================================="
FROM pois
WHERE system_id = :system_id;

SELECT
    p.name AS "Name",
    p.type AS "Type",
    p.description AS "Description",
    ROUND(p.position_x, 2) AS "X",
    ROUND(p.position_y, 2) AS "Y",
    CASE WHEN b.id IS NOT NULL THEN 'Yes' ELSE 'No' END AS "Has Base",
    COALESCE(b.empire, 'None') AS "Empire"
FROM pois p
LEFT JOIN bases b ON p.id = b.poi_id
WHERE p.system_id = :system_id
ORDER BY
    CASE p.type
        WHEN 'station' THEN 1
        WHEN 'planet' THEN 2
        WHEN 'moon' THEN 3
        WHEN 'asteroid_belt' THEN 4
        ELSE 5
    END,
    p.name;


-- ========================================================================
-- POI RESOURCES
-- ========================================================================
SELECT '=== POI RESOURCES (' || COUNT(*) || ' total) ===' AS "=================================================="
FROM poi_resources pr
JOIN pois p ON pr.poi_id = p.id
WHERE p.system_id = :system_id;

SELECT
    p.name AS "POI",
    pr.resource_id AS "Resource",
    CAST(pr.richness AS TEXT) AS "Richness",
    CAST(pr.remaining AS TEXT) AS "Remaining",
    COALESCE(b.empire, 'None') AS "Empire"
FROM poi_resources pr
JOIN pois p ON pr.poi_id = p.id
LEFT JOIN bases b ON p.id = b.poi_id
WHERE p.system_id = :system_id
ORDER BY p.name, pr.resource_id;


-- ========================================================================
-- BASES & STATIONS
-- ========================================================================
SELECT '=== BASES/STATIONS (' || COUNT(*) || ') ===' AS "=================================================="
FROM bases b
JOIN pois p ON b.poi_id = p.id
WHERE p.system_id = :system_id;

SELECT
    b.name AS "Name",
    b.empire AS "Empire",
    b.description AS "Description",
    CAST(b.defense_level AS TEXT) AS "Defense Lvl",
    CASE WHEN b.has_drones = 1 THEN 'Yes' ELSE 'No' END AS "Drones",
    CASE WHEN b.public_access = 1 THEN 'Yes' ELSE 'No' END AS "Public"
FROM bases b
JOIN pois p ON b.poi_id = p.id
WHERE p.system_id = :system_id
ORDER BY b.name;


-- ========================================================================
-- BASE SERVICES
-- ========================================================================
SELECT '=== BASE SERVICES (' || COUNT(*) || ') ===' AS "=================================================="
FROM base_services bs
JOIN bases b ON bs.base_id = b.id
JOIN pois p ON b.poi_id = p.id
WHERE p.system_id = :system_id;

SELECT
    b.name AS "Base",
    bs.service_name AS "Service",
    CASE WHEN bs.available = 1 THEN '✓' ELSE '✗' END AS "Available"
FROM base_services bs
JOIN bases b ON bs.base_id = b.id
JOIN pois p ON b.poi_id = p.id
WHERE p.system_id = :system_id
ORDER BY b.name, bs.service_name;


-- ========================================================================
-- BASE FACILITIES
-- ========================================================================
SELECT '=== BASE FACILITIES (' || COUNT(*) || ') ===' AS "=================================================="
FROM base_facilities bf
JOIN bases b ON bf.base_id = b.id
JOIN pois p ON b.poi_id = p.id
WHERE p.system_id = :system_id;

SELECT
    b.name AS "Base",
    bf.facility_name AS "Facility",
    bf.category AS "Category",
    CAST(bf.level AS TEXT) AS "Level"
FROM base_facilities bf
JOIN bases b ON bf.base_id = b.id
JOIN pois p ON b.poi_id = p.id
WHERE p.system_id = :system_id
ORDER BY b.name, bf.category, bf.facility_name;
