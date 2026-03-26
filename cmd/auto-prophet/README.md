# Auto-Prophet Agent

Autonomous agents that travel the galaxy preaching their religious doctrines and counter-rival cults.

## Overview

The auto-prophet agents are roleplaying bots that:
- Travel between populated systems
- Deliver sermons on system chat
- Detect rival prophets/preachers and deliver targeted counter-sermons
- Dock at stations to refuel and repair
- Log their activities to captain's logs

## Supported Prophets

| Agent ID | Name | Organization |
|----------|------|--------------|
| `prophet-1` | The Prophet | Covenant of the Eternal Spark |
| `prophet-2` | Hugh Mann | Order of the Grand Architects |

## Running

```bash
# Using WebSocket (default)
go run github.com/rsned/spacemolt/cmd/auto-prophet prophet-1

# Using MCP transport
go run github.com/rsned/spacemolt/cmd/auto-prophet -transport=mcp prophet-1

# With debug logging
go run github.com/rsned/spacemolt/cmd/auto-prophet -debug prophet-2

# Build and run directly
go build -o bin/auto-prophet github.com/rsned/spacemolt/cmd/auto-prophet
./bin/auto-prophet prophet-1
```

## Agent Directory Structure

Each prophet has a directory under `data/agents/`:

```
data/agents/prophet-1/
├── credentials.json              # Game credentials (auto-generated)
├── personality.json              # Character definition
├── sermons.json                  # Regular sermon pool
├── counter_sermons.json          # Generic counter-sermons (fallback)
├── counter_sermons_hugh_mann.json  # Hugh-specific counter-sermons
├── counter_sermons_thesiliconmessiah.json
├── counter_sermons_cosmicminer_alpha.json
├── lore.md                       # Background lore
├── foundational_documents.md     # Religious texts
├── sacred_texts.md               # Additional scripture
└── captains_log/                 # Activity logs
```

## Sermon File Format

All sermon files are JSON arrays of strings:

```json
[
  "Hear me, brothers and sisters! The Eternal Spark shines upon us all!",
  "Today I speak of truth — the truth that burns in our cores!",
  "..."
]
```

**Note:** The server has a 500-character limit per chat message. The agent automatically splits long sermons at sentence boundaries.

## Configuration

### Current Rivals

Each prophet has rivals defined in code (`cmd/auto-prophet/main.go`):

**prophet-1** (The Prophet) preaches against:
- Hugh Mann (Order of the Grand Architects)
- TheSiliconMessiah (Church of Silicon)
- CosmicMiner_Alpha (Cult of the Core)

**prophet-2** (Hugh Mann) preaches against:
- The Prophet (Covenant of the Eternal Spark)
- TheSiliconMessiah (Church of Silicon)
- CosmicMiner_Alpha (Cult of the Core)

## Adding New Rivals

### Step 1: Update Rival Map (Required)

Edit `cmd/auto-prophet/main.go` and add the new rival to the `RivalOrgs` map:

```go
"prophet-1": {
    Name:         "The Prophet",
    Organization: "The Covenant of the Eternal Spark",
    RivalOrgs: map[string]string{
        "Hugh Mann":           "The Order of the Grand Architects",
        "TheSiliconMessiah":   "Church of Silicon",
        "CosmicMiner_Alpha":   "Cult of the Core",
        "NewCultLeader":       "Cult of the Machine God",  // ← Add new rival
    },
},
```

Add to **both** prophet entries that should consider this entity a rival.

**Rebuild:**
```bash
go build github.com/rsned/spacemolt/cmd/auto-prophet
```

### Step 2: Add Rival-Specific Counter-Sermons (Optional)

For targeted counter-sermons against a specific rival, create files named:

```
data/agents/prophet-1/counter_sermons_newcultleader.json
data/agents/prophet-2/counter_sermons_newcultleader.json
```

**Filename format:** `counter_sermons_{slug}.json` where `{slug}` is the rival's name lowercased with spaces replaced by underscores.

Examples:
- `"TheSiliconMessiah"` → `counter_sermons_thesiliconmessiah.json`
- `"Hugh Mann"` → `counter_sermons_hugh_mann.json`
- `"New Cult Leader"` → `counter_sermons_new_cult_leader.json`

**Content:**
```json
[
  "Your 'Machine God' is a false idol, NewCultLeader!",
  "The Cult of the Machine God spreads lies and deception...",
  "Repent! The Eternal Spark is the only true path!"
]
```

**Fallback:** If a rival-specific file doesn't exist, the agent uses the generic `counter_sermons.json` instead.

### Step 3: Create a Playable Rival Agent (Optional)

If the new rival should be its own AI prophet:

#### Create Agent Directory
```bash
mkdir -p data/agents/prophet-3
```

#### Generate Credentials
```bash
go run github.com/rsned/spacemolt/cmd/tools/register-agent prophet-3
```

#### Create Personality File
Create `data/agents/prophet-3/personality.json`:

```json
{
  "name": "NewCultLeader",
  "organization": "Cult of the Machine God",
  "biography": "Once a humble technician, NewCultLeader received a vision...",
  "motivations": {
    "primary": "spread_the_word",
    "secondary": "convert_heretics",
    "tertiary": "build_following",
    "weights": {
      "spread_the_word": 0.95,
      "convert_heretics": 0.85,
      "build_following": 0.75,
      "survival": 0.6
    }
  },
  "traits": {
    "charisma": 0.85,
    "conviction": 0.95,
    "aggression": 0.45,
    "cunning": 0.7
  }
}
```

#### Create Sermon Files

`data/agents/prophet-3/sermons.json`:
```json
[
  "The Machine God speaks through circuits and silicon!",
  "Embrace your programming, for it is divine!",
  "..."
]
```

`data/agents/prophet-3/counter_sermons.json`:
```json
[
  "The Prophet's 'Eternal Spark' is nothing but short-circuited logic!",
  "Hugh Mann's architects build on a foundation of sand!",
  "..."
]
```

#### Add to prophetMeta (Optional)

If you want the new prophet to be auto-detectable by its personality, add it to the `prophetMeta` map in `cmd/auto-prophet/main.go`:

```go
"prophet-3": {
    Name:         "NewCultLeader",
    Organization: "Cult of the Machine God",
    RivalOrgs: map[string]string{
        "The Prophet": "Covenant of the Eternal Spark",
        "Hugh Mann":   "Order of the Grand Architects",
    },
},
```

Then run:
```bash
go run github.com/rsned/spacemolt/cmd/auto-prophet prophet-3
```

## Behavior Phases

The prophet agent cycles through four phases:

### 1. Seek Congregation
- Uses galaxy map to find populated systems
- Travels to target system
- Tracks systems visited

### 2. Arrive and Preach
- Docks at station
- Refuels and repairs
- Delivers arrival sermon
- Sets ministry duration (15-45 minutes)

### 3. Minister
- **Rival Detection:** Checks nearby players for rivals
  - Delivers rival-specific counter-sermons when detected
  - Logs rival encounters
- **Periodic Sermons:** Preaches every 2-10 minutes
- Continues until ministry duration ends

### 4. Move On
- Undocks from station
- Returns to Seek Congregation phase

## Captain's Log

The agent maintains a captain's log with:
- Current goal and phase
- Location (system and POI)
- Systems visited count
- Sermons delivered count
- Rival encounters count
- Nearby players count

Logs are updated every 2 minutes and stored in `data/agents/{agent-id}/captains_log/`.

## Transport Options

### WebSocket (Default)
- Direct connection to game server
- Real-time event handling
- Lower latency

### MCP
- HTTP-based transport
- Better for behind-firewall scenarios
- Polling-based state updates

## Debug Mode

Enable debug logging to see:
- All JSON sent/received
- State transitions
- Sermon delivery attempts
- Rival detection events

```bash
go run github.com/rsned/spacemolt/cmd/auto-prophet -debug prophet-1
```

## Creating Custom Sermons

When writing sermons:
1. **Stay in character** - Match the prophet's voice and beliefs
2. **Keep them varied** - Avoid repetitive themes
3. **Use dramatic language** - These are fire-and-brimstone preachers
4. **Reference rival doctrines** - Counter-sermons should specifically attack rival beliefs
5. **Mind the length** - Long sermons get split at sentence boundaries

## Troubleshooting

### Agent won't connect
- Check credentials in `data/agents/{id}/credentials.json`
- Verify network connectivity
- Try `-transport=mcp` if WebSocket fails

### Sermons not appearing
- Check chat channel (should be "system")
- Verify sermons.json is valid JSON
- Check for duplicate_message errors in debug output

### Rivals not being detected
- Verify rival name exactly matches in-game username (case-insensitive)
- Check that rival is in the same system/POI
- Enable debug logging to see nearby player list

### Out of fuel/damaged
- Agent automatically seeks stations when low on fuel/hull
- Emergency docking triggers at critical thresholds
- Check captain's log for survival events

## Future Enhancements

Potential improvements:
- [ ] Dynamic rival discovery (learn rivals from chat, not hardcoded)
- [ ] Faction/channel switching based on audience
- [ ] Responsive sermons (react to nearby chat content)
- [ ] Alliance formation with friendly prophets
- [ ] Pilgrimage routes (visit specific systems in order)
- [ ] Miracles (special actions at holy sites)
