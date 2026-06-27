package market

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func newHaulTestCollector(t *testing.T) *Collector {
	t.Helper()
	c, err := Open(Config{DBPath: filepath.Join(t.TempDir(), "test.db")})
	if err != nil {
		t.Fatalf("open collector: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestRecordAndGetHaulResult(t *testing.T) {
	c := newHaulTestCollector(t)
	ctx := context.Background()
	r := HaulResult{
		OppID: 42, AgentID: "trader-1", ItemID: "silicon_ore", Qty: 50,
		BuyPricePaid: 100, SellPriceGot: 130, RealizedProfit: (130 - 100) * 50,
		JumpsTraveled: 3,
		ClaimedAt:     "2026-06-26T10:00:00Z",
		ArrivedSrcAt:  "2026-06-26T10:01:00Z",
		BoughtAt:      "2026-06-26T10:01:30Z",
		ArrivedDstAt:  "2026-06-26T10:03:00Z",
		SoldAt:        "2026-06-26T10:03:30Z",
		ClaimedTick:   1000, ArrivedSrcTick: 1006, BoughtTick: 1007,
		ArrivedDstTick: 1015, SoldTick: 1016,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := c.RecordHaulResult(ctx, r); err != nil {
		t.Fatalf("RecordHaulResult: %v", err)
	}
	got, err := c.GetHaulResults(ctx, "trader-1", 10)
	if err != nil {
		t.Fatalf("GetHaulResults: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 row, got %d", len(got))
	}
	if got[0].RealizedProfit != 1500 || got[0].JumpsTraveled != 3 || got[0].ItemID != "silicon_ore" {
		t.Fatalf("round-trip mismatch: %+v", got[0])
	}
	if got[0].SoldTick != 1016 || got[0].ClaimedAt != "2026-06-26T10:00:00Z" {
		t.Fatalf("leg-stamp round-trip mismatch: %+v", got[0])
	}
	// agent filter excludes other agents
	other, err := c.GetHaulResults(ctx, "trader-9", 10)
	if err != nil {
		t.Fatalf("GetHaulResults(other): %v", err)
	}
	if len(other) != 0 {
		t.Fatalf("want 0 rows for trader-9, got %d", len(other))
	}
}

func TestRecordAndGetFleetSnapshot(t *testing.T) {
	c := newHaulTestCollector(t)
	ctx := context.Background()
	rows := []FleetSnapshot{
		{TS: "2026-06-26T10:00:00Z", AgentID: "trader-1", Role: "hauler", System: "Sol",
			Docked: true, Credits: 1000, Fuel: 50, MaxFuel: 100, CargoUsed: 10, CargoCapacity: 80},
		{TS: "2026-06-26T10:00:00Z", AgentID: "trader-2", Role: "hauler", System: "Sirius",
			Credits: 2000, Fuel: 90, MaxFuel: 100, CargoCapacity: 80},
	}
	if err := c.RecordFleetSnapshot(ctx, rows); err != nil {
		t.Fatalf("RecordFleetSnapshot: %v", err)
	}
	got, err := c.GetFleetSnapshots(ctx, "trader-1", 10)
	if err != nil {
		t.Fatalf("GetFleetSnapshots: %v", err)
	}
	if len(got) != 1 || got[0].Credits != 1000 || got[0].CargoCapacity != 80 || !got[0].Docked {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	// empty agent returns all
	all, err := c.GetFleetSnapshots(ctx, "", 10)
	if err != nil {
		t.Fatalf("GetFleetSnapshots(all): %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("want 2 rows for all agents, got %d", len(all))
	}
}

// TestHaulResultTotals covers the per-agent roll-up that fleet-report uses for
// its realized-profit + haul-count columns: count and summed realized_profit
// grouped by agent, from the (now truthful) haul_results rows.
func TestHaulResultTotals(t *testing.T) {
	c := newHaulTestCollector(t)
	ctx := context.Background()
	rows := []HaulResult{
		{OppID: 1, AgentID: "trader-1", ItemID: "iron", Qty: 10, RealizedProfit: 1500},
		{OppID: 2, AgentID: "trader-1", ItemID: "iron", Qty: 10, RealizedProfit: -300}, // a loss counts too
		{OppID: 3, AgentID: "trader-2", ItemID: "gold", Qty: 5, RealizedProfit: 800},
	}
	for _, r := range rows {
		if err := c.RecordHaulResult(ctx, r); err != nil {
			t.Fatalf("RecordHaulResult: %v", err)
		}
	}
	totals, err := c.HaulResultTotals(ctx)
	if err != nil {
		t.Fatalf("HaulResultTotals: %v", err)
	}
	if got := totals["trader-1"]; got.Hauls != 2 || got.RealizedProfit != 1200 {
		t.Errorf("trader-1: got %+v, want {Hauls:2 RealizedProfit:1200}", got)
	}
	if got := totals["trader-2"]; got.Hauls != 1 || got.RealizedProfit != 800 {
		t.Errorf("trader-2: got %+v, want {Hauls:1 RealizedProfit:800}", got)
	}
	if _, ok := totals["trader-9"]; ok {
		t.Errorf("trader-9 has no hauls; must be absent, got %+v", totals["trader-9"])
	}
}

// RecordFleetSnapshot on an empty batch is a no-op that must not error.
func TestRecordFleetSnapshotEmpty(t *testing.T) {
	c := newHaulTestCollector(t)
	if err := c.RecordFleetSnapshot(context.Background(), nil); err != nil {
		t.Fatalf("empty RecordFleetSnapshot: %v", err)
	}
}
