# Captain's Log - Agent Memory System

## Overview

The Captain's Log system provides persistent memory for agents, allowing them to:
- Track their current mission/goals
- Remember what they've learned
- Resume operations after restarts or disconnections
- Maintain context across sessions

## Storage Location

Logs are stored in: `data/agents/{AGENT_ID}/captains_log/`

Files are named: `captains_log.YYYYMMDDHHMM.log`

## Key Features

✅ **Rate Limiting**: Won't write more than once per minute
✅ **Deduplication**: Won't write if nothing has changed
✅ **Automatic Recovery**: Read latest log on startup
✅ **JSON Format**: Easy to parse and inspect
✅ **Chronological**: Files sorted by timestamp

## Basic Usage

### Writing a Log Entry

```go
package main

import (
    "log"
    "github.com/rsned/spacemolt/pkg/game"
)

func updateCaptainsLog(agentID string, client *game.Client) {
    state := client.GetState()

    entry := &game.AgentLog{
        AgentName:   state.Player.Username,
        CurrentGoal: "Exploring the galaxy, scanning every POI and docking at every station",
        Location:    fmt.Sprintf("System: %s, POI: %s", state.CurrentSystem, state.CurrentPOI),
        Notes: []string{
            fmt.Sprintf("Credits: %.2f", state.Credits),
            fmt.Sprintf("Fuel: %.0f/%.0f", state.Fuel, state.MaxFuel),
            "Discovered rich mineral deposits at sol_belt",
        },
    }

    if err := game.WriteCaptainsLog(agentID, entry); err != nil {
        log.Printf("Failed to write captain's log: %v", err)
    }
}
```

### Reading the Latest Log on Startup

```go
func recoverFromCaptainsLog(agentID string, logger *log.Logger) string {
    log, err := game.ReadLatestCaptainsLog(agentID)
    if err != nil {
        logger.Printf("Failed to read captain's log: %v", err)
        return ""
    }

    if log != nil {
        logger.Printf("=== Resuming from Captain's Log ===")
        logger.Printf("Last Mission: %s", log.CurrentGoal)
        logger.Printf("Last Location: %s", log.Location)
        logger.Printf("Last Update: %s", log.Timestamp.Format("2006-01-02 15:04"))

        if len(log.Notes) > 0 {
            logger.Printf("Recent Notes:")
            for _, note := range log.Notes {
                logger.Printf("  - %s", note)
            }
        }

        return log.CurrentGoal
    }

    return ""
}
```

### Complete Agent Example

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"
    "time"

    "github.com/rsned/spacemolt/pkg/game"
)

func main() {
    agentID := "explorer-1"
    logger := log.New(os.Stdout, fmt.Sprintf("[%s] ", agentID), log.LstdFlags)
    ctx := context.Background()

    // Step 1: Check captain's log for previous mission
    previousMission := recoverFromCaptainsLog(agentID, logger)
    if previousMission != "" {
        logger.Printf("Continuing previous mission: %s", previousMission)
    } else {
        logger.Printf("Starting new mission")
    }

    // Step 2: Initialize agent
    client, _, err := game.InitializeAgent(agentID, logger, ctx)
    if err != nil {
        log.Fatalf("Failed to initialize agent: %v", err)
    }
    defer client.Close()

    // Step 3: Main agent loop
    ticker := time.NewTicker(1 * time.Minute)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            // Update captain's log periodically
            updateCaptainsLog(agentID, client)
        }
    }
}

func updateCaptainsLog(agentID string, client *game.Client) {
    state := client.GetState()

    // Determine current goal based on agent logic
    currentGoal := determineCurrentGoal(state)

    // Collect observations/learnings
    notes := collectNotes(state)

    entry := &game.AgentLog{
        AgentName:   state.Player.Username,
        CurrentGoal: currentGoal,
        Location:    fmt.Sprintf("System: %s, POI: %s", state.CurrentSystem, state.CurrentPOI),
        Notes:       notes,
    }

    game.WriteCaptainsLog(agentID, entry)
}

func determineCurrentGoal(state *game.State) string {
    if state.InCombat {
        return "Engaged in combat, defending ship"
    }
    if state.Traveling {
        return fmt.Sprintf("Traveling to %s", state.TravelProgress.Destination)
    }
    if state.Doc {
        return "Docked at station, managing inventory and repairs"
    }
    return "Exploring system, searching for opportunities"
}

func collectNotes(state *game.State) []string {
    var notes []string

    notes = append(notes, fmt.Sprintf("Credits: %.2f", state.Credits))
    notes = append(notes, fmt.Sprintf("Hull: %.0f/%.0f (%.1f%%)",
        state.Hull, state.MaxHull, (state.Hull/state.MaxHull)*100))
    notes = append(notes, fmt.Sprintf("Fuel: %.0f/%.0f", state.Fuel, state.MaxFuel))

    if len(state.Ship.Cargo) > 0 {
        notes = append(notes, fmt.Sprintf("Cargo: %d items", len(state.Ship.Cargo)))
    }

    if len(state.Nearby) > 0 {
        notes = append(notes, fmt.Sprintf("Nearby ships: %d", len(state.Nearby)))
    }

    return notes
}

func recoverFromCaptainsLog(agentID string, logger *log.Logger) string {
    log, err := game.ReadLatestCaptainsLog(agentID)
    if err != nil {
        logger.Printf("Failed to read captain's log: %v", err)
        return ""
    }

    if log != nil {
        logger.Printf("\n%s\n", game.FormatCaptainsLogForDisplay(log))
        return log.CurrentGoal
    }

    return ""
}
```

## Example Log File

**File**: `data/agents/explorer-1/captains_log/captains_log.202602141430.log`

```json
{
  "timestamp": "2026-02-14T14:30:00Z",
  "agent_id": "explorer-1",
  "agent_name": "Stella 'Starblazer' Sterling",
  "current_goal": "Exploring the galaxy, scanning every POI and docking at every station",
  "location": "System: frontier_17, POI: ancient_ruins",
  "notes": [
    "Credits: 15432.50",
    "Hull: 850/1000 (85.0%)",
    "Fuel: 120/150",
    "Discovered rare artifacts worth 5000 credits",
    "Market prices favorable for titanium ore",
    "System has 3 unexplored POIs remaining"
  ]
}
```

## API Reference

### Types

```go
type AgentLog struct {
    Timestamp   time.Time `json:"timestamp"`
    AgentID     string    `json:"agent_id"`
    AgentName   string    `json:"agent_name"`
    CurrentGoal string    `json:"current_goal"` // What they're doing now
    Location    string    `json:"location"`     // Current system/POI
    Notes       []string  `json:"notes"`        // Things learned/observed
}
```

### Functions

#### WriteCaptainsLog
```go
func WriteCaptainsLog(agentID string, entry *AgentLog) error
```
Writes a captain's log entry if more than 1 minute has passed AND content has changed.

#### ReadLatestCaptainsLog
```go
func ReadLatestCaptainsLog(agentID string) (*AgentLog, error)
```
Reads the most recent captain's log entry. Returns `nil` if no logs exist.

#### ReadAllCaptainsLogs
```go
func ReadAllCaptainsLogs(agentID string) ([]*AgentLog, error)
```
Reads all captain's log entries, newest first. Returns `nil` if no logs exist.

#### FormatCaptainsLogForDisplay
```go
func FormatCaptainsLogForDisplay(entry *AgentLog) string
```
Formats a log entry for human-readable console output.

#### GetCaptainsLogDir
```go
func GetCaptainsLogDir(agentID string) string
```
Returns the path to the captain's log directory for an agent.

## Integration Patterns

### Pattern 1: Periodic Updates
Update the log every N minutes during normal operations:

```go
ticker := time.NewTicker(2 * time.Minute)
for range ticker.C {
    updateCaptainsLog(agentID, client)
}
```

### Pattern 2: Event-Based Updates
Update the log when significant events occur:

```go
func OnMessage(resp protocol.Response) {
    switch resp.Type {
    case protocol.TypeLoggedIn:
        updateCaptainsLog(agentID, client)
    case protocol.TypeDocked:
        updateCaptainsLog(agentID, client)
    case protocol.TypePirateWarning:
        updateCaptainsLog(agentID, client)
    }
}
```

### Pattern 3: State Change Updates
Update the log only when mission goals change:

```go
var lastGoal string
currentGoal := determineCurrentGoal(state)
if currentGoal != lastGoal {
    updateCaptainsLog(agentID, client)
    lastGoal = currentGoal
}
```

## Best Practices

1. **Keep Goals Concise**: 1-2 sentences describing what the agent is doing
2. **Location Format**: Use consistent format like "System: {name}, POI: {poi}"
3. **Meaningful Notes**: Only include significant observations or learnings
4. **Don't Spam**: Trust the rate limiting and deduplication
5. **Read on Startup**: Always check for previous log to maintain continuity
6. **Handle Errors**: Log write failures shouldn't crash the agent

## Troubleshooting

**Problem**: Logs aren't being written
**Solution**: Check that more than 1 minute has passed and content has changed

**Problem**: Too many log files accumulating
**Solution**: Implement log rotation or cleanup of old entries (>7 days)

**Problem**: Can't read log on startup
**Solution**: It's normal if agent has never written a log; ReadLatestCaptainsLog returns `nil`

## Future Enhancements

Potential additions to this system:
- Log rotation (delete entries older than X days)
- Compression of old logs
- Search/query functionality across all logs
- Integration with LLM context for decision-making
- Web dashboard for viewing agent histories
