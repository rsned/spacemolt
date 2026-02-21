# Game API Data Import Check Tool

## Overview

This tool checks whether data from JSON files in `data/game-api/` has been imported into the database. It scans all JSON files and compares them against the database to find:

1. **Missing items** - Data found in files but not in the database
2. **Elements without ID** - Items that cannot be imported because they lack an ID field

## Usage

```bash
# Check all game-api data
go run cmd/check-game-api-import/main.go data/game-api/

# Check specific agent directory
go run cmd/check-game-api-import/main.go data/game-api/trader-1/

# Use custom database path
SPACEMOLT_DB=/path/to/db go run cmd/check-game-api-import/main.go data/game-api/
```

## What It Checks

The tool processes the following file types:

| File Type | Database Table | Description |
|-----------|---------------|-------------|
| `get_system.json` | `systems` | System information |
| `get_map.json` | `systems` | Star map with all systems |
| `get_base.json` | `bases` | Space base/station data |
| `catalog_items.json` | `items` | Item catalog |
| `catalog_skills.json` | `skills` | Skill definitions |
| `catalog_recipes.json` | `recipes` | Crafting recipes |
| `catalog_ships.json` | `ship_classes` | Ship types |
| `get_poi.json` | - | POI data (not imported) |
| `get_wrecks.json` | - | Wreck data (not imported) |
| `get_ship.json` | - | Player ship (not imported) |
| `get_status.json` | - | Player status (not imported) |
| `get_skills.json` | - | Player skills (not imported) |
| `get_ships.json` | - | Ship listings (ephemeral) |
| `get_listings.json` | - | Market listings (ephemeral) |
| `get_nearby.json` | - | Nearby players (ephemeral) |

## Report Format

The tool generates a report with:

### Missing Items
- Data found in JSON files but not in database
- Grouped by data type
- Shows first 5 examples per type
- Includes source file name

### Elements Without ID
- Items that cannot be imported (no ID field)
- Grouped by data type
- Shows first 3 examples per type
- Includes file and context information

### Recommendations
- Suggests which import tools to run
- Flags items that need investigation

## Example Output

```
================================================================================
GAME API DATA IMPORT CHECK REPORT
================================================================================

📊 SUMMARY: 15 items found in files but missing from database
--------------------------------------------------------------------------------

🔍 system: 10 missing items across 3 files
  - alpha_centauri (from craftsman-1/get_map.json)
  - tau_ceti (from craftsman-1/get_map.json)
  ... and 8 more

🔍 base: 5 missing items across 2 files
  - haven_base (from explorer-1/get_base.json)
  - sol_base (from trader-2/get_base.json)
  ... and 3 more

⚠️  ELEMENTS WITHOUT ID: 212 items
--------------------------------------------------------------------------------

🚫 catalog_item: 212 items without ID
  - Index 268 in catalog_items.json (item at index 268)
  - Index 269 in catalog_items.json (item at index 269)
  ... and 209 more
  ℹ️  Note: These items may have 'type_id' instead of 'id'

================================================================================
❌ Found 15 missing items and 212 items without ID

📝 Recommendations:
  - Run import tools for missing data types
  - Use: go run cmd/import-catalog-*/main.go <path-to-json>
  - Use: go run cmd/import-base-data/main.go <path-to-json>
  - Use: go run cmd/import-map-data/main.go <path-to-json>
  - Investigate files with missing ID fields
  - These items were skipped during import
================================================================================
```

## Common Issues

### Items Without ID

Some items in the API responses have empty `id` fields. This is intentional:

- **Modules** (weapons, shields, mining lasers, etc.) use `type_id` instead of `id`
- These are not imported into the `items` table by design
- The import tools correctly skip these items

### Base Data Showing as Missing

When an agent is not at a base, `get_base.json` contains `"base": null`. The tool correctly skips these files.

### Map Data Using system_id

The `get_map.json` endpoint uses `system_id` instead of `id`. The tool handles this correctly.

## Import Tools

To import missing data, use the appropriate import tool:

```bash
# Import catalog items
go run cmd/import-catalog-items/main.go data/game-api/trader-1/catalog_items.json

# Import catalog skills
go run cmd/import-catalog-skills/main.go data/game-api/trader-1/catalog_skills.json

# Import catalog recipes
go run cmd/import-catalog-recipes/main.go data/game-api/trader-1/catalog_recipes.json

# Import catalog ships
go run cmd/import-catalog-ships/main.go data/game-api/trader-1/catalog_ships.json

# Import base data
go run cmd/import-base-data/main.go data/game-api/trader-1/get_base.json

# Import map data
go run cmd/import-map-data/main.go data/game-api/trader-1/get_map.json
```

## Database Schema

The tool checks against the following database tables:

- `systems` - Star systems
- `bases` - Space bases/stations
- `pois` - Points of interest
- `items` - Catalog items
- `skills` - Skill definitions
- `recipes` - Crafting recipes
- `ship_classes` - Ship types

## Notes

- Player-specific data (ship, status, skills) is not imported
- Ephemeral data (market listings, nearby players) is not imported
- Wreck data is captured but not imported to database
- POI data exists in files but is not imported separately (it's part of system data)
