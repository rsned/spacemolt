package main

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/rsned/spacemolt/pkg/pricing"
)

// BOM expansion multiplies fractional per-run yields, so a component quantity
// carries float64 accumulation noise: 5.2 arrives as 5.200000000000001 and
// 15.6 as 15.599999999999996. Rendering those with a shortest-repr float
// printed every digit, which both looked wrong and overflowed the 8-wide QTY
// column, shunting the whole row out of alignment.
//
// QTY is rendered like every other numeric column in this table: two decimals.
func TestRenderPriceTextFormatsQtyToTwoDecimals(t *testing.T) {
	rep := &pricing.PriceReport{
		ItemID:      "bom_item",
		OutputUnits: 1,
		Components: []pricing.PricedComponent{
			{Component: pricing.Component{ItemID: "chlorine_gas", Qty: 5.200000000000001}},
			{Component: pricing.Component{ItemID: "water_ice", Qty: 15.599999999999996}},
			{Component: pricing.Component{ItemID: "silicon_ore", Qty: 2.4000000000000004}},
			{Component: pricing.Component{ItemID: "anchor_plate", Qty: 6}},
		},
		Nearby:  pricing.Basis{Total: 4},
		MktBest: pricing.Basis{Total: 4},
		Mkt:     pricing.Basis{Total: 4},
	}

	out := renderPriceText("bom_item", "sol", 2, 20, []modeReport{{Label: "BOM (ore)", R: rep}}, "")

	// Compare the QTY cell EXACTLY, not by substring: "15.60" is a substring
	// of "15.600", so a substring check would pass any precision >= 2 and this
	// test would not pin the format it claims to.
	want := map[string]string{
		"chlorine_gas": "5.20", "water_ice": "15.60",
		"silicon_ore": "2.40", "anchor_plate": "6.00",
	}
	seen := 0
	for _, ln := range strings.Split(out, "\n") {
		f := strings.Fields(ln)
		if len(f) < 2 {
			continue
		}
		w, ok := want[f[0]]
		if !ok || strings.Contains(ln, "missing:") {
			continue
		}
		seen++
		if f[1] != w {
			t.Errorf("%s QTY = %q, want %q", f[0], f[1], w)
		}
	}
	if seen != len(want) {
		t.Fatalf("checked %d component rows, want %d — row matcher is wrong:\n%s", seen, len(want), out)
	}
}

// The QTY column is 8 wide and every component row must respect it, or the
// columns to its right stop lining up. This is the property the noisy floats
// actually broke.
func TestRenderPriceTextKeepsComponentRowsAligned(t *testing.T) {
	rep := &pricing.PriceReport{
		ItemID:      "bom_item",
		OutputUnits: 1,
		Components: []pricing.PricedComponent{
			{Component: pricing.Component{ItemID: "water_ice", Qty: 15.599999999999996},
				NearbyUnit: 6328, NearbyFound: true},
			{Component: pricing.Component{ItemID: "anchor_plate", Qty: 6}},
		},
		Nearby:  pricing.Basis{Total: 2, Covered: 1},
		MktBest: pricing.Basis{Total: 2},
		Mkt:     pricing.Basis{Total: 2},
	}

	out := renderPriceText("bom_item", "sol", 2, 20, []modeReport{{Label: "BOM (ore)", R: rep}}, "")

	var header string
	var rows []string
	for _, ln := range strings.Split(out, "\n") {
		switch {
		case strings.Contains(ln, "COMPONENT") && strings.Contains(ln, "QTY"):
			header = ln
		// Component rows only — the coverage line names the same items.
		case strings.HasPrefix(ln, "  water_ice"), strings.HasPrefix(ln, "  anchor_plate"):
			rows = append(rows, ln)
		}
	}
	if header == "" || len(rows) != 2 {
		t.Fatalf("expected a header and 2 component rows, got header=%q rows=%d:\n%s", header, len(rows), out)
	}
	// Every component row ends where the header does: fixed-width columns.
	// Counted in runes, not bytes — the em dash placeholder is 3 bytes wide.
	for _, r := range rows {
		if got, want := utf8.RuneCountInString(r), utf8.RuneCountInString(header); got != want {
			t.Errorf("row width %d != header width %d — columns misaligned\n  header: %q\n  row:    %q",
				got, want, header, r)
		}
	}
}
