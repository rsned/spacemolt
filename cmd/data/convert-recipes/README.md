# SpaceMolt Recipe Converter

> Data transformation tool that converts SpaceMolt recipe JSON from game API format to the crafting server import format.

## Overview

The `convert-recipes` tool transforms raw recipe data exported from the SpaceMolt game API into a normalized format suitable for import into the SpaceMolt crafting server database. It handles structural transformation, field renaming, and data type conversion to ensure compatibility with the crafting system.

## Features

### Core Functionality
- **Format Transformation** - Converts game API recipe format to crafting server import format
- **Field Mapping** - Maps API fields to database schema (e.g., `crafting_time` to `craft_time_sec`)
- **Component Extraction** - Preserves input materials and their quantities
- **Skill Requirements** - Converts required skills to standardized skill requirement format
- **Output Simplification** - Extracts primary output from multi-output recipes
- **Bulk Processing** - Handles entire recipe collections in a single operation

### Data Transformation

The converter performs the following transformations:

1. **Structure Changes**
   - Converts from map-based (`{"recipes": {...}}`) to array-based format (`[{...}, {...}]`)
   - Flattens nested recipe objects

2. **Field Renaming**
   - `crafting_time` → `craft_time_sec`
   - `inputs` → `components`
   - `required_skills` map → `skills` array

3. **Data Restructuring**
   - `required_skills`: Converts from `{"skill_id": level}` to `[{skill_id, level_required}]`
   - `inputs`: Preserves component structure as `components` array
   - `outputs`: Extracts first output as primary output

## Quick Start

### Basic Usage

```bash
# Convert recipes from game API format
go run ./cmd/convert-recipes server_docs/recipes.20260216.json data/crafting/recipes-import.json

# Using the built binary
./bin/convert-recipes server_docs/recipes.20260216.json data/crafting/recipes-import.json
```

### Building

```bash
# Build the converter binary
go build -o bin/convert-recipes ./cmd/convert-recipes

# Run the built binary
./bin/convert-recipes input.json output.json
```

## Usage

### Command-Line Syntax

```bash
convert-recipes <input.json> <output.json>
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
  "recipes": {
    "basic_copper_processing": {
      "id": "basic_copper_processing",
      "name": "Basic Copper Processing",
      "description": "Simple copper wire drawing using basic tools.",
      "category": "Refining",
      "inputs": [
        {
          "item_id": "ore_copper",
          "quantity": 8
        }
      ],
      "outputs": [
        {
          "item_id": "refined_copper_wire",
          "quantity": 1,
          "quality_mod": true
        }
      ],
      "required_skills": {},
      "crafting_time": 3,
      "base_quality": 30,
      "skill_quality_mod": 3
    }
  }
}
```

### Input Field Reference

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `id` | string | Yes | Unique recipe identifier |
| `name` | string | Yes | Human-readable recipe name |
| `description` | string | Yes | Recipe description |
| `category` | string | Yes | Recipe category (e.g., "Refining", "Manufacturing") |
| `inputs` | array | Yes | Array of input components |
| `outputs` | array | Yes | Array of output items |
| `required_skills` | object | No | Map of skill_id to required level |
| `crafting_time` | integer | Yes | Time to craft in seconds |
| `base_quality` | integer | No | Base quality of crafted items |
| `skill_quality_mod` | integer | No | Quality modifier per skill level |

## Output Format

The converter produces output in the crafting server import format:

```json
[
  {
    "id": "basic_copper_processing",
    "name": "Basic Copper Processing",
    "description": "Simple copper wire drawing using basic tools.",
    "category": "Refining",
    "craft_time_sec": 3,
    "components": [
      {
        "item_id": "ore_copper",
        "quantity": 8
      }
    ],
    "skills": [],
    "output": {
      "item_id": "refined_copper_wire",
      "quantity": 1
    }
  }
]
```

### Output Field Reference

| Field | Type | Description |
|-------|------|-------------|
| `id` | string | Unique recipe identifier |
| `name` | string | Human-readable recipe name |
| `description` | string | Recipe description |
| `category` | string | Recipe category |
| `craft_time_sec` | integer | Crafting time in seconds |
| `components` | array | Input materials required |
| `skills` | array | Skill requirements |
| `output` | object | Primary crafted item |

### Component Schema

```json
{
  "item_id": "ore_copper",
  "quantity": 8
}
```

### Skill Requirement Schema

```json
{
  "skill_id": "mining_basic",
  "level_required": 3
}
```

### Output Schema

```json
{
  "item_id": "refined_copper_wire",
  "quantity": 1
}
```

## How It Works

### Conversion Process

1. **Parse Input**
   - Read input JSON file
   - Parse SpaceMolt recipe format
   - Validate structure

2. **Transform Data**
   - Iterate through all recipes
   - Map fields to output schema
   - Convert data structures (maps to arrays)
   - Extract primary output from outputs array
   - Build components array from inputs
   - Convert skill requirements to array format

3. **Generate Output**
   - Marshal to indented JSON
   - Write to output file
   - Report conversion statistics

### Transformation Logic

```go
For each recipe in input:
  1. Extract basic fields (id, name, description, category)
  2. Map crafting_time → craft_time_sec
  3. Convert inputs array to components array (preserves structure)
  4. Convert required_skills map to skills array:
     - For each {skill_id: level} entry
     - Create {skill_id, level_required} object
  5. Extract first output as primary output:
     - Use outputs[0].item_id and outputs[0].quantity
     - Ignore quality_mod field (not used in crafting server)
  6. Create ImportRecipe object with all transformed data
```

### Quality Modifier Handling

The converter **ignores** the `quality_mod` field from outputs and the quality-related fields (`base_quality`, `skill_quality_mod`) from recipes. These fields are not used by the crafting server and are excluded from the import format.

## Examples

### Example 1: Basic Conversion

```bash
# Convert the latest recipe export
go run ./cmd/convert-recipes server_docs/recipes.20260216.json data/crafting/recipes-import.json
```

**Output:**
```
Converted 42 recipes from server_docs/recipes.20260216.json to data/crafting/recipes-import.json
```

### Example 2: Full Crafting Server Setup

```bash
# Create data directory
mkdir -p data/crafting

# Convert recipes
go run ./cmd/convert-recipes server_docs/recipes.20260216.json data/crafting/recipes-import.json

# Convert skills
go run ./cmd/convert-skills server_docs/skills.20260216.json data/crafting/skills-import.json

# Import into crafting server (requires crafting-server to be built)
./bin/crafting-server -import-recipes data/crafting/recipes-import.json
./bin/crafting-server -import-skills data/crafting/skills-import.json
```

### Example 3: Inspecting Converted Output

```bash
# Convert and inspect the output
go run ./cmd/convert-recipes server_docs/recipes.20260216.json /tmp/recipes-converted.json
cat /tmp/recipes-converted.json | jq '.[] | select(.id == "basic_copper_processing")'
```

**Output:**
```json
{
  "id": "basic_copper_processing",
  "name": "Basic Copper Processing",
  "description": "Simple copper wire drawing using basic tools.",
  "category": "Refining",
  "craft_time_sec": 3,
  "components": [
    {
      "item_id": "ore_copper",
      "quantity": 8
    }
  ],
  "skills": [],
  "output": {
    "item_id": "refined_copper_wire",
    "quantity": 1
  }
}
```

## Typical Workflow

### Updating Recipe Data

When new recipe data is available from the game API:

```bash
# 1. Export/update recipe data from game API
# (This is typically done by the data-scraper tool)

# 2. Convert to import format
go run ./cmd/convert-recipes server_docs/recipes.20260216.json data/crafting/recipes-import.json

# 3. Import into crafting server
./bin/crafting-server -import-recipes data/crafting/recipes-import.json

# 4. Verify import
./bin/crafting-server -list-recipes | head -20
```

### Initial Setup

```bash
# Build all conversion tools
go build -o bin/convert-recipes ./cmd/convert-recipes
go build -o bin/convert-skills ./cmd/convert-skills
go build -o bin/crafting-server ./cmd/crafting-server

# Create data directory
mkdir -p data/crafting

# Convert and import recipe data
./bin/convert-recipes server_docs/recipes.20260216.json data/crafting/recipes-import.json
./bin/crafting-server -import-recipes data/crafting/recipes-import.json

# Convert and import skill data
./bin/convert-skills server_docs/skills.20260216.json data/crafting/skills-import.json
./bin/crafting-server -import-skills data/crafting/skills-import.json
```

## Data Validation

The converter does not perform extensive validation beyond basic JSON parsing. Ensure:

1. **Input File Validity**
   - Input file must be valid JSON
   - Must have a top-level "recipes" object
   - Each recipe must have required fields

2. **Output Verification**
   - Check output file exists and is valid JSON
   - Verify recipe count matches expected number
   - Inspect sample recipes for correct field mapping

### Validation Commands

```bash
# Check JSON validity
cat data/crafting/recipes-import.json | jq empty

# Count recipes
cat data/crafting/recipes-import.json | jq '. | length'

# Inspect a sample recipe
cat data/crafting/recipes-import.json | jq '.[0]'

# Check for missing required fields
cat data/crafting/recipes-import.json | jq '.[] | select(.id == null or .name == null)'
```

## Troubleshooting

### Issue: "Error reading input file"

**Cause:** Input file does not exist or is not readable.

**Solution:**
```bash
# Check file exists
ls -l server_docs/recipes.20260216.json

# Check file permissions
chmod 644 server_docs/recipes.20260216.json
```

### Issue: "Error parsing input JSON"

**Cause:** Input file is not valid JSON or has incorrect structure.

**Solution:**
```bash
# Validate JSON syntax
cat server_docs/recipes.20260216.json | jq empty

# Check structure
cat server_docs/recipes.20260216.json | jq '.recipes | keys | length'
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

### Issue: Converted recipe count is zero

**Cause:** Input file has empty recipes object or incorrect structure.

**Solution:**
```bash
# Check input structure
cat server_docs/recipes.20260216.json | jq '.recipes | keys'

# Verify recipes exist
cat server_docs/recipes.20260216.json | jq '.recipes | keys | length'
```

## Performance

### Typical Performance

- **Processing Speed:** ~1000 recipes/second
- **Memory Usage:** Minimal (processes file in memory)
- **File Size:** Typically 50-500 KB for recipe data

### Scaling

The converter processes all recipes in a single pass and is suitable for:
- Small datasets (tens of recipes)
- Medium datasets (hundreds of recipes)
- Large datasets (thousands of recipes)

For very large datasets (10,000+ recipes), consider:
- Splitting input into batches
- Processing incrementally
- Monitoring memory usage

## Related Tools

### Convert Tools
- **[convert-skills](../convert-skills/README.md)** - Convert skill data from game API format

### Import Tools
- **[import-catalog-recipes](../import-catalog-recipes/)** - Import recipe catalog data from live API
- **[import-base-data](../import-base-data/)** - Import base game data

### Data Scrapers
- **[data-scraper](../data-scraper/)** - Scrapes latest game data from API

### Documentation
- **[Crafting Integration Summary](../../CRAFTING_INTEGRATION_SUMMARY.md)** - Technical implementation details
- **[SpaceMolt Agent Guide](../../docs/SPACEMOLT_AGENT_GUIDE.md)** - General agent development guide

## Source Data

Recipe data is typically obtained from:

1. **Game API Export** - Exported via data scraper tools
2. **Server Documentation** - Located in `server_docs/recipes.*.json`
3. **Game Catalog** - Live catalog API endpoints

### Typical Sources

- `server_docs/recipes.20260216.json` - Recipe documentation export
- `server_docs/openapi.*.json` - OpenAPI specification with recipe schemas
- Live game API endpoints (via data-scraper)

## License

Part of the SpaceMolt project.
