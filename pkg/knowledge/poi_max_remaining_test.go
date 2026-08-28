package knowledge

import (
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

// max_remaining is a deposit's capacity, and only get_poi reports it. Without
// it `remaining` is a bare number: 8,046 units reads the same whether the
// deposit is nearly full (cap 9,000) or nearly stripped (cap 50,000).
//
// It also fixes each resource's MAXIMUM ever mining ceiling, since
// supported_power is floor(remaining/20) — 2,500 for a 50k deposit but only
// 450 for a 9k one. That is what says whether a heavy rig can EVER work a
// resource, as opposed to only while it is near full.
func TestPOIResource_CapturesMaxRemaining(t *testing.T) {
	kb := newTestKB(t)
	ctx := t.Context()

	poi := POI{
		ID: "frostmarket_flats", SystemID: "haven", Name: "Frostmarket Flats", Type: "ice_field", LastUpdatedTick: 100,
		Resources: []game.POIResource{
			{ResourceID: "water_ice", Richness: 75, Remaining: 30141, MaxRemaining: 50000},
			{ResourceID: "carbon_dioxide_ice", Richness: 18, Remaining: 8046, MaxRemaining: 9000},
		},
	}
	if err := kb.RememberPOI(ctx, poi); err != nil {
		t.Fatalf("SavePOI: %v", err)
	}

	got := map[string]float64{}
	rows, err := kb.db.QueryContext(ctx,
		"SELECT resource_id, max_remaining FROM poi_resources WHERE poi_id = ?", poi.ID)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id string
		var mr float64
		if err := rows.Scan(&id, &mr); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got[id] = mr
	}
	if got["water_ice"] != 50000 {
		t.Errorf("water_ice max_remaining = %v, want 50000", got["water_ice"])
	}
	if got["carbon_dioxide_ice"] != 9000 {
		t.Errorf("carbon_dioxide_ice max_remaining = %v, want 9000", got["carbon_dioxide_ice"])
	}
}

// get_location reports resources without max_remaining. A later capture from
// that command must not wipe a capacity learned from get_poi -- same reasoning
// as richness keeping its MAX: a source that cannot see a field says nothing
// about it.
func TestPOIResource_MaxRemainingSurvivesASourceThatOmitsIt(t *testing.T) {
	kb := newTestKB(t)
	ctx := t.Context()

	withCap := POI{
		ID: "frostmarket_flats", SystemID: "haven", Type: "ice_field", LastUpdatedTick: 100,
		Resources: []game.POIResource{{ResourceID: "water_ice", Richness: 75, Remaining: 30141, MaxRemaining: 50000}},
	}
	if err := kb.RememberPOI(ctx, withCap); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// A later, newer get_location capture: same deposit, no capacity field.
	noCap := POI{
		ID: "frostmarket_flats", SystemID: "haven", Type: "ice_field", LastUpdatedTick: 100,
		Resources: []game.POIResource{{ResourceID: "water_ice", Richness: 75, Remaining: 30500}},
	}
	if err := kb.RememberPOI(ctx, noCap); err != nil {
		t.Fatalf("update: %v", err)
	}

	var mr, remaining float64
	if err := kb.db.QueryRowContext(ctx,
		"SELECT max_remaining, remaining FROM poi_resources WHERE poi_id = ? AND resource_id = 'water_ice'",
		"frostmarket_flats").Scan(&mr, &remaining); err != nil {
		t.Fatalf("query: %v", err)
	}
	if mr != 50000 {
		t.Errorf("max_remaining = %v, want 50000 kept; a source that omits the field must not zero it", mr)
	}
	if remaining != 30500 {
		t.Errorf("remaining = %v, want the newer 30500", remaining)
	}
}
