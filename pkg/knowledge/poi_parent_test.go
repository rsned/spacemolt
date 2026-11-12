package knowledge

import (
	"context"
	"testing"
)

// v0.692.0 parent_id is stored, read back, and not erased by a read that
// omits it.
func TestRememberPOIParentID(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()
	if err := kb.RememberSystem(ctx, System{ID: "deep_range", Name: "Deep Range", LastUpdatedTick: 1}); err != nil {
		t.Fatal(err)
	}
	if err := kb.RememberPOI(ctx, POI{ID: "deep_range_outpost", SystemID: "deep_range", Name: "Deep Range Outpost", Type: "station",
		ParentID: "deep_range_mineral_fields", LastUpdatedTick: 1}); err != nil {
		t.Fatal(err)
	}
	if err := kb.RememberPOI(ctx, POI{ID: "deep_range_outpost", SystemID: "deep_range", Name: "Deep Range Outpost", Type: "station", LastUpdatedTick: 2}); err != nil {
		t.Fatal(err)
	}
	pois, err := kb.GetPOIs(ctx, "deep_range")
	if err != nil {
		t.Fatal(err)
	}
	if len(pois) != 1 || pois[0].ParentID != "deep_range_mineral_fields" {
		t.Fatalf("pois = %+v, want parent kept", pois)
	}
}
