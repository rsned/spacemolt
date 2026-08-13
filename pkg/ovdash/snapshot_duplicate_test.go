package ovdash

import (
	"context"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/overmind/balances"
)

// TestHaulFleetReadsTheFileTheHaulOvermindWrites.
//
// The registry mapped Label "haul" to File "fleet", i.e. fleet-status.json. The
// haul overmind writes haul-status.json and has for some time, so the dashboard's
// haul panel was rendering a file nothing updates. Live 2026-08-13 it was 17
// hours stale: every haul agent shown at a long-dead position, the whole fleet
// permanently grey, and haul-status.json — fresh, 21 workers — never read at all.
func TestHaulFleetReadsTheFileTheHaulOvermindWrites(t *testing.T) {
	var haul *FleetDef
	for i := range Fleets {
		if Fleets[i].Label == "haul" {
			haul = &Fleets[i]
		}
	}
	if haul == nil {
		t.Fatal("no fleet labelled haul in the registry")
	}
	if haul.File != "haul" {
		t.Errorf("haul reads %q-status.json; the haul overmind writes haul-status.json", haul.File)
	}
}

// TestAnAgentInTwoFleetsAppearsOnceAtItsFreshestPosition.
//
// A stale fleet's agents are deliberately kept (greying out is UI policy, data
// completeness is ours) — but when a LIVE fleet reports the same agent, the two
// positions contradict each other and the stale one is simply wrong.
//
// Left duplicated, Diff keys by agent_id and sees the id at two systems on every
// single poll, so it emits a Moved event forever: the dashboard drew trader-10
// flying HR 8832 -> Gudja about once a second, indefinitely, while the real
// worker sat still. Both endpoints were positions no agent occupied.
func TestAnAgentInTwoFleetsAppearsOnceAtItsFreshestPosition(t *testing.T) {
	g, err := LoadGalaxy(context.Background(), fixtureKB(t))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	fresh := now.Add(-5 * time.Second).Format(time.RFC3339)
	old := now.Add(-17 * time.Hour).Format(time.RFC3339)

	// The dead fleet still lists it at where it was seventeen hours ago...
	writeStatus(t, dir, "haul", old, []balances.LiveRecord{
		{AgentID: "trader-10", Role: "hauler", System: "Sol", Seen: true, Healthy: true},
	})
	// ...while the fleet that actually owns it now reports the truth.
	writeStatus(t, dir, "unlock", fresh, []balances.LiveRecord{
		{AgentID: "trader-10", Role: "unlock", System: "Nova Terra", Seen: true, Healthy: true},
	})

	s, err := ReadSnapshot(dir, g, now, time.Minute)
	if err != nil {
		t.Fatalf("ReadSnapshot: %v", err)
	}
	var seen []AgentState
	for _, a := range append(append([]AgentState{}, s.Agents...), s.OffMap...) {
		if a.AgentID == "trader-10" {
			seen = append(seen, a)
		}
	}
	if len(seen) != 1 {
		t.Fatalf("trader-10 appears %d times; one agent cannot be in two systems", len(seen))
	}
	if seen[0].SystemName != "Nova Terra" {
		t.Errorf("kept the %s copy; the freshest capture is the truthful one", seen[0].SystemName)
	}
}

// TestASteadyFleetProducesNoPhantomMoves is the symptom itself: two consecutive
// reads of an unchanged world must diff to nothing. A duplicate agent makes this
// fail on every poll, which is what produced the endless flight animation.
func TestASteadyFleetProducesNoPhantomMoves(t *testing.T) {
	g, err := LoadGalaxy(context.Background(), fixtureKB(t))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	writeStatus(t, dir, "haul", now.Add(-17*time.Hour).Format(time.RFC3339), []balances.LiveRecord{
		{AgentID: "trader-10", Role: "hauler", System: "Sol", Seen: true, Healthy: true},
	})
	writeStatus(t, dir, "unlock", now.Add(-5*time.Second).Format(time.RFC3339), []balances.LiveRecord{
		{AgentID: "trader-10", Role: "unlock", System: "Nova Terra", Seen: true, Healthy: true},
	})

	a, err := ReadSnapshot(dir, g, now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	b, err := ReadSnapshot(dir, g, now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if d := Diff(a, b); len(d.Moved) != 0 {
		t.Errorf("nothing moved, yet Diff reported %+v — this animates forever", d.Moved)
	}
}
