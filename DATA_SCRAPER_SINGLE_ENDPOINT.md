# Data Scraper Single Endpoint Support - 2026-02-20

## Summary
Added support for scraping a single endpoint when specified as a command line argument, making the scraper more flexible and efficient for targeted data collection.

## Changes Made

### 1. Updated `main()` Function
Modified argument parsing to support optional endpoint parameter:
- Added second argument `endpoint` (optional)
- Updated initialization to check for endpoint argument
- Routes to either `scrapeAll()` or `scrapeOne()` based on arguments

### 2. Added `printUsage()` Function
Created comprehensive usage information:
- Shows agent-id and endpoint arguments
- Lists all 15 available endpoints with descriptions
- Provides practical examples
- Displays help when arguments are missing

### 3. Added `scrapeOne()` Method
New method for single endpoint scraping:
- Maps endpoint names to scrape functions
- Validates endpoint name and provides helpful error for invalid endpoints
- Scrapes only the specified endpoint
- Maintains same error handling as `scrapeAll()`

## Usage Examples

### Scrape All Endpoints
```bash
go run cmd/data-scraper/main.go craftsman-1
```

### Scrape Single Endpoint
```bash
# Status only
go run cmd/data-scraper/main.go craftsman-1 status

# Map data only
go run cmd/data-scraper/main.go craftsman-1 map

# Market listings only
go run cmd/data-scraper/main.go craftsman-1 listings

# Shipyard data only
go run cmd/data-scraper/main.go craftsman-1 ships
```

### Invalid Endpoint
```bash
go run cmd/data-scraper/main.go craftsman-1 invalid
# Output: Scraping failed: unknown endpoint: invalid
```

## Available Endpoints

| Endpoint | Description | Output File |
|----------|-------------|-------------|
| `status` | Status & Player Info | `get_status.json` |
| `ship` | Ship Info | `get_ship.json` |
| `poi` | Current POI | `get_poi.json` |
| `system` | System Data | `get_system.json` |
| `map` | Map Data | `get_map.json` |
| `listings` | Market Listings | `get_listings.json` |
| `ships` | Shipyard Showroom | `get_ships.json` |
| `nearby` | Nearby Players | `get_nearby.json` |
| `skills` | Skills | `get_skills.json` |
| `recipes` | Recipes | `get_recipes.json` |
| `wrecks` | Wrecks | `get_wrecks.json` |
| `drones` | Drones | `get_drones.json` |
| `base` | Base Info | `get_base.json` |
| `faction` | Faction Info | `faction_info.json` |
| `log` | Captain's Log | `captains_log_list.json` |

## Benefits

1. **Faster**: Only scrape what you need
2. **Efficient**: Reduce server load and network traffic
3. **Targeted**: Focus on specific data for debugging or testing
4. **Flexible**: Choose between full scrape or single endpoint
5. **User-friendly**: Clear help and error messages

## Testing

All functionality tested and working:
- ✅ Usage help displays correctly
- ✅ All 15 endpoints work individually
- ✅ Invalid endpoints show helpful error messages
- ✅ Scraping all endpoints still works
- ✅ Error handling maintained for failed endpoints
- ✅ Code passes `golangci-lint` without issues

## Code Quality

- ✅ Follows project Go 1.24 standards
- ✅ Proper error handling with helpful messages
- ✅ Clean code structure with clear separation
- ✅ Comprehensive documentation
- ✅ Backward compatible (old usage still works)
