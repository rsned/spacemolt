package market

import (
	"context"
	"path/filepath"
	"testing"
)

func newMissionTestCollector(t *testing.T) *Collector {
	t.Helper()
	c, err := Open(Config{DBPath: filepath.Join(t.TempDir(), "test.db")})
	if err != nil {
		t.Fatalf("open collector: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestRecordAndGetMissionResult(t *testing.T) {
	c := newMissionTestCollector(t)
	ctx := context.Background()
	r := MissionResult{
		AgentID: "engineer-1", MissionID: "m-123", TemplateID: "",
		MissionType: "delivery", Title: "Supply Run: Steel",
		FromBaseID: "haven_station", ToBaseID: "sol_station",
		ItemID: "steel", Qty: 20,
		ExpectedReward: 2500, CreditsEarned: 2500, ItemCost: 400, FuelCost: 60,
		Jumps: 2, Outcome: "completed",
		AcceptedAt: "2026-07-16T10:00:00Z", FinishedAt: "2026-07-16T10:20:00Z",
		AcceptedTick: 1000, FinishedTick: 1120,
		CreatedAt: "2026-07-16T10:20:01Z",
	}
	if err := c.RecordMissionResult(ctx, r); err != nil {
		t.Fatalf("record: %v", err)
	}
	// Second agent's row must not leak into the first agent's query.
	r2 := r
	r2.AgentID, r2.MissionID, r2.Outcome = "engineer-2", "m-456", "abandoned"
	if err := c.RecordMissionResult(ctx, r2); err != nil {
		t.Fatalf("record 2: %v", err)
	}

	got, err := c.GetMissionResults(ctx, "engineer-1", 0)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d rows, want 1", len(got))
	}
	if got[0].MissionID != "m-123" || got[0].CreditsEarned != 2500 || got[0].Outcome != "completed" {
		t.Fatalf("row mismatch: %+v", got[0])
	}

	all, err := c.GetMissionResults(ctx, "", 0)
	if err != nil {
		t.Fatalf("get all: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("got %d rows for all agents, want 2", len(all))
	}
}
