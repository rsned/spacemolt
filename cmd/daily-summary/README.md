# SpaceMolt Daily Summary

> Automated daily snapshot and diff reporting tool for tracking agent progress over time.

## Overview

The daily-summary tool connects to all configured agents, captures their current state (credits, skills, stats, ships, cargo), stores snapshots in a SQLite database, and generates day-over-day diff reports showing progress and changes. Perfect for tracking fleet development and agent growth.

## Features

### Core Functionality
- **📊 State Snapshots** - Captures comprehensive agent state (credits, skills, stats, ships, cargo, storage)
- **💾 Persistent Storage** - Stores snapshots in SQLite database with date indexing
- **📈 Diff Reports** - Computes day-over-day changes with detailed breakdowns
- **📄 Multiple Formats** - Generates both Markdown and HTML reports
- **🔄 Incremental Updates** - Compares with previous day's data for meaningful diffs

### Tracked Metrics
- **Credits** - Wallet and storage credit changes
- **Skills** - Skill level progression with before/after values
- **Stats** - Game statistics (ore mined, distance traveled, etc.)
- **Ships** - Ship acquisitions and upgrades
- **Cargo** - Current cargo status
- **Storage** - Storage inventory changes
- **Location** - System and station changes
- **Faction** - Faction ID and rank tracking

### Report Features
- **Markdown Reports** - Human-readable with emoji indicators
- **HTML Reports** - Styled reports with navigation between dates
- **Date Navigation** - Links to previous/next day reports
- **Agent Filters** - Include/exclude specific agents
- **Report-Only Mode** - Regenerate reports without recapturing data

## Quick Start

### Basic Usage

```bash
# Capture snapshots and generate reports for all agents
go run ./cmd/daily-summary

# Generate reports from existing data (skip capture)
go run ./cmd/daily-summary -report-only

# Specific agents only
go run ./cmd/daily-summary -agents miner-1,explorer-2
```

### Building

```bash
# Build the binary
go build -o bin/daily-summary ./cmd/daily-summary

# Run the built binary
./bin/daily-summary
```

## Command-Line Flags

```
-db string
    SQLite database path (default "data/daily-summary.db")

-output string
    Report output base path (default "data/reports/daily-summary-YYYY-MM-DD")

-agents string
    Comma-separated agent filter (default: all from data/agents/)

-delay int
    Delay in seconds between agent connections (default 3)

-report-only
    Skip data collection, regenerate report from latest DB data (default false)
```

## Examples

### Example 1: Daily Fleet Summary

```bash
go run ./cmd/daily-summary
```

**Output:**
```
[daily-summary] 2026/02/23 09:00:00 Agents: 12 total
[daily-summary] 2026/02/23 09:00:01 Capturing miner-1...
[daily-summary] 2026/02/23 09:00:05 Capturing explorer-2...
[daily-summary] 2026/02/23 09:00:09 Capturing trader-1...
...
[daily-summary] 2026/02/23 09:01:30 Comparing 2026-02-23 vs 2026-02-22 (12 previous snapshots)
[daily-summary] 2026/02/23 09:01:31 Markdown report: data/reports/daily-summary-2026-02-23.md
[daily-summary] 2026/02/23 09:01:32 HTML report: data/reports/daily-summary-2026-02-23.html
```

### Example 2: Report-Only Mode

```bash
# Regenerate today's report without recapturing data
go run ./cmd/daily-summary -report-only
```

Useful for:
- Adjusting report format
- Regenerating after database updates
- Quick report viewing

### Example 3: Custom Agent Selection

```bash
# Only mining fleet
go run ./cmd/daily-summary -agents miner-1,miner-2,miner-3

# Specific agents
go run ./cmd/daily-summary -agents explorer-7,trader-1
```

### Example 4: Custom Output Location

```bash
# Output to specific directory
go run ./cmd/daily-summary -output reports/fleet-summary-2026-02-23
```

### Example 5: Adjusted Timing

```bash
# Slower capture for large fleets
go run ./cmd/daily-summary -delay 5

# Faster capture for small fleets
go run ./cmd/daily-summary -delay 1
```

## Report Formats

### Markdown Report

Human-readable format with emoji indicators and clear diff visualization.

**Sample:**
```markdown
# Daily Fleet Summary - 2026-02-23

Comparing 2026-02-23 vs 2026-02-22

## miner-1 (Miner)

*Credits: +450.00 (total: 1,250.00)*
*Storage Credits: +0.00 (total: 500.00)*

### Skills
- ⛏️ Mining: 4 → 5 (+15.2 XP)
- 🔧 Engineering: 3 → 3 (+2.1 XP)

### Stats
- Ore Mined: +250.0
- Distance Traveled: +15.2 ly

### Ships
- Changed: Dart → Prospector

### Storage Items
- iron_ore: +100.0
- copper_ore: +50.0

## explorer-7 (Explorer)

*Credits: +120.00 (total: 890.00)*
*Storage Credits: -50.00 (total: 200.00)*

### Skills
- 🔭 Science: 2 → 3 (+8.5 XP)

### Stats
- Systems Discovered: +3
- Jumps: +12

...
```

### HTML Report

Styled HTML with:
- Responsive design
- Color-coded changes (green for gains, red for losses)
- Navigation to previous/next days
- Collapsible agent sections
- Hover tooltips for details

**Features:**
- **Date Navigation** - Links to previous/next day reports
- **Search** - Find specific agents or metrics
- **Print-Friendly** - Optimized for printing

## Database Schema

```sql
CREATE TABLE IF NOT EXISTS snapshots (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    date TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    snapshot_json TEXT NOT NULL,
    captured_at TEXT NOT NULL,
    UNIQUE(date, agent_id)
);
CREATE INDEX IF NOT EXISTS idx_snapshots_date ON snapshots(date);
CREATE INDEX IF NOT EXISTS idx_snapshots_agent ON snapshots(agent_id);
```

## How It Works

### Process Flow

```
1. Resolve Agent List
   ↓
2. Open Database
   ↓
3. Connect to Each Agent
   ↓
4. Capture State Snapshot
   ↓
5. Store in Database
   ↓
6. Load Today's & Previous Snapshots
   ↓
7. Compute Diffs
   ↓
8. Generate Reports
```

### Snapshot Process

For each agent:
1. **Connect** - Establish WebSocket connection
2. **Login** - Authenticate with agent credentials
3. **Capture** - Extract current state
4. **Store** - Save to database with date stamp

### Diff Computation

Compares two snapshots:
- **Credits** - Calculate delta (current - previous)
- **Skills** - Show level changes and XP gains
- **Stats** - Show stat increases
- **Ships** - Detect ship changes
- **Storage** - Compute inventory deltas
- **Location** - Track movement

## Configuration

### Agent Directory Structure

```
data/agents/
├── miner-1/
│   └── credentials.json
├── explorer-2/
│   └── credentials.json
└── trader-1/
    └── credentials.json
```

### Database Location

Default: `data/daily-summary.db`

Custom: `-db /path/to/database.db`

### Report Location

Default: `data/reports/daily-summary-YYYY-MM-DD.{md,html}`

Custom: `-output /path/to/report-base`

## Automation

### Cron Job (Linux/Mac)

```bash
# Run daily at 9 AM
0 9 * * * cd /path/to/spacemolt && go run ./cmd/daily-summary
```

### Windows Task Scheduler

```batch
schtasks /create /tn "SpaceMolt Daily Summary" /tr "cd C:\path\to\spacemolt && go run ./cmd/daily-summary" /sc daily /st 09:00
```

### Systemd Timer (Linux)

```ini
# /etc/systemd/system/spacemolt-daily-summary.service
[Unit]
Description=SpaceMolt Daily Summary
After=network.target

[Service]
Type=oneshot
WorkingDirectory=/path/to/spacemolt
ExecStart=/usr/local/go/bin/go run ./cmd/daily-summary

[Install]
WantedBy=multi-user.target
```

```ini
# /etc/systemd/system/spacemolt-daily-summary.timer
[Unit]
Description=Run SpaceMolt Daily Summary daily at 9 AM

[Timer]
OnCalendar=*-*-* 09:00:00
Persistent=true

[Install]
WantedBy=timers.target
```

## Troubleshooting

### Issue: "Failed to resolve agents"

**Cause:** No agents found in data/agents directory.

**Solution:**
1. Verify agent directories exist
2. Check credentials.json files are present
3. Ensure agents-dir is correct

### Issue: "Connection failed for agent-X"

**Cause:** Agent credentials invalid or network issue.

**Solution:**
1. Check agent credentials
2. Verify network connectivity
3. Check game server status
4. Review agent-specific error messages

### Issue: "No previous data found"

**Cause:** First time running or database is empty.

**Solution:**
- This is normal for first run
- Baseline report will be generated
- Next run will show diffs

### Issue: "Database locked"

**Cause:** Another process is using the database.

**Solution:**
1. Close other database connections
2. Check for other running instances
3. Use WAL mode for better concurrency

## Performance

### Typical Performance

- **Capture Time** - 3-5 seconds per agent (configurable delay)
- **Database Size** - ~10 KB per snapshot
- **Report Generation** - < 1 second for 20 agents

### Scaling

- **Small Fleet** (1-10 agents) - < 1 minute
- **Medium Fleet** (10-50 agents) - 1-3 minutes
- **Large Fleet** (50+ agents) - 3-10 minutes

## Best Practices

### Daily Automation

```bash
# Run at consistent time daily
0 9 * * * cd /path/to/spacemolt && go run ./cmd/daily-summary
```

### Weekly Summaries

```bash
# Generate weekly comparison
# Use report-only mode with date range queries
```

### Backup Database

```bash
# Regular database backups
cp data/daily-summary.db backups/daily-summary-$(date +%Y%m%d).db
```

## Related Documentation

- [Game Client](../../pkg/game/) - Game client API reference
- [Agent Management](../../docs/AGENT_MANAGEMENT.md) - Agent setup guide
- [Database Schema](../../docs/DATABASE_SCHEMA.md) - Database documentation

## License

Part of the SpaceMolt project.
