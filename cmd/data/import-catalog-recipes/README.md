# SpaceMolt Import Catalog Recipes

> Tool for importing crafting recipe data from SpaceMolt game API responses into the knowledge base.

## Overview

`import-catalog-recipes` is a command-line utility that imports crafting recipes from SpaceMolt game API responses. It captures recipe metadata including ingredients, outputs, skill requirements, crafting time, and quality modifiers, storing them in the SpaceMolt knowledge base for crafting agents.

## Features

### Core Functionality
- **Complete Recipe Catalog** - Imports all crafting recipes from the game
- **Ingredient Tracking** - Records input items and quantities required
- **Output Details** - Captures output items, quantities, and quality modifiers
- **Skill Requirements** - Stores required skills and levels for crafting
- **Quality System** - Supports base quality and skill-based quality modifiers

### Recipe Information
- Recipe ID, name, description, and category
- Crafting time in seconds
- Base quality and skill quality modifiers
- Multiple input ingredients per recipe
- Multiple output products per recipe
- Complex skill requirement mappings

## Quick Start

### Basic Usage

```bash
# Build the tool
go build -o bin/import-catalog-recipes ./cmd/import-catalog-recipes

# Import catalog recipes from a JSON file
./bin/import-catalog-recipes data/catalog-recipes.json

# Import with custom database path
SPACEMOLT_DB=/path/to/custom.db ./bin/import-catalog-recipes data/catalog-recipes.json
```

### Building from Source

```bash
# Build the binary
go build -o bin/import-catalog-recipes ./cmd/import-catalog-recipes

# Run with go run (for development)
go run ./cmd/import-catalog-recipes data/catalog-recipes.json
```

## Usage

### Command-Line Syntax

```bash
import-catalog-recipes <catalog-recipes.json>
```

### Arguments

| Argument | Type | Required | Description |
|----------|------|----------|-------------|
| `catalog-recipes.json` | string | Yes | Path to JSON file containing recipe catalog from the SpaceMolt API |

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `SPACEMOLT_DB` | Path to SQLite knowledge base database | `data/spacemolt-knowledge.db` |

## Input Format

The tool expects a JSON file matching the SpaceMolt `catalog_recipes` API response format:

```json
{
  "items": [
    {
      "id": "string",
      "name": "string",
      "description": "string",
      "category": "string",
      "crafting_time": 0,
      "base_quality": 0,
      "skill_quality_mod": 0,
      "required_skills": {
        "skill_id": 0
      },
      "inputs": [
        {
          "item_id": "string",
          "quantity": 0
        }
      ],
      "outputs": [
        {
          "item_id": "string",
          "quantity": 0,
          "quality_mod": false
        }
      ]
    }
  ]
}
```

### Field Descriptions

**Recipe Metadata:**
- `id` - Unique recipe identifier
- `name` - Recipe name
- `description` - Recipe description
- `category` - Recipe category (e.g., "smelting", "manufacturing", "assembly")
- `crafting_time` - Time to craft in seconds (integer)
- `base_quality` - Base quality level (integer)
- `skill_quality_mod` - Quality bonus from skills (integer)
- `required_skills` - Map of skill IDs to required levels

**Ingredients (inputs):**
- `item_id` - ID of required input item
- `quantity` - Quantity of input item required

**Products (outputs):**
- `item_id` - ID of output item
- `quantity` - Quantity of output item produced
- `quality_mod` - Whether quality affects this output (boolean)

## How It Works

### Import Process

1. **JSON Parsing** - Reads and validates the JSON input file
2. **Array Validation** - Ensures at least one recipe exists in the response
3. **Recipe Conversion** - Converts JSON recipes to `RecipeDef` objects
4. **Ingredient Mapping** - Converts input ingredients to `RecipeIngredient` objects
5. **Product Mapping** - Converts output products to `RecipeProduct` objects
6. **Database Storage** - Stores all recipes using `StoreRecipes()`

### Data Structures

The tool converts JSON to the following internal structures:

```go
type RecipeDef struct {
    ID              string
    Name            string
    Description     string
    Category        string
    CraftingTime    int
    BaseQuality     int
    SkillQualityMod int
    RequiredSkills  map[string]int
    Inputs          []RecipeIngredient
    Outputs         []RecipeProduct
    LastUpdatedTick int64
}

type RecipeIngredient struct {
    ItemID   string
    Quantity int
}

type RecipeProduct struct {
    ItemID     string
    Quantity   int
    QualityMod bool
}
```

### Error Handling

- Empty recipe arrays are fatal
- JSON parsing errors are fatal
- Database errors are fatal
- Missing or invalid fields in individual recipes don't halt execution (they're stored as-is)

## Output

### Success Output

```
✓ Successfully imported {count} recipes
```

### Error Output

```
Fatal: No recipes found in JSON file
Fatal: Failed to read file: {error}
Fatal: Failed to parse JSON: {error}
Fatal: Failed to store recipes: {error}
```

## Examples

### Example 1: Basic Import

```bash
# Import recipe catalog from a downloaded API response
./bin/import-catalog-recipes server_docs/catalog-recipes.20260221.json
```

**Output:**
```
✓ Successfully imported 89 recipes
```

### Example 2: Import with Custom Database

```bash
# Use a custom database path
SPACEMOLT_DB=/tmp/spacemolt.db ./bin/import-catalog-recipes data/catalog-recipes.json
```

### Example 3: Fetch and Import

```bash
# Fetch catalog from API and import directly
curl -s "https://api.spacemolt.com/v1/catalog_recipes" \
  | ./bin/import-catalog-recipes /dev/stdin
```

### Example 4: Verify Import

```bash
# After import, query specific recipes
sqlite3 data/spacemolt-knowledge.db <<EOF
SELECT name, category, crafting_time
FROM catalog_recipes
WHERE category='smelting'
LIMIT 10;
EOF
```

## Data Storage

### Database Schema

Imported data is stored in the following tables:

**catalog_recipes** - Recipe metadata
- `id` - Primary key (recipe ID)
- `name` - Recipe name
- `description` - Recipe description
- `category` - Recipe category
- `crafting_time` - Crafting time in seconds
- `base_quality` - Base quality level
- `skill_quality_mod` - Quality modifier from skills
- `last_updated_tick` - Game tick of last update

**recipe_required_skills** - Skill requirements
- `recipe_id` - Foreign key to recipe
- `skill_id` - Required skill ID
- `required_level` - Required skill level

**recipe_inputs** - Input ingredients
- `recipe_id` - Foreign key to recipe
- `item_id` - Input item ID
- `quantity` - Quantity required

**recipe_outputs** - Output products
- `recipe_id` - Foreign key to recipe
- `item_id` - Output item ID
- `quantity` - Quantity produced
- `quality_mod` - Whether quality affects output

### Data Retrieval

Query imported recipes using the knowledge base:

```go
// Get specific recipe
recipe, err := kb.GetRecipe(ctx, "basic_iron_smelting")

// Get all recipes
recipes, err := kb.GetRecipes(ctx)

// Get recipes by category
smeltingRecipes, err := kb.GetRecipesByCategory(ctx, "smelting")
```

## Integration

### Using with Agent Tools

This tool is typically used in conjunction with:

- **import-catalog-items** - Import items referenced in recipes
- **import-catalog-skills** - Import skills referenced in requirements
- **crafting-server** - MCP server for crafting decisions
- **auto-miner** - Autonomous mining with crafting integration

### Example Workflow

```bash
# 1. Import item catalog first (recipes reference items)
./bin/import-catalog-items server_docs/catalog-items.json

# 2. Import skill catalog (recipes reference skills)
./bin/import-catalog-skills server_docs/catalog-skills.json

# 3. Import recipe catalog
./bin/import-catalog-recipes server_docs/catalog-recipes.json

# 4. Verify import
sqlite3 data/spacemolt-knowledge.db <<EOF
SELECT
    r.name,
    COUNT(DISTINCT ri.item_id) as input_count,
    COUNT(DISTINCT ro.item_id) as output_count
FROM catalog_recipes r
LEFT JOIN recipe_inputs ri ON r.id = ri.recipe_id
LEFT JOIN recipe_outputs ro ON r.id = ro.recipe_id
GROUP BY r.id
LIMIT 10;
EOF
```

## Common Categories

Recipe categories typically include:

- **Smelting** - Raw ore to refined materials (iron_ore → refined_iron)
- **Manufacturing** - Components and parts (refined_steel → steel_frame)
- **Assembly** - Complex items (components → equipment)
- **Chemistry** - Chemical processes
- **Electronics** - Circuitry and devices
- **Construction** - Building materials

## Quality System

Recipes support a quality system:

- **Base Quality** - Default output quality level
- **Skill Quality Mod** - Additional quality from player skills
- **Quality Mod Per Output** - Whether each output is affected by quality

Higher quality items may have:
- Better stats (equipment)
- Higher value (trade goods)
- Improved durability

## Troubleshooting

### Issue: "No recipes found in JSON file"

**Cause:** The JSON structure doesn't match the expected format or the items array is empty.

**Solution:**
1. Verify the JSON file contains an `items` array
2. Validate JSON syntax using `jq . data/catalog-recipes.json`
3. Ensure the file is from the `catalog_recipes` API endpoint

### Issue: "Failed to store recipes: FOREIGN KEY constraint failed"

**Cause:** Recipe references items or skills that don't exist in the database.

**Solution:**
1. Import items first: `./bin/import-catalog-items items.json`
2. Import skills first: `./bin/import-catalog-skills skills.json`
3. Then import recipes

### Issue: Recipes not showing up in queries

**Cause:** Recipes were imported but item/skill references are missing.

**Solution:**
1. Verify all referenced items exist: `SELECT COUNT(*) FROM catalog_items;`
2. Verify all referenced skills exist: `SELECT COUNT(*) FROM catalog_skills;`
3. Check for missing IDs in the import data

### Issue: Crafting server can't find recipes

**Cause:** Crafting server is using a different database or the database path is wrong.

**Solution:**
1. Verify `SPACEMOLT_DB` environment variable
2. Check crafting server configuration
3. Ensure both tools use the same database path

## Related Tools

- **import-catalog-items** - Import items referenced in recipes
- **import-catalog-skills** - Import skills required by recipes
- **import-catalog-ships** - Import ship catalog
- **crafting-server** - MCP server for crafting decisions
- **auto-miner** - Autonomous mining agent with crafting support

## License

Part of the SpaceMolt project.
