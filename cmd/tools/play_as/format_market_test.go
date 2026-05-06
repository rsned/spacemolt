package main

import (
	"strings"
	"testing"
)

// TestFormatMarket_SingleItem_ShowsAllRowsAndMyOrderMarker exercises the
// view_market formatter against a real response captured from the server when
// the caller passed a specific item_id (so the response holds exactly one
// item). The order book has 9 station rows plus 1 own-order row; the renderer
// should display every row (no 2-row truncation) and prefix the own-order row
// with "✓ ".
func TestFormatMarket_SingleItem_ShowsAllRowsAndMyOrderMarker(t *testing.T) {
	raw := []byte(`{
		"action": "view_market",
		"base": "Market Prime Exchange",
		"items": [{
			"item_id": "station_reactor_core",
			"item_name": "Station Reactor Core",
			"category": "component",
			"buy_orders": [
				{"price_each": 20494, "quantity": 2,   "source": "station"},
				{"price_each": 20491, "quantity": 2,   "source": "station"},
				{"price_each": 20488, "quantity": 6,   "source": "station"},
				{"price_each": 15765, "quantity": 2,   "source": "station"},
				{"price_each": 15763, "quantity": 2,   "source": "station"},
				{"price_each": 15760, "quantity": 6,   "source": "station"},
				{"price_each": 11035, "quantity": 4,   "source": "station"},
				{"price_each": 11034, "quantity": 4,   "source": "station"},
				{"price_each": 11032, "quantity": 12,  "source": "station"},
				{"price_each": 2,     "quantity": 101, "my_quantity": 101}
			],
			"sell_orders": []
		}]
	}`)

	out := formatMarket(raw)

	// All 9 station price levels should appear with the "*" marker.
	for _, p := range []string{"20494", "20491", "20488", "15765", "15763", "15760", "11035", "11034", "11032"} {
		if !strings.Contains(out, "* "+p) {
			t.Errorf("missing station row for price %s\noutput:\n%s", p, out)
		}
	}

	// The own buy order should appear with the "✓" marker.
	if !strings.Contains(out, "✓ 2") {
		t.Errorf("missing own-order marker '✓ 2'\noutput:\n%s", out)
	}

	// Truncation footer should NOT appear: 10 rows at 25-row cap = no truncate.
	if strings.Contains(out, "more") {
		t.Errorf("unexpected truncation footer for single-item view\noutput:\n%s", out)
	}
}

// TestFormatMarket_MultiItem_TruncationAsymmetric verifies that when only one
// side of an item's order book is truncated, the trailer row shows "... N more"
// in just that side's columns and leaves the other side's columns blank
// (rather than printing "0 more sells" / "0 more buys").
func TestFormatMarket_MultiItem_TruncationAsymmetric(t *testing.T) {
	raw := []byte(`{
		"action": "view_market",
		"items": [
			{
				"item_id": "fury_alloy",
				"item_name": "Fury Alloy",
				"category": "material",
				"buy_orders": [
					{"price_each": 120, "quantity": 2, "source": "station"},
					{"price_each": 118, "quantity": 2, "source": "station"},
					{"price_each": 116, "quantity": 2, "source": "station"},
					{"price_each": 114, "quantity": 2, "source": "station"},
					{"price_each": 112, "quantity": 2, "source": "station"},
					{"price_each": 110, "quantity": 2, "source": "station"},
					{"price_each": 108, "quantity": 2, "source": "station"},
					{"price_each": 106, "quantity": 2, "source": "station"},
					{"price_each": 104, "quantity": 2, "source": "station"},
					{"price_each": 102, "quantity": 2, "source": "station"},
					{"price_each": 100, "quantity": 2, "source": "station"},
					{"price_each":  98, "quantity": 2, "source": "station"},
					{"price_each":  96, "quantity": 2, "source": "station"},
					{"price_each":  94, "quantity": 2, "source": "station"},
					{"price_each":  92, "quantity": 2, "source": "station"},
					{"price_each":  90, "quantity": 2, "source": "station"}
				],
				"sell_orders": []
			},
			{
				"item_id": "flex_polymer",
				"item_name": "Flex Polymer",
				"category": "material",
				"buy_orders": [
					{"price_each": 15, "quantity": 362, "source": "station"},
					{"price_each":  2, "quantity": 101, "my_quantity": 101}
				],
				"sell_orders": [
					{"price_each": 20, "quantity":  76, "source": "station"},
					{"price_each": 21, "quantity": 314, "source": "station"},
					{"price_each": 22, "quantity": 100, "source": "station"},
					{"price_each": 23, "quantity": 100, "source": "station"},
					{"price_each": 24, "quantity": 100, "source": "station"},
					{"price_each": 25, "quantity": 100, "source": "station"}
				]
			}
		]
	}`)

	out := formatMarket(raw)

	// fury_alloy has 16 buys (14 over the 2-row cap), 0 sells.
	// Expect "... 14 more" in the trailer, and NO "more sells" / "0 more" text.
	if !strings.Contains(out, "... 14 more") {
		t.Errorf("expected '... 14 more' for fury_alloy\noutput:\n%s", out)
	}

	// flex_polymer has 2 buys (cap), 6 sells (4 over). Expect "... 4 more".
	if !strings.Contains(out, "... 4 more") {
		t.Errorf("expected '... 4 more' for flex_polymer\noutput:\n%s", out)
	}

	// Old combined-counter format must not appear.
	for _, bad := range []string{"0 more", "more buys", "more sells"} {
		if strings.Contains(out, bad) {
			t.Errorf("unexpected legacy truncation text %q\noutput:\n%s", bad, out)
		}
	}
}

// TestFormatMarket_MultiItem_KeepsTwoRowCap verifies that the unfiltered
// (multi-item) market view still shows only the top 2 orders per item.
func TestFormatMarket_MultiItem_KeepsTwoRowCap(t *testing.T) {
	raw := []byte(`{
		"action": "view_market",
		"items": [
			{
				"item_id": "iron_ore",
				"item_name": "Iron Ore",
				"category": "ore",
				"buy_orders": [
					{"price_each": 50, "quantity": 100, "source": "station"},
					{"price_each": 49, "quantity": 100, "source": "station"},
					{"price_each": 48, "quantity": 100, "source": "station"},
					{"price_each": 47, "quantity": 100, "source": "station"}
				],
				"sell_orders": []
			},
			{
				"item_id": "copper_ore",
				"item_name": "Copper Ore",
				"category": "ore",
				"buy_orders": [
					{"price_each": 30, "quantity": 50, "source": "station"}
				],
				"sell_orders": []
			}
		]
	}`)

	out := formatMarket(raw)

	// Top 2 of iron_ore should appear; 3rd/4th should not.
	if !strings.Contains(out, "* 50") || !strings.Contains(out, "* 49") {
		t.Errorf("missing top-2 buys for iron_ore\noutput:\n%s", out)
	}
	if strings.Contains(out, "* 48") || strings.Contains(out, "* 47") {
		t.Errorf("unexpected 3rd/4th row for multi-item view\noutput:\n%s", out)
	}
}
