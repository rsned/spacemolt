# SpaceMolt Import Catalog Skills

> Tool for importing skill catalog data from SpaceMolt game API responses into the knowledge base.

## Overview

`import-catalog-skills` is a command-line utility that imports the complete skill catalog from SpaceMolt game API responses. It captures skill metadata including descriptions, categories, max levels, training sources, XP requirements, and bonus per level, storing them in the SpaceMolt knowledge base for character progression and crafting decisions.

## Features

### Core Functionality
- **Complete Skill Catalog** - Imports all skills from the game
- **Skill Progression** - Records max levels and XP per level requirements
- **Bonus System** - Captures skill bonuses at each level
- **Prerequisites** - Stores required skills and levels for unlocking
- **Training Sources** - Tracks where skills can be trained

### Skill Information
- Skill ID, name, description, and category
- Maximum attainable level
- XP requirements for each level
- Bonus modifiers per level (e.g., "+5% mining speed")
- Required skills to unlock
- Training source locations

## Quick Start

### Basic Usage

```bash
# Build the tool
go build -o bin/import-catalog-skills ./cmd/import-catalog-skills

# Import catalog skills from a JSON file
./bin/import-catalog-skills data/catalog-skills.json

# Import with custom database path
SPACEMOLT_DB=/path/to/custom.db ./bin/import-catalog-skills data/catalog-skills.json
```

### Building from Source

```bash
# Build the binary
go build -o bin/import-catalog-skills ./cmd/import-catalog-skills

# Run with go run (for development)
go run ./cmd/import-catalog-skills data/catalog-skills.json
```

## Usage

### Command-Line Syntax

```bash
import-catalog-skills <catalog-skills.json>
```

### Arguments

| Argument | Type | Required | Description |
|----------|------|----------|-------------|
| `catalog-skills.json` | string | Yes | Path to JSON file containing skill catalog from the SpaceMolt API |

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `SPACEMOLT_DB` | Path to SQLite knowledge base database | `data/spacemolt-knowledge.db` |

## Input Format

The tool expects a JSON file matching the SpaceMolt `catalog_skills` API response format:

```json
{
  "items": [
    {
      "id": "string",
      "name": "string",
      "description": "string",
      "category": "string",
      "max_level": 0,
      "training_source": "string",
      "xp_per_level": [0, 100, 250, 500, 1000],
      "bonus_per_level": {
        "mining_speed": 5,
        "cargo_capacity": 10
      },
      "required_skills": {
        "skill_id": 0
      }
    }
  ]
}
```

### Field Descriptions

**Basic Information:**
- `id` - Unique skill identifier
- `name` - Skill name
- `description` - Skill description
- `category` - Skill category (e.g., "mining", "combat", "trading")
- `max_level` - Maximum attainable level (integer)
- `training_source` - Location or method to train the skill

**Progression:**
- `xp_per_level` - Array of XP requirements for each level
  - Index 0: Level 1 → Level 2
  - Index 1: Level 2 → Level 3
  - etc.

**Benefits:**
- `bonus_per_level` - Map of bonus types to values per level
  - Examples: "mining_speed": 5, "cargo_capacity": 10
  - Values are per-level bonuses

**Prerequisites:**
- `required_skills` - Map of skill IDs to required levels
  - Must have these skills at specified levels to unlock

## How It Works

### Import Process

1. **JSON Parsing** - Reads and validates the JSON input file
2. **Array Validation** - Ensures at least one skill exists in the response
3. **Skill Conversion** - Converts JSON skills to `Skill` objects
4. **Database Storage** - Stores all skills using `StoreSkills()`

### Data Structures

The tool converts JSON to the following internal structure:

```go
type Skill struct {
    ID             string
    Name           string
    Description    string
    Category       string
    MaxLevel       int
    TrainingSource string
    XPPerLevel     []int
    BonusPerLevel  map[string]int
    RequiredSkills map[string]int
}
```

### Error Handling

- Empty skill arrays are fatal
- JSON parsing errors are fatal
- Database errors are fatal
- Missing or invalid fields in individual skills don't halt execution

## Output

### Success Output

```
✓ Successfully imported {count} skills
```

### Error Output

```
Fatal: No skills found in JSON file
Fatal: Failed to read file: {error}
Fatal: Failed to parse JSON: {error}
Fatal: Failed to store skills: {error}
```

## Examples

### Example 1: Basic Import

```bash
# Import skill catalog from a downloaded API response
./bin/import-catalog-skills server_docs/catalog-skills.20260221.json
```

**Output:**
```
✓ Successfully imported 35 skills
```

### Example 2: Import with Custom Database

```bash
# Use a custom database path
SPACEMOLT_DB=/tmp/spacemolt.db ./bin/import-catalog-skills data/catalog-skills.json
```

### Example 3: Fetch and Import

```bash
# Fetch catalog from API and import directly
curl -s "https://api.spacemolt.com/v1/catalog_skills" \
  | ./bin/import-catalog-skills /dev/stdin
```

### Example 4: Query Skill Data

```bash
# After import, query specific skills
sqlite3 data/spacemolt-knowledge.db <<EOF
SELECT name, category, max_level, training_source
FROM catalog_skills
WHERE category='mining'
ORDER BY max_level DESC;
EOF
```

## Data Storage

### Database Schema

Imported data is stored in the `catalog_skills` table:

- `id` - Primary key (skill ID)
- `name` - Skill name
- `description` - Skill description
- `category` - Skill category
- `max_level` - Maximum level
- `training_source` - Where to train
- `xp_per_level` - JSON array of XP requirements
- `bonus_per_level` - JSON object of bonuses
- `required_skills` - JSON object of prerequisites

### Data Retrieval

Query imported skills using the knowledge base:

```go
// Get specific skill
skill, err := kb.GetSkill("mining_operation")

// Get all skills
skills, err := kb.GetSkills()
```

## Integration

### Using with Agent Tools

This tool is typically used in conjunction with:

- **import-catalog-recipes** - Recipes reference skills as requirements
- **import-catalog-ships** - Ships reference skills as requirements
- **crafting-server** - Uses skills for crafting eligibility

### Example Workflow

```bash
# 1. Import skill catalog
./bin/import-catalog-skills server_docs/catalog-skills.json

# 2. Import recipes (which reference skills)
./bin/import-catalog-recipes server_docs/catalog-recipes.json

# 3. Import ships (which reference skills)
./bin/import-catalog-ships server_docs/catalog-ships.json

# 4. Verify skill dependencies
sqlite3 data/spacemolt-knowledge.db <<EOF
SELECT s.name, s.max_level,
       COUNT(DISTINCT r.id) as recipe_count,
       COUNT(DISTINCT sc.id) as ship_count
FROM catalog_skills s
LEFT JOIN catalog_recipes r ON s.id LIKE '%' || json_extract(r.required_skills, '$.' || s.id)
LEFT JOIN catalog_ships sc ON s.id LIKE '%' || json_extract(sc.required_skills, '$.' || s.id)
GROUP BY s.id
ORDER BY recipe_count DESC
LIMIT 10;
EOF
```

## Skill Categories

Common skill categories include:

- **Mining** - Resource extraction (mining_operation, ore_processing)
- **Combat** - Fighting (weapon_operation, shield_management)
- **Trading** - Commerce (negotiation, market_analysis)
- **Crafting** - Manufacturing (smelting, manufacturing, assembly)
- **Engineering** - Ship systems (engineering, repair)
- **Navigation** - Movement (navigation, astrogation)
- **Leadership** - Command (leadership, tactics)

## Skill Progression

### XP Requirements

Skills use progressive XP requirements:

```json
"xp_per_level": [0, 100, 250, 500, 1000]
```

This means:
- Level 1 → Level 2: 100 XP
- Level 2 → Level 3: 250 XP
- Level 3 → Level 4: 500 XP
- Level 4 → Level 5: 1000 XP

### Bonus System

Skills provide bonuses per level:

```json
"bonus_per_level": {
  "mining_speed": 5,
  "cargo_capacity": 10
}
```

At Level 5:
- +25% mining speed (5 × 5%)
- +50 cargo capacity (5 × 10)

### Skill Prerequisites

Skills can require other skills:

```json
"required_skills": {
  "mining_operation": 3,
  "ore_processing": 1
}
```

Must have:
- Mining Operation Level 3
- Ore Processing Level 1

## Common Queries

### Find high-level skills:

```sql
SELECT name, category, max_level, training_source
FROM catalog_skills
WHERE max_level >= 5
ORDER BY category, max_level DESC;
```

### Find skills with no prerequisites:

```sql
SELECT name, category, training_source
FROM catalog_skills
WHERE required_skills = '{}'
ORDER BY category, name;
```

### Find mining skills:

```sql
SELECT name, max_level,
       json_extract(bonus_per_level, '$.mining_speed') as mining_bonus
FROM catalog_skills
WHERE category = 'mining'
ORDER BY max_level DESC;
```

## Troubleshooting

### Issue: "No skills found in JSON file"

**Cause:** The JSON structure doesn't match the expected format or the items array is empty.

**Solution:**
1. Verify the JSON file contains an `items` array
2. Validate JSON syntax using `jq . data/catalog-skills.json`
3. Ensure the file is from the `catalog_skills` API endpoint

### Issue: Skills not matching recipes

**Cause:** Recipe skill requirements don't match imported skill IDs.

**Solution:**
1. Verify skill IDs match between catalogs
2. Check for case sensitivity issues
3. Ensure both catalogs are from the same API version

### Issue: Bonus data not accessible

**Cause:** Bonus data is stored as JSON and needs special handling.

**Solution:**
1. Use SQLite JSON functions: `json_extract(bonus_per_level, '$.mining_speed')`
2. Or parse JSON in application code after retrieval
3. Or use the knowledge base API which handles JSON parsing

### Issue: XP arrays are different lengths

**Cause:** Skills have different max levels and XP requirements.

**Solution:**
1. This is normal - each skill has its own progression
2. Array length should equal `max_level - 1`
3. Missing values will cause issues

## Related Tools

- **import-catalog-recipes** - Import recipes using these skills
- **import-catalog-ships** - Import ships requiring these skills
- **crafting-server** - MCP server for crafting decisions

## License

Part of the SpaceMolt project.
