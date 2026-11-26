package main

import (
	"testing"
	"time"
)

// The fixture is pirate-9's real Nitrogen Bladder run of 2026-11-25: a 1,288
// credit need that cost two refuels and a case of slugs.
func TestComputePnLChargesAJobWhatWasSpentWhileHeld(t *testing.T) {
	events := []event{
		// Before the accept: counts for the hunter, not for the job.
		{Type: "ship.refuel", CreatedAt: "2026-11-25T01:00:00Z", Data: map[string]string{"fuel": "100", "total_cost": "1000"}},
		{Type: "mission.accepted", CreatedAt: "2026-11-25T02:00:00Z", Data: map[string]string{"mission_id": "creature_a~1"}},
		{Type: "ship.refuel", CreatedAt: "2026-11-25T02:10:00Z", Data: map[string]string{"fuel": "225", "total_cost": "2925"}},
		{Type: "trading.exchange_fill", CreatedAt: "2026-11-25T02:20:00Z", Data: map[string]string{"role": "buyer", "total": "112", "sales_tax": "1"}},
		{Type: "mission.completed", CreatedAt: "2026-11-25T03:30:00Z", Data: map[string]string{"mission_id": "creature_a~1", "title": "Station Need: Nitrogen Bladder", "credits": "1288"}},
		// Accepted before the window: reward counts, spend is unknown.
		{Type: "mission.completed", CreatedAt: "2026-11-25T04:00:00Z", Data: map[string]string{"mission_id": "creature_b~2", "credits": "500"}},
		{Type: "mission.accepted", CreatedAt: "2026-11-25T04:10:00Z", Data: map[string]string{"mission_id": "creature_c~3"}},
		{Type: "mission.abandoned", CreatedAt: "2026-11-25T05:00:00Z", Data: map[string]string{"mission_id": "creature_c~3"}},
	}
	p := computePnL("pirate-9", events)

	if p.Completed != 2 || p.Abandoned != 1 || p.Income != 1788 {
		t.Fatalf("completed/abandoned/income: got %d/%d/%d want 2/1/1788", p.Completed, p.Abandoned, p.Income)
	}
	if p.Fuel != 3925 || p.FuelUnits != 325 || p.Buys != 113 {
		t.Fatalf("fuel/units/buys: got %d/%d/%d want 3925/325/113", p.Fuel, p.FuelUnits, p.Buys)
	}
	if got, want := p.Net(), 1788-3925-113; got != want {
		t.Errorf("net: got %d want %d", got, want)
	}
	j := p.Jobs[0]
	if !j.Paired || j.Spend != 2925+113 || j.Net() != 1288-3038 || j.Hours != 1.5 {
		t.Errorf("job: paired=%v spend=%d net=%d hours=%v", j.Paired, j.Spend, j.Net(), j.Hours)
	}
	if p.Jobs[1].Paired {
		t.Error("a completion with no accept in the window must not be paired")
	}
	if p.Losers() != 1 {
		t.Errorf("losers: got %d want 1", p.Losers())
	}
	if p.LastEvent != "2026-11-25T05:00:00Z" {
		t.Errorf("last event: %s", p.LastEvent)
	}
}

func TestParseSince(t *testing.T) {
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	if got, err := parseSince("12h", now); err != nil || !got.Equal(now.Add(-12*time.Hour)) {
		t.Errorf("duration: %v %v", got, err)
	}
	if got, err := parseSince("2026-11-25T00:27:00Z", now); err != nil || got.Hour() != 0 || got.Minute() != 27 {
		t.Errorf("instant: %v %v", got, err)
	}
	if _, err := parseSince("yesterday", now); err == nil {
		t.Error("want an error for an unparseable --since")
	}
}

func TestAgentOrderIsNumeric(t *testing.T) {
	if !agentOrder("pirate-5", "pirate-10") || agentOrder("pirate-13", "pirate-6") {
		t.Error("pirate-5 must sort before pirate-10")
	}
}
