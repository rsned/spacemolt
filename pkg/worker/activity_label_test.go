package worker

import (
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/market"
)

func TestHaulActivityLabel(t *testing.T) {
	got := haulActivityLabel(market.ArbitrageOpportunity{
		ID: 100042, SourceUnits: 24, ItemName: "power_cell",
		FromStationName: "Sol Station", ToStationName: "Gold Run Extraction Hub",
	}, 100)
	want := "Opportunity #100042 · buying up to 24 of 24 power_cell · Sol Station → Gold Run Extraction Hub"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestHaulActivityLabelFallsBackToIDs(t *testing.T) {
	// Names unjoined -> fall back to ids so the line is never blank mid-fields.
	got := haulActivityLabel(market.ArbitrageOpportunity{
		ID: 7, SourceUnits: 5, ItemID: "iron_ore",
		FromStationID: "stn_a", ToStationID: "stn_b",
	}, 100)
	want := "Opportunity #7 · buying up to 5 of 5 iron_ore · stn_a → stn_b"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestHuntActivityLabel(t *testing.T) {
	job := huntJob{
		boardID: "first_hunt_belt_grazers", title: "First Hunt: Belt-Grazers",
		required: "belt_grazer", target: 3, baseline: 1,
	}
	if got, want := huntActivityLabel(job), "Hunt First Hunt: Belt-Grazers · belt_grazer 1/3"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	// A resumed instance has been seen with no title; the board id keeps the
	// line from reading "Hunt  · ...".
	job.title = ""
	if got, want := huntActivityLabel(job), "Hunt first_hunt_belt_grazers · belt_grazer 1/3"; got != want {
		t.Errorf("no title: got %q, want %q", got, want)
	}
	// A mission outside huntMissionSpecies hunts anything eligible, so there is
	// no species to name.
	job.required = ""
	if got, want := huntActivityLabel(job), "Hunt first_hunt_belt_grazers 1/3"; got != want {
		t.Errorf("no species: got %q, want %q", got, want)
	}
}

func TestHuntBreakOffLabel(t *testing.T) {
	job := huntJob{boardID: "first_hunt_belt_grazers", title: "First Hunt: Belt-Grazers", target: 3}
	if got, want := huntBreakOffLabel(job, 1), "Hunt First Hunt: Belt-Grazers · broke off at 1/3"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestMissionActivityLabel(t *testing.T) {
	if got := missionActivityLabel(nil); got != "" {
		t.Errorf("empty set: got %q, want \"\"", got)
	}
	single := []missionCandidate{{Entry: serverapi.MissionBoardEntry{Title: "Steel Plate Order"}}}
	if got := missionActivityLabel(single); got != "Mission Steel Plate Order" {
		t.Errorf("single: got %q", got)
	}
	multi := []missionCandidate{
		{Entry: serverapi.MissionBoardEntry{Title: "Steel Plate Order"}},
		{Entry: serverapi.MissionBoardEntry{Title: "Copper Requisition"}},
		{Entry: serverapi.MissionBoardEntry{Title: "Iron Supply Run"}},
	}
	if got := missionActivityLabel(multi); got != "Mission Steel Plate Order (+2 more)" {
		t.Errorf("multi: got %q", got)
	}
}
