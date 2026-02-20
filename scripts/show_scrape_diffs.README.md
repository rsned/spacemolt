# show_scrape_diffs.sh

Compare IDs between two JSON scrape files to see what's changed.

## Usage

```bash
./scripts/show_scrape_diffs.sh <old_file.json> <new_file.json>
```

## Examples

Compare old and new recipe scrapes:
```bash
./scripts/show_scrape_diffs.sh data/game-api/old/get_recipes.json data/game-api/new/catalog_recipes.json
```

Compare skill definitions from two different dates:
```bash
./scripts/show_scrape_diffs.sh data/game-api/craftsman-1/catalog_skills.json.20250201 data/game-api/craftsman-1/catalog_skills.json
```

## Output Format

The script shows differences in a format similar to `git diff`:

```
Comparing IDs between:
  Old: data/game-api/old/get_recipes.json (379 IDs)
  New: data/game-api/new/catalog_recipes.json (394 IDs)

Summary: 394 IDs (+15 added)

@@ -13,367 +13,382 @@

Detailed breakdown:
  Added IDs (15):
    + craft_ancillary_armor_repairer
    + craft_auto_targeting_system
    + craft_point_defense_1
    ...
  Unchanged IDs: 379
```

### Understanding the Output

- `@@ -13,367 +13,382 @@` - Shows where changes occurred in the sorted ID list
- `+ new_id` - IDs that were added (green in terminal)
- `- old_id` - IDs that were removed (red in terminal)

## Supported File Formats

The script automatically detects and handles multiple JSON formats:

1. **Array format**: `[{"id": "xxx", ...}, ...]`
2. **Items wrapper**: `{"items": [{"id": "xxx", ...}], ...}`
3. **Object keys format**: `{"recipes": {"recipe_id": {...}, ...}}`
4. **Nested extraction**: Falls back to searching for any `id` field

## Use Cases

- Track new recipes added to the game
- Monitor skill additions/changes
- Detect removed or deprecated items
- Validate data scraper updates
- Compare data from different time periods
- Verify catalog completeness

## Requirements

- `jq` - JSON processor (install via: `sudo apt-get install jq` or `brew install jq`)
- `bash` - Shell (version 4+)
- Standard Unix utilities: `diff`, `sort`, `comm`, `nl`, `wc`

## Exit Codes

- `0` - Success, files compared successfully
- `1` - Error (missing files, no IDs found, etc.)
