# Catalog Data Import Tools

These tools import game catalog data from JSON files into the SpaceMolt knowledge base database.

## Available Tools

### Individual Import Tools

- **`import-catalog-items`** - Imports item catalog into the `items` table
- **`import-catalog-ships`** - Imports ship class catalog into `ship_classes` and `ship_class_build_materials` tables
- **`import-catalog-skills`** - Imports skill catalog into the `skills` table
- **`import-catalog-recipes`** - Imports recipe catalog into `recipes`, `recipe_inputs`, and `recipe_outputs` tables

### Bulk Import Script

- **`scripts/import-all-catalogs.sh`** - Imports all catalog files from a trader directory at once

## Usage

### Individual Imports

```bash
# Import items
./bin/import-catalog-items data/game-api/trader-1/catalog_items.json

# Import ships
./bin/import-catalog-ships data/game-api/trader-1/catalog_ships.json

# Import skills
./bin/import-catalog-skills data/game-api/trader-1/catalog_skills.json

# Import recipes
./bin/import-catalog-recipes data/game-api/trader-1/catalog_recipes.json
```

### Bulk Import

```bash
# Import all catalogs from a specific trader directory
./scripts/import-all-catalogs.sh trader-1

# Import from trader-2
./scripts/import-all-catalogs.sh trader-2
```

## Database Location

By default, the tools use `data/spacemolt-knowledge.db`. You can override this with the `SPACEMOLT_DB` environment variable:

```bash
SPACEMOLT_DB=/path/to/custom.db ./bin/import-catalog-items data/game-api/trader-1/catalog_items.json
```

## Data Normalization

The import tools properly normalize data across multiple tables:

### Ships
- Main ship data → `ship_classes` table
- Build materials → `ship_class_build_materials` table (normalized)
- JSON arrays (required_skills, default_modules, flavor_tags) stored as TEXT

### Recipes
- Main recipe data → `recipes` table
- Input items → `recipe_inputs` table (normalized)
- Output items → `recipe_outputs` table (normalized)
- required_skills map stored as TEXT JSON

### Skills
- Main skill data → `skills` table
- JSON arrays/maps (xp_per_level, bonus_per_level, required_skills) stored as TEXT

### Items
- Item data → `items` table
- All fields stored as native SQL types

## Notes

- Items with empty IDs are automatically skipped (with a warning)
- All import operations are transactional - either all data imports or none
- Existing catalog data is replaced (DELETE + INSERT strategy)
- The tools respect the database schema defined in `scripts/sql/initialize_database.sql`
