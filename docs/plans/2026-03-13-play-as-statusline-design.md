# play_as Statusline Design

## Overview

The play_as tool gets a persistent statusline that renders after each command completes, immediately before the `$ ` prompt. It pulls data from `client.GetState()` and formats it according to a user-configurable format string with named segments like `{fuel}`, `{system}`, `{dock}`.

- **Config location:** `--config` flag, defaulting to `~/.config/spacemolt/play_as.yaml`. If no config file exists, built-in defaults are used — the tool works out of the box with no config.
- **Update timing:** After every command completes, before the next prompt. No background goroutines or live updates.
- **Default bar style:** Mini bar (`[████░░] 73%`). Configurable to `numeric` (`73%`) or `bar_only` (`[████░░]`).

**Default format string:**
```
{agent} | {system} {poi} {dock} | {ship_class} | Hull:{hull} Shld:{shield} Fuel:{fuel} Cargo:{cargo} | Tick:{tick}
```

## Config File Structure

```yaml
statusline:
  enabled: true
  format: "{agent} | {system} {poi} {dock} | {ship_class} | Hull:{hull} Shld:{shield} Fuel:{fuel} Cargo:{cargo} | Tick:{tick}"
  bar_style: "bar"        # "bar" = [████░░] 73%, "numeric" = 73%, "bar_only" = [████░░]
  bar_width: 6            # number of characters inside the bar brackets

  thresholds:
    critical: 15
    warning: 35
    good: 65
    # above good = excellent (bright green)

  security_colors:
    "High Security": green
    "Medium Security": yellow
    "Low Security": red
    "Lawless": bright_red
    "Pirate Stronghold": magenta

  indicators:
    docked: "⚓Docked"
    in_space: "🚀Space"
    in_combat: "⚔COMBAT"
    cloaked: "👻Cloak"
```

**Color names** map to ANSI codes. Supported values: `red`, `bright_red`, `yellow`, `green`, `bright_green`, `magenta`, `cyan`, `white`, `gray`. Fixed set — no arbitrary ANSI codes or hex colors.

**All fields are optional.** Missing fields fall back to the defaults shown above. A completely empty or missing config file produces the same output as the full default config.

## Implementation Architecture

**New files:**
- `cmd/tools/play_as/statusline.go` — Segment expansion, bar rendering, color application, statusline formatting
- `cmd/tools/play_as/config.go` — Config struct, YAML loading, defaults

**Changes to existing files:**
- `cmd/tools/play_as/main.go` — Load config at startup, pass to `runREPL`; render statusline after each command before next prompt

**No new packages.** This is entirely local to the play_as tool — no changes to `pkg/game` or any shared code. The statusline reads from `client.GetState()` which already exists.

**Dependencies:** `gopkg.in/yaml.v3` for config parsing. ANSI color output via direct escape codes — no color library needed.

**Flow:**
1. `main()` loads config (flag path → file → defaults)
2. Config + agentID passed to `runREPL()`
3. After each command executes, `renderStatusline(client, config, agentID)` is called
4. `renderStatusline` calls `client.GetState()`, iterates through the format string, expands each `{segment}`, applies colors, prints the line
5. `$ ` prompt follows

**Segment expansion** is a simple string replacer — scan for `{name}`, look up in a map of segment functions, replace with the rendered output. Unknown segments render as-is (so `{unknown}` stays literal, no crash).

**Bar rendering** for a value like fuel 34/100:
```
[██░░░░] 34%
```
Uses `█` for filled and `░` for empty, width controlled by `bar_width`. The entire segment (bar + percentage text) gets colored by the threshold.

## Available Segments

### Percentage-based: `{hull}`, `{shield}`, `{fuel}`, `{cargo}`, `{cpu}`, `{power}`

- Compute `pct = current / max * 100` (handle max=0 as 0%)
- Color by thresholds: `<=critical` red, `<=warning` yellow, `<=good` green, above = bright green
- `{cargo}`, `{cpu}`, `{power}` are **inverted**: high usage = bad. So the threshold check flips — 85% cargo used gets red, 15% cargo used gets bright green

### Location: `{system}`, `{system_id}`, `{security}`, `{poi}`, `{dock}`

- `{system}` — system name, colored by security status via `security_colors` map
- `{system_id}` — raw system ID, same coloring
- `{security}` — security status text, same coloring
- `{poi}` — current POI ID from state, no special coloring
- `{dock}` — renders configured indicator string (`⚓Docked` or `🚀Space`)

### Identity: `{agent}`, `{player}`, `{empire}`, `{faction}`

- Plain text, no coloring

### Status: `{tick}`, `{credits}`, `{combat}`, `{cloak}`, `{nearby}`

- `{tick}` — current game tick number, plain text
- `{credits}` — formatted with commas (e.g., `1,234,567`)
- `{combat}` — shows indicator only when `state.InCombat || state.InBattle`, empty string otherwise
- `{cloak}` — shows indicator only when `state.Player.IsCloaked`, empty string otherwise
- `{nearby}` — count of nearby players, plain text

### Edge Cases

When a conditional segment (`{combat}`, `{cloak}`) is empty, adjacent `|` separators may look awkward (`|| Tick:42`). This is the user's responsibility to manage in their format string.

## Example Output

With defaults, after a command:
```
explorer-1 | Sol station-alpha ⚓Docked | Frigate | Hull:[██████] 100% Shld:[██████] 100% Fuel:[██░░░░] 34% Cargo:[█░░░░░] 12% | Tick:4821
$
```
