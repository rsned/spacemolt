#!/bin/bash
# Import all catalog data from a trader directory into the database
# Usage: ./scripts/import-all-catalogs.sh <trader-number>

set -e

if [ -z "$1" ]; then
    echo "Usage: $0 <trader-number>"
    echo "Example: $0 trader-1"
    exit 1
fi

TRADER_DIR="data/game-api/$1"

if [ ! -d "$TRADER_DIR" ]; then
    echo "Error: Directory $TRADER_DIR does not exist"
    exit 1
fi

echo "Importing catalog data from $TRADER_DIR..."

# Import items
if [ -f "$TRADER_DIR/catalog_items.json" ]; then
    echo "→ Importing items..."
    ./bin/import-catalog-items "$TRADER_DIR/catalog_items.json"
else
    echo "⚠ catalog_items.json not found, skipping"
fi

# Import ships
if [ -f "$TRADER_DIR/catalog_ships.json" ]; then
    echo "→ Importing ships..."
    ./bin/import-catalog-ships "$TRADER_DIR/catalog_ships.json"
else
    echo "⚠ catalog_ships.json not found, skipping"
fi

# Import skills
if [ -f "$TRADER_DIR/catalog_skills.json" ]; then
    echo "→ Importing skills..."
    ./bin/import-catalog-skills "$TRADER_DIR/catalog_skills.json"
else
    echo "⚠ catalog_skills.json not found, skipping"
fi

# Import recipes
if [ -f "$TRADER_DIR/catalog_recipes.json" ]; then
    echo "→ Importing recipes..."
    ./bin/import-catalog-recipes "$TRADER_DIR/catalog_recipes.json"
else
    echo "⚠ catalog_recipes.json not found, skipping"
fi

echo "✓ All catalog data imported successfully!"
