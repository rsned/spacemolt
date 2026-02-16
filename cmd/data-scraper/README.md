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
go run cmd/data-scraper/main.go
```

### What It Does

1. **Connects** to game server via WebSocket
2. **Logs in** using credentials
3. **Scrapes data** from multiple endpoints:
   - Status & Player Info
   - Ship Info
   - Current POI
   - System Data
   - Market Listings
   - Ship Listings
   - Nearby Players
   - Skills
   - Recipes
   - Notifications
   - Wrecks
   - Drones
   - Base Info
   - Faction Info
   - Captain's Log
4. **Saves all responses** to `data/game-api/*.json`

### Output

All API responses are saved to `data/game-api/` with descriptive filenames:

```
data/game-api/
├── get_status.json
├── get_ship.json
├── get_poi.json
├── get_system.json
├── get_listings.json
├── get_ships.json
├── get_nearby.json
├── get_skills.json
├── get_recipes.json
├── get_notifications.json
├── get_wrecks.json
├── get_drones.json
├── get_base.json
├── faction_info.json
└── captains_log_list.json
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

### Market Listings (`get_listings.json`)
- Market data from current location
- Buy/sell orders
- Prices and quantities

### Ship Listings (`get_ships.json`)
- Available ships for purchase
- Prices and specifications

### Skills (`get_skills.json`)
- Player skills and XP
- Skill definitions
- Next level requirements

### Recipes (`get_recipes.json`)
- Available crafting recipes
- Required materials
- Output items

### Nearby Players (`get_nearby.json`)
- Other players at current location
- Their ships and statuses

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
