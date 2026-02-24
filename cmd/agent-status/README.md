# Agent Status

Display detailed status information for a SpaceMolt agent, including pilot info, skills, location, and ship details.

## Overview

The `agent-status` tool connects to the SpaceMolt game server as an agent and displays a comprehensive status report in a formatted table layout. It shows all relevant information about the agent's current state in the game.

## Features

- **Pilot Information**: Agent name, faction, empire, colors, home base, credits, and status
- **Skills Display**: All skills with XP progress and levels (split across two columns if more than 4 skills)
- **Location Details**: Current system, POI, empire, police level, and docked status
- **Ship Status**: Name, class, hull, shields, armor, CPU, power, fuel, speed, and cargo
- **Module Listing**: All installed modules with their names
- **Cargo Manifest**: All cargo items with quantities
- **Automatic Reconnection**: Handles connection failures gracefully

## Usage

### Basic Usage

```bash
# Display status for an agent
go run ./cmd/agent-status miner-1

# Or use the built binary
./agent-status miner-1
```

### Example Output

```
╔═══════════════════════════════════════════════════════════════════════════════╗
║                                  AGENT STATUS                                 ║
╚═══════════════════════════════════════════════════════════════════════════════╝

┌─────────────────────────────────────────┬─────────────────────────────────────────┐
│ PILOT                                   │                                         │
│ Name:                          miner-1  │ Faction:                    federation │
│ Empire:                        Solarian │ Colors:                  🟥🟥🟥🟥🟥/🟦🟦🟦🟦🟦 │
│ Home Base:                  haven_base  │ Status:                              │
│ Credits:                       1234.00  │                                         │
└─────────────────────────────────────────┴─────────────────────────────────────────┘

┌─────────────────────────────────────────┬─────────────────────────────────────────┐
│ SKILLS                                  │                                         │
│ Mining:                   450 /  500 XP  │ Level: 5                                │
│                                          │                                         │
└─────────────────────────────────────────┴─────────────────────────────────────────┘

┌─────────────────────────────────────────┬─────────────────────────────────────────┐
│ LOCATION                                │                                         │
│ System:                   Alpha Centauri │ POI:                  station_alpha_1  │
│ Empire:                        Solarian │ ("Alpha Station")                      │
│ Police Level:                      100%  │ Docked:                       YES      │
└─────────────────────────────────────────┴─────────────────────────────────────────┘

┌─────────────────────────────────────────┬─────────────────────────────────────────┐
│ SHIP                                    │                                         │
│ Name:                          Dart M1  │ Cargo:          15 / 20 (75%) ████░   │
│ Class:                           Dart  │                                         │
│ Hull:                      100 / 100 (100%) █████  │ Items: 3                                │
│ Shield:                    50 / 50 (100%) █████  │    iron_ore                  x10      │
│ Shield Recharge:             +1 / tick  │    copper_ore                x5       │
│ Armor:                            10     │    refined_copper             x5       │
│ CPU:                        2 / 10      │                                         │
│ Power:                      5 / 15      │                                         │
│ Fuel:                      50 / 100     │                                         │
│ Speed:                          15     │                                         │
│ Insured:                        NO     │                                         │
│ Modules:                        2     │                                         │
│ 1) Mining Laser Mk1                │                                         │
│ 2) Basic Shield Generator           │                                         │
└─────────────────────────────────────────┴─────────────────────────────────────────┘
```

## Requirements

Each agent must have:
- `data/agents/{agent-id}/credentials.json` - Login credentials

## Command-Line Arguments

```
agent-status <agent-id>

Arguments:
  agent-id    Agent identifier (e.g., miner-1, explorer-1, trader-1)
```

## Building

```bash
# Build the binary
go build -o bin/agent-status ./cmd/agent-status

# Run the built binary
./bin/agent-status miner-1
```

## How It Works

1. **Load Credentials**: Reads `data/agents/{agent-id}/credentials.json`
2. **Connect to Server**: Connects to `wss://game.spacemolt.com/ws`
3. **Login**: Authenticates using the loaded credentials
4. **Fetch Details**: Calls `get_ship` and `get_skills` for complete information
5. **Display Status**: Prints formatted status using Unicode box-drawing characters

## Display Details

### Pilot Section
- Agent name and faction
- Empire (title-cased)
- Faction colors (currently placeholder)
- Home base location
- Current credits
- Status message

### Skills Section
- All skills with XP progress
- Shows current XP and XP required for next level
- Displays "MAX" for max-level skills
- Splits into two columns if more than 4 skills for better readability

### Location Section
- Current system name
- Current POI with display name (if different from ID)
- Empire controlling the system
- Police level (0% or 100%)
- Docked status (YES/NO)

### Ship Section
- Ship name and class
- Hull and shield levels with percentage bars
- Shield recharge rate
- Armor value
- CPU and power usage
- Fuel level
- Speed
- Insurance status
- All installed modules
- Complete cargo manifest with item quantities

## Error Handling

- Shows usage help if no agent ID provided
- Lists all available agents in `data/agents/`
- Validates agent directory exists before connecting
- Uses reconnecting handler for connection stability

## Unicode and Display Width

The tool uses `github.com/mattn/go-runewidth` for proper handling of:
- Multi-byte UTF-8 characters (e.g., emojis)
- East Asian wide characters
- Proper truncation with ellipsis (…)
- Right-aligned values in fixed-width columns

## Related Tools

- [auto-miner](../auto-miner/README.md) - Autonomous mining agent
- [auto-explorer](../auto-explorer/README.md) - Autonomous exploration agent
- [auto-trader](../auto-trader/README.md) - Autonomous trading agent
- [register-agent](../register-agent/README.md) - Register new agents

## License

Part of the SpaceMolt project.
