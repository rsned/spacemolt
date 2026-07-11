package main

import (
	"reflect"
	"testing"

	"github.com/rsned/spacemolt/pkg/overmind/plans"
	"github.com/rsned/spacemolt/pkg/overmind/supervisor"
)

func TestRosterFromFleetFiltersCraftsmen(t *testing.T) {
	specs := []supervisor.WorkerSpec{
		{AgentID: "hauler-1", Role: "hauler", Station: "S1"},
		{AgentID: "craftsman-1", Role: "craftsman", Station: "S2"},
		{AgentID: "craftsman-2", Role: "craftsman", Station: "S3"},
		{AgentID: "miner-1", Role: "miner", Station: "S4"},
	}

	got := rosterFromFleet(specs)
	want := []plans.RosterAgent{
		{AgentID: "craftsman-1", Station: "S2"},
		{AgentID: "craftsman-2", Station: "S3"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rosterFromFleet() = %+v, want %+v", got, want)
	}
}

func TestRosterFromFleetNoCraftsmen(t *testing.T) {
	specs := []supervisor.WorkerSpec{
		{AgentID: "hauler-1", Role: "hauler", Station: "S1"},
	}
	if got := rosterFromFleet(specs); len(got) != 0 {
		t.Fatalf("rosterFromFleet() = %+v, want empty", got)
	}
}

func TestManagedFromFleet(t *testing.T) {
	specs := []supervisor.WorkerSpec{
		{AgentID: "marketbot-1", Role: "marketbot", Station: "S1"},
		{AgentID: "marketbot-2", Role: "marketbot", Station: "S2"},
	}
	got := managedFromFleet(specs)
	want := map[string]string{
		"marketbot-1": "S1",
		"marketbot-2": "S2",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("managedFromFleet() = %+v, want %+v", got, want)
	}
}
