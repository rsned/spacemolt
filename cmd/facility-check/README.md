# SpaceMolt Facility Check

> Diagnostic tool for querying facility information from the SpaceMolt game server.

## Overview

The facility-check tool is a diagnostic utility used to query facility types and details from the SpaceMolt game server. It connects as an agent and sends facility query requests to retrieve information about available facility types (e.g., personal_workbench).

## Features

### Core Functionality
- **🔍 Facility Queries** - Query facility type information from game server
- **📊 Response Inspection** - Display raw facility data for debugging
- **🔧 Diagnostic Tool** - Useful for testing and development

## Quick Start

### Basic Usage

```bash
# Run facility check (hardcoded to pirate-4 and personal_workbench)
go run ./cmd/facility-check

# Build and run
go build -o bin/facility-check ./cmd/facility-check
./bin/facility-check
```

## How It Works

### Process Flow

```
1. Load Credentials (pirate-4)
   ↓
2. Connect to Game Server
   ↓
3. Login
   ↓
4. Send Facility Query (personal_workbench)
   ↓
5. Display Response
```

### What It Queries

The tool sends a facility query request:

```json
{
  "type": "facility",
  "payload": {
    "action": "types",
    "facility_type": "personal_workbench"
  },
  "timestamp": 1234567890
}
```

## Examples

### Example 1: Successful Query

```bash
go run ./cmd/facility-check
```

**Output:**
```
[pirate-4] Connected!
[GAME] Response:
{
  "action": "types",
  "facility_type": "personal_workbench",
  "types": [
    {
      "type_id": "basic_smelting",
      "name": "Basic Smelting",
      "description": "Smelt raw ores into refined metals",
      "requirements": {
        "skills": {
          "mining": 3
        },
        "items": {
          "iron_ore": 10
        }
      }
    }
  ],
  "timestamp": 1737657890123
}
```

### Example 2: No Results

```bash
go run ./cmd/facility-check
```

**Output:**
```
[pirate-4] Connected!
[GAME] Response:
{
  "action": "types",
  "facility_type": "personal_workbench",
  "types": [],
  "timestamp": 1737657890123
}
```

## Configuration

### Hardcoded Settings

The tool currently has hardcoded values:
- **Agent:** pirate-4
- **Facility Type:** personal_workbench
- **Action:** types

### Credentials Location

```
data/agents/pirate-4/credentials.json
```

### Credentials Format

```json
{
  "username": "pirate-4-username",
  "password": "pirate-4-password"
}
```

## Usage Notes

### Development Tool

This is primarily a development/diagnostic tool for:
- Testing facility query functionality
- Inspecting facility type data
- Debugging facility-related features
- Understanding facility API responses

### Customization

To query different agents or facility types, modify the hardcoded values in `main.go`:
- Agent directory (line 53)
- Facility type (line 77)

## Limitations

### Current Limitations

- **Hardcoded Agent** - Only works with pirate-4
- **Hardcoded Facility** - Only queries personal_workbench
- **No CLI Arguments** - All parameters are hardcoded
- **Single Query** - Runs one query then exits

### Future Enhancements

Potential improvements:
- Add CLI arguments for agent selection
- Add CLI arguments for facility type
- Support multiple facility types
- Add output formatting options
- Add query result filtering

## Troubleshooting

### Issue: "Failed to load credentials"

**Cause:** pirate-4 credentials not found.

**Solution:**
1. Verify pirate-4 directory exists
2. Check credentials.json file exists
3. Ensure file is readable

### Issue: "Failed to connect"

**Cause:** Network issue or server unavailable.

**Solution:**
1. Check network connectivity
2. Verify game server is accessible
3. Check firewall settings

### Issue: "No response received"

**Cause:** Query timeout or server not responding.

**Solution:**
1. Increase wait timeout (line 86)
2. Check server logs
3. Verify query format is correct

## Related Tools

- [play-simple](../play-simple/) - Interactive agent control
- [claim-code](../claim-code/) - Registration code claiming
- [Game Client](../../pkg/game/) - Game client API reference

## License

Part of the SpaceMolt project.
