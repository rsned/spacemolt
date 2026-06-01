package knowledge

import (
	"context"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

// resourceRow reads back a single poi_resources row for assertions.
type resourceRow struct {
	richness   float64
	remaining  float64
	tick       int64
	detectedBy string
}

func readResource(t *testing.T, kb *SQLiteKB, poiID, resourceID string) (resourceRow, bool) {
	t.Helper()
	var r resourceRow
	err := kb.db.QueryRow(
		`SELECT richness, remaining, last_updated_tick, COALESCE(detected_by,'')
		   FROM poi_resources WHERE poi_id=? AND resource_id=?`, poiID, resourceID,
	).Scan(&r.richness, &r.remaining, &r.tick, &r.detectedBy)
	if err != nil {
		return resourceRow{}, false
	}
	return r, true
}

func countResources(t *testing.T, kb *SQLiteKB, poiID string) int {
	t.Helper()
	var n int
	if err := kb.db.QueryRow(`SELECT COUNT(*) FROM poi_resources WHERE poi_id=?`, poiID).Scan(&n); err != nil {
		t.Fatalf("count resources: %v", err)
	}
	return n
}

// TestRememberPOI_WeakScanKeepsUnmentionedResources: a second write that
// resolves only a subset of resources must not wipe the ones it didn't see.
func TestRememberPOI_WeakScanKeepsUnmentionedResources(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()

	strong := POI{
		ID: "belt1", SystemID: "sysA", Name: "Belt", Type: "asteroid_belt",
		LastUpdatedTick: 1000, DetectedBy: "agentA",
		Resources: []game.POIResource{
			{ResourceID: "ore_x", Richness: 0.9, Remaining: 1000},
			{ResourceID: "ore_y", Richness: 0.7, Remaining: 500},
		},
	}
	if err := kb.RememberPOI(ctx, strong); err != nil {
		t.Fatalf("strong write: %v", err)
	}

	// Weaker later scan only resolves ore_x.
	weak := POI{
		ID: "belt1", SystemID: "sysA", Name: "Belt", Type: "asteroid_belt",
		LastUpdatedTick: 1050, DetectedBy: "agentB",
		Resources: []game.POIResource{
			{ResourceID: "ore_x", Richness: 0.4, Remaining: 800},
		},
	}
	if err := kb.RememberPOI(ctx, weak); err != nil {
		t.Fatalf("weak write: %v", err)
	}

	if n := countResources(t, kb, "belt1"); n != 2 {
		t.Fatalf("got %d resources, want 2 (ore_y must survive the weak scan)", n)
	}
	if y, ok := readResource(t, kb, "belt1", "ore_y"); !ok || y.richness != 0.7 {
		t.Errorf("ore_y richness=%v (ok=%v), want 0.7 preserved", y.richness, ok)
	}
}

// TestRememberPOI_MaxRichnessLatestRemaining: for a re-observed resource, keep
// the highest richness ever seen but the newest remaining + provenance.
func TestRememberPOI_MaxRichnessLatestRemaining(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()

	first := POI{
		ID: "belt2", SystemID: "sysA", Type: "asteroid_belt",
		LastUpdatedTick: 1000, DetectedBy: "agentA",
		Resources:       []game.POIResource{{ResourceID: "ore_x", Richness: 0.9, Remaining: 1000}},
	}
	second := POI{
		ID: "belt2", SystemID: "sysA", Type: "asteroid_belt",
		LastUpdatedTick: 1050, DetectedBy: "agentB",
		Resources:       []game.POIResource{{ResourceID: "ore_x", Richness: 0.5, Remaining: 600}},
	}
	if err := kb.RememberPOI(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := kb.RememberPOI(ctx, second); err != nil {
		t.Fatal(err)
	}

	r, ok := readResource(t, kb, "belt2", "ore_x")
	if !ok {
		t.Fatal("ore_x missing")
	}
	if r.richness != 0.9 {
		t.Errorf("richness=%v, want 0.9 (max preserved)", r.richness)
	}
	if r.remaining != 600 {
		t.Errorf("remaining=%v, want 600 (latest)", r.remaining)
	}
	if r.tick != 1050 {
		t.Errorf("tick=%d, want 1050", r.tick)
	}
	if r.detectedBy != "agentB" {
		t.Errorf("detected_by=%q, want agentB (latest)", r.detectedBy)
	}
}

// TestRememberPOI_OlderWriteDoesNotDowngrade: an out-of-order older write must
// not pull richness/remaining/tick backwards.
func TestRememberPOI_OlderWriteDoesNotDowngrade(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()

	newer := POI{
		ID: "belt3", SystemID: "sysA", Type: "asteroid_belt",
		LastUpdatedTick: 1050, DetectedBy: "agentB",
		Resources:       []game.POIResource{{ResourceID: "ore_x", Richness: 0.9, Remaining: 600}},
	}
	older := POI{
		ID: "belt3", SystemID: "sysA", Type: "asteroid_belt",
		LastUpdatedTick: 1000, DetectedBy: "agentA",
		Resources:       []game.POIResource{{ResourceID: "ore_x", Richness: 0.5, Remaining: 1000}},
	}
	if err := kb.RememberPOI(ctx, newer); err != nil {
		t.Fatal(err)
	}
	if err := kb.RememberPOI(ctx, older); err != nil {
		t.Fatal(err)
	}

	r, _ := readResource(t, kb, "belt3", "ore_x")
	if r.richness != 0.9 {
		t.Errorf("richness=%v, want 0.9 (max, not downgraded)", r.richness)
	}
	if r.remaining != 600 {
		t.Errorf("remaining=%v, want 600 (newer tick kept, older ignored)", r.remaining)
	}
	if r.tick != 1050 {
		t.Errorf("tick=%d, want 1050 (not pulled back)", r.tick)
	}
}

// TestRememberPOI_OlderWriteDoesNotUnhide: a stale write must not flip the
// pois.hidden flag or stomp the tick.
func TestRememberPOI_OlderWriteDoesNotUnhide(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()

	if err := kb.RememberPOI(ctx, POI{
		ID: "p1", SystemID: "sysA", Name: "P", Type: "station",
		Hidden: false, LastUpdatedTick: 1050, DetectedBy: "agentB",
	}); err != nil {
		t.Fatal(err)
	}
	// Older write claims hidden=true; must be ignored.
	if err := kb.RememberPOI(ctx, POI{
		ID: "p1", SystemID: "sysA", Name: "P", Type: "station",
		Hidden: true, LastUpdatedTick: 1000, DetectedBy: "agentA",
	}); err != nil {
		t.Fatal(err)
	}

	var hidden bool
	var tick int64
	if err := kb.db.QueryRow(`SELECT hidden, last_updated_tick FROM pois WHERE id=?`, "p1").
		Scan(&hidden, &tick); err != nil {
		t.Fatal(err)
	}
	if hidden {
		t.Error("hidden=true, want false (older write must not re-hide)")
	}
	if tick != 1050 {
		t.Errorf("tick=%d, want 1050 (not pulled back)", tick)
	}
}
