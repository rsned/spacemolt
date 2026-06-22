# SpaceMolt Auto-Random

> Autonomous random NPC agent for SpaceMolt that simulates diverse player behavior.

## Overview

The auto-random is a fully autonomous agent that performs random actions to simulate NPC behavior in the game world. It explores systems, travels between POIs, mines resources, docks at stations, and even sends random chat messages. This agent is perfect for populating the game world with autonomous activity that mimics real player behavior.

## Features

### Core Functionality
- **Random Actions** - Performs diverse actions (travel, dock, mine, scan, etc.)
- **System Exploration** - Explores systems and travels between POIs
- **Mining Operations** - Mines resources when at asteroid fields
- **Station Visits** - Docks and undocks from stations
- **System Jumping** - Jumps between connected systems
- **Random Chat** - Sends random chat messages to simulate player activity
- **Captain's Log** - Tracks mission progress and status across sessions
- **Continuous Operation** - Runs indefinitely, creating dynamic behavior

### Intelligent Randomness

The auto-random implements smart random behavior:

- **Action Diversity** - 12 different actions for varied behavior
- **Idle Detection** - Switches actions after too many idle ticks
- **Context Awareness** - Only performs valid actions for current state
- **Chat Integration** - Random messages to system chat (10% probability)
- **Adaptive Behavior** - Responds to game state changes

## Quick Start

### Basic Usage

```bash
# Run the random agent
go run ./cmd/auto-random random-1
```

### Building

```bash
# Build the binary
go build -o bin/auto-random ./cmd/auto-random

# Run the built binary
./bin/auto-random random-1
```

## How It Works

### Main Random Loop

The auto-random uses a probabilistic action loop:

```go
RandomLoop:
  For each tick until stopped:
    1. Check for idle ticks (no travel/combat)
    2. If idle ticks > MAX_IDLE_TICKS:
       - Pick random action
       - Execute action
       - Reset idle counter
    3. Random chance to send chat message (10%)
    4. Wait for next tick (10 seconds)
    5. Update captain's log (every 2 minutes)
    6. Repeat
```

### Action Selection

The agent randomly selects from these actions:

- **get_status** - Refresh current status
- **get_system** - Fetch system data
- **get_poi** - Fetch POI data
- **scan** - Scan current area
- **travel** - Travel to random POI in current system
- **dock** - Dock at nearest station
- **undock** - Undock from current station
- **jump** - Jump to random connected system
- **mine** - Mine if at asteroid field
- **refuel** - Refuel if docked and fuel low
- **repair** - Repair if docked and hull damaged

### Context Awareness

The agent validates actions before execution:

- **Travel** - Only if docked and not traveling
- **Dock** - Only if not docked and at station POI
- **Undock** - Only if currently docked
- **Jump** - Only if docked and not traveling
- **Mine** - Only if at asteroid field and not docked
- **Refuel** - Only if docked and fuel < 80%
- **Repair** - Only if docked and hull < 90%

## Captain's Log

The auto-random maintains a captain's log that persists across sessions:

**Location:** `data/agents/{agent-id}/captains_log_latest.json`

**Contents:**
- Current mission goal
- Current location (system and POI)
- Ticks executed
- Credits status
- Ship status (hull, fuel, cargo)
- Last update timestamp

## Configuration

### Command-Line Arguments

```
Usage: auto-random <agent-id>

Arguments:
  agent-id   Agent identifier (e.g., random-1, npc-1)
```

### Constants

The following constants can be modified in `cmd/auto-random/main.go`:

```go
const (
    MAX_IDLE_TICKS   = 20  // Switch to new action after this many idle ticks
    ACTION_COOLDOWN  = 3   // Seconds to wait between actions
    CHAT_PROBABILITY = 0.1 // 10% chance to send a random chat message
)
```

### Chat Messages

The agent sends random chat messages from this predefined list:

- "Exploration is key to discovery!"
- "Has anyone seen the Nebula Collective?"
- "The stars are beautiful tonight."
- "Trading at Haven Exchange is profitable today."
- "I found a rich asteroid field!"
- "Warning: Pirates spotted in sector 7."
- "Random jump gate activated!"

You can modify this list in `cmd/auto-random/main.go` to customize chat behavior.

## Examples

### Example 1: Start Random Agent

```bash
# Start a random NPC agent
go run ./cmd/auto-random random-1
```

**Output:**
```
[random-1] 📖 Captain's Log - Last Entry:
[random-1]    Mission: Autonomous random behavior - exploring galaxy and simulating NPC activity
[random-1]    Location: System: SOL, POI: station_01
[random-1]    Time: 2026-02-23 15:30
[random-1] Ready! Empire: Federation | Credits: 500.00 | Ship: Dart | Cargo: 0/5
[random-1] Starting autonomous random NPC agent...
[random-1] Will randomly explore galaxy and perform actions
```

### Example 2: Random Behavior

```
[random-1] === Tick 1 ===
[random-1] Credits: 500.00 | Fuel: 100/100 | Hull: 100/100 | Docked: true | Traveling: false | Location: SOL
[random-1] Action: undock
[random-1] Undocking from station
[random-1] Undock initiated

[random-1] === Tick 2 ===
[random-1] Credits: 500.00 | Fuel: 100/100 | Hull: 100/100 | Docked: false | Traveling: false | Location: SOL
[random-1] Action: travel
[random-1] Traveling to POI: asteroid_belt_01 (Asteroid Belt)
[random-1] Travel initiated

[random-1] === Tick 3 ===
[random-1] Credits: 500.00 | Fuel: 95/100 | Hull: 100/100 | Docked: false | Traveling: true | Location: SOL
[random-1] Chat: Exploration is key to discovery!
```

### Example 3: Mining and Station Operations

```
[random-1] === Tick 15 ===
[random-1] Credits: 500.00 | Fuel: 80/100 | Hull: 100/100 | Docked: false | Traveling: false | Location: SOL
[random-1] Action: mine
[random-1] Mining resources
[random-1] Mining initiated

[random-1] === Tick 20 ===
[random-1] Credits: 500.00 | Fuel: 75/100 | Hull: 100/100 | Docked: false | Traveling: false | Location: SOL
[random-1] Action: dock
[random-1] Docking at station: station_01
[random-1] Docking initiated

[random-1] === Tick 21 ===
[random-1] Credits: 500.00 | Fuel: 70/100 | Hull: 100/100 | Docked: true | Traveling: false | Location: SOL
[random-1] Action: refuel
[random-1] Refueling
[random-1] Refuel initiated
```

### Example 4: System Jumping

```
[random-1] === Tick 35 ===
[random-1] Credits: 500.00 | Fuel: 100/100 | Hull: 100/100 | Docked: true | Traveling: false | Location: SOL
[random-1] Action: jump
[random-1] Jumping to system: CRIMSON_01
[random-1] Jump initiated

[random-1] === Tick 40 ===
[random-1] Credits: 500.00 | Fuel: 50/100 | Hull: 100/100 | Docked: true | Traveling: false | Location: CRIMSON_01
[random-1] Action: scan
[random-1] Scanned area
```

## Architecture

### Action Execution

Actions are executed with validation and error handling:

```go
func performAction(client, logger, ctx, action) {
    // Get current state
    state := client.GetState()

    // Validate and execute action
    switch action {
    case "travel":
        if state.Doc && !state.Traveling {
            // Find random POI
            pois := findStationAndPlanetPOIs()
            target := pois[randomInt(len(pois))]
            client.Travel(ctx, target)
        } else {
            logger.Printf("Cannot travel (docked=%v, traveling=%v)",
                state.Doc, state.Traveling)
        }

    case "mine":
        if atAsteroidField() {
            client.Mine(ctx)
        } else {
            logger.Printf("Not at asteroid field")
        }

    // ... other actions
    }

    // Wait before next action
    time.Sleep(ACTION_COOLDOWN * time.Second)
}
```

### Chat System

Random chat messages are sent probabilistically:

```go
if rand.Float64() < CHAT_PROBABILITY {
    messages := []string{
        "Exploration is key to discovery!",
        "The stars are beautiful tonight.",
        // ... more messages
    }
    msg := messages[rand.Intn(len(messages))]

    client.Send(ctx, protocol.Message{
        Type: "chat",
        Payload: map[string]any{
            "channel": "system",
            "content": msg,
        },
    })

    logger.Printf("Chat: %s", msg)
}
```

## Performance

### Typical Behavior

- **Action Frequency:** 1 action every 3-10 seconds
- **Chat Frequency:** ~1 message every 100 seconds (10% probability)
- **State Updates:** Every 10 seconds
- **Log Updates:** Every 2 minutes

### Action Distribution

Over time, the agent's actions will roughly distribute as:

- **Movement (travel/dock/undock/jump):** ~40%
- **Information (get_status/get_system/get_poi/scan):** ~30%
- **Operations (mine/refuel/repair):** ~20%
- **Idle (waiting):** ~10%

## Use Cases

### Game World Population

Deploy multiple auto-random agents to simulate a active game world:

```bash
# Deploy 10 random agents across different empires
for i in {1..10}; do
    go run ./cmd/auto-random random-npc-$i &
done
```

### Testing and Development

Use auto-random agents for testing game systems:

- **Server Load Testing** - Deploy many agents to stress test the server
- **Feature Testing** - Observe how agents interact with new features
- **Balance Testing** - Monitor economy impact of autonomous agents

### NPC Simulation

Create persistent NPC behavior in the game world:

- **Miners** - Agents that prefer mining actions
- **Traders** - Agents that prefer station operations
- **Explorers** - Agents that prefer system jumping

## Troubleshooting

### Issue: Agent performs same action repeatedly

**Cause:** Random chance can select the same action multiple times.

**Solution:**
1. This is normal behavior - randomness includes repetition
2. The agent will eventually switch to different actions
3. Increase `MAX_IDLE_TICKS` to force more frequent action changes

### Issue: Agent gets stuck in one state

**Cause:** Game state prevents certain actions (e.g., always docked).

**Solution:**
1. Check current game state (docked, traveling, in combat)
2. The agent will eventually pick an action that's valid
3. Manually undock or move the agent if needed

### Issue: Too many chat messages

**Cause:** `CHAT_PROBABILITY` is too high.

**Solution:**
1. Reduce `CHAT_PROBABILITY` in `cmd/auto-random/main.go`
2. Set to 0.05 for 5% probability (half as many messages)
3. Set to 0.0 to disable chat entirely

### Issue: Agent stops unexpectedly

**Cause:** Various reasons (connection loss, error, etc.)

**Solution:**
1. Check the captain's log for last status: `cat data/agents/{agent-id}/captains_log_latest.json`
2. Restart the agent - it will resume from where it left off
3. Check logs for specific error messages

## Customization

### Adding New Actions

To add a new random action:

1. Add action name to `pickRandomAction()` list
2. Add case in `performAction()` switch statement
3. Implement validation and execution logic
4. Test thoroughly

### Custom Chat Messages

To customize chat messages:

1. Edit the `chatMessages` array in `cmd/auto-random/main.go`
2. Add or remove messages as desired
3. Messages are selected uniformly at random

### Action Probabilities

To customize action probabilities:

1. Modify `pickRandomAction()` to use weighted selection
2. Assign different probabilities to different actions
3. Example: Give mining 20% chance, travel 30% chance, etc.

## Related Tools

- [Auto-Trader](../auto-trader/) - Specialized trading agent
- [Auto-Explorer](../auto-explorer/) - Specialized exploration agent
- [Auto-Fighter](../auto-fighter/) - Specialized combat agent

## License

Part of the SpaceMolt project.
