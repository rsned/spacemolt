# play_as

Interactive game terminal for playing Space Molt as a specific agent. Connects via MCP transport and provides a shell-like REPL for sending game commands and viewing responses in real time.

## Usage

```
play_as [flags] <agent-id>
```

### Examples

```bash
play_as explorer-1
play_as --debug explorer-1
play_as --config ~/my-config.yaml miner-2
```

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--debug` | `false` | Enable debug logging (show library logs and sent/received JSON) |
| `--config` | `~/.config/spacemolt/play_as.yaml` | Path to config file |

## Features

- Full command set: navigation, mining, combat, trading, crafting, storage, and more
- Command history with up/down arrow keys (session-scoped)
- Configurable statusline showing ship vitals, location, and game state
- Multiple output formats: `raw`, `json` (default), `styled`
- Color-coded gauges for fuel, hull, shield, and cargo

## REPL Commands

Type `help` in the terminal for a full command list. A few highlights:

| Command | Description |
|---------|-------------|
| `status` | Get current player/ship status |
| `undock` / `dock` | Leave or enter a station |
| `travel <poi>` | Travel to a point of interest |
| `jump <system>` | Jump to another system |
| `mine` | Mine at current location |
| `sell <item> <qty>` | Sell items at station |
| `cargo` | View ship cargo |
| `set_format <mode>` | Change output format (`raw`, `json`, `styled`) |
| `state` | Show compact state summary |
| `help` | Full command list |
| `exit` | Quit the terminal |

## Crafting Smarts

Two commands help decide what to craft and what's blocking a target build.

### `craftable`

List every recipe you can build right now (cargo + current-station storage,
station-legal). Facility-only recipes (the ∞-can_make ones that require a
crafting facility, not a regular station) are hidden by default — pass
`--include-facility-only` to opt them back in.

```
craftable                          # immediately buildable, compact table
craftable --reachable              # also list recipes reachable via intermediate crafts
craftable --category Refining      # substring filter on category
craftable --search lance           # substring filter on name and outputs
craftable --include-faction        # also count faction storage
craftable --include-facility-only  # include facility-only recipes (default: hidden)
craftable --include-hidden         # include recipes the server flags as hidden
craftable --detail                 # per-recipe drill-down (no table)
craftable --recipe <id> --detail   # detail for one specific recipe
craftable --refresh                # bypass session recipe-catalog cache
craftable --max 200                # widen the table (default 100)
```

### `plan <recipe-or-item-id> [qty]`

Gap analysis to a target. If the agent has everything, prints the literal
`craft …` command. Otherwise shows the shortfall by item:

```
plan alloy_titanium_ingot 10
plan titanium_alloy                # accepts item_id; picks lowest-skill alternative
plan build_emergency_warp_device --reachable   # flat ore/gas shortfall via BOM
```

`--reachable` needs the crafting DB (`bill_of_materials` table). Set
`CRAFTING_DB=path/to/crafting.db` or keep the default
`../../spacemolt-crafting-server/database/crafting.db`. Without the DB the
command falls back to a friendly "BOM unavailable" message.

## Variable Tokens

Commands may contain `$TOKEN$` placeholders that resolve from live game state
right before each command runs. This makes loops portable across systems and
agents.

**POI-type tokens** resolve to a POI in the current system whose type matches
the (lowercased) token name. When more than one POI of that type exists, the one
whose ID sorts first alphabetically is used.

| Token | Resolves to |
|-------|-------------|
| `$STATION$` | first `station` POI ID |
| `$ASTEROID_BELT$` | first `asteroid_belt` POI ID |
| `$GAS_CLOUD$` | first `gas_cloud` POI ID |
| `$ICE_FIELD$` | first `ice_field` POI ID |
| `$<TYPE>$` | first POI of type `<type>` (any known POI type) |

**State tokens:**

| Token | Resolves to |
|-------|-------------|
| `$SYSTEM$` | current system ID |
| `$SHIP$` | active ship ID |
| `$CREDITS$` | current credit balance (integer) |

If a token can't be resolved (no matching POI, or an unknown name), the command
fails — and inside a loop the **entire loop aborts immediately**, even under
`-f`.

Example:

```
loop 10 { travel $ASTEROID_BELT$; mine; travel $STATION$; sell_all }
```

## Scripts

Save reusable command scripts and run them later — the same script works for any
agent because tokens resolve to each agent's own system.

| Command | Description |
|---------|-------------|
| `run <name\|path>` | Run a script. A bare name is looked up in the per-agent dir then the shared dir; an argument with a `/` or ending in `.smolt` is loaded as a literal path. |
| `scripts` | List available scripts (per-agent entries shadow same-named shared ones). |
| `save <name>` | Save the **last command** to the shared scripts dir as `<name>.smolt`. |

Script files are plain command text — multi-line `loop` blocks, `;`/newline
separators, `#` comments, and `$TOKEN$` variables all work. Locations:

- Shared: `data/scripts/<name>.smolt`
- Per-agent override: `data/agents/<id>/scripts/<name>.smolt`

## Configuration

Config is loaded from `~/.config/spacemolt/play_as.yaml` by default (override with `--config`). All fields are optional -- missing fields use sensible defaults. A missing config file is not an error; the tool works out of the box.

### Full Sample Config

```yaml
# Output format for server responses: "raw", "json", or "styled"
output_format: "json"

statusline:
  # Set to false to disable the statusline entirely
  enabled: true

  # Format string with {segment} placeholders. Reorder, remove, or add
  # separators and literal text however you like.
  format: "{agent} | {system} {poi} {dock} | {ship_class} | Hull:{hull} Shld:{shield} Fuel:{fuel} Cargo:{cargo} | Tick:{tick}"

  # How percentage gauges render:
  #   "bar"      - [████░░] 73%  (default)
  #   "numeric"  - 73%
  #   "bar_only" - [████░░]
  bar_style: "bar"

  # Number of characters inside the gauge brackets
  bar_width: 6

  # Percentage thresholds for color coding (applies to hull, shield, fuel,
  # cargo, cpu, power). Values at or below the threshold get that color.
  # Above "good" = bright green.
  thresholds:
    critical: 15    # red
    warning: 35     # yellow
    good: 65        # green

  # Map security status strings to color names for the {system}, {system_id},
  # and {security} segments.
  security_colors:
    "High Security": green
    "Medium Security": yellow
    "Low Security": red
    "Lawless": bright_red
    "Pirate Stronghold": magenta

  # Text/emoji indicators for dock and alert states
  indicators:
    docked: "⚓Docked"
    in_space: "🚀Space"
    in_combat: "⚔COMBAT"
    cloaked: "👻Cloak"
```

### Minimal Config Examples

Disable the statusline:

```yaml
statusline:
  enabled: false
```

Use numeric gauges with wider bars:

```yaml
statusline:
  bar_style: "numeric"
```

Custom compact format showing only essentials:

```yaml
statusline:
  format: "{system} {dock} | Fuel:{fuel} Cargo:{cargo}"
```

Styled output with a combat-focused layout:

```yaml
output_format: "styled"
statusline:
  format: "{agent} | {system} {dock} | Hull:{hull} Shld:{shield} | {combat}{cloak} | Tick:{tick}"
```

## Statusline Segments

The statusline renders after each command, just before the `$ ` prompt. Segments are placeholders in the format string, written as `{name}`.

### Identity

| Segment | Description | Example |
|---------|-------------|---------|
| `{agent}` | Agent ID (from CLI argument) | `explorer-1` |
| `{player}` | Player username | `CaptainNova` |
| `{empire}` | Empire name | `Terran` |
| `{faction}` | Faction ID (empty if none) | `shadow-fleet` |

### Location

| Segment | Description | Color |
|---------|-------------|-------|
| `{system}` | System name | Colored by security status |
| `{system_id}` | Raw system ID | Colored by security status |
| `{security}` | Security status text | Colored by security status |
| `{poi}` | Current POI ID | None |
| `{dock}` | Dock/space indicator | None (uses configured indicator text) |

Security colors are configured in `security_colors` and apply to `{system}`, `{system_id}`, and `{security}`.

### Ship Gauges

| Segment | Description | Color Logic |
|---------|-------------|-------------|
| `{hull}` | Hull percentage | Low = bad (red) |
| `{shield}` | Shield percentage | Low = bad (red) |
| `{fuel}` | Fuel percentage | Low = bad (red) |
| `{cargo}` | Cargo usage percentage | High = bad (red, inverted) |
| `{cpu}` | CPU usage percentage | High = bad (red, inverted) |
| `{power}` | Power usage percentage | High = bad (red, inverted) |

Gauge segments are color-coded by thresholds. For `{hull}`, `{shield}`, and `{fuel}`, low values are critical. For `{cargo}`, `{cpu}`, and `{power}`, the logic is inverted -- high usage triggers warning/critical colors.

**Threshold colors (default values):**

| Range | Color |
|-------|-------|
| 0-15% | Red (critical) |
| 16-35% | Yellow (warning) |
| 36-65% | Green (good) |
| 66-100% | Bright green (excellent) |

For inverted segments (cargo, cpu, power), these ranges apply to the *remaining* capacity: 85% cargo used means 15% free, which shows as red.

### Status

| Segment | Description | Notes |
|---------|-------------|-------|
| `{tick}` | Current game tick | Plain text |
| `{credits}` | Credit balance with commas | e.g., `1,234,567` |
| `{combat}` | Combat indicator | Only visible when in combat |
| `{cloak}` | Cloak indicator | Only visible when cloaked |
| `{nearby}` | Count of nearby players | Plain text |

The `{combat}` and `{cloak}` segments render as empty strings when inactive. Keep this in mind when placing them next to `|` separators -- adjacent separators may appear if neither is active. Place them together or at the end of the format string to avoid gaps.

### Example Output

Default format, docked at a station with low fuel:

```
explorer-1 | Sol station-alpha ⚓Docked | Frigate | Hull:[██████] 100% Shld:[██████] 100% Fuel:[██░░░░] 34% Cargo:[█░░░░░] 12% | Tick:4821
$
```

Numeric style:

```
explorer-1 | Sol station-alpha ⚓Docked | Frigate | Hull:100% Shld:100% Fuel:34% Cargo:12% | Tick:4821
$
```

## Available Colors

These color names can be used in `security_colors`:

`red`, `bright_red`, `yellow`, `green`, `bright_green`, `magenta`, `cyan`, `white`, `gray`
