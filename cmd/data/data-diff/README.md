# Data Diff Tool

Compare two JSON files from the data-scraper to detect additions, deletions, and modifications.

## Overview

This tool compares JSON files produced by the data-scraper and reports:
- **Additions**: New items/systems in the newer file
- **Deletions**: Items/systems removed from the newer file
- **Modifications**: Changes to existing items/systems (with `-changes` flag)

This is useful for detecting game updates, such as new skills, ships, modules, or systems added to the game.

## Features

- ✅ Compares catalog files (skills, ships, items, recipes, modules)
- ✅ Compares map files (systems)
- ✅ Colorized output for easy reading
- ✅ Optional field-level change detection
- ✅ Smart ID-based comparison (not line-based diff)

## Prerequisites

- Go 1.24 or later
- Two JSON files from data-scraper to compare

## Usage

Run the diff tool from the project root:

```bash
go run cmd/data/data-diff/main.go [options] <old-file> <new-file>
```

### Arguments

- `old-file` - Path to the previous/older JSON file
- `new-file` - Path to the newer JSON file

### Options

| Option | Shorthand | Description |
|--------|-----------|-------------|
| `-changes` | `-c` | Show field-level changes (not just additions/deletions) |

### Examples

```bash
# Compare skill catalogs between two scrapes
go run cmd/data/data-diff/main.go \
  data/game-api/craftsman-1.prev/catalog_skills.json \
  data/game-api/craftsman-1/catalog_skills.json

# Compare ship catalogs with detailed changes
go run cmd/data/data-diff/main.go -changes \
  data/game-api/craftsman-1.bak/catalog_ships.json \
  data/game-api/craftsman-1/catalog_ships.json

# Compare map data
go run cmd/data/data-diff/main.go \
  data/game-api/salvager-1.prev/get_map.json \
  data/game-api/salvager-1/get_map.json

# Quick compare of all catalog files
for f in catalog_*.json; do
  echo "Comparing $f..."
  go run cmd/data/data-diff/main.go \
    data/game-api/craftsman-1.prev/$f \
    data/game-api/craftsman-1/$f
done
```

## Output Format

The tool provides a colorized summary:

```
SUMMARY
━━━━━━━
  Additions:  3
  Deletions:  1
  Modified:   2

ADDITIONS
─────────
  + Radioactive Mining (radioactive_mining)
  + Hazard Suit Module (hazard_protection_module)
  + Mining Drone Mk II (mining_drone_mk2)

DELETIONS
─────────
  - Old Mining Laser (mining_laser_v1)

MODIFIED ITEMS
──────────────
  • mining_laser_v2
    bonus_per_level:
      - 2
      + 3
```

## Comparison Behavior

### Catalog Files (`catalog_*.json`)

- Compares items by their `id` field
- Tracks additions (new IDs in newer file)
- Tracks deletions (IDs missing from newer file)
- With `-changes`: tracks modifications to existing items

Supported catalog types:
- `catalog_skills.json`
- `catalog_ships.json`
- `catalog_items.json`
- `catalog_recipes.json`
- `catalog_modules.json`

### Map Files (`get_map.json`)

- Compares systems by their `system_id` field
- Tracks new systems added to the game
- Tracks systems removed from the game
- With `-changes`: tracks changes to system properties (connections, POI counts, etc.)

## Use Cases

### Detect Game Updates

After a game update, run the data-scraper and compare against a previous scrape:

```bash
# Step 1: Backup current data
cp -r data/game-api/craftsman-1 data/game-api/craftsman-1.pre-update

# Step 2: Wait for game update, then re-scrape
go run cmd/data/data-scraper/main.go craftsman-1

# Step 3: Compare all catalog files
for f in catalog_*.json get_map.json; do
  go run cmd/data/data-diff/main.go \
    data/game-api/craftsman-1.pre-update/$f \
    data/game-api/craftsman-1/$f
done
```

### Track Agent Progress

Compare snapshots over time to see what an agent has unlocked:

```bash
# Compare today's data with yesterday's
go run cmd/data/data-diff/main.go \
  data/game-api/explorer-1.yesterday/get_skills.json \
  data/game-api/explorer-1/get_skills.json
```

### Validate Data Integrity

Ensure data-scraped files are consistent:

```bash
# Compare two agents' scrapes (should be identical for catalog data)
go run cmd/data/data-diff/main.go \
  data/game-api/agent-1/catalog_skills.json \
  data/game-api/agent-2/catalog_skills.json
```

## Building

Build the diff tool as a standalone binary:

```bash
go build -o bin/data-diff cmd/data/data-diff/main.go
```

Then use it directly:

```bash
./bin/data-diff data/game-api/craftsman-1.prev/catalog_skills.json data/game-api/craftsman-1/catalog_skills.json
```

## Troubleshooting

### "unknown JSON format" Error

The tool expects specific JSON structures:
- Catalog files must have an `items` array at the root
- Map files must have a `systems` array at the root

If you see this error, check that:
- The file is a valid JSON file from data-scraper
- You're comparing the correct file types (catalog with catalog, map with map)

### No Changes Detected

If you expect changes but none are shown:
- Verify the file paths are correct
- Check file modification dates: `ls -l data/game-api/*/`
- Ensure you're comparing old → new (not reversed)

### Color Output Issues

If colors don't display correctly:
- The tool detects terminal support automatically
- Pipe output to `less -R` to preserve colors: `data-diff ... | less -R`
- Use `NO_COLOR=1` environment variable to disable colors

## Related Tools

- **data-scraper** (`cmd/data/data-scraper/`) - Scrapes JSON data from the game server
- **SPACEMOLT_AGENT_GUIDE.md** - Guide for autonomous agent gameplay
- **pkg/game/client.go** - WebSocket client implementation

## License

Part of the spacemolt-spacemolt project.

## Author

Created for SpaceMolt autonomous agent development.
