#!/bin/bash
#
# Check for POI resources that don't appear in any crafting recipe.
# Run periodically to spot new resources the game adds that aren't yet craftable.
#
# Usage: ./scripts/check-unused-resources.sh

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$SCRIPT_DIR/.."
DB="$ROOT_DIR/data/spacemolt-knowledge.db"
RECIPES="$ROOT_DIR/data/game-api/craftsman-10/get_recipes.json"

if [ ! -f "$DB" ]; then
    echo "❌ Database not found: $DB"
    exit 1
fi

if [ ! -f "$RECIPES" ]; then
    echo "❌ Recipe file not found: $RECIPES"
    echo "   Run the data scraper to fetch recipe data first."
    exit 1
fi

# Extract all recipe input item_ids
recipe_inputs=$(python3 -c "
import json, sys
with open('$RECIPES') as f:
    data = json.load(f)
inputs = set()
for name, r in data['recipes'].items():
    for ing in (r.get('inputs') or []):
        inputs.add(ing['item_id'])
for i in sorted(inputs):
    print(i)
")

# Get all distinct resource_ids from poi_resources
poi_resources=$(sqlite3 "$DB" "SELECT DISTINCT resource_id FROM poi_resources ORDER BY resource_id;")

# Find resources not in any recipe
unused=$(comm -23 <(echo "$poi_resources" | sort) <(echo "$recipe_inputs" | sort))

if [ -z "$unused" ]; then
    echo "✅ All POI resources are used in at least one recipe."
    exit 0
fi

echo "⚠️  POI resources NOT used in any recipe:"
echo ""

for res in $unused; do
    echo "  $res"
    sqlite3 "$DB" "
        SELECT '    ' || p.name || ' (' || p.id || ', ' || p.type || ', richness: ' || r.richness || ')'
        FROM poi_resources r
        JOIN pois p ON r.poi_id = p.id
        WHERE r.resource_id = '$res'
        ORDER BY r.richness DESC;
    "
    echo ""
done

# Reverse check: raw material recipe inputs (ore_/gas_) not found in any POI
missing=$(comm -23 <(echo "$recipe_inputs" | grep -E '^(ore_|gas_)' | sort) <(echo "$poi_resources" | sort))

if [ -z "$missing" ]; then
    echo "✅ All recipe inputs are available from at least one POI."
else
    echo "⚠️  Recipe inputs NOT found at any POI:"
    echo ""
    for res in $missing; do
        # Find which recipes use this input
        recipes_using=$(python3 -c "
import json
with open('$RECIPES') as f:
    data = json.load(f)
for name, r in sorted(data['recipes'].items()):
    for ing in (r.get('inputs') or []):
        if ing['item_id'] == '$res':
            print('    ' + name)
            break
")
        echo "  $res"
        echo "$recipes_using"
        echo ""
    done
fi

# Summary
total_poi=$(echo "$poi_resources" | wc -l)
unused_count=$(echo "$unused" | wc -l)
total_raw=$(echo "$recipe_inputs" | grep -cE '^(ore_|gas_)' || true)
missing_count=$(echo "$missing" | grep -c . || true)
echo "Summary: $unused_count/$total_poi POI resource types have no recipe."
echo "         $missing_count/$total_raw raw material recipe inputs (ore_/gas_) not found at any POI."
