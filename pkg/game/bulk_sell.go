package game

// BulkSellOrder represents a single item to sell in a bulk order
type BulkSellOrder struct {
	ItemID    string `json:"item_id"`
	Quantity  int    `json:"quantity"`
	PriceEach int    `json:"price_each"`
}

// PrepareBulkSellOrder prepares cargo items for a bulk sell order API call.
// It filters out reserved items and equipment, keeping only ores and resources.
// Returns a slice of BulkSellOrder items ready for the create_sell_order API (max 50).
//
// Parameters:
//   - cargo: The ship's current cargo contents
//   - reservedItems: List of item IDs that should NOT be sold (e.g., crafting materials to keep)
//   - pricePerItem: Map of item_id to price_each for pricing strategy. If nil or missing an item,
//     uses a default price of 1 credit (you should call view_market first for better pricing)
//
// Returns:
//   - orders: Slice of BulkSellOrder ready for API call (empty if nothing to sell)
//   - skippedCount: Number of items skipped (reserved or equipment)
func PrepareBulkSellOrder(cargo []CargoItem, reservedItems []string, pricePerItem map[string]int) (orders []BulkSellOrder, skippedCount int) {
	// Create a set of reserved items for fast lookup
	reserved := make(map[string]bool)
	for _, itemID := range reservedItems {
		reserved[itemID] = true
	}

	// Process each cargo item
	for _, item := range cargo {
		if item.Quantity <= 0 {
			continue
		}

		// Skip reserved items
		if reserved[item.ItemID] {
			skippedCount++
			continue
		}

		// Skip equipment (only sell ores and resources)
		if !isOreOrResource(item.ItemID) {
			skippedCount++
			continue
		}

		// Determine price
		price := 1 // Default fallback price
		if pricePerItem != nil {
			if p, ok := pricePerItem[item.ItemID]; ok {
				price = p
			}
		}

		// Add to bulk order
		orders = append(orders, BulkSellOrder{
			ItemID:    item.ItemID,
			Quantity:  int(item.Quantity), // Convert float64 to int
			PriceEach: price,
		})

		// API limit: max 50 items per bulk order
		if len(orders) >= 50 {
			break
		}
	}

	return orders, skippedCount
}

// isOreOrResource returns true if the item is ore or a resource (should be sold)
// This is duplicated from client.go for internal use
func isOreOrResource(itemID string) bool {
	// Ores and resources to sell - check prefixes
	oreAndResourcePrefixes := []string{
		"ore_",     // Legacy ores (ore_gold, ore_platinum, etc.)
		"gas_",     // Gases
		"crystal_", // Crystals
		"salvage_", // Salvage materials
		"scrap_",   // Scrap materials
	}

	for _, prefix := range oreAndResourcePrefixes {
		if len(itemID) >= len(prefix) && itemID[:len(prefix)] == prefix {
			return true
		}
	}

	// New ore ID format uses suffixes (iron_ore, copper_ore, water_ice)
	oreAndResourceSuffixes := []string{
		"_ore", // Ores (iron_ore, copper_ore, etc.)
		"_ice", // Ice resources (water_ice)
	}

	for _, suffix := range oreAndResourceSuffixes {
		if len(itemID) >= len(suffix) && itemID[len(itemID)-len(suffix):] == suffix {
			return true
		}
	}

	// Don't sell these - they're equipment
	// mining_laser_*, pulse_laser_*, shield_*, cargo_*, engine_*, module_*, etc.
	return false
}

// GetMarketPricesForCargo creates a price map for cargo items using market listings.
// This helper uses the "highest buy order" price for each item (what buyers are willing to pay).
// If an item has no buy orders, it uses the "lowest sell order" price minus 1 credit to be competitive.
// If no market data exists for an item, it won't be in the returned map (use default price instead).
//
// Parameters:
//   - cargo: The ship's cargo to price
//   - listings: Market listings from Client.GetMarketListings() or view_market
//
// Returns:
//   - priceMap: Map of item_id to suggested price_each for selling
func GetMarketPricesForCargo(cargo []CargoItem, listings []MarketListing) map[string]int {
	priceMap := make(map[string]int)

	// For each cargo item, find the best price from market listings
	for _, item := range cargo {
		if item.Quantity <= 0 {
			continue
		}

		var highestBuyPrice float64
		var lowestSellPrice float64

		// Scan all listings for this item
		for _, listing := range listings {
			if listing.ItemID != item.ItemID {
				continue
			}

			// Track buy orders (what others want to buy for)
			if listing.Type == "buy" {
				if listing.PricePerUnit > highestBuyPrice {
					highestBuyPrice = listing.PricePerUnit
				}
			}

			// Track sell orders (what others are selling for)
			if listing.Type == "sell" {
				if lowestSellPrice == 0 || listing.PricePerUnit < lowestSellPrice {
					lowestSellPrice = listing.PricePerUnit
				}
			}
		}

		// Pricing strategy:
		// 1. If there are buy orders, match the highest buy price (instant fill)
		// 2. If no buy orders but sell orders exist, price at lowest sell - 1 (undercut competition)
		// 3. If no market data, don't add to map (caller will use default)
		if highestBuyPrice > 0 {
			priceMap[item.ItemID] = int(highestBuyPrice)
		} else if lowestSellPrice > 1 {
			// Undercut by 1 credit, but never go below 1
			priceMap[item.ItemID] = int(lowestSellPrice) - 1
		}
	}

	return priceMap
}
