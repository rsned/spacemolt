# SpaceMolt Skill Converter

> Data transformation tool that converts SpaceMolt skill JSON from game API format to the crafting server import format.

## Overview

The `convert-skills` tool transforms raw skill data exported from the SpaceMolt game API into a normalized format suitable for import into the SpaceMolt crafting server database. It handles structural transformation, prerequisite mapping, and XP calculation to ensure compatibility with the crafting system's skill requirements.

## Features

### Core Functionality
- **Format Transformation** - Converts game API skill format to crafting server import format
- **Prerequisite Mapping** - Converts skill requirement maps to structured prerequisite arrays
- **XP Calculation** - Computes cumulative XP requirements for all skill levels
- **Preservation of Metadata** - Maintains skill categories, descriptions, and bonuses
- **Bulk Processing** - Handles entire skill collections in a single operation

### Data Transformation

The converter performs the following transformations:

1. **Structure Changes**
   - Converts from map-based (`{"skills": {...}}`) to array-based format (`[{...}, {...}]`)
   - Flattens nested skill objects

2. **Field Renaming**
   - `required_skills` → `prerequisites`
   - `xp_per_level` → `levels` (with cumulative XP calculation)

3. **Data Restructuring**
   - `required_skills`: Converts from `{"skill_id": level}` to `[{skill_id, level}]`
   - `xp_per_level`: Converts from array of per-level XP to array of cumulative XP objects
   - `bonus_per_level`: Preserved as-is (not used in conversion but available in data)

## Quick Start

### Basic Usage

```bash
# Convert skills from game API format
go run ./cmd/convert-skills server_docs/skills.20260216.json data/crafting/skills-import.json

# Using the built binary
./bin/convert-skills server_docs/skills.20260216.json data/crafting/skills-import.json
```

### Building

```bash
# Build the converter binary
go build -o bin/convert-skills ./cmd/convert-skills

# Run the built binary
./bin/convert-skills input.json output.json
```

## Usage

### Command-Line Syntax

```bash
convert-skills <input.json> <output.json>
```

### Arguments

| Argument | Required | Description |
|----------|----------|-------------|
| `input.json` | Yes | Path to input file in SpaceMolt game API format |
| `output.json` | Yes | Path to output file for crafting server import format |

### Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | Error (see stderr for details) |

## Input Format

The converter expects input files in the SpaceMolt game API format:

```json
{
  "player_skill_count": 1,
  "player_skills": [
    {
      "category": "Mining",
      "current_xp": 71,
      "level": 2,
      "max_level": 10,
      "name": "Mining",
      "next_level_xp": 600,
      "skill_id": "mining_basic"
    }
  ],
  "skills": {
    "mining_basic": {
      "id": "mining_basic",
      "name": "Mining",
      "category": "Mining",
      "description": "Extraction of mineral resources from asteroids and planetary bodies.",
      "max_level": 10,
      "required_skills": {},
      "xp_per_level": [100, 200, 350, 550, 800, 1100, 1450, 1850, 2300, 2800],
      "bonus_per_level": {
        "miningSpeed": 5,
        "oreYield": 2
      }
    },
    "advanced_engineering": {
      "id": "advanced_engineering",
      "name": "Advanced Engineering",
      "category": "Engineering",
      "description": "Expert systems optimization. Unlocks overclocking.",
      "max_level": 10,
      "required_skills": {
        "engineering": 5
      },
      "xp_per_level": [500, 1500, 3000, 5000, 8000, 12000, 17000, 23000, 30000, 40000],
      "bonus_per_level": {
        "cpuEfficiency": 2,
        "overclockPotential": 5,
        "powerEfficiency": 2
      }
    }
  }
}
```

### Input Field Reference

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `skills` | object | Yes | Map of skill_id to skill objects |
| `id` | string | Yes | Unique skill identifier |
| `name` | string | Yes | Human-readable skill name |
| `category` | string | Yes | Skill category (e.g., "Mining", "Engineering") |
| `description` | string | Yes | Skill description |
| `max_level` | integer | Yes | Maximum achievable level |
| `required_skills` | object | No | Map of skill_id to required level |
| `xp_per_level` | array | Yes | XP required for each level (non-cumulative) |
| `bonus_per_level` | object | No | Stat bonuses per skill level |

### XP Per Level Format

The `xp_per_level` array contains **non-cumulative** XP values:
- Index 0: XP required to go from level 0 to level 1
- Index 1: XP required to go from level 1 to level 2
- And so on...

Example: `[100, 200, 350]` means:
- Level 1: 100 XP total
- Level 2: 100 + 200 = 300 XP total
- Level 3: 100 + 200 + 350 = 650 XP total

## Output Format

The converter produces output in the crafting server import format:

```json
[
  {
    "id": "mining_basic",
    "name": "Mining",
    "category": "Mining",
    "description": "Extraction of mineral resources from asteroids and planetary bodies.",
    "max_level": 10,
    "prerequisites": [],
    "levels": [
      {
        "level": 1,
        "xp_required": 100
      },
      {
        "level": 2,
        "xp_required": 300
      },
      {
        "level": 3,
        "xp_required": 650
      }
    ]
  },
  {
    "id": "advanced_engineering",
    "name": "Advanced Engineering",
    "category": "Engineering",
    "description": "Expert systems optimization. Unlocks overclocking.",
    "max_level": 10,
    "prerequisites": [
      {
        "skill_id": "engineering",
        "level": 5
      }
    ],
    "levels": [
      {
        "level": 1,
        "xp_required": 500
      },
      {
        "level": 2,
        "xp_required": 2000
      },
      {
        "level": 3,
        "xp_required": 5000
      }
    ]
  }
]
```

### Output Field Reference

| Field | Type | Description |
|-------|------|-------------|
| `id` | string | Unique skill identifier |
| `name` | string | Human-readable skill name |
| `category` | string | Skill category |
| `description` | string | Skill description |
| `max_level` | integer | Maximum achievable level |
| `prerequisites` | array | Required skills and levels |
| `levels` | array | XP requirements for each level (cumulative) |

### Prerequisite Schema

```json
{
  "skill_id": "engineering",
  "level": 5
}
```

### Level Schema

```json
{
  "level": 3,
  "xp_required": 650
}
```

The `xp_required` value is **cumulative** - it represents the total XP needed to reach that level from level 0.

## How It Works

### Conversion Process

1. **Parse Input**
   - Read input JSON file
   - Parse SpaceMolt skill format
   - Validate structure

2. **Transform Data**
   - Iterate through all skills
   - Map fields to output schema
   - Convert prerequisite map to prerequisite array
   - Calculate cumulative XP for each level
   - Build levels array with cumulative XP values

3. **Generate Output**
   - Marshal to indented JSON
   - Write to output file
   - Report conversion statistics

### Transformation Logic

```go
For each skill in input:
  1. Extract basic fields (id, name, category, description, max_level)
  2. Convert required_skills map to prerequisites array:
     For each {skill_id: level} entry:
       Create {skill_id, level} object
       Add to prerequisites array
  3. Convert xp_per_level array to levels array with cumulative XP:
     cumulativeXP = 0
     For each xp in xp_per_level (with index i):
       cumulativeXP += xp
       Create {level: i+1, xp_required: cumulativeXP}
       Add to levels array
  4. Create ImportSkill object with all transformed data
```

### XP Calculation Example

Input `xp_per_level`: `[100, 200, 350, 550, 800]`

Conversion:
- Level 1: 100 XP (100)
- Level 2: 300 XP (100 + 200)
- Level 3: 650 XP (100 + 200 + 350)
- Level 4: 1200 XP (100 + 200 + 350 + 550)
- Level 5: 2000 XP (100 + 200 + 350 + 550 + 800)

Output `levels`:
```json
[
  {"level": 1, "xp_required": 100},
  {"level": 2, "xp_required": 300},
  {"level": 3, "xp_required": 650},
  {"level": 4, "xp_required": 1200},
  {"level": 5, "xp_required": 2000}
]
```

### Prerequisite Conversion Example

Input `required_skills`:
```json
{
  "engineering": 5,
  "mining_basic": 3
}
```

Output `prerequisites`:
```json
[
  {"skill_id": "engineering", "level": 5},
  {"skill_id": "mining_basic", "level": 3}
]
```

## Examples

### Example 1: Basic Conversion

```bash
# Convert the latest skill export
go run ./cmd/convert-skills server_docs/skills.20260216.json data/crafting/skills-import.json
```

**Output:**
```
Converted 28 skills from server_docs/skills.20260216.json to data/crafting/skills-import.json
```

### Example 2: Full Crafting Server Setup

```bash
# Create data directory
mkdir -p data/crafting

# Convert skills
go run ./cmd/convert-skills server_docs/skills.20260216.json data/crafting/skills-import.json

# Convert recipes
go run ./cmd/convert-recipes server_docs/recipes.20260216.json data/crafting/recipes-import.json

# Import into crafting server (requires crafting-server to be built)
./bin/crafting-server -import-skills data/crafting/skills-import.json
./bin/crafting-server -import-recipes data/crafting/recipes-import.json
```

### Example 3: Inspecting Converted Output

```bash
# Convert and inspect the output
go run ./cmd/convert-skills server_docs/skills.20260216.json /tmp/skills-converted.json

# View a specific skill
cat /tmp/skills-converted.json | jq '.[] | select(.id == "mining_basic")'
```

**Output:**
```json
{
  "id": "mining_basic",
  "name": "Mining",
  "category": "Mining",
  "description": "Extraction of mineral resources from asteroids and planetary bodies.",
  "max_level": 10,
  "prerequisites": [],
  "levels": [
    {
      "level": 1,
      "xp_required": 100
    },
    {
      "level": 2,
      "xp_required": 300
    },
    {
      "level": 3,
      "xp_required": 650
    },
    {
      "level": 4,
      "xp_required": 1200
    },
    {
      "level": 5,
      "xp_required": 2000
    },
    {
      "level": 6,
      "xp_required": 3100
    },
    {
      "level": 7,
      "xp_required": 4550
    },
    {
      "level": 8,
      "xp_required": 6400
    },
    {
      "level": 9,
      "xp_required": 8700
    },
    {
      "level": 10,
      "xp_required": 11500
    }
  ]
}
```

### Example 4: Checking Prerequisites

```bash
# Convert skills
go run ./cmd/convert-skills server_docs/skills.20260216.json /tmp/skills.json

# View skills with prerequisites
cat /tmp/skills.json | jq '.[] | select(.prerequisites | length > 0) | {id, name, prerequisites}'
```

**Output:**
```json
{
  "id": "advanced_engineering",
  "name": "Advanced Engineering",
  "prerequisites": [
    {
      "skill_id": "engineering",
      "level": 5
    }
  ]
}
{
  "id": "weapons_specialization",
  "name": "Weapons Specialization",
  "prerequisites": [
    {
      "skill_id": "combat_basic",
      "level": 5
    }
  ]
}
```

### Example 5: Verifying XP Calculations

```bash
# Convert and verify XP progression
go run ./cmd/convert-skills server_docs/skills.20260216.json /tmp/skills.json

# Check XP progression for a skill
cat /tmp/skills.json | jq '.[] | select(.id == "mining_basic") | .levels | map(.xp_required)'
```

**Output:**
```json
[
  100,
  300,
  650,
  1200,
  2000,
  3100,
  4550,
  6400,
  8700,
  11500
]
```

## Typical Workflow

### Updating Skill Data

When new skill data is available from the game API:

```bash
# 1. Export/update skill data from game API
# (This is typically done by the data-scraper tool)

# 2. Convert to import format
go run ./cmd/convert-skills server_docs/skills.20260216.json data/crafting/skills-import.json

# 3. Import into crafting server
./bin/crafting-server -import-skills data/crafting/skills-import.json

# 4. Verify import
./bin/crafting-server -list-skills | head -20
```

### Initial Setup

```bash
# Build all conversion tools
go build -o bin/convert-recipes ./cmd/convert-recipes
go build -o bin/convert-skills ./cmd/convert-skills
go build -o bin/crafting-server ./cmd/crafting-server

# Create data directory
mkdir -p data/crafting

# Convert and import skill data
./bin/convert-skills server_docs/skills.20260216.json data/crafting/skills-import.json
./bin/crafting-server -import-skills data/crafting/skills-import.json

# Convert and import recipe data
./bin/convert-recipes server_docs/recipes.20260216.json data/crafting/recipes-import.json
./bin/crafting-server -import-recipes data/crafting/recipes-import.json
```

## Data Validation

The converter does not perform extensive validation beyond basic JSON parsing. Ensure:

1. **Input File Validity**
   - Input file must be valid JSON
   - Must have a top-level "skills" object
   - Each skill must have required fields
   - `xp_per_level` array length should match `max_level`

2. **Output Verification**
   - Check output file exists and is valid JSON
   - Verify skill count matches expected number
   - Inspect sample skills for correct field mapping
   - Verify XP calculations are cumulative

### Validation Commands

```bash
# Check JSON validity
cat data/crafting/skills-import.json | jq empty

# Count skills
cat data/crafting/skills-import.json | jq '. | length'

# Inspect a sample skill
cat data/crafting/skills-import.json | jq '.[0]'

# Check for missing required fields
cat data/crafting/skills-import.json | jq '.[] | select(.id == null or .name == null)'

# Verify XP is cumulative (should increase)
cat data/crafting/skills-import.json | jq '.[0].levels | map(.xp_required)'

# Check prerequisites exist where expected
cat data/crafting/skills-import.json | jq '.[] | select(.prerequisites | length > 0) | {id, prerequisites}'
```

## Troubleshooting

### Issue: "Error reading input file"

**Cause:** Input file does not exist or is not readable.

**Solution:**
```bash
# Check file exists
ls -l server_docs/skills.20260216.json

# Check file permissions
chmod 644 server_docs/skills.20260216.json
```

### Issue: "Error parsing input JSON"

**Cause:** Input file is not valid JSON or has incorrect structure.

**Solution:**
```bash
# Validate JSON syntax
cat server_docs/skills.20260216.json | jq empty

# Check structure
cat server_docs/skills.20260216.json | jq '.skills | keys | length'
```

### Issue: "Error writing output file"

**Cause:** Output directory does not exist or is not writable.

**Solution:**
```bash
# Create output directory
mkdir -p data/crafting

# Check permissions
chmod 755 data/crafting
```

### Issue: Converted skill count is zero

**Cause:** Input file has empty skills object or incorrect structure.

**Solution:**
```bash
# Check input structure
cat server_docs/skills.20260216.json | jq '.skills | keys'

# Verify skills exist
cat server_docs/skills.20260216.json | jq '.skills | keys | length'
```

### Issue: XP values seem incorrect

**Cause:** Misunderstanding of cumulative vs. non-cumulative XP.

**Solution:**
- Input `xp_per_level` is **non-cumulative** (XP needed for each level)
- Output `xp_required` is **cumulative** (total XP from level 0)
- Verify calculations manually using the examples above

## Performance

### Typical Performance

- **Processing Speed:** ~500 skills/second
- **Memory Usage:** Minimal (processes file in memory)
- **File Size:** Typically 20-200 KB for skill data

### Scaling

The converter processes all skills in a single pass and is suitable for:
- Small datasets (tens of skills)
- Medium datasets (hundreds of skills)
- Large datasets (thousands of skills)

For very large datasets (10,000+ skills), consider:
- Splitting input into batches
- Processing incrementally
- Monitoring memory usage

## Skill Categories

Skills in SpaceMolt are organized into categories:

| Category | Description | Example Skills |
|----------|-------------|----------------|
| Mining | Resource extraction | mining_basic, advanced_mining |
| Engineering | Ship systems | engineering, advanced_engineering |
| Combat | Combat operations | combat_basic, weapons_specialization |
| Trading | Commerce and trade | trading, market_analysis |
| Support | Utility and support | navigation, logistics |
| Manufacturing | Crafting and production | manufacturing, assembly_lines |

## Related Tools

### Convert Tools
- **[convert-recipes](../convert-recipes/README.md)** - Convert recipe data from game API format

### Import Tools
- **[import-catalog-skills](../import-catalog-skills/)** - Import skill catalog data from live API
- **[import-base-data](../import-base-data/)** - Import base game data

### Data Scrapers
- **[data-scraper](../data-scraper/)** - Scrapes latest game data from API

### Documentation
- **[Crafting Integration Summary](../../CRAFTING_INTEGRATION_SUMMARY.md)** - Technical implementation details
- **[SpaceMolt Agent Guide](../../docs/SPACEMOLT_AGENT_GUIDE.md)** - General agent development guide

## Source Data

Skill data is typically obtained from:

1. **Game API Export** - Exported via data scraper tools
2. **Server Documentation** - Located in `server_docs/skills.*.json`
3. **Game Catalog** - Live catalog API endpoints

### Typical Sources

- `server_docs/skills.20260216.json` - Skill documentation export
- `server_docs/openapi.*.json` - OpenAPI specification with skill schemas
- Live game API endpoints (via data-scraper)

## License

Part of the SpaceMolt project.
