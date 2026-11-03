package knowledge

import (
	"context"
	"testing"
	"time"
)

func TestPruneMarketHistory(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()

	old := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	recent := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	var demand []DemandHistorySample
	var supply []SupplyHistorySample
	for _, at := range []time.Time{old, recent} {
		demand = append(demand, DemandHistorySample{StationID: "stn1", ItemID: "iron_ore", BucketAt: at, CapturedAt: at, BestPrice: 5})
		supply = append(supply, SupplyHistorySample{StationID: "stn1", ItemID: "iron_ore", BucketAt: at, CapturedAt: at, BestPrice: 6})
	}
	if err := kb.RecordDemandHistory(ctx, demand); err != nil {
		t.Fatalf("RecordDemandHistory: %v", err)
	}
	if err := kb.RecordSupplyHistory(ctx, supply); err != nil {
		t.Fatalf("RecordSupplyHistory: %v", err)
	}

	cutoff := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	d, s, err := kb.PruneMarketHistory(ctx, cutoff)
	if err != nil {
		t.Fatalf("PruneMarketHistory: %v", err)
	}
	if d != 1 || s != 1 {
		t.Fatalf("deleted demand=%d supply=%d, want 1 and 1", d, s)
	}

	gotD, err := kb.LoadDemandHistory(ctx, "iron_ore", "", 0)
	if err != nil {
		t.Fatalf("LoadDemandHistory: %v", err)
	}
	gotS, err := kb.LoadSupplyHistory(ctx, "iron_ore", "", 0)
	if err != nil {
		t.Fatalf("LoadSupplyHistory: %v", err)
	}
	if len(gotD) != 1 || !gotD[0].BucketAt.Equal(recent) || len(gotS) != 1 || !gotS[0].BucketAt.Equal(recent) {
		t.Fatalf("kept demand=%v supply=%v, want only the %s bucket", gotD, gotS, recent)
	}

	// Idempotent at the same cutoff.
	if d, s, err = kb.PruneMarketHistory(ctx, cutoff); err != nil || d != 0 || s != 0 {
		t.Fatalf("second prune = (%d, %d, %v), want (0, 0, nil)", d, s, err)
	}
}
