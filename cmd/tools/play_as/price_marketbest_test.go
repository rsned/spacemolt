package main

import (
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/pricing"
)

// The market-wide MEAN can be dominated by one outlier listing (titanium_ore
// showed a 10000 mkt-avg that made a ghost_rounds BoM read ~96k/unit), which
// buries the fact that the same ore is available far cheaper somewhere. Report
// the cheapest ask anywhere next to the mean so both are visible.
func TestRenderPriceTextShowsMarketBestColumn(t *testing.T) {
	rep := &pricing.PriceReport{
		ItemID:      "widget",
		OutputUnits: 1,
		Components: []pricing.PricedComponent{
			{
				Component:  pricing.Component{ItemID: "titanium_ore", Qty: 2},
				NearbyUnit: 0, NearbyFound: false,
				MktBestUnit: 12.5, MktBestFound: true,
				MktUnit: 10000, MktFound: true,
			},
		},
		Nearby:  pricing.Basis{Total: 1},
		MktBest: pricing.Basis{BuildCost: 25, PerUnit: 25, Margin: 5, Suggested: 30, Covered: 1, Total: 1},
		Mkt:     pricing.Basis{BuildCost: 20000, PerUnit: 20000, Margin: 4000, Suggested: 24000, Covered: 1, Total: 1},

		CurAskNearby: 900, HasAskNearby: true,
		CurAskMktBest: 750, HasAskMktBest: true,
		CurAskMkt: 1100, HasAskMkt: true,
	}

	out := renderPriceText("widget", "sol", 2, 20, []modeReport{{Label: "BOM (ore)", R: rep}}, "")

	for _, want := range []string{
		"MKT-BEST", // new column header
		"12.50",    // per-component best unit price
		"30.00",    // best-basis SUGGESTED, orders of magnitude below the mean
		"24000.00", // mean-basis SUGGESTED still reported
		"750",      // finished good's own cheapest ask anywhere, on CURRENT MARKET
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing %q in:\n%s", want, out)
		}
	}
	// The best column must sit next to the mean on the header row.
	header := ""
	for _, ln := range strings.Split(out, "\n") {
		if strings.Contains(ln, "COMPONENT") {
			header = ln
			break
		}
	}
	if !strings.Contains(header, "MKT-BEST") || !strings.Contains(header, "MKT-AVG") {
		t.Fatalf("header must carry both market columns, got %q", header)
	}
	if strings.Index(header, "MKT-BEST") > strings.Index(header, "MKT-AVG") {
		t.Errorf("MKT-BEST should precede MKT-AVG (cheapest -> mean), got %q", header)
	}
}
