# Data Scraper Catalog API Updates

## Summary

Updated `cmd/data-scraper` to work with the new SpaceMolt game server API that uses a unified `catalog` endpoint for reference data instead of separate `get_ships`, `get_recipes`, and `get_skills` endpoints.

## Changes Made

### 1. New Catalog-Based Endpoints

The scraper now supports the following new catalog-based endpoints:

- **`ship_catalog`** - Scrapes all ship type definitions from catalog
- **`skill_defs`** - Scrapes all skill definitions from catalog
- **`recipes`** - Scrapes all recipe definitions from catalog (changed from old `get_recipes`)
- **`items`** - Scrapes all item definitions from catalog

### 2. Updated Existing Endpoints

- **`skills`** - Now scrapes player's current skill levels and XP (changed from scraping all definitions)
- **`ships`** - Still uses `shipyard_showroom` for station-specific ship availability

### 3. New Scrape Methods

Added to `cmd/data-scraper/main.go`:

```go
func (s *Scraper) scrapeSkillDefinitions() error
func (s *Scraper) scrapeRecipeDefinitions() error
func (s *Scraper) scrapeShipCatalog() error
func (s *Scraper) scrapeItemDefinitions() error
```

Each method:
- Sends a `catalog` message with appropriate `type` parameter
- Requests larger page size (100) for comprehensive data
- Saves to `catalog_<type>.json` files

### 4. Updated Documentation

Updated `cmd/data-scraper/README.md` with:
- New endpoint descriptions
- Clarification between player skills vs skill definitions
- New section explaining API changes
- Updated output file listings
- Documentation for new catalog endpoints

## API Changes

### Old API (Removed)
- `get_skills` - Returned all skill definitions
- `get_recipes` - Returned all recipe definitions
- `get_ships` - Returned all ship definitions

### New API (Current)
- `get_skills` - Returns only player's current skill levels and XP
- `catalog` - Unified endpoint for reference data with parameters:
  - `type`: "ships" | "skills" | "recipes" | "items"
  - `page`: Page number (default: 1)
  - `page_size`: Results per page (default: 20, max: 50)
  - `category`: Filter by category
  - `search`: Text search across name and description
  - `id`: Get specific entry by ID

## Usage Examples

### Scrape All Endpoints
```bash
go run cmd/data-scraper/main.go craftsman-1
```

This will scrape:
- Status & Player Info
- Ship Info
- Current POI
- System Data
- Map Data
- Market Listings
- Shipyard Showroom (station-specific)
- Ship Catalog (all ship types)
- Nearby Players
- Player Skills (your levels)
- Skill Definitions (all skills)
- Recipe Definitions (all recipes)
- Item Definitions (all items)
- Wrecks
- Drones
- Base Info
- Faction Info
- Captain's Log

### Scrape Specific Catalog Type
```bash
# Get all ship definitions
go run cmd/data-scraper/main.go craftsman-1 ship_catalog

# Get all skill definitions
go run cmd/data-scraper/main.go craftsman-1 skill_defs

# Get all recipe definitions
go run cmd/data-scraper/main.go craftsman-1 recipes

# Get all item definitions
go run cmd/data-scraper/main.go craftsman-1 items
```

### Scrape Player Skills Only
```bash
go run cmd/data-scraper/main.go craftsman-1 skills
```

## Output Files

The scraper now creates the following files in `data/game-api/<agent-id>/`:

```
get_status.json           # Player status and info
get_ship.json             # Current ship details
get_poi.json              # Current POI information
get_system.json           # Current system data
get_map.json              # Galaxy map data
get_listings.json         # Market listings
get_ships.json            # Shipyard showroom (station-specific)
catalog_ships.json        # All ship type definitions
get_nearby.json           # Nearby players
get_skills.json           # Player's skill levels and XP
catalog_skills.json       # All skill definitions
catalog_recipes.json      # All recipe definitions
catalog_items.json        # All item definitions
get_wrecks.json           # Wrecks in current system
get_drones.json           # Drones in current system
get_base.json             # Base/station information
faction_info.json         # Faction information (if in faction)
captains_log_list.json    # Captain's log entries
```

## Testing

- ✅ Code compiles without errors
- ✅ Passes `golangci-lint` with 0 issues
- ✅ Help text displays correctly
- ✅ All new methods added and integrated

## Benefits of Catalog API

1. **Unified Interface** - Single endpoint for all reference data
2. **Pagination** - Handle large datasets efficiently
3. **Filtering** - Filter by category for specific data
4. **Search** - Text search across names and descriptions
5. **Consistency** - Same structure for all catalog types

## Migration Notes

For code using the old data-scraper output:

1. **Player Skills** - Use `get_skills.json` instead of old format
2. **Skill Definitions** - Use `catalog_skills.json` instead of old `get_skills.json`
3. **Recipe Definitions** - Use `catalog_recipes.json` instead of old `get_recipes.json`
4. **Ship Definitions** - Use `catalog_ships.json` instead of old `get_ships.json`

The catalog responses have a different structure:
```json
{
  "items": [...],      // Array of catalog entries
  "page": 1,
  "page_size": 100,
  "message": "..."
}
```

## Future Enhancements

Potential improvements:
- Add support for category filtering in catalog scrapes
- Add support for text search in catalog scrapes
- Add pagination support for large catalogs (currently limited to page_size=100)
- Add combined catalog scrape that gets all types with pagination
