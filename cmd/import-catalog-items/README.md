# SpaceMolt Import Catalog Items

> Tool for importing item catalog data from SpaceMolt game API responses into the knowledge base.

## Overview

`import-catalog-items` is a command-line utility that imports the complete item catalog from SpaceMolt game API responses. It captures item metadata including names, descriptions, categories, rarity, size, base value, and trading properties, storing them in the SpaceMolt knowledge base for agent decision-making.

## Features

### Core Functionality
- **Complete Item Catalog** - Imports all items from the game catalog
- **Item Metadata** - Captures name, description, category, and rarity
- **Trading Properties** - Records base value, stackability, and tradeability
- **Size Information** - Stores item size for cargo calculations
- **Validation** - Skips items with empty IDs and logs warnings

### Data Quality
- Automatic validation of item IDs
- Warning messages for skipped items
- Support for all item categories (resources, components, equipment, etc.)
- Full rarity tier tracking

## Quick Start

### Basic Usage

```bash
# Build the tool
go build -o bin/import-catalog-items ./cmd/import-catalog-items

# Import catalog items from a JSON file
./bin/import-catalog-items data/catalog-items.json

# Import with custom database path
SPACEMOLT_DB=/path/to/custom.db ./bin/import-catalog-items data/catalog-items.json
```

### Building from Source

```bash
# Build the binary
go build -o bin/import-catalog-items ./cmd/import-catalog-items

# Run with go run (for development)
go run ./cmd/import-catalog-items data/catalog-items.json
```

## Usage

### Command-Line Syntax

```bash
import-catalog-items <catalog-items.json>
```

### Arguments

| Argument | Type | Required | Description |
|----------|------|----------|-------------|
| `catalog-items.json` | string | Yes | Path to JSON file containing item catalog from the SpaceMolt API |

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `SPACEMOLT_DB` | Path to SQLite knowledge base database | `data/spacemolt-knowledge.db` |

## Input Format

The tool expects a JSON file matching the SpaceMolt `catalog_items` API response format:

```json
{
  "items": [
    {
      "id": "string",
      "name": "string",
      "description": "string",
      "category": "string",
      "rarity": "string",
      "size": 0,
      "base_value": 0,
      "stackable": false,
      "tradeable": false
    }
  ]
}
```

### Field Descriptions

- `id` - Unique item identifier (required, items with empty IDs are skipped)
- `name` - Item display name
- `description` - Item description
- `category` - Item category (e.g., "resource", "component", "equipment")
- `rarity` - Rarity tier (e.g., "common", "uncommon", "rare", "epic", "legendary")
- `size` - Cargo size units (integer)
- `base_value` - Base price in credits (integer)
- `stackable` - Whether multiple units can stack in one cargo slot (boolean)
- `tradeable` - Whether item can be traded on markets (boolean)

## How It Works

### Import Process

1. **JSON Parsing** - Reads and validates the JSON input file
2. **Array Validation** - Ensures at least one item exists in the response
3. **Item Conversion** - Converts JSON items to `CatalogItem` objects
4. **ID Validation** - Skips items with empty IDs and logs warnings
5. **Database Storage** - Stores all valid items using `StoreItems()`

### Data Validation

The tool performs the following validation:

- **Empty ID Check** - Items with empty `id` fields are skipped
- **Array Length Check** - Fatal error if no items found in JSON
- **JSON Syntax** - Fatal error if JSON is malformed

### Error Handling

- Items with empty IDs are skipped with a warning
- JSON parsing errors are fatal
- Empty item arrays are fatal
- Database errors are fatal

## Output

### Success Output

```
✓ Successfully imported {count} items
```

With warnings for skipped items:
```
Warning: skipped {count} items with empty IDs
✓ Successfully imported {count} items
```

### Error Output

```
Fatal: No items found in JSON file
Fatal: Failed to read file: {error}
Fatal: Failed to parse JSON: {error}
Fatal: Failed to store items: {error}
```

## Examples

### Example 1: Basic Import

```bash
# Import item catalog from a downloaded API response
./bin/import-catalog-items server_docs/catalog-items.20260221.json
```

**Output:**
```
✓ Successfully imported 247 items
```

### Example 2: Import with Skipped Items

```bash
# Some items may have empty IDs and be skipped
./bin/import-catalog-items data/incomplete-catalog.json
```

**Output:**
```
Warning: skipped 3 items with empty IDs
✓ Successfully imported 244 items
```

### Example 3: Import with Custom Database

```bash
# Use a custom database path
SPACEMOLT_DB=/tmp/spacemolt.db ./bin/import-catalog-items data/catalog-items.json
```

### Example 4: Fetch and Import

```bash
# Fetch catalog from API and import directly
curl -s "https://api.spacemolt.com/v1/catalog_items" \
  | ./bin/import-catalog-items /dev/stdin
```

## Data Storage

### Database Schema

Imported data is stored in the `catalog_items` table:

- `id` - Primary key (item ID)
- `name` - Item name
- `description` - Item description
- `category` - Item category
- `rarity` - Rarity tier
- `size` - Cargo size
- `base_value` - Base price
- `stackable` - Can stack in cargo
- `tradeable` - Can be traded

### Data Retrieval

Query imported items using the knowledge base:

```go
// Get specific item
item, err := kb.GetItem(ctx, "iron_ore")

// Get all items
items, err := kb.GetItems(ctx)

// Get items by category
resources, err := kb.GetItemsByCategory(ctx, "resource")
```

## Integration

### Using with Agent Tools

This tool is typically used in conjunction with:

- **import-base-data** - Import base data with market listings
- **import-catalog-recipes** - Import recipes that reference these items
- **import-catalog-ships** - Import ships that reference these items

### Example Workflow

```bash
# 1. Download catalog from API
curl -s "https://api.spacemolt.com/v1/catalog_items" > data/catalog-items.json

# 2. Import into knowledge base
./bin/import-catalog-items data/catalog-items.json

# 3. Verify import
sqlite3 data/spacemolt-knowledge.db "SELECT COUNT(*) FROM catalog_items;"

# 4. Query specific category
sqlite3 data/spacemolt-knowledge.db "SELECT name, base_value FROM catalog_items WHERE category='resource' LIMIT 10;"
```

## Common Categories

The item catalog typically includes:

- **Resources** - Raw materials (iron_ore, copper_ore, crystal_shard, etc.)
- **Components** - Crafted components (refined_steel, circuit_board, etc.)
- **Equipment** - Ship modules (mining_laser_mk1, shield_generator, etc.)
- **Ammunition** - Weapons ammo (missile_light, plasma_cell, etc.)
- **Consumables** - One-use items (repair_kit, fuel_cell, etc.)
- **Trade Goods** - Items for trading (luxury_goods, medicine, etc.)

## Troubleshooting

### Issue: "No items found in JSON file"

**Cause:** The JSON structure doesn't match the expected format or the items array is empty.

**Solution:**
1. Verify the JSON file contains an `items` array
2. Validate JSON syntax using `jq . data/catalog-items.json`
3. Ensure the file is from the `catalog_items` API endpoint

### Issue: "Warning: skipped N items with empty IDs"

**Cause:** Some items in the catalog have empty or missing ID fields.

**Solution:**
1. This is a warning, not an error - the import continues
2. Check the source data for completeness
3. Report missing IDs to the game API maintainers if persistent

### Issue: "Failed to store items: UNIQUE constraint failed"

**Cause:** Items with the same ID already exist in the database.

**Solution:**
1. This may indicate attempting to import the same catalog twice
2. Clear the catalog_items table before reimporting: `DELETE FROM catalog_items;`
3. Or use a fresh database

### Issue: Import is slow

**Cause:** Large catalogs with thousands of items may take time to process.

**Solution:**
1. This is normal for complete catalogs
2. Performance depends on disk I/O speed
3. Consider using an SSD for the database file

## Related Tools

- **import-catalog-recipes** - Import crafting recipes using these items
- **import-catalog-ships** - Import ship catalog using these items
- **import-base-data** - Import market data referencing these items
- **data-scraper** - Download game data from SpaceMolt API

## License

Part of the SpaceMolt project.
