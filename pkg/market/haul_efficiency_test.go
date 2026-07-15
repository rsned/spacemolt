package market

import (
	"context"
	"testing"
	"time"
)

func TestHaulEfficiencySince(t *testing.T) {
	c := newHaulTestCollector(t) // shared helper in haul_results_test.go
	ctx := context.Background()
	rows := []HaulResult{
		{OppID: 1, AgentID: "trader-1", ItemID: "iron", Qty: 10, RealizedProfit: 1000, JumpsTraveled: 4, SoldAt: "2026-07-15T10:00:00Z"},
		{OppID: 2, AgentID: "trader-1", ItemID: "iron", Qty: 10, RealizedProfit: 500, JumpsTraveled: 1, SoldAt: "2026-07-15T11:00:00Z"},
		{OppID: 3, AgentID: "trader-2", ItemID: "gold", Qty: 5, RealizedProfit: 900, JumpsTraveled: 3, SoldAt: "2026-07-15T10:30:00Z"},
		// Old row (before the 07-10 window):
		{OppID: 4, AgentID: "trader-1", ItemID: "iron", Qty: 10, RealizedProfit: 300, JumpsTraveled: 2, SoldAt: "2026-07-01T10:00:00Z"},
		// Zero-jump degenerate row (must be excluded everywhere):
		{OppID: 5, AgentID: "trader-2", ItemID: "gold", Qty: 1, RealizedProfit: 50, JumpsTraveled: 0, SoldAt: "2026-07-15T12:00:00Z"},
	}
	for _, r := range rows {
		if err := c.RecordHaulResult(ctx, r); err != nil {
			t.Fatalf("RecordHaulResult: %v", err)
		}
	}

	since, _ := time.Parse(time.RFC3339, "2026-07-10T00:00:00Z")
	perAgent, fleet, err := c.HaulEfficiencySince(ctx, since)
	if err != nil {
		t.Fatalf("HaulEfficiencySince: %v", err)
	}
	byAgent := map[string]HaulEfficiencyRow{}
	for _, r := range perAgent {
		byAgent[r.AgentID] = r
	}
	if g := byAgent["trader-1"]; g.Hauls != 2 || g.SumProfit != 1500 || g.SumJumps != 5 {
		t.Errorf("trader-1 windowed = %+v, want Hauls2 Profit1500 Jumps5", g)
	}
	if g := byAgent["trader-2"]; g.Hauls != 1 || g.SumProfit != 900 || g.SumJumps != 3 {
		t.Errorf("trader-2 windowed = %+v, want Hauls1 Profit900 Jumps3 (zero-jump excluded)", g)
	}
	if fleet.Hauls != 3 || fleet.SumProfit != 2400 || fleet.SumJumps != 8 {
		t.Errorf("fleet windowed = %+v, want Hauls3 Profit2400 Jumps8", fleet)
	}

	_, fleetAll, err := c.HaulEfficiencySince(ctx, time.Time{})
	if err != nil {
		t.Fatalf("HaulEfficiencySince(all): %v", err)
	}
	if fleetAll.Hauls != 4 || fleetAll.SumProfit != 2700 || fleetAll.SumJumps != 10 {
		t.Errorf("fleet all-time = %+v, want Hauls4 Profit2700 Jumps10", fleetAll)
	}
}
