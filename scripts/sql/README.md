# SQL Scripts

This directory contains all SQL scripts and schemas for the SpaceMolt project.

## Directory Structure

- **`initialize_database.sql`** - Main database schema for the knowledge base
- **`schema_crafting.sql`** - Crafting server database schema (symlink to embedded file)
- **`view_system.sql`** - Query script for viewing system information
- **`migrations/`** - Database migration scripts

## Database Location

The default database location is: `data/spacemolt-knowledge.db`

## Schema Files

### initialize_database.sql

Complete database schema for the SpaceMolt agent knowledge base (Schema Version 4).

**Usage:**
```bash
# Initialize a fresh database
sqlite3 data/spacemolt-knowledge.db < scripts/sql/initialize_database.sql
```

### schema_crafting.sql

Database schema for the crafting server. This file is embedded in the Go code and is provided here for reference.

## Query Scripts

### view_system.sql

Displays comprehensive information about a specific system, including:
- System overview (position, security, faction, visit history)
- Connected systems with security levels
- Points of Interest (POIs)
- POI resources and richness
- Bases and stations
- Base services
- Base facilities

**Usage:**

```bash
# Using the wrapper script (recommended)
./scripts/view-system.sh <system_id>

# Examples
./scripts/view-system.sh sol
./scripts/view-system.sh procyon
./scripts/view-system.sh sys_0137

# Using SQLite directly with parameters
sqlite3 data/spacemolt-knowledge.db \
  ".param set :system_id sol" \
  ".read scripts/sql/view_system.sql"

# Using sed (for automation)
sed 's/:system_id/sol/g' scripts/sql/view_system.sql | \
  sqlite3 data/spacemolt-knowledge.db
```

## Common Queries

### List all systems

```bash
sqlite3 data/spacemolt-knowledge.db \
  "SELECT id, name, ROUND(pos_x, 2) as x, ROUND(pos_y, 2) as y FROM systems ORDER BY name;"
```

### Find systems by security level

```bash
# Lawless systems
sqlite3 data/spacemolt-knowledge.db \
  "SELECT id, name FROM systems WHERE police_level = 0 ORDER BY name;"

# High security systems
sqlite3 data/spacemolt-knowledge.db \
  "SELECT id, name FROM systems WHERE police_level = 3 ORDER BY name;"
```

### Find systems with bases

```bash
sqlite3 data/spacemolt-knowledge.db \
  "SELECT DISTINCT s.id, s.name, b.empire
   FROM systems s
   JOIN pois p ON s.id = p.system_id
   JOIN bases b ON p.id = b.poi_id
   ORDER BY s.name;"
```

### Find systems with resources

```bash
sqlite3 data/spacemolt-knowledge.db \
  "SELECT DISTINCT s.id, s.name, COUNT(DISTINCT pr.resource_id) as resource_count
   FROM systems s
   JOIN pois p ON s.id = p.system_id
   JOIN poi_resources pr ON p.id = pr.poi_id
   GROUP BY s.id, s.name
   HAVING resource_count > 0
   ORDER BY resource_count DESC;"
```

### Count systems, POIs, and bases

```bash
sqlite3 data/spacemolt-knowledge.db \
  "SELECT
     (SELECT COUNT(*) FROM systems) as systems,
     (SELECT COUNT(*) FROM pois) as pois,
     (SELECT COUNT(*) FROM bases) as bases,
     (SELECT COUNT(*) FROM connections) as connections;"
```

## Database Schema

### Core Tables

- **systems** - Solar system data (position, security, faction)
- **connections** - System-to-system connections (routes)
- **pois** - Points of Interest (planets, stations, asteroid belts)
- **poi_resources** - Resources available at POIs
- **bases** - Space stations and bases
- **base_services** - Services available at bases
- **base_facilities** - Production and service facilities
- **base_market** - Items for sale (volatile data)
- **market_snapshots** - Historical market data (volatile)
- **ship_listings** - Ships for sale (volatile)

## Finding System IDs

To find a system ID to use with these scripts:

```bash
# List all systems with IDs
sqlite3 -header -column data/spacemolt-knowledge.db \
  "SELECT id, name FROM systems ORDER BY name LIMIT 20;"

# Search for a system by name
sqlite3 -header -column data/spacemolt-knowledge.db \
  "SELECT id, name FROM systems WHERE name LIKE '%Procyon%';"
```

## Tips

1. **Use the wrapper script** `view-system.sh` for easier interaction
2. **Enable box mode** in SQLite for nicer tables: `.mode box`
3. **Enable headers**: `.headers on`
4. **Set null values**: `.nullvalue 'N/A'`
5. **Pipe to less** for long output: `./scripts/view-system.sh sol | less`
