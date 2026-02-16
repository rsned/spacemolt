# Base Data Integration Guide

The knowledge base now supports storing information about space stations, outposts, bases, and fortresses.

## Overview

Bases are structures located at POIs (Points of Interest) that provide services like:
- Markets (buying/selling items)
- Shipyard (purchasing ships)
- Refueling
- Repairs
- Crafting
- Storage
- And more...

## Database Schema

### Tables Created (Migration 6)

- **bases**: Core base information (ID, name, empire, defense level, etc.)
- **base_services**: Available services at each base (cloning, crafting, market, etc.)
- **base_facilities**: Special facilities at each base (refineries, factories, etc.)
- **base_market**: Items for sale at each base

## Type Definitions

```go
// SpaceBase represents knowledge about a space station, outpost, base, or fortress
type SpaceBase struct {
    ID              string                 // Unique base ID
    POIID           string                 // POI where this base is located
    Name            string                 // Base name
    Description     string                 // Base description
    Empire          string                 // Faction empire (solarian, etc.)
    DefenseLevel    int                    // Defense level (0-100)
    HasDrones       bool                   // Whether base has drone capability
    PublicAccess    bool                   // Whether base is publicly accessible
    Services        map[string]bool        // Available services
    Facilities      []string               // Available facilities
    Market          []BaseMarketItem       // Items for sale
    DiscoveredBy    string                 // Agent who discovered this base
    LastUpdatedTick int64                  // Game tick when last updated
}

// BaseMarketItem represents an item for sale at a base market
type BaseMarketItem struct {
    ID        string  // Unique listing ID
    ItemID    string  // Item being sold
    PriceEach float64 // Price per unit
    Quantity  int     // Available quantity
    IsNPC     bool    // Whether sold by NPC (true) or player (false)
}
```

## API Methods

### Saving Base Data

```go
import "github.com/rsned/spacemolt/pkg/knowledge"

// Method 1: Parse from raw JSON and save
rawJSON := client.GetRawJSON("base")
if rawJSON != nil {
    base, err := knowledge.BaseDataFromRawJSON(rawJSON, agentID, state.GetTick())
    if err != nil {
        logger.Printf("Failed to parse base data: %v", err)
        return err
    }
    if err := kb.RememberBase(ctx, *base); err != nil {
        logger.Printf("Failed to save base: %v", err)
        return err
    }
}

// Method 2: Construct manually and save
base := knowledge.SpaceBase{
    ID:              "sol_base",
    POIID:           "sol_station",
    Name:            "Confederacy Central Command",
    Description:     "The seat of Solarian government...",
    Empire:          "solarian",
    DefenseLevel:    100,
    HasDrones:       true,
    PublicAccess:    true,
    Services: map[string]bool{
        "market":    true,
        "shipyard":  true,
        "refuel":    true,
        "repair":    true,
        "crafting":  true,
    },
    Facilities: []string{
        "solarian_fusion_plant",
        "iron_refinery",
        "circuit_fabricator",
    },
    DiscoveredBy:    "my-agent",
    LastUpdatedTick: state.GetTick(),
}
if err := kb.RememberBase(ctx, base); err != nil {
    logger.Printf("Failed to save base: %v", err)
}
```

### Retrieving Base Data

```go
// Get base by ID
base, err := kb.GetBase(ctx, "sol_base")
if err != nil {
    logger.Printf("Base not found: %v", err)
    return
}

// Get base by POI ID (useful when you know the POI but not the base ID)
base, err := kb.GetBaseByPOI(ctx, "sol_station")
if err != nil {
    logger.Printf("No base at this POI: %v", err)
    return
}

// Access base information
logger.Printf("Base: %s", base.Name)
logger.Printf("Empire: %s", base.Empire)
logger.Printf("Defense: %d", base.DefenseLevel)
logger.Printf("Services: %d", len(base.Services))
logger.Printf("Facilities: %d", len(base.Facilities))
logger.Printf("Market items: %d", len(base.Market))

// Check for specific services
if base.Services["market"] {
    logger.Printf("This base has a market")
}
if base.Services["shipyard"] {
    logger.Printf("This base has a shipyard")
}

// Browse market items
for _, item := range base.Market {
    if item.IsNPC {
        logger.Printf("NPC selling: %s @ %dcr each (%d available)",
            item.ItemID, int(item.PriceEach), item.Quantity)
    }
}
```

## Complete Example: Auto-Explorer Integration

Here's how to integrate base data collection into an explorer agent:

```go
func exploreBase(client *game.Client, kb knowledge.Base, ctx context.Context, expState *ExplorerState, logger *log.Logger) error {
    state := client.GetState()

    // Only attempt to get base info if docked at a station
    if !state.Doc {
        logger.Printf("Not docked - skipping base info collection")
        return nil
    }

    // Request base information
    msg := protocol.Message{
        Type: "get_base",
    }
    if err := client.Send(ctx, msg); err != nil {
        return fmt.Errorf("failed to request base info: %w", err)
    }

    // Wait for response
    time.Sleep(2 * time.Second)

    // Get raw JSON response
    rawJSON := client.GetRawJSON("base")
    if rawJSON == nil {
        // Check if there was an error
        errResp := client.GetLastError()
        if len(errResp) > 0 {
            logger.Printf("Base info unavailable: %v", errResp)
            return nil
        }
        return fmt.Errorf("no base data available")
    }

    // Parse and save to knowledge base
    base, err := knowledge.BaseDataFromRawJSON(rawJSON, expState.AgentID, state.GetTick())
    if err != nil {
        return fmt.Errorf("failed to parse base data: %w", err)
    }

    if err := kb.RememberBase(ctx, *base); err != nil {
        return fmt.Errorf("failed to save base to knowledge base: %w", err)
    }

    logger.Printf("✓ Saved base: %s (%d facilities, %d market items)",
        base.Name, len(base.Facilities), len(base.Market))

    return nil
}
```

## Import Tool

A tool is provided to import base data from JSON files:

```bash
# Import base data from a get_base.json response file
./bin/import-base-data data/game-api/get_base.json

# Output:
# ✓ Successfully imported base: Confederacy Central Command (ID: sol_base)
#   POI: sol_station
#   Empire: solarian
#   Facilities: 18
#   Market items: 8
#   Services: 9
```

## Database Queries

### Find all bases in an empire:
```sql
SELECT id, name, empire, defense_level
FROM bases
WHERE empire = 'solarian';
```

### Find bases with specific services:
```sql
SELECT b.name, b.empire
FROM bases b
JOIN base_services bs ON b.id = bs.base_id
WHERE bs.service_name = 'shipyard' AND bs.available = 1;
```

### Find bases buying specific items:
```sql
SELECT b.name, bm.item_id, bm.price_each
FROM bases b
JOIN base_market bm ON b.id = bm.base_id
WHERE bm.item_id = 'ore_iron'
ORDER BY bm.price_each ASC;
```

## Migration Notes

- **Migration 6** adds the bases tables and runs automatically when opening the database
- Existing databases will be automatically migrated
- No data loss occurs during migration

## Testing

Verify the implementation:

```bash
# Run tests
go test ./pkg/knowledge/...

# Import sample data
./bin/import-base-data data/game-api/get_base.json

# Query database
sqlite3 spacemolt-knowledge.db "SELECT * FROM bases;"
```
