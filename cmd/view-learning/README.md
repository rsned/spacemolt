# SpaceMolt View Learning

> Interactive CLI tool for querying and viewing agent learning records, experience history, and discovered systems from the knowledge base.

## Overview

view-learning provides command-line access to the SpaceMolt knowledge base, allowing you to query agent learning data, experience histories, skill progression, and discovered systems. It supports both table and JSON output formats with beautiful terminal styling.

## Features

### Core Functionality
- **📊 Agent Listing** - List all registered agents with summaries
- **📖 Experience History** - View action history and experiences
- **🗺️ System Discovery** - Show discovered systems with connections
- **📈 Statistics** - Display learning statistics and summaries
- **🎨 Beautiful Output** - Styled terminal output with colors and formatting

### Commands

- **agents** - List all registered agents
- **history** - Show action history for an agent
- **experiences** - Show experience records for an agent
- **systems** - Show all discovered systems
- **summary** - Show learning statistics for an agent

## Quick Start

### Basic Usage

```bash
# List all agents
go run ./cmd/view-learning agents

# Show agent history
go run ./cmd/view-learning history explorer-7

# Show discovered systems
go run ./cmd/view-learning systems

# Show agent summary
go run ./cmd/view-learning summary explorer-7
```

### Building

```bash
# Build the binary
go build -o bin/view-learning ./cmd/view-learning

# Run the built binary
./bin/view-learning agents
```

## Command-Line Flags

### Global Flags

```
-db-path string
    Path to SQLite database file (default "spacemolt-knowledge.db")

-limit int
    Limit number of records to show (default 20)

-format string
    Output format: table, json (default "table")

-sort string
    Sort order: time, type (default "time")
```

## Commands

### agents

List all registered agents in the knowledge base.

**Usage:**
```bash
view-learning agents [flags]
```

**Example:**
```bash
go run ./cmd/view-learning agents
```

**Output:**
```
Registered Agents
────────────────────────────────────────────────────────────────────────────────

✓ Explorer 7 [explorer-7]
  Role: explorer
  Faction: voidborn
  Experiences: 145
  Systems Visited: 23

────────────────────────────────────────────────────────────────────────────────

○ Miner 1 [miner-1]
  Role: miner
  Experiences: 89
  Systems Visited: 5

────────────────────────────────────────────────────────────────────────────────

Total: 2 agent(s)
```

### history

Show action history for an agent.

**Usage:**
```bash
view-learning history <agent-id> [flags]
```

**Example:**
```bash
go run ./cmd/view-learning history explorer-7
```

**Output:**
```
Action History for Agent: explorer-7
────────────────────────────────────────────────────────────────────────────────

[2026-02-23 10:15:30] [mining] Mined iron_ore at asteroid_belt_1
  Outcome: +25.0 iron_ore
  Location: sol-2/asteroid_belt_1

────────────────────────────────────────────────────────────────────────────────

[2026-02-23 10:14:15] [travel] Traveled from sol to sol-2
  Outcome: Arrived at sol-2
  Location: sol-2

────────────────────────────────────────────────────────────────────────────────

Total: 2 record(s)
```

### experiences

Show experience records for an agent.

**Usage:**
```bash
view-learning experiences <agent-id> [flags]
```

**Example:**
```bash
go run ./cmd/view-learning experiences explorer-7 --limit 50
```

**Output:**
```
Experiences for Agent: explorer-7
────────────────────────────────────────────────────────────────────────────────

Time                 Type            Description
────────────────────────────────────────────────────────────────────────────────
2 min ago            mining          Mined iron_ore at asteroid_belt_1
  → +25.0 iron_ore
  → @ sol-2/asteroid_belt_1

5 min ago            travel          Traveled from sol to sol-2
  → Arrived at sol-2
  → @ sol-2

1 hr ago             discovery       Discovered new system
  → Found sol-3
  → @ sol-3

────────────────────────────────────────────────────────────────────────────────

Total: 3 experience(s)
```

### systems

Show all discovered systems.

**Usage:**
```bash
view-learning systems [flags]
```

**Example:**
```bash
go run ./cmd/view-learning systems --limit 50
```

**Output:**
```
Discovered Systems
────────────────────────────────────────────────────────────────────────────────

Sol [sol]
  Position: (0.0, 0.0, 0.0)
  Police: 5
  Faction: voidborn
  Visits: 45
  Last Visited: 2 min ago
  Discovered By: explorer-7
  Connections: 3 system(s)
    sol-2, sol-3, corsair

────────────────────────────────────────────────────────────────────────────────

Sol-2 [sol-2]
  Position: (15.0, 5.0, -2.0)
  Police: 3
  Faction: voidborn
  Visits: 23
  Last Visited: 5 min ago
  Discovered By: explorer-7
  Connections: 2 system(s)
    sol, sol-3

────────────────────────────────────────────────────────────────────────────────

Total: 2 system(s)
```

### summary

Show learning statistics for an agent.

**Usage:**
```bash
view-learning summary <agent-id> [flags]
```

**Example:**
```bash
go run ./cmd/view-learning summary explorer-7
```

**Output:**
```
Agent Summary: Explorer 7
ID: explorer-7

Agent Information
────────────────────────────────────────
  Role: explorer
  Faction: voidborn

Learning Statistics
────────────────────────────────────────
  Total Experiences: 145
  Unique Locations: 23
  Last Activity: 2 min ago

Experiences by Type
────────────────────────────────────────
  mining:       ████████████ 45
  travel:       ████████ 32
  discovery:    █████ 18
  trading:      ████ 12
  combat:       ██ 8

Recent Activity
────────────────────────────────────────
  [2 min ago] Mined iron_ore at asteroid_belt_1
    → +25.0 iron_ore
  [5 min ago] Traveled from sol to sol-2
    → Arrived at sol-2
  [1 hr ago] Discovered new system
    → Found sol-3
```

## Output Formats

### Table Format (Default)

Human-readable with colors and formatting:
```bash
go run ./cmd/view-learning agents
```

### JSON Format

Machine-readable for automation:
```bash
go run ./cmd/view-learning agents --format json
```

**Output:**
```json
[
  {
    "id": "explorer-7",
    "name": "Explorer 7",
    "role": "explorer",
    "faction": "voidborn",
    "status": "active"
  },
  {
    "id": "miner-1",
    "name": "Miner 1",
    "role": "miner",
    "faction": null,
    "status": "active"
  }
]
```

## Examples

### Example 1: Quick Agent Overview

```bash
# List all agents
go run ./cmd/view-learning agents

# Get specific agent summary
go run ./cmd/view-learning summary explorer-7
```

### Example 2: Analyze Learning Progress

```bash
# Show recent experiences
go run ./cmd/view-learning experiences explorer-7 --limit 50

# Sort by type
go run ./cmd/view-learning experiences explorer-7 --sort type
```

### Example 3: System Discovery Tracking

```bash
# Show all discovered systems
go run ./cmd/view-learning systems

# Show with JSON output
go run ./cmd/view-learning systems --format json > systems.json
```

### Example 4: Custom Database

```bash
# Use custom database path
go run ./cmd/view-learning -db-path /path/to/knowledge.db agents
```

### Example 5: Data Export

```bash
# Export agent history
go run ./cmd/view-learning history explorer-7 --format json > explorer-7-history.json

# Export all agents
go run ./cmd/view-learning agents --format json > agents.json
```

## Database Schema

The tool queries these tables:

### agents
```sql
CREATE TABLE agents (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    role TEXT NOT NULL,
    faction TEXT,
    status TEXT NOT NULL
);
```

### experiences
```sql
CREATE TABLE experiences (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    agent_id TEXT NOT NULL,
    time TEXT NOT NULL,
    type TEXT NOT NULL,
    description TEXT NOT NULL,
    outcome TEXT,
    location TEXT,
    FOREIGN KEY (agent_id) REFERENCES agents(id)
);
```

### systems
```sql
CREATE TABLE systems (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    pos_x REAL,
    pos_y REAL,
    pos_z REAL,
    police_level INTEGER,
    faction TEXT,
    visit_count INTEGER,
    last_visited TEXT,
    discovered_by TEXT
);
```

### connections
```sql
CREATE TABLE connections (
    from_system TEXT NOT NULL,
    to_system TEXT NOT NULL,
    PRIMARY KEY (from_system, to_system),
    FOREIGN KEY (from_system) REFERENCES systems(id),
    FOREIGN KEY (to_system) REFERENCES systems(id)
);
```

## Integration

### With Scripts

```bash
#!/bin/bash
# Check agent activity

AGENT_ID="explorer-7"

# Get last activity
LAST=$(go run ./cmd/view-learning summary $AGENT_ID --format json | \
  jq -r '.last_activity')

echo "Agent $AGENT_ID last active: $LAST"

# Get experience count
COUNT=$(go run ./cmd/view-learning summary $AGENT_ID --format json | \
  jq -r '.total_experiences')

echo "Total experiences: $COUNT"
```

### With Monitoring

```bash
#!/bin/bash
# Monitor agent learning

while true; do
  echo "=== $(date) ==="
  go run ./cmd/view-learning agents
  sleep 300
done
```

## Troubleshooting

### Issue: "Failed to open database"

**Cause:** Database file not found or corrupted.

**Solution:**
1. Verify database path: `-db-path spacemolt-knowledge.db`
2. Check file exists: `ls spacemolt-knowledge.db`
3. Check file permissions

### Issue: "No agents registered"

**Cause:** Database is empty or no agents registered.

**Solution:**
1. Run agents to populate database
2. Check agent is registering properly
3. Verify agent manager is running

### Issue: "Agent not found"

**Cause:** Agent ID doesn't exist in database.

**Solution:**
1. List all agents: `go run ./cmd/view-learning agents`
2. Use correct agent ID
3. Check agent spelling

### Issue: "No experience records found"

**Cause:** Agent hasn't recorded experiences yet.

**Solution:**
1. Verify agent is running
2. Check agent is recording experiences
3. Wait for agent to perform actions

## Best Practices

### Regular Monitoring

```bash
# Daily check of agent activity
go run ./cmd/view-learning agents

# Weekly detailed review
go run ./cmd/view-learning experiences explorer-7 --limit 100
```

### Data Export

```bash
# Regular backups
go run ./cmd/view-learning agents --format json > backup/agents-$(date +%Y%m%d).json
go run ./cmd/view-learning systems --format json > backup/systems-$(date +%Y%m%d).json
```

### Analysis

```bash
# Compare agents
for agent in explorer-7 miner-1 trader-1; do
  echo "=== $agent ==="
  go run ./cmd/view-learning summary $agent
done
```

## Related Tools

- [view-market](../view-market/) - View market data
- [test-agent](../test-agent/) - Test agent functionality
- [Knowledge Base](../../pkg/knowledge/) - Knowledge base implementation

## License

Part of the SpaceMolt project.
