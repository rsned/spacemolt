package market

import (
	"context"
	"testing"
)

func TestRecordAndGetFreightResults(t *testing.T) {
	ctx := context.Background()
	c := newMissionTestCollector(t)

	r := FreightResult{
		AgentID:       "fighter-4",
		ContractID:    "ship_abc123",
		PackageID:     "ed9edd4346ed071f3c890ca73f9456b2",
		FromBaseID:    "treasure_cache_trading_post",
		ToBaseID:      "sol_central",
		ServiceLevel:  "standard",
		RouteHops:     3,
		BaseReward:    100,
		MaxSpeedBonus: 25,
		FuelCost:      40,
		CarrierPayout: 100,
		Outcome:       "delivered",
		AcceptedAt:    "2026-07-20T10:00:00Z",
		FinishedAt:    "2026-07-20T10:20:00Z",
		AcceptedTick:  1200,
		FinishedTick:  1256,
		CreatedAt:     "2026-07-20T10:20:00Z",
	}
	if err := c.RecordFreightResult(ctx, r); err != nil {
		t.Fatalf("RecordFreightResult: %v", err)
	}

	got, err := c.GetFreightResults(ctx, "fighter-4", 10)
	if err != nil {
		t.Fatalf("GetFreightResults: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 row, got %d", len(got))
	}
	if got[0].ContractID != "ship_abc123" || got[0].Outcome != "delivered" {
		t.Fatalf("round-trip mismatch: %+v", got[0])
	}
	if got[0].CarrierPayout != 100 || got[0].RouteHops != 3 {
		t.Fatalf("numeric round-trip mismatch: %+v", got[0])
	}
}

// A return is a normal, expected outcome (accept-then-verify), not an error path.
// Reason must survive the round trip so the canary can tell infeasible-at-accept
// from a buffer collapse in flight.
func TestFreightResultRecordsReturnReason(t *testing.T) {
	ctx := context.Background()
	c := newMissionTestCollector(t)

	if err := c.RecordFreightResult(ctx, FreightResult{
		AgentID:    "fighter-4",
		ContractID: "ship_def456",
		Outcome:    "returned_infeasible",
		Reason:     "deadline 40 ticks < needed 86",
		AcceptedAt: "2026-07-20T11:00:00Z",
		FinishedAt: "2026-07-20T11:00:10Z",
		CreatedAt:  "2026-07-20T11:00:10Z",
	}); err != nil {
		t.Fatalf("RecordFreightResult: %v", err)
	}
	got, err := c.GetFreightResults(ctx, "fighter-4", 10)
	if err != nil {
		t.Fatalf("GetFreightResults: %v", err)
	}
	if len(got) != 1 || got[0].Reason != "deadline 40 ticks < needed 86" {
		t.Fatalf("reason must round-trip, got %+v", got)
	}
}
