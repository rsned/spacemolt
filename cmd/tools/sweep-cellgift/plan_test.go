package main

import (
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/handoff"
	"github.com/rsned/spacemolt/pkg/overmind/balances"
)

var (
	testNow = time.Date(2026, 11, 13, 20, 0, 0, 0, time.UTC)
	testCfg = config{Holder: "marketbot_010", POI: "ehr", Station: "ehr-base", Item: "fuel_cell", Qty: 40, Cooldown: 12 * time.Hour}
)

func TestPlanGiftsDockedExplorerAtStation(t *testing.T) {
	ws := []balances.LiveRecord{
		{AgentID: "explorer-2", Docked: true, POI: "ehr", CargoUsed: 20, CargoCapacity: 165},
		{AgentID: "explorer-4", Docked: false, POI: "ehr"},
		{AgentID: "explorer-5", Docked: true, POI: "elsewhere", CargoCapacity: 165},
	}
	got := plan(ws, nil, testNow, testCfg)
	if len(got) != 1 {
		t.Fatalf("got %d records %+v, want 1 for explorer-2", len(got), got)
	}
	r := got[0]
	if r.Recipient != "explorer-2" || r.Holder != "marketbot_010" || r.Station != "ehr-base" || r.ItemID != "fuel_cell" || r.Qty != 40 {
		t.Errorf("record %+v", r)
	}
}

func TestPlanCapsQtyAtFreeCargo(t *testing.T) {
	ws := []balances.LiveRecord{{AgentID: "explorer-2", Docked: true, POI: "ehr", CargoUsed: 150, CargoCapacity: 165}}
	got := plan(ws, nil, testNow, testCfg)
	if len(got) != 1 || got[0].Qty != 15 {
		t.Fatalf("got %+v, want one record of 15", got)
	}
	ws[0].CargoUsed = 165
	if got := plan(ws, nil, testNow, testCfg); len(got) != 0 {
		t.Fatalf("full hold planned %+v", got)
	}
}

func TestPlanSkipsRecentGiftAndHolder(t *testing.T) {
	ws := []balances.LiveRecord{
		{AgentID: "explorer-2", Docked: true, POI: "ehr", CargoCapacity: 165},
		{AgentID: "marketbot_010", Docked: true, POI: "ehr", CargoCapacity: 110},
	}
	recent := []handoff.Record{{ID: "sweepcells/explorer-2/x", Recipient: "explorer-2", UpdatedAt: testNow.Add(-time.Hour).Format(time.RFC3339)}}
	if got := plan(ws, recent, testNow, testCfg); len(got) != 0 {
		t.Fatalf("planned %+v despite a gift an hour ago", got)
	}
	old := []handoff.Record{{ID: "sweepcells/explorer-2/x", Recipient: "explorer-2", UpdatedAt: testNow.Add(-13 * time.Hour).Format(time.RFC3339)}}
	if got := plan(ws, old, testNow, testCfg); len(got) != 1 {
		t.Fatalf("planned %+v, want a fresh gift after the cooldown", got)
	}
}
