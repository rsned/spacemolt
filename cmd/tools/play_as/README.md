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
