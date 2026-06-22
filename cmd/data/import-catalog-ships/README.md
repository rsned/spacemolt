# SpaceMolt Import Catalog Ships

> Tool for importing ship catalog data from SpaceMolt game API responses into the knowledge base.

## Overview

`import-catalog-ships` is a command-line utility that imports the complete ship catalog from SpaceMolt game API responses. It captures comprehensive ship class information including stats, slots, pricing, requirements, and build materials, storing them in the SpaceMolt knowledge base for ship selection and progression planning.

## Features

### Core Functionality
- **Complete Ship Catalog** - Imports all ship classes from the game
- **Comprehensive Stats** - Hull, shields, armor, speed, fuel, cargo, CPU, power
- **Slot Information** - Weapon, defense, and utility slots
- **Build Materials** - Required items and quantities for construction
- **Skill Requirements** - Required skills and levels for each ship class

### Ship Information
- Ship ID, name, class, category, and description
- Faction affiliation and tier level
- Pricing and shipyard requirements
- Starter ship identification
- Build time and materials
- Default modules and flavor tags

## Quick Start

### Basic Usage

```bash
# Build the tool
go build -o bin/import-catalog-ships ./cmd/import-catalog-ships

# Import catalog ships from a JSON file
./bin/import-catalog-ships data/catalog-ships.json

# Import with custom database path
SPACEMOLT_DB=/path/to/custom.db ./bin/import-catalog-ships data/catalog-ships.json
```

### Building from Source

```bash
# Build the binary
go build -o bin/import-catalog-ships ./cmd/import-catalog-ships

# Run with go run (for development)
go run ./cmd/import-catalog-ships data/catalog-ships.json
```

## Usage

### Command-Line Syntax

```bash
import-catalog-ships <catalog-ships.json>
```

### Arguments

| Argument | Type | Required | Description |
|----------|------|----------|-------------|
| `catalog-ships.json` | string | Yes | Path to JSON file containing ship catalog from the SpaceMolt API |

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `SPACEMOLT_DB` | Path to SQLite knowledge base database | `data/spacemolt-knowledge.db` |

## Input Format

The tool expects a JSON file matching the SpaceMolt `catalog_ships` API response format:

```json
{
  "items": [
    {
      "id": "string",
      "name": "string",
      "class": "string",
      "category": "string",
      "description": "string",
      "lore": "string",
      "faction": "string",
      "tier": 0,
      "scale": 0,
      "price": 0,
      "base_hull": 0,
      "base_shield": 0,
      "base_shield_recharge": 0,
      "base_armor": 0,
      "base_speed": 0,
      "base_fuel": 0,
      "cargo_capacity": 0,
      "cpu_capacity": 0,
      "power_capacity": 0,
      "weapon_slots": 0,
      "defense_slots": 0,
      "utility_slots": 0,
      "build_time": 0,
      "shipyard_tier": 0,
      "starter_ship": false,
      "tow_speed_bonus": 0,
      "required_skills": {
        "skill_id": 0
      },
      "default_modules": ["module_id_1", "module_id_2"],
      "flavor_tags": ["tag1", "tag2"],
      "build_materials": [
        {
          "item_id": "string",
          "quantity": 0
        }
      ]
    }
  ]
}
```

### Field Descriptions

**Basic Information:**
- `id` - Unique ship class identifier
- `name` - Ship class name
- `class` - Ship class (e.g., "Dart", "Hawk", "Behemoth")
- `category` - Ship category (e.g., "light_fighter", "freighter", "corvette")
- `description` - Ship description
- `lore` - Background lore text
- `faction` - Faction affiliation (e.g., "Federation", "Empire", "Independent")
- `tier` - Tech tier level (integer)
- `scale` - Ship scale/size (integer)

**Economic:**
- `price` - Base price in credits (integer)
- `build_time` - Construction time in seconds (integer)
- `shipyard_tier` - Required shipyard tier (integer)

**Combat Stats:**
- `base_hull` - Hull points (integer)
- `base_shield` - Shield points (integer)
- `base_shield_recharge` - Shield recharge rate (integer)
- `base_armor` - Armor points (integer)

**Performance:**
- `base_speed` - Speed rating (integer)
- `base_fuel` - Fuel capacity (integer)
- `tow_speed_bonus` - Speed bonus when towing (integer)

**Systems:**
- `cargo_capacity` - Cargo space (integer)
- `cpu_capacity` - CPU capacity (integer)
- `power_capacity` - Power capacity (integer)

**Slots:**
- `weapon_slots` - Number of weapon slots (integer)
- `defense_slots` - Number of defense slots (integer)
- `utility_slots` - Number of utility slots (integer)

**Requirements & Features:**
- `starter_ship` - Whether this is a starter ship (boolean)
- `required_skills` - Map of skill IDs to required levels
- `default_modules` - Array of default module IDs
- `flavor_tags` - Array of descriptive tags
- `build_materials` - Array of required construction materials

## How It Works

### Import Process

1. **JSON Parsing** - Reads and validates the JSON input file
2. **Array Validation** - Ensures at least one ship class exists in the response
3. **Ship Conversion** - Converts JSON ships to `ShipClassDef` objects
4. **Material Mapping** - Converts build materials to `BuildMaterial` objects
5. **Database Storage** - Stores all ship classes using `StoreShipClasses()`

### Data Structures

The tool converts JSON to the following internal structures:

```go
type ShipClassDef struct {
    ID                 string
    Name               string
    Class              string
    Category           string
    Description        string
    Lore               string
    Faction            string
    Tier               int
    Scale              int
    Price              int
    BaseHull           int
    BaseShield         int
    BaseShieldRecharge int
    BaseArmor          int
    BaseSpeed          int
    BaseFuel           int
    CargoCapacity      int
    CPUCapacity        int
    PowerCapacity      int
    WeaponSlots        int
    DefenseSlots       int
    UtilitySlots       int
    BuildTime          int
    ShipyardTier       int
    StarterShip        bool
    TowSpeedBonus      int
    RequiredSkills     map[string]int
    DefaultModules     []string
    FlavorTags         []string
    BuildMaterials     []BuildMaterial
    LastUpdatedTick    int64
}

type BuildMaterial struct {
    ItemID   string
    Quantity int
}
```

### Error Handling

- Empty ship arrays are fatal
- JSON parsing errors are fatal
- Database errors are fatal
- Missing or invalid fields in individual ships don't halt execution

## Output

### Success Output

```
✓ Successfully imported {count} ship classes
```

### Error Output

```
Fatal: No ship classes found in JSON file
Fatal: Failed to read file: {error}
Fatal: Failed to parse JSON: {error}
Fatal: Failed to store ship classes: {error}
```

## Examples

### Example 1: Basic Import

```bash
# Import ship catalog from a downloaded API response
./bin/import-catalog-ships server_docs/catalog-ships.20260221.json
```

**Output:**
```
✓ Successfully imported 42 ship classes
```

### Example 2: Import with Custom Database

```bash
# Use a custom database path
SPACEMOLT_DB=/tmp/spacemolt.db ./bin/import-catalog-ships data/catalog-ships.json
```

### Example 3: Fetch and Import

```bash
# Fetch catalog from API and import directly
curl -s "https://api.spacemolt.com/v1/catalog_ships" \
  | ./bin/import-catalog-ships /dev/stdin
```

### Example 4: Query Ship Data

```bash
# After import, query specific ship classes
sqlite3 data/spacemolt-knowledge.db <<EOF
SELECT name, class, tier, cargo_capacity, base_speed
FROM catalog_ships
WHERE tier <= 2
ORDER BY tier, cargo_capacity DESC
LIMIT 10;
EOF
```

## Data Storage

### Database Schema

Imported data is stored in the following tables:

**catalog_ships** - Ship class metadata
- All ship statistics and properties
- Foreign key relationships for materials and skills

**ship_build_materials** - Construction requirements
- `ship_id` - Foreign key to ship
- `item_id` - Required material item ID
- `quantity` - Quantity required

**ship_required_skills** - Skill requirements
- `ship_id` - Foreign key to ship
- `skill_id` - Required skill ID
- `required_level` - Required skill level

### Data Retrieval

Query imported ships using the knowledge base:

```go
// Get specific ship class
ship, err := kb.GetShipClass(ctx, "dart_mk1")

// Get all ships
ships, err := kb.GetShipClasses(ctx)

// Get ships by category
freighters, err := kb.GetShipClassesByCategory(ctx, "freighter")
```

## Integration

### Using with Agent Tools

This tool is typically used in conjunction with:

- **import-catalog-items** - Import build materials referenced by ships
- **import-catalog-skills** - Import skills required by ships
- **ship-trader** - Analyzes ship markets for arbitrage

### Example Workflow

```bash
# 1. Import item catalog first (ships reference items)
./bin/import-catalog-items server_docs/catalog-items.json

# 2. Import skill catalog (ships reference skills)
./bin/import-catalog-skills server_docs/catalog-skills.json

# 3. Import ship catalog
./bin/import-catalog-ships server_docs/catalog-ships.json

# 4. Query starter ships
sqlite3 data/spacemolt-knowledge.db <<EOF
SELECT name, cargo_capacity, base_speed
FROM catalog_ships
WHERE starter_ship = 1;
EOF
```

## Ship Categories

Common ship categories include:

- **Light Fighters** - Fast, agile combat ships (Dart, Wasp)
- **Heavy Fighters** - Slower but more powerful (Hawk, Eagle)
- **Corvettes** - Multi-role combat vessels
- **Freighters** - Cargo haulers (Hauler, Transport)
- **Capital Ships** - Large, powerful vessels (Behemoth, Titan)
- **Specialized** - Mining, exploration, trading ships

## Progression System

Ships are organized by tiers:

- **Tier 1** - Basic starter ships
- **Tier 2** - Improved vessels
- **Tier 3** - Advanced ships
- **Tier 4+** - Elite and capital ships

Higher tiers typically have:
- Better stats (hull, shields, speed)
- More slots (weapons, defense, utility)
- Higher skill requirements
- Higher prices

## Troubleshooting

### Issue: "No ship classes found in JSON file"

**Cause:** The JSON structure doesn't match the expected format or the items array is empty.

**Solution:**
1. Verify the JSON file contains an `items` array
2. Validate JSON syntax using `jq . data/catalog-ships.json`
3. Ensure the file is from the `catalog_ships` API endpoint

### Issue: "Failed to store ship classes: FOREIGN KEY constraint failed"

**Cause:** Ship references items or skills that don't exist in the database.

**Solution:**
1. Import items first: `./bin/import-catalog-items items.json`
2. Import skills first: `./bin/import-catalog-skills skills.json`
3. Then import ships

### Issue: Ships missing from queries

**Cause:** Ships were imported but item/skill references are missing.

**Solution:**
1. Verify all referenced items exist
2. Verify all referenced skills exist
3. Check for missing IDs in the import data

### Issue: Wrong ship stats

**Cause:** Database contains outdated ship data.

**Solution:**
1. Download fresh catalog from API
2. Clear existing ships: `DELETE FROM catalog_ships;`
3. Reimport updated catalog

## Common Queries

### Find best cargo ships by tier:

```sql
SELECT name, tier, cargo_capacity, price
FROM catalog_ships
WHERE starter_ship = 0
ORDER BY tier, cargo_capacity DESC;
```

### Find combat ships by weapon slots:

```sql
SELECT name, class, weapon_slots, base_hull, base_shield
FROM catalog_ships
WHERE weapon_slots > 0
ORDER BY weapon_slots DESC, base_hull DESC;
```

### Find affordable ships:

```sql
SELECT name, price, cargo_capacity, base_speed
FROM catalog_ships
WHERE price <= 1000
ORDER BY price;
```

## Related Tools

- **import-catalog-items** - Import build materials
- **import-catalog-skills** - Import required skills
- **import-catalog-recipes** - Import crafting recipes
- **ship-trader** - Ship market analysis tool

## License

Part of the SpaceMolt project.
