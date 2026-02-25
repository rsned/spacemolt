# SpaceMolt Play Simple

> Interactive command-line tool for manually controlling SpaceMolt agents.

## Overview

play-simple is an interactive CLI tool that allows you to manually control a SpaceMolt agent. It provides a simple REPL (Read-Eval-Print Loop) interface for sending commands to the game server and viewing responses in real-time. Perfect for testing, debugging, and manual agent operation.

## Features

### Core Functionality
- **🎮 Interactive Control** - Send commands to game server via simple CLI
- **📊 Status Display** - View agent status (credits, ship, cargo, location)
- **💬 Real-time Feedback** - See server responses immediately
- **🔧 Command Parsing** - Simple command format with key=value arguments
- **📝 Help System** - Built-in help and status commands

### Display Features
- **Formatted Status** - Beautiful status display with emojis
- **Cargo Summary** - Current cargo usage and items
- **Ship Details** - Ship class, fuel, hull, and modules
- **Location Info** - Current system and POI

## Quick Start

### Basic Usage

```bash
# Start interactive session for an agent
go run ./cmd/play-simple pirate-4

# Build and run
go build -o bin/play-simple ./cmd/play-simple
./bin/play-simple miner-1
```

## Commands

### Interactive Commands

Once connected, you can enter commands at the prompt:

```
> help                    # Show help
> status                  # Show current status
> quit                    # Exit the tool

> move system=sol-2       # Move to system
> dock station=station-1  # Dock at station
> mine                    # Start mining
> sell                    # Sell all cargo

> buy item=iron_ore quantity=10   # Buy items
> sell_item item=iron_ore quantity=5  # Sell items

# Any protocol message
> message_type param1=value1 param2=value2
```

### Command Format

Commands follow a simple format:

```
command-name key1=value1 key2=value2
```

The command is sent as:
```json
{
  "type": "command-name",
  "payload": {
    "key1": "value1",
    "key2": "value2"
  }
}
```

## Examples

### Example 1: Starting a Session

```bash
go run ./cmd/play-simple pirate-4
```

**Output:**
```
[pirate-4] 🏴‍☠️ Connecting as pirate-4-user...
[GAME] Connected!
[pirate-4] ✓ Connected! Credits: 100.00, System: sol, Docked: true
[pirate-4] Logging in...
[GAME] Login successful
[GAME] ✓ Logged in

============================================================
☣ CRIMSON CORSAIRS ENFORCER
============================================================
💰 Credits: 150.00
🌍 System: sol
📍 POI: station-1
🚢 Ship: Dart
⛽ Fuel: 100.0/100.0
🛡️ Hull: 50.0/50.0
📦 Cargo: 0/5
⚓ Docked: true
============================================================

Enter commands (or 'help', 'status', 'quit'):
> _
```

### Example 2: Mining Operations

```bash
> status
System: sol, POI: station-1, Credits: 150.00, Fuel: 100.0/100.0, Docked: true
> undock
[GAME] ✓ Undocked from station-1
> move system=sol-2 poi=asteroid_belt_1
[GAME] ✓ Moved to sol-2/asteroid_belt_1
> mine
[GAME] ✓ Mining started
> status
System: sol-2, POI: asteroid_belt_1, Credits: 150.00, Fuel: 95.0/100.0, Docked: false
```

### Example 3: Cargo Management

```bash
> dock station=station-2
[GAME] ✓ Docked at station-2
> sell
[GAME] ✓ Sold all cargo for 250.00 credits
> status
System: sol-2, POI: station-2, Credits: 400.00, Fuel: 95.0/100.0, Docked: true
```

### Example 4: Custom Commands

```bash
# Send any protocol message
> faction_info
[GAME] Response:
{
  "faction_id": "crc_faction_123",
  "faction_rank": "Member",
  ...
}

> facility_get_types facility_type=personal_workbench
[GAME] Response:
{
  "types": [...]
}
```

## Configuration

### Agent Directory Structure

```
data/agents/
└── pirate-4/
    └── credentials.json
```

### Credentials Format

```json
{
  "username": "pirate-4-user",
  "password": "pirate-4-password",
  "empire": "voidborn"
}
```

## Status Display

The status command shows:

```
============================================================
☣ CRIMSON CORSAIRS ENFORCER
============================================================
💰 Credits: 150.00
🌍 System: sol
📍 POI: station-1
🚢 Ship: Dart
⛽ Fuel: 100.0/100.0
🛡️ Hull: 50.0/50.0
📦 Cargo: 3/5
⚓ Docked: true
============================================================
```

- **Credits** - Current wallet balance
- **System** - Current star system
- **POI** - Current point of interest
- **Ship** - Ship name
- **Fuel** - Current/max fuel
- **Hull** - Current/max hull integrity
- **Cargo** - Current/max cargo capacity
- **Docked** - Whether docked at station

## Protocol Messages

The tool sends messages in the SpaceMolt protocol format:

```json
{
  "type": "command_type",
  "payload": {
    "key1": "value1",
    "key2": "value2"
  },
  "timestamp": 1737657890123
}
```

### Common Commands

**Movement:**
- `move system=<system-id> poi=<poi-id>`
- `dock station=<station-id>`
- `undock`

**Mining:**
- `mine`
- `mine_target target=<target-id>`

**Cargo:**
- `sell`
- `buy item=<item-id> quantity=<amount>`
- `sell_item item=<item-id> quantity=<amount>`

**Facilities:**
- `facility_get_types facility_type=<type>`

**Faction:**
- `faction_info`
- `faction_get_invites`

## Tips and Tricks

### Keyboard Shortcuts

- **Ctrl+C** - Gracefully exit
- **Ctrl+D** - Exit (EOF)

### Command History

The tool doesn't currently support command history. Use your terminal's history instead (up arrow).

### Quick Status Check

```bash
> status
```

Shows a quick one-line status summary.

### Testing Commands

Use play-simple to test new game commands before implementing them in automated agents:

```bash
# Test a new command
> new_command param1=value1

# See the response
# [GAME] Response: {...}
```

## Limitations

### Current Limitations

- **No Command History** - Doesn't support arrow key history
- **No Auto-Complete** - Commands must be typed fully
- **No Multi-Line** - Each command must be on one line
- **Basic Parsing** - Simple key=value parsing only
- **No Validation** - Doesn't validate commands before sending

### Future Enhancements

Potential improvements:
- Command history (readline support)
- Tab completion
- Command validation
- Syntax highlighting
- Multi-line commands
- Command macros/aliases

## Troubleshooting

### Issue: "Failed to load credentials"

**Cause:** Agent credentials not found.

**Solution:**
1. Verify agent directory exists
2. Check credentials.json file
3. Ensure agent name is correct

### Issue: "Failed to connect"

**Cause:** Network issue or server unavailable.

**Solution:**
1. Check network connectivity
2. Verify game server is accessible
3. Try again later

### Issue: "Command not recognized"

**Cause:** Typo or invalid command.

**Solution:**
1. Check command spelling
2. Verify command is supported by server
3. Check response for error details

### Issue: "No response"

**Cause:** Server didn't respond or connection lost.

**Solution:**
1. Check network connection
2. Verify server is running
3. Check game logs

## Related Tools

- [claim-code](../claim-code/) - Registration code claiming
- [facility-check](../facility-check/) - Facility queries
- [Game Client](../../pkg/game/) - Game client API reference
- [Protocol](../../internal/protocol/) - Protocol message definitions

## License

Part of the SpaceMolt project.
