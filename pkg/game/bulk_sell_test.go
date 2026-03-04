package game

import (
	"testing"
)

func TestPrepareBulkSellOrder(t *testing.T) {
	tests := []struct {
		name          string
		cargo         []CargoItem
		reservedItems []string
		pricePerItem  map[string]int
		wantOrders    int
		wantSkipped   int
	}{
		{
			name: "basic ores - no reserved items",
			cargo: []CargoItem{
				{ItemID: "iron_ore", Quantity: 100},
				{ItemID: "copper_ore", Quantity: 50},
				{ItemID: "ore_gold", Quantity: 25},
			},
			reservedItems: nil,
			pricePerItem: map[string]int{
				"iron_ore":   5,
				"copper_ore": 8,
				"ore_gold":   20,
			},
			wantOrders:  3,
			wantSkipped: 0,
		},
		{
			name: "mixed cargo - ores and equipment",
			cargo: []CargoItem{
				{ItemID: "iron_ore", Quantity: 100},
				{ItemID: "mining_laser_i", Quantity: 1},
				{ItemID: "copper_ore", Quantity: 50},
				{ItemID: "pulse_laser_i", Quantity: 1},
				{ItemID: "salvage_metal", Quantity: 30},
			},
			reservedItems: nil,
			pricePerItem: map[string]int{
				"iron_ore":        5,
				"copper_ore":      8,
				"salvage_metal":   3,
				"mining_laser_i":  100, // Should be skipped (equipment)
				"pulse_laser_i":  200, // Should be skipped (equipment)
			},
			wantOrders:  3, // Only ores and salvage
			wantSkipped: 2, // Equipment skipped
		},
		{
			name: "reserved items",
			cargo: []CargoItem{
				{ItemID: "iron_ore", Quantity: 100},
				{ItemID: "copper_ore", Quantity: 50},
				{ItemID: "crystal_blue", Quantity: 10},
			},
			reservedItems: []string{"copper_ore", "crystal_blue"},
			pricePerItem: map[string]int{
				"iron_ore":     5,
				"copper_ore":   8,
				"crystal_blue": 50,
			},
			wantOrders:  1, // Only ore_iron
			wantSkipped: 2, // Reserved items
		},
		{
			name: "empty cargo",
			cargo:         []CargoItem{},
			reservedItems: nil,
			pricePerItem:  nil,
			wantOrders:    0,
			wantSkipped:   0,
		},
		{
			name: "zero quantity items",
			cargo: []CargoItem{
				{ItemID: "iron_ore", Quantity: 0},
				{ItemID: "copper_ore", Quantity: 50},
			},
			reservedItems: nil,
			pricePerItem: map[string]int{
				"iron_ore":   5,
				"copper_ore": 8,
			},
			wantOrders:  1, // Only copper (iron has 0 quantity)
			wantSkipped: 0,
		},
		{
			name: "default pricing when not in map",
			cargo: []CargoItem{
				{ItemID: "iron_ore", Quantity: 100},
				{ItemID: "copper_ore", Quantity: 50},
			},
			reservedItems: nil,
			pricePerItem: map[string]int{
				"iron_ore": 5,
				// ore_copper not in map - should use default price of 1
			},
			wantOrders:  2,
			wantSkipped: 0,
		},
		{
			name: "gas and scrap materials",
			cargo: []CargoItem{
				{ItemID: "gas_helium", Quantity: 100},
				{ItemID: "scrap_metal", Quantity: 50},
				{ItemID: "iron_ore", Quantity: 25},
			},
			reservedItems: nil,
			pricePerItem: map[string]int{
				"gas_helium":   10,
				"scrap_metal":  2,
				"iron_ore":     5,
			},
			wantOrders:  3,
			wantSkipped: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orders, skipped := PrepareBulkSellOrder(tt.cargo, tt.reservedItems, tt.pricePerItem)

			if len(orders) != tt.wantOrders {
				t.Errorf("PrepareBulkSellOrder() got %d orders, want %d", len(orders), tt.wantOrders)
			}

			if skipped != tt.wantSkipped {
				t.Errorf("PrepareBulkSellOrder() skipped %d items, want %d", skipped, tt.wantSkipped)
			}

			// Verify order structure
			for i, order := range orders {
				if order.ItemID == "" {
					t.Errorf("Order %d has empty ItemID", i)
				}
				if order.Quantity <= 0 {
					t.Errorf("Order %d has invalid quantity: %d", i, order.Quantity)
				}
				if order.PriceEach <= 0 {
					t.Errorf("Order %d has invalid price: %d", i, order.PriceEach)
				}

				// Verify it's a sellable item (ore/resource)
				if !isOreOrResource(order.ItemID) {
					t.Errorf("Order %d contains equipment item: %s", i, order.ItemID)
				}

				// Verify it's not reserved
				for _, reserved := range tt.reservedItems {
					if order.ItemID == reserved {
						t.Errorf("Order %d contains reserved item: %s", i, order.ItemID)
					}
				}
			}
		})
	}
}

func TestPrepareBulkSellOrder_50ItemLimit(t *testing.T) {
	// Create cargo with 60 items
	cargo := make([]CargoItem, 60)
	priceMap := make(map[string]int)
	for i := range 60 {
		itemID := "ore_item_" + string(rune('a'+i%26))
		cargo[i] = CargoItem{
			ItemID:   itemID,
			Quantity: 10,
		}
		priceMap[itemID] = 5
	}

	// However, many won't match the ore_ prefix due to the way we generate names
	// Let's fix this:
	for i := range 60 {
		cargo[i].ItemID = "iron_ore"
		if i%2 == 0 {
			cargo[i].ItemID = "copper_ore"
		}
		if i%3 == 0 {
			cargo[i].ItemID = "ore_gold"
		}
		priceMap[cargo[i].ItemID] = 5
	}

	orders, _ := PrepareBulkSellOrder(cargo, nil, priceMap)

	// Should be limited to 50 items
	if len(orders) != 50 {
		t.Errorf("Expected 50 orders (API limit), got %d", len(orders))
	}
}

func TestGetMarketPricesForCargo(t *testing.T) {
	tests := []struct {
		name     string
		cargo    []CargoItem
		listings []MarketListing
		want     map[string]int
	}{
		{
			name: "buy orders exist - use highest buy price",
			cargo: []CargoItem{
				{ItemID: "iron_ore", Quantity: 100},
			},
			listings: []MarketListing{
				{ItemID: "iron_ore", Type: "buy", PricePerUnit: 5},
				{ItemID: "iron_ore", Type: "buy", PricePerUnit: 7},
				{ItemID: "iron_ore", Type: "sell", PricePerUnit: 10},
			},
			want: map[string]int{
				"iron_ore": 7, // Highest buy order
			},
		},
		{
			name: "no buy orders - undercut lowest sell",
			cargo: []CargoItem{
				{ItemID: "copper_ore", Quantity: 50},
			},
			listings: []MarketListing{
				{ItemID: "copper_ore", Type: "sell", PricePerUnit: 10},
				{ItemID: "copper_ore", Type: "sell", PricePerUnit: 8},
			},
			want: map[string]int{
				"copper_ore": 7, // Lowest sell (8) minus 1
			},
		},
		{
			name: "no market data - no entry in map",
			cargo: []CargoItem{
				{ItemID: "ore_rare", Quantity: 10},
			},
			listings: []MarketListing{
				{ItemID: "iron_ore", Type: "sell", PricePerUnit: 5},
			},
			want: map[string]int{}, // Empty - no data for ore_rare
		},
		{
			name: "multiple items with different data",
			cargo: []CargoItem{
				{ItemID: "iron_ore", Quantity: 100},
				{ItemID: "copper_ore", Quantity: 50},
				{ItemID: "ore_gold", Quantity: 10},
			},
			listings: []MarketListing{
				{ItemID: "iron_ore", Type: "buy", PricePerUnit: 5},
				{ItemID: "copper_ore", Type: "sell", PricePerUnit: 10},
				// No data for ore_gold
			},
			want: map[string]int{
				"iron_ore":   5, // Buy order
				"copper_ore": 9, // Undercut sell
				// ore_gold not in map
			},
		},
		{
			name: "don't undercut below 1 credit",
			cargo: []CargoItem{
				{ItemID: "ore_cheap", Quantity: 100},
			},
			listings: []MarketListing{
				{ItemID: "ore_cheap", Type: "sell", PricePerUnit: 1},
			},
			want: map[string]int{}, // Can't undercut below 1, so no entry
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetMarketPricesForCargo(tt.cargo, tt.listings)

			if len(got) != len(tt.want) {
				t.Errorf("GetMarketPricesForCargo() returned %d prices, want %d", len(got), len(tt.want))
			}

			for itemID, wantPrice := range tt.want {
				gotPrice, ok := got[itemID]
				if !ok {
					t.Errorf("GetMarketPricesForCargo() missing price for %s", itemID)
					continue
				}
				if gotPrice != wantPrice {
					t.Errorf("GetMarketPricesForCargo() price for %s = %d, want %d", itemID, gotPrice, wantPrice)
				}
			}

			// Check for unexpected items
			for itemID := range got {
				if _, ok := tt.want[itemID]; !ok {
					t.Errorf("GetMarketPricesForCargo() has unexpected item %s = %d", itemID, got[itemID])
				}
			}
		})
	}
}

func TestIsOreOrResource(t *testing.T) {
	tests := []struct {
		itemID string
		want   bool
	}{
		// Ores
		{"iron_ore", true},
		{"copper_ore", true},
		{"ore_gold", true},
		{"ore_platinum", true},

		// Gases
		{"gas_helium", true},
		{"gas_hydrogen", true},

		// Crystals
		{"crystal_blue", true},
		{"crystal_red", true},

		// Salvage
		{"salvage_metal", true},
		{"salvage_electronics", true},

		// Scrap
		{"scrap_metal", true},
		{"scrap_hull", true},

		// Equipment (should NOT be sold)
		{"mining_laser_i", false},
		{"mining_laser_advanced", false},
		{"pulse_laser_i", false},
		{"weapon_missile_launcher", false},
		{"shield_generator_1", false},
		{"cargo_expansion_1", false},
		{"engine_upgrade_1", false},
		{"module_scanner", false},

		// Edge cases
		{"", false},
		{"unknown_item", false},
		{"ore", false}, // Too short
	}

	for _, tt := range tests {
		t.Run(tt.itemID, func(t *testing.T) {
			got := isOreOrResource(tt.itemID)
			if got != tt.want {
				t.Errorf("isOreOrResource(%q) = %v, want %v", tt.itemID, got, tt.want)
			}
		})
	}
}
