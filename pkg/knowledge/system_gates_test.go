package knowledge

import (
	"context"
	"testing"
)

// v0.720.0: get_map lists player-built jump gates per system with an online
// flag. The map import keeps them in system_gates, reconciled per system like
// connections, so a dismantled gate disappears and an offline one is marked.
func TestUpsertSystemFromMap_StoresAndReconcilesGates(t *testing.T) {
	t.Parallel()
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()
	ctx := context.Background()

	if err := kb.UpsertSystemFromMap(ctx, MapSystemData{
		ID: "cloverfield", Name: "Cloverfield",
		Connections:     []string{"far_reach"},
		Gates:           []MapGate{{To: "far_reach", Online: true}, {To: "deep_cluster", Online: false}},
		LastUpdatedTick: 100,
	}); err != nil {
		t.Fatalf("UpsertSystemFromMap: %v", err)
	}
	gates, err := kb.SystemGates(ctx)
	if err != nil {
		t.Fatalf("SystemGates: %v", err)
	}
	want := map[string]bool{"far_reach": true, "deep_cluster": false}
	if len(gates) != 2 {
		t.Fatalf("gates = %+v, want 2", gates)
	}
	for _, g := range gates {
		if g.From != "cloverfield" || want[g.To] != g.Online || g.LastUpdatedTick != 100 {
			t.Errorf("gate %+v", g)
		}
	}

	// Next import: deep_cluster's gate dismantled, far_reach's gone offline.
	if err := kb.UpsertSystemFromMap(ctx, MapSystemData{
		ID: "cloverfield", Name: "Cloverfield",
		Gates:           []MapGate{{To: "far_reach", Online: false}},
		LastUpdatedTick: 200,
	}); err != nil {
		t.Fatalf("UpsertSystemFromMap (again): %v", err)
	}
	gates, _ = kb.SystemGates(ctx)
	if len(gates) != 1 || gates[0].To != "far_reach" || gates[0].Online || gates[0].LastUpdatedTick != 200 {
		t.Errorf("after re-import gates = %+v, want far_reach offline only", gates)
	}
}
