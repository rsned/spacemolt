# SpaceMolt Import Map Data

> Tool for importing system and connection data from SpaceMolt game API responses into the knowledge base.

## Overview

`import-map-data` is a command-line utility that imports galactic map data from SpaceMolt game API responses. It captures system locations, connections between systems, empire affiliations, stronghold status, and other spatial information, storing them in the SpaceMolt knowledge base for navigation and exploration agents.

## Features

### Core Functionality
- **System Registration** - Imports all systems with names and positions
- **Connection Mapping** - Records travel connections between systems
- **Empire Tracking** - Records controlling empire for each system
- **Stronghold Detection** - Identifies stronghold systems
- **Partial Upsert** - Preserves richer data from exploration while updating basic info

### Smart Data Handling
- **Non-destructive Updates** - Uses partial upserts to preserve explorer data
- **Position Data** - Stores X,Y coordinates for distance calculations
- **Connection Validation** - Maintains system-to-system travel routes
- **Empire Mapping** - Tracks faction control of systems

### What Gets Preserved

The map import tool intentionally preserves richer data collected by exploration:

- **Police Level** - Not in map data, preserved from exploration
- **Descriptions** - Not in map data, preserved from exploration
- **POI Details** - Not in map data, preserved from exploration

## Quick Start

### Basic Usage

```bash
# Build the tool
go build -o bin/import-map-data ./cmd/import-map-data

# Import map data from a JSON file
./bin/import-map-data data/map-data.json

# Import with custom database path
SPACEMOLT_DB=/path/to/custom.db ./bin/import-map-data data/map-data.json
```

### Building from Source

```bash
# Build the binary
go build -o bin/import-map-data ./cmd/import-map-data

# Run with go run (for development)
go run ./cmd/import-map-data data/map-data.json
```

## Usage

### Command-Line Syntax

```bash
import-map-data <map-data.json>
```

### Arguments

| Argument | Type | Required | Description |
|----------|------|----------|-------------|
| `map-data.json` | string | Yes | Path to JSON file containing map data from the SpaceMolt API |

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `SPACEMOLT_DB` | Path to SQLite knowledge base database | `data/spacemolt-knowledge.db` |

## Input Format

The tool expects a JSON file matching the SpaceMolt `get_map` API response format:

```json
{
  "systems": [
    {
      "system_id": "string",
      "name": "string",
      "empire": "string",
      "position": {
        "x": 0.0,
        "y": 0.0
      },
      "connections": ["system_id_1", "system_id_2"],
      "poi_count": 0,
      "online": 0,
      "visited": false,
      "visited_at": "string",
      "is_stronghold": false
    }
  ]
}
```

### Field Descriptions

**System Information:**
- `system_id` - Unique system identifier
- `name` - System name
- `empire` - Controlling empire (optional, e.g., "Federation", "Empire")
- `position` - X,Y coordinates in galactic space
- `is_stronghold` - Whether this is a stronghold system (optional)

**Navigation:**
- `connections` - Array of system IDs this system connects to
- Used for route planning and pathfinding

**Status (not imported but present in API):**
- `poi_count` - Number of POIs in system
- `online` - Player count online
- `visited` - Whether player has visited
- `visited_at` - Timestamp of last visit

## How It Works

### Import Process

1. **JSON Parsing** - Reads and validates the JSON input file
2. **Array Validation** - Ensures at least one system exists in the response
3. **System Upsert** - Uses `UpsertSystemFromMap()` for each system:
   - Updates name, position, connections, empire, stronghold status
   - Preserves police_level, description, and other explorer data
4. **Connection Tracking** - Records all system-to-system connections
5. **Statistics** - Reports total systems and connections imported

### Partial Upsert Strategy

The tool uses a **partial upsert** approach:

**Updates from Map Data:**
- System name
- Position (X, Y)
- Connections to other systems
- Empire affiliation
- Stronghold status

**Preserves from Exploration:**
- Police level
- System description
- POI details
- Visit history
- Resource information

This ensures that rich exploration data isn't overwritten by basic map data.

### Data Structures

The tool converts JSON to the following internal structure:

```go
type MapSystemData struct {
    ID           string
    Name         string
    Empire       string
    PositionX    float64
    PositionY    float64
    IsStronghold bool
    Connections  []string
}
```

### Error Handling

- Empty system arrays are fatal
- JSON parsing errors are fatal
- Individual system import failures are logged but don't halt execution
- Database errors for individual systems are logged and skipped

## Output

### Success Output

```
✓ Successfully imported map data:
  Systems: {count}
  Connections: {count}
```

### Warning Output

```
Warning: failed to save system {system_id}: {error}
```

### Error Output

```
Fatal: No systems found in JSON file
Fatal: Failed to read file: {error}
Fatal: Failed to parse JSON: {error}
```

## Examples

### Example 1: Basic Import

```bash
# Import map data from a downloaded API response
./bin/import-map-data server_docs/map-data.20260221.json
```

**Output:**
```
✓ Successfully imported map data:
  Systems: 127
  Connections: 342
```

### Example 2: Import with Custom Database

```bash
# Use a custom database path
SPACEMOLT_DB=/tmp/spacemolt.db ./bin/import-map-data data/map-data.json
```

### Example 3: Fetch and Import

```bash
# Fetch map from API and import directly
curl -s "https://api.spacemolt.com/v1/get_map" \
  | ./bin/import-map-data /dev/stdin
```

### Example 4: Query Map Data

```bash
# After import, query systems by empire
sqlite3 data/spacemolt-knowledge.db <<EOF
SELECT name, empire, position_x, position_y
FROM systems
WHERE empire = 'Federation'
ORDER BY position_x, position_y
LIMIT 10;
EOF
```

## Data Storage

### Database Schema

Imported data is stored in the following tables:

**systems** - System metadata
- `id` - Primary key (system ID)
- `name` - System name
- `position_x` - X coordinate
- `position_y` - Y coordinate
- `empire` - Controlling empire (if any)
- `police_level` - Police presence (preserved from exploration)
- `description` - System description (preserved from exploration)
- `is_stronghold` - Stronghold status

**connections** - System connections
- `from_system` - Origin system ID
- `to_system` - Destination system ID
- Used for route planning and navigation

### Data Retrieval

Query imported systems using the knowledge base:

```go
// Get specific system
system, err := kb.GetSystem(ctx, "sol")

// Get all systems
systems := kb.GetSystems()

// Get unknown connections
unknown, err := kb.GetUnknownConnections(ctx, "sol")
```

## Integration

### Using with Agent Tools

This tool is typically used in conjunction with:

- **explorer** - Explores systems and enriches map data
- **import-base-data** - Imports bases within systems
- **route-planner** - Uses connections for pathfinding
- **trader** - Uses empire data for trade route planning

### Example Workflow

```bash
# 1. Import basic map data from API
./bin/import-map-data server_docs/map-data.json

# 2. Run explorer to enrich with POIs and descriptions
./bin/explorer explorer-1

# 3. Import base data for stations
./bin/import-base-data server_docs/bases.json

# 4. Query system with POIs
sqlite3 data/spacemolt-knowledge.db <<EOF
SELECT s.name, s.empire, COUNT(p.id) as poi_count
FROM systems s
LEFT JOIN pois p ON s.id = p.system_id
GROUP BY s.id
ORDER BY poi_count DESC
LIMIT 10;
EOF
```

## Map Data vs. Exploration Data

### Map Data Provides:
- System names
- Positions (X, Y coordinates)
- Connections between systems
- Empire affiliations (when available)
- Stronghold status (when available)

### Exploration Provides:
- Police levels
- System descriptions
- POI locations and types
- Resource information
- Visit history

### Combined Value:

The partial upsert ensures you get the best of both:
- Complete galaxy layout from map data
- Rich details from exploration
- No data loss from repeated imports

## Common Queries

### Find all systems in an empire:

```sql
SELECT name, empire, position_x, position_y
FROM systems
WHERE empire = 'Federation'
ORDER BY position_x, position_y;
```

### Find unvisited systems:

```sql
SELECT name, empire
FROM systems
WHERE id NOT IN (SELECT DISTINCT system_id FROM pois)
ORDER BY empire, name;
```

### Calculate distance between systems:

```sql
SELECT
    s1.name as system1,
    s2.name as system2,
    SQRT(POW(s2.position_x - s1.position_x, 2) +
         POW(s2.position_y - s1.position_y, 2)) as distance
FROM systems s1, systems s2
WHERE s1.name = 'Sol' AND s2.name = 'Alpha Centauri';
```

### Find connection path:

```sql
-- Find all connections from Sol
SELECT s2.name, s2.empire
FROM systems s1
JOIN connections c ON s1.id = c.from_system
JOIN systems s2 ON c.to_system = s2.id
WHERE s1.name = 'Sol';
```

## Troubleshooting

### Issue: "No systems found in JSON file"

**Cause:** The JSON structure doesn't match the expected format or the systems array is empty.

**Solution:**
1. Verify the JSON file contains a `systems` array
2. Validate JSON syntax using `jq . data/map-data.json`
3. Ensure the file is from the `get_map` API endpoint

### Issue: "Warning: failed to save system"

**Cause:** A specific system failed to import but others succeeded.

**Solution:**
1. Check the system ID format
2. Verify position coordinates are valid numbers
3. Check connection IDs reference valid systems
4. This is non-fatal - other systems will import

### Issue: Exploration data disappeared

**Cause:** Should not happen with partial upsert, but possible if database was reset.

**Solution:**
1. Verify you're using `UpsertSystemFromMap()` not a full replace
2. Check database backups for lost data
3. Re-run exploration agents to recollect data

### Issue: Missing connections

**Cause:** Connection IDs reference systems not yet imported.

**Solution:**
1. Import the full map data (all systems)
2. Connections are validated on import
3. Missing target systems are logged but don't cause failure

## Positional Data

### Coordinate System

Systems use a 2D coordinate system:
- X coordinate: Horizontal position
- Y coordinate: Vertical position
- Both are floating-point values
- Origin (0,0) is typically galactic center

### Distance Calculation

Calculate distance between two systems:

```sql
SELECT
    s1.name as from_system,
    s2.name as to_system,
    SQRT(POW(s2.position_x - s1.position_x, 2) +
         POW(s2.position_y - s1.position_y, 2)) as distance
FROM systems s1
JOIN systems s2 ON s2.name = 'Destination'
WHERE s1.name = 'Origin';
```

## Related Tools

- **explorer** - Enriches map data with POIs and details
- **import-base-data** - Imports bases within systems
- **route-planner** - Calculates optimal routes
- **trader** - Plans trade routes using map data

## License

Part of the SpaceMolt project.
