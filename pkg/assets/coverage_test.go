package assets

import (
	"context"
	"testing"
	"time"
)

// TestCoverageCountsStalePerSource pins the anti-rot surface. Every previous
// unsupervised capture job in this codebase died silently — daily-summary for
// 25 days, market-prune until the DB hit 62GB, the arbitrage scanner mimicking
// "no opportunities". Coverage is how this one gets noticed instead.
func TestCoverageCountsStalePerSource(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	fresh := now.Add(-10 * time.Minute)
	old := now.Add(-72 * time.Hour)

	if err := st.UpsertProfile(ctx, Profile{PlayerID: "fresh-1", CapturedAt: fresh}); err != nil {
		t.Fatalf("fresh profile: %v", err)
	}
	if err := st.UpsertProfile(ctx, Profile{PlayerID: "stale-1", CapturedAt: old}); err != nil {
		t.Fatalf("stale profile: %v", err)
	}
	if err := st.UpsertCarrier(ctx, "fresh-1", Carrier{Tier: "licensed"}, fresh); err != nil {
		t.Fatalf("carrier: %v", err)
	}

	rows, err := Coverage(ctx, st.DB(), now, 24*time.Hour)
	if err != nil {
		t.Fatalf("Coverage: %v", err)
	}
	by := map[string]CoverageRow{}
	for _, r := range rows {
		by[r.Source] = r
	}

	if got := by["agent_profile"]; got.Agents != 2 || got.Stale != 1 {
		t.Errorf("agent_profile = %d agents / %d stale, want 2 / 1", got.Agents, got.Stale)
	}
	if got := by["agent_carrier"]; got.Agents != 1 || got.Stale != 0 {
		t.Errorf("agent_carrier = %d agents / %d stale, want 1 / 0", got.Agents, got.Stale)
	}
}

// TestCoverageOnEmptyDBIsNotAnError pins that a brand-new ledger reports zeroes
// rather than failing the dashboard that reads it.
func TestCoverageOnEmptyDBIsNotAnError(t *testing.T) {
	st := openTestStore(t)
	rows, err := Coverage(context.Background(), st.DB(),
		time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC), 24*time.Hour)
	if err != nil {
		t.Fatalf("Coverage on empty DB: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("Coverage must report a row per source even when empty")
	}
	for _, r := range rows {
		if r.Agents != 0 || r.Stale != 0 {
			t.Errorf("%s: got %d agents / %d stale on an empty DB", r.Source, r.Agents, r.Stale)
		}
	}
}
