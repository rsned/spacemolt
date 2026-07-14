package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

func TestFillItem(t *testing.T) {
	cases := []struct {
		name         string
		cargo        float64
		orders       []serverapi.MarketOrder
		wantQty      float64
		wantProceeds float64
		wantAvg      float64
		wantFills    []sellableFill
	}{
		{
			name:  "zero cargo returns empty",
			cargo: 0,
			orders: []serverapi.MarketOrder{
				{PriceEach: 10, Quantity: 100},
			},
			wantQty: 0, wantProceeds: 0, wantAvg: 0, wantFills: nil,
		},
		{
			name:         "no buy orders returns empty",
			cargo:        50,
			orders:       nil,
			wantQty:      0,
			wantProceeds: 0,
			wantAvg:      0,
			wantFills:    nil,
		},
		{
			name:  "single buyer exact match",
			cargo: 100,
			orders: []serverapi.MarketOrder{
				{PriceEach: 10, Quantity: 100},
			},
			wantQty: 100, wantProceeds: 1000, wantAvg: 10,
			wantFills: []sellableFill{{Price: 10, Qty: 100, Proceeds: 1000}},
		},
		{
			name:  "single buyer cargo less than order",
			cargo: 30,
			orders: []serverapi.MarketOrder{
				{PriceEach: 10, Quantity: 100},
			},
			wantQty: 30, wantProceeds: 300, wantAvg: 10,
			wantFills: []sellableFill{{Price: 10, Qty: 30, Proceeds: 300}},
		},
		{
			name:  "single buyer cargo greater than order — leftover unsold",
			cargo: 200,
			orders: []serverapi.MarketOrder{
				{PriceEach: 10, Quantity: 100},
			},
			wantQty: 100, wantProceeds: 1000, wantAvg: 10,
			wantFills: []sellableFill{{Price: 10, Qty: 100, Proceeds: 1000}},
		},
		{
			name:  "multi-buyer ladder — cargo exceeds total demand, all consumed, sorted desc",
			cargo: 5000,
			orders: []serverapi.MarketOrder{
				// Intentionally out of order so the test exercises the sort.
				{PriceEach: 14, Quantity: 2246},
				{PriceEach: 26, Quantity: 676},
				{PriceEach: 20, Quantity: 1570},
			},
			// 676*26 + 1570*20 + 2246*14 = 17576 + 31400 + 31444 = 80420; qty = 4492; avg = 17.9029...
			wantQty: 4492, wantProceeds: 80420, wantAvg: 80420.0 / 4492.0,
			wantFills: []sellableFill{
				{Price: 26, Qty: 676, Proceeds: 17576},
				{Price: 20, Qty: 1570, Proceeds: 31400},
				{Price: 14, Qty: 2246, Proceeds: 31444},
			},
		},
		{
			name:  "multi-buyer ladder — cargo exhausts mid-ladder",
			cargo: 1000,
			orders: []serverapi.MarketOrder{
				{PriceEach: 26, Quantity: 676},
				{PriceEach: 20, Quantity: 1570},
				{PriceEach: 14, Quantity: 2246},
			},
			// 676 @ 26 = 17576, then 324 @ 20 = 6480; total 24056; qty 1000; avg 24.056
			wantQty: 1000, wantProceeds: 24056, wantAvg: 24.056,
			wantFills: []sellableFill{
				{Price: 26, Qty: 676, Proceeds: 17576},
				{Price: 20, Qty: 324, Proceeds: 6480},
			},
		},
		{
			name:  "ties at same price keep server order",
			cargo: 200,
			orders: []serverapi.MarketOrder{
				{PriceEach: 10, Quantity: 50, Source: "station"},
				{PriceEach: 10, Quantity: 50, Source: "player"},
				{PriceEach: 10, Quantity: 50, Source: "station"},
			},
			wantQty: 150, wantProceeds: 1500, wantAvg: 10,
			wantFills: []sellableFill{
				{Price: 10, Qty: 50, Proceeds: 500},
				{Price: 10, Qty: 50, Proceeds: 500},
				{Price: 10, Qty: 50, Proceeds: 500},
			},
		},
		{
			name:  "skips zero-price and zero-quantity orders defensively",
			cargo: 100,
			orders: []serverapi.MarketOrder{
				{PriceEach: 0, Quantity: 50},
				{PriceEach: 5, Quantity: 0},
				{PriceEach: 10, Quantity: 30},
			},
			wantQty: 30, wantProceeds: 300, wantAvg: 10,
			wantFills: []sellableFill{{Price: 10, Qty: 30, Proceeds: 300}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotQty, gotProceeds, gotAvg, gotFills := fillItem(tc.cargo, tc.orders)
			if gotQty != tc.wantQty {
				t.Errorf("sellable_qty = %v, want %v", gotQty, tc.wantQty)
			}
			if gotProceeds != tc.wantProceeds {
				t.Errorf("total_proceeds = %v, want %v", gotProceeds, tc.wantProceeds)
			}
			// Use a small epsilon for the weighted average.
			if abs(gotAvg-tc.wantAvg) > 1e-6 {
				t.Errorf("avg_price = %v, want %v", gotAvg, tc.wantAvg)
			}
			if len(gotFills) != len(tc.wantFills) {
				t.Fatalf("fills len = %d, want %d (got %+v)", len(gotFills), len(tc.wantFills), gotFills)
			}
			for i, f := range gotFills {
				if f != tc.wantFills[i] {
					t.Errorf("fill[%d] = %+v, want %+v", i, f, tc.wantFills[i])
				}
			}
		})
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func TestRenderSellableStyledEmpty(t *testing.T) {
	plan := sellablePlan{StationID: "nova_terra_central"}
	got := renderSellableStyled(plan, false)
	want := "(no cargo or storage at this station)\n"
	if got != want {
		t.Errorf("empty render = %q, want %q", got, want)
	}
}

func TestRenderSellableStyledHeaderAndRows(t *testing.T) {
	plan := sellablePlan{
		StationID:     "nova_terra_central",
		ItemCount:     2,
		TotalProceeds: 80602,
		Items: []sellableRow{
			{
				ItemID: "aluminum_ore", Name: "Aluminum Ore",
				Cargo: 4865, Storage: 1000,
				SellableQty: 4492, TotalProceeds: 80420, AvgPrice: 80420.0 / 4492.0,
				Fills: []sellableFill{
					{Price: 26, Qty: 676, Proceeds: 17576},
					{Price: 20, Qty: 1570, Proceeds: 31400},
					{Price: 14, Qty: 2246, Proceeds: 31444},
				},
			},
			{
				ItemID: "steel_plate", Name: "Steel Plate",
				Cargo: 7, Storage: 0,
				SellableQty: 7, TotalProceeds: 182, AvgPrice: 26,
				Fills: []sellableFill{{Price: 26, Qty: 7, Proceeds: 182}},
			},
		},
	}
	got := renderSellableStyled(plan, false)
	checks := []string{
		"Sellable @ nova_terra_central",
		"2 items",
		"80,602 cr",
		"aluminum_ore",
		"Aluminum Ore",
		"steel_plate",
		"Total:",
	}
	for _, want := range checks {
		if !strings.Contains(got, want) {
			t.Errorf("styled render missing %q\n--- output ---\n%s", want, got)
		}
	}
	if strings.Contains(got, "676 @") {
		t.Errorf("styled render unexpectedly included a detail block:\n%s", got)
	}
}

// The Total line used to be padded by a hardcoded width and then have its label
// printed *after* that padding, so it ran off the right-hand edge of the table.
// It also has to fit a sum, which is routinely wider than any single row's
// proceeds. No line may exceed the header rule.
func TestRenderSellableStyledTotalFitsTableWidth(t *testing.T) {
	// Row proceeds are all narrow; the total is far wider. Sizing the Proceeds
	// column from the rows alone leaves the total overhanging.
	plan := sellablePlan{
		StationID:     "grand_exchange",
		ItemCount:     3,
		TotalProceeds: 6927328,
		Items: []sellableRow{
			{ItemID: "weapon_core", Name: "Weapon Core", Storage: 296, SellableQty: 75, AvgPrice: 480.93, TotalProceeds: 1},
			{ItemID: "wiring_harness", Name: "Wiring Harness", Storage: 6598, SellableQty: 0, TotalProceeds: 0},
			{ItemID: "xenon_gas", Name: "Xenon Gas", Storage: 297, SellableQty: 1, AvgPrice: 1, TotalProceeds: 1},
		},
	}

	got := renderSellableStyled(plan, false)

	var ruleW int
	for _, line := range strings.Split(got, "\n") {
		// The header rule is the first line made only of the table's box chars.
		if strings.HasPrefix(line, "  -") && strings.Contains(line, "-+-") {
			ruleW = runeLen(line)
			break
		}
	}
	if ruleW == 0 {
		t.Fatalf("no header rule found in output:\n%s", got)
	}

	for _, line := range strings.Split(got, "\n") {
		if w := runeLen(line); w > ruleW {
			t.Errorf("line is %d wide, past the %d-wide table edge:\n%q\n--- output ---\n%s",
				w, ruleW, line, got)
		}
	}

	if !strings.Contains(got, "6,927,328") {
		t.Errorf("grand total missing from output:\n%s", got)
	}
}

// runeLen measures display width in runes, not bytes: the "—" placeholder in the
// Avg Price column is 3 bytes, so len() would report false overruns.
func runeLen(s string) int {
	return len([]rune(s))
}

func TestRenderSellableStyledDetail(t *testing.T) {
	plan := sellablePlan{
		StationID: "s", ItemCount: 1, TotalProceeds: 80420,
		Items: []sellableRow{{
			ItemID: "aluminum_ore", Name: "Aluminum Ore",
			Cargo: 4865, Storage: 0,
			SellableQty: 4492, TotalProceeds: 80420, AvgPrice: 80420.0 / 4492.0,
			Fills: []sellableFill{
				{Price: 26, Qty: 676, Proceeds: 17576},
				{Price: 20, Qty: 1570, Proceeds: 31400},
				{Price: 14, Qty: 2246, Proceeds: 31444},
			},
		}},
	}
	got := renderSellableStyled(plan, true)
	for _, want := range []string{
		"676 @ 26",
		"1570 @ 20",
		"2246 @ 14",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("detail block missing %q\n--- output ---\n%s", want, got)
		}
	}
}

func TestRenderSellableStyledSingleBuyerNotDetailed(t *testing.T) {
	plan := sellablePlan{
		StationID: "s", ItemCount: 1, TotalProceeds: 182,
		Items: []sellableRow{{
			ItemID: "steel_plate", Name: "Steel Plate",
			Cargo: 7, SellableQty: 7, TotalProceeds: 182, AvgPrice: 26,
			Fills: []sellableFill{{Price: 26, Qty: 7, Proceeds: 182}},
		}},
	}
	got := renderSellableStyled(plan, true)
	if strings.Contains(got, "7 @ 26") {
		t.Errorf("single-buyer item rendered an unwanted detail block:\n%s", got)
	}
}

func TestBuildSellablePlan(t *testing.T) {
	mkOrder := func(price, qty float64) serverapi.MarketOrder {
		return serverapi.MarketOrder{PriceEach: price, Quantity: qty}
	}

	t.Run("inventory union: cargo only, storage only, both", func(t *testing.T) {
		market := []serverapi.ViewMarketItem{
			{ItemID: "iron_ore", ItemName: "Iron Ore",
				BuyOrders: []serverapi.MarketOrder{mkOrder(10, 1000)}},
		}
		cargo := []storageItem{{ItemID: "iron_ore", Name: "Iron Ore", Quantity: 50}}
		storage := []storageItem{
			{ItemID: "iron_ore", Name: "Iron Ore", Quantity: 200},
			{ItemID: "carbon_ore", Name: "Carbon Ore", Quantity: 75},
		}
		plan := buildSellablePlan("nova_terra_central", market, cargo, storage)

		if plan.StationID != "nova_terra_central" {
			t.Errorf("station_id = %q, want nova_terra_central", plan.StationID)
		}
		if got, want := len(plan.Items), 2; got != want {
			t.Fatalf("len(plan.Items) = %d, want %d", got, want)
		}
		if plan.Items[0].ItemID != "carbon_ore" {
			t.Errorf("Items[0].ItemID = %q, want carbon_ore", plan.Items[0].ItemID)
		}
		iron := plan.Items[1]
		if iron.Cargo != 50 || iron.Storage != 200 {
			t.Errorf("iron cargo/storage = %v/%v, want 50/200", iron.Cargo, iron.Storage)
		}
		// Sellable pool walks cargo+storage (operator can withdraw before
		// selling): 50 cargo + 200 storage = 250 @ 10 = 2500.
		if iron.SellableQty != 250 || iron.TotalProceeds != 2500 {
			t.Errorf("iron sellable/proceeds = %v/%v, want 250/2500", iron.SellableQty, iron.TotalProceeds)
		}
		carbon := plan.Items[0]
		if carbon.SellableQty != 0 {
			t.Errorf("carbon sellable = %v, want 0", carbon.SellableQty)
		}
	})

	t.Run("name fallback: cargo > storage > market.item_name > item_id", func(t *testing.T) {
		market := []serverapi.ViewMarketItem{
			{ItemID: "x_a", ItemName: "X-A from market"},
			{ItemID: "x_b", ItemName: "X-B from market"},
			{ItemID: "x_c"},
		}
		cargo := []storageItem{
			{ItemID: "x_a", Name: "Cargo Name", Quantity: 1},
			{ItemID: "x_b", Name: "", Quantity: 1},
			{ItemID: "x_c", Name: "", Quantity: 1},
		}
		storage := []storageItem{
			{ItemID: "x_b", Name: "Storage Name", Quantity: 1},
		}
		plan := buildSellablePlan("s", market, cargo, storage)
		want := map[string]string{"x_a": "Cargo Name", "x_b": "Storage Name", "x_c": "x_c"}
		for _, row := range plan.Items {
			if got := row.Name; got != want[row.ItemID] {
				t.Errorf("name for %s = %q, want %q", row.ItemID, got, want[row.ItemID])
			}
		}
	})

	t.Run("duplicate cargo / storage entries are summed", func(t *testing.T) {
		cargo := []storageItem{
			{ItemID: "iron_ore", Name: "Iron Ore", Quantity: 30},
			{ItemID: "iron_ore", Name: "Iron Ore", Quantity: 20},
		}
		storage := []storageItem{
			{ItemID: "iron_ore", Name: "Iron Ore", Quantity: 5},
			{ItemID: "iron_ore", Name: "Iron Ore", Quantity: 7},
		}
		plan := buildSellablePlan("s", nil, cargo, storage)
		if got, want := len(plan.Items), 1; got != want {
			t.Fatalf("len = %d, want %d", got, want)
		}
		row := plan.Items[0]
		if row.Cargo != 50 || row.Storage != 12 {
			t.Errorf("cargo/storage = %v/%v, want 50/12", row.Cargo, row.Storage)
		}
	})

	t.Run("plan totals roll up", func(t *testing.T) {
		market := []serverapi.ViewMarketItem{
			{ItemID: "a", BuyOrders: []serverapi.MarketOrder{mkOrder(10, 100)}},
			{ItemID: "b", BuyOrders: []serverapi.MarketOrder{mkOrder(5, 50)}},
		}
		cargo := []storageItem{
			{ItemID: "a", Quantity: 10},
			{ItemID: "b", Quantity: 50},
		}
		plan := buildSellablePlan("s", market, cargo, nil)
		if plan.TotalProceeds != 350 {
			t.Errorf("plan.TotalProceeds = %v, want 350", plan.TotalProceeds)
		}
		if plan.ItemCount != 2 {
			t.Errorf("plan.ItemCount = %v, want 2", plan.ItemCount)
		}
	})

	t.Run("no inventory yields empty items slice", func(t *testing.T) {
		plan := buildSellablePlan("s", nil, nil, nil)
		if len(plan.Items) != 0 {
			t.Errorf("Items len = %d, want 0", len(plan.Items))
		}
		if plan.TotalProceeds != 0 || plan.ItemCount != 0 {
			t.Errorf("totals = %v/%v, want 0/0", plan.TotalProceeds, plan.ItemCount)
		}
	})
}

func TestRenderSellableJSON(t *testing.T) {
	plan := sellablePlan{
		StationID: "nova_terra_central", ItemCount: 1, TotalProceeds: 182,
		Items: []sellableRow{{
			ItemID: "steel_plate", Name: "Steel Plate",
			Cargo: 7, Storage: 0,
			SellableQty: 7, TotalProceeds: 182, AvgPrice: 26,
			Fills: []sellableFill{{Price: 26, Qty: 7, Proceeds: 182}},
		}},
	}
	out := renderSellableJSON(plan)
	var round struct {
		StationID     string  `json:"station_id"`
		ItemCount     int     `json:"item_count"`
		TotalProceeds float64 `json:"total_proceeds"`
		Items         []struct {
			ItemID string `json:"item_id"`
			Fills  []struct {
				Price float64 `json:"price"`
				Qty   float64 `json:"qty"`
			} `json:"fills"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &round); err != nil {
		t.Fatalf("output not valid JSON: %v\n%s", err, out)
	}
	if round.StationID != "nova_terra_central" {
		t.Errorf("station_id = %q, want nova_terra_central", round.StationID)
	}
	if round.ItemCount != 1 || round.TotalProceeds != 182 {
		t.Errorf("totals = %v/%v, want 1/182", round.ItemCount, round.TotalProceeds)
	}
	if len(round.Items) != 1 || round.Items[0].ItemID != "steel_plate" {
		t.Fatalf("items = %+v", round.Items)
	}
	if len(round.Items[0].Fills) != 1 || round.Items[0].Fills[0].Price != 26 {
		t.Errorf("fills[0] = %+v, want price=26 qty=7", round.Items[0].Fills)
	}
}
