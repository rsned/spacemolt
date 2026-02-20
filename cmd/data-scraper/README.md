# Data API Scraper

A WebSocket-based scraper for the SpaceMolt game API.

## Overview

This tool connects to the SpaceMolt game server via **WebSocket** (the same protocol used by all auto-agents), authenticates, and scrapes all available game state data. This is useful for:

- **API Documentation**: Understanding the complete API surface
- **Response Examples**: Seeing real response structures
- **Testing**: Verifying server endpoints are working
- **Development**: Having reference data for agent development
- **Data Analysis**: Exporting game data for analysis

## Features

- ✅ WebSocket connection (persistent session)
- ✅ No rate limiting (single connection)
- ✅ Scrapes 15+ different data types
- ✅ Saves all responses as formatted JSON
- ✅ Uses production `game.Client` library
- ✅ Fast and efficient

## Prerequisites

- Go 1.24 or later
- Valid SpaceMolt account credentials
- Internet connection to `wss://game.spacemolt.com/ws`

## Configuration

The scraper uses credentials from `data/agents/random-2/credentials.json`:

```json
{
  "username": "your-username",
  "password": "your-password",
  "empire": "solarian"
}
```

## Usage

Run the scraper from the project root:

```bash
# Scrape all endpoints for an agent
go run cmd/data-scraper/main.go <agent-id>

# Scrape a specific endpoint
go run cmd/data-scraper/main.go <agent-id> <endpoint>
```

### Arguments

- `agent-id` - Agent identifier (e.g., craftsman-1, explorer-1)
- `endpoint` - Optional: Specific endpoint to scrape (see list below)

### Available Endpoints

| Endpoint | Description |
|----------|-------------|
| `status` | Status & Player Info |
| `ship` | Ship Info |
| `poi` | Current POI |
| `system` | System Data |
| `map` | Map Data |
| `listings` | Market Listings |
| `ships` | Shipyard Showroom |
| `ship_catalog` | Ship Catalog (all ship types) |
| `nearby` | Nearby Players |
| `skills` | Player Skills (your levels) |
| `skill_defs` | Skill Definitions (catalog) |
| `recipes` | Recipe Definitions (catalog) |
| `items` | Item Definitions (catalog) |
| `wrecks` | Wrecks |
| `drones` | Drones |
| `base` | Base Info |
| `faction` | Faction Info |
| `log` | Captain's Log |

### Examples

```bash
# Scrape all data for craftsman-1
go run cmd/data-scraper/main.go craftsman-1

# Scrape only status data
go run cmd/data-scraper/main.go craftsman-1 status

# Scrape only map data
go run cmd/data-scraper/main.go craftsman-1 map

# Scrape market listings
go run cmd/data-scraper/main.go craftsman-1 listings
```

### What It Does

When scraping all endpoints:
1. **Connects** to game server via WebSocket
2. **Logs in** using credentials
3. **Scrapes data** from multiple endpoints:
   - Status & Player Info
   - Ship Info
   - Current POI
   - System Data
   - Map Data
   - Market Listings
   - Shipyard Showroom (ships available for purchase)
   - Nearby Players
   - Player Skills (your current levels and XP)
   - Skill Definitions (all skills in game via catalog)
   - Recipe Definitions (all recipes in game via catalog)
   - Ship Catalog (all ship types via catalog)
   - Item Definitions (all items via catalog)
   - Wrecks
   - Drones
   - Base Info
   - Faction Info
   - Captain's Log
4. **Saves all responses** to `data/game-api/<agent-id>/*.json`

When scraping a single endpoint:
1. **Connects** to game server via WebSocket
2. **Logs in** using credentials
3. **Scrapes** only the specified endpoint
4. **Saves response** to `data/game-api/<agent-id>/<filename>.json`

### Output

All API responses are saved to `data/game-api/<agent-id>/` with descriptive filenames:

```
data/game-api/
├── craftsman-1/
│   ├── get_status.json
│   ├── get_ship.json
│   ├── get_poi.json
│   ├── get_system.json
│   ├── get_map.json
│   ├── get_listings.json
│   ├── get_ships.json (shipyard showroom)
│   ├── get_nearby.json
│   ├── get_skills.json (your player skills)
│   ├── catalog_skills.json (all skill definitions)
│   ├── catalog_recipes.json (all recipe definitions)
│   ├── catalog_ships.json (all ship types)
│   └── catalog_items.json (all items)
│   ├── get_wrecks.json
│   ├── get_drones.json
│   ├── get_base.json
│   ├── faction_info.json (if in faction)
│   └── captains_log_list.json
└── craftsman-3/
    └── ...
```

## Data Retrieved

### Status & Player Info (`get_status.json`)
- Player ID, username, empire
- Current system, POI, ship
- Credits, fuel, hull, shields
- Ship cargo and modules

### Ship Info (`get_ship.json`)
- Ship ID, class, name
- Hull, shields, armor
- Cargo capacity and usage
- CPU and power usage
- Weapon and utility slots
- Installed modules

### System Data (`get_system.json`)
- System ID, name, description
- Empire, police level
- Connections to other systems
- POIs in the system

### Map Data (`get_map.json`)
- All star systems with coordinates and connections
- Systems marked as discovered/visited
- Online player counts per system
- Police levels and security status

### Market Listings (`get_listings.json`)
- Market data from current location
- Buy/sell orders
- Prices and quantities

### Nearby Players (`get_nearby.json`)
- Other players at current location
- Their ships and statuses

### Shipyard Showroom (`get_ships.json`)
- Ships available for purchase at current station
- Shipyard level and base information
- Use `commission_ship` to order custom builds

### Player Skills (`get_skills.json`)
- Your current skill levels and XP
- Progress toward next skill level
- Personal skill advancement data

### Skill Definitions (`catalog_skills.json`)
- All available skills in the game
- Skill descriptions and requirements
- Category information
- Use `catalog` with type="skills" to browse

### Recipe Definitions (`catalog_recipes.json`)
- All available crafting recipes
- Required materials and quantities
- Output items and quantities
- Recipe categories
- Use `catalog` with type="recipes" to browse

### Ship Catalog (`catalog_ships.json`)
- All ship types in the game
- Ship specifications and stats
- Class information
- Use `catalog` with type="ships" to browse

### Item Definitions (`catalog_items.json`)
- All items in the game
- Item descriptions and properties
- Categories and types
- Use `catalog` with type="items" to browse

### Nearby Players (`get_nearby.json`)
- Other players at current location
- Their ships and statuses

## API Changes

The SpaceMolt game server has been updated with a new catalog system:

- **get_skills** now returns only your player's current skill levels and XP
- **Skill definitions** are now retrieved via the `catalog` endpoint with type="skills"
- **Recipe definitions** are now retrieved via the `catalog` endpoint with type="recipes"
- **Ship definitions** are retrieved via the `catalog` endpoint with type="ships"
- The catalog endpoint supports pagination, filtering by category, and text search

The scraper has been updated to work with these changes:
- `get_skills.json` contains your player's skill progress
- `catalog_skills.json` contains all skill definitions
- `catalog_recipes.json` contains all recipe definitions
- Shipyard showroom still uses `shipyard_showroom` endpoint

## Why WebSocket Instead of HTTP MCP?

The previous scraper used the HTTP MCP endpoint (`/mcp`) which had severe limitations:

| HTTP MCP (`/mcp`) | WebSocket (`/ws`) |
|-------------------|-------------------|
| ❌ Each POST creates new session | ✅ Single persistent session |
| ❌ Rate limited (~30 sessions/min) | ✅ No rate limiting |
| ❌ Slow (delays between calls) | ✅ Fast (no delays needed) |
| ❌ Designed for Claude Code MCP | ✅ Designed for game clients |

**Conclusion:** WebSocket is the correct protocol for game data scraping!

## Development

### Code Quality

The code follows project standards:
```bash
golangci-lint run cmd/data-scraper/main.go
```

### Adding More Scrapers

To add more data to scrape:

1. Add a new scrape method to the `Scraper` struct
2. Call it from the `scrapeAll()` function
3. Use `client.Send()` for protocol messages or `client.Get*()` for high-level methods

```go
func (s *Scraper) scrapeNewData() error {
    ctx := context.Background()

    msg := protocol.Message{
        Type: "new_endpoint",
    }
    if err := s.client.Send(ctx, msg); err != nil {
        return err
    }
    time.Sleep(2 * time.Second)

    rawJSON := s.client.GetRawJSON("new_endpoint")
    return s.saveJSON("new_endpoint.json", rawJSON)
}
```

## Troubleshooting

### "Connection failed"

Check internet connection and server status:
```bash
curl https://game.spacemolt.com
```

### "Login failed"

Verify credentials file exists and is valid:
```bash
cat data/agents/random-2/credentials.json
```

### Empty JSON files

Some endpoints may return empty results if:
- Not docked at a station (no market listings)
- No ships for sale (no ship listings)
- No nearby players (empty nearby list)

This is expected behavior based on game state.

## Related Tools

- **SPACEMOLT_AGENT_GUIDE.md** - Guide for autonomous agent gameplay
- **pkg/game/client.go** - WebSocket client implementation
- **cmd/auto-*** - Auto-agents using the same client

## License

Part of the spacemolt-spacemolt project.

## Author

Created for SpaceMolt autonomous agent development.
