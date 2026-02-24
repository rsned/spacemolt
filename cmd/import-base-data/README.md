# SpaceMolt Import Base Data

> Tool for importing space station/base data from SpaceMolt game API responses into the knowledge base.

## Overview

`import-base-data` is a command-line utility that imports detailed information about space stations and bases from SpaceMolt game API responses. It extracts base metadata, facilities, market data, and services, storing them in the SpaceMolt knowledge base for agent use.

## Features

### Core Functionality
- **Base Information** - Imports base ID, name, description, empire, and defense level
- **Facility Tracking** - Records available facilities with category and level information
- **Market Data** - Imports market listings including prices, quantities, and NPC status
- **Service Mapping** - Tracks available services at each base
- **POI Integration** - Links bases to their parent Points of Interest (POIs)

### Data Processing
- Automatic facility category mapping using predefined facility types
- Graceful handling of unknown facilities
- Market item validation and error recovery
- Support for both public and private bases

## Quick Start

### Basic Usage

```bash
# Build the tool
go build -o bin/import-base-data ./cmd/import-base-data

# Import base data from a JSON file
./bin/import-base-data data/base-data.json

# Import with custom database path
SPACEMOLT_DB=/path/to/custom.db ./bin/import-base-data data/base-data.json
```

### Building from Source

```bash
# Build the binary
go build -o bin/import-base-data ./cmd/import-base-data

# Run with go run (for development)
go run ./cmd/import-base-data data/base-data.json
```

## Usage

### Command-Line Syntax

```bash
import-base-data <base-data.json>
```

### Arguments

| Argument | Type | Required | Description |
|----------|------|----------|-------------|
| `base-data.json` | string | Yes | Path to JSON file containing base data from the SpaceMolt API |

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `SPACEMOLT_DB` | Path to SQLite knowledge base database | `data/spacemolt-knowledge.db` |

## Input Format

The tool expects a JSON file matching the SpaceMolt `get_base` API response format:

```json
{
  "base": {
    "id": "string",
    "poi_id": "string",
    "name": "string",
    "description": "string",
    "empire": "string",
    "defense_level": 0,
    "has_drones": false,
    "public_access": false,
    "services": {
      "service_name": true
    },
    "facilities": ["facility_id_1", "facility_id_2"],
    "market": [
      {
        "id": "string",
        "item_id": "string",
        "price_each": 0.0,
        "quantity": 0,
        "is_npc": false
      }
    ]
  },
  "poi": {
    "id": "string",
    "name": "string",
    "system_id": "string",
    "description": "string",
    "position": {
      "x": 0.0,
      "y": 0.0
    },
    "type": "string"
  }
}
```

### Field Descriptions

**Base Fields:**
- `id` - Unique identifier for the base
- `poi_id` - ID of the POI containing this base
- `name` - Base name
- `description` - Base description
- `empire` - Controlling empire (e.g., "Federation", "Empire")
- `defense_level` - Military defense strength
- `has_drones` - Whether drones are present
- `public_access` - Whether base is open to public
- `services` - Map of available services
- `facilities` - Array of facility IDs
- `market` - Array of market listings

**POI Fields:**
- `id` - POI identifier
- `name` - POI name
- `system_id` - Parent system ID
- `description` - POI description
- `position` - X,Y coordinates in system
- `type` - POI type (e.g., "station", "asteroid_field")

## How It Works

### Import Process

1. **JSON Parsing** - Reads and validates the JSON input file
2. **Market Processing** - Parses market items with error recovery for malformed entries
3. **Facility Mapping** - Maps facility IDs to categories and levels using predefined mappings
4. **Base Construction** - Creates a `SpaceBase` object with all collected data
5. **Database Storage** - Stores the base in the knowledge base using `RememberBase()`

### Facility Category Mapping

The tool automatically maps facility IDs to categories and levels using `knowledge.FacilityCategoryMapping`. Unknown facilities are stored with category "unknown" and level 0.

### Error Handling

- Malformed market items are logged and skipped
- Unknown facilities are preserved with minimal metadata
- JSON parsing errors are fatal and halt execution
- Database errors are fatal and halt execution

## Output

### Success Output

```
✓ Successfully imported base: {name} (ID: {id})
  POI: {poi_id}
  Empire: {empire}
  Facilities: {count}
  Market items: {count}
  Services: {count}
```

### Error Output

```
Warning: failed to parse market item: {error}
Fatal: Failed to read file: {error}
Fatal: Failed to parse JSON: {error}
Fatal: Failed to save base: {error}
```

## Examples

### Example 1: Basic Import

```bash
# Import base data from a downloaded API response
./bin/import-base-data server_docs/base-unity-station.json
```

**Output:**
```
✓ Successfully imported base: Unity Station (ID: base_unity_station)
  POI: poi_unity_station
  Empire: Federation
  Facilities: 8
  Market items: 24
  Services: 6
```

### Example 2: Import with Custom Database

```bash
# Use a custom database path
SPACEMOLT_DB=/tmp/spacemolt.db ./bin/import-base-data data/base-data.json
```

### Example 3: Fetch and Import

```bash
# Fetch base data from API and import
curl -s "https://api.spacemolt.com/v1/get_base?base_id=base_unity_station" \
  | ./bin/import-base-data /dev/stdin
```

## Data Storage

### Database Schema

Imported data is stored in the following tables:

- **bases** - Base metadata (ID, name, empire, etc.)
- **facilities** - Facility records with category and level
- **base_facilities** - Junction table linking bases to facilities
- **base_market** - Market listings with prices and quantities
- **base_services** - Available services at each base

### Data Retrieval

Query imported bases using the knowledge base:

```go
// Get base by ID
base, err := kb.GetBase(ctx, "base_unity_station")

// Get base by POI ID
base, err := kb.GetBaseByPOI(ctx, "poi_unity_station")
```

## Integration

### Using with Agent Tools

This tool is typically used in conjunction with:

- **data-scraper** - Download base data from the SpaceMolt API
- **import-map-data** - Import system and POI location data
- **import-catalog-items** - Import item catalog for market item validation

### Example Workflow

```bash
# 1. Download base data from API
./bin/data-scraper --bases > data/bases.json

# 2. Import into knowledge base
./bin/import-base-data data/bases.json

# 3. Verify import
sqlite3 data/spacemolt-knowledge.db "SELECT name, empire FROM bases LIMIT 10;"
```

## Troubleshooting

### Issue: "No base found in JSON file"

**Cause:** The JSON structure doesn't match the expected format or the file is empty.

**Solution:**
1. Verify the JSON file contains `base` and `poi` objects
2. Validate JSON syntax using `jq . data/base-data.json`
3. Ensure the file is from the `get_base` API endpoint

### Issue: "Warning: failed to parse market item"

**Cause:** A market item has missing or invalid fields.

**Solution:**
1. Check the market item has required fields: `id`, `item_id`, `price_each`, `quantity`, `is_npc`
2. Verify the market array contains valid JSON objects
3. The import will continue, skipping invalid items

### Issue: "Failed to save base: database is locked"

**Cause:** Another process is writing to the database.

**Solution:**
1. Stop other agents or tools using the database
2. Ensure only one import tool runs at a time
3. Check for zombie processes: `lsof data/spacemolt-knowledge.db`

## Related Tools

- **import-catalog-items** - Import item catalog for market validation
- **import-catalog-ships** - Import ship catalog for shipyard data
- **import-map-data** - Import system and POI location data
- **data-scraper** - Download game data from SpaceMolt API

## License

Part of the SpaceMolt project.
