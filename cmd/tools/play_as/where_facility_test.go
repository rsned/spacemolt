package main

import (
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

func TestFormatWhereFacility(t *testing.T) {
	rows := []knowledge.PublicFacility{
		{StationID: "grand_exchange_station", Level: 2, RentalFeePerRun: 50, OwnerFaction: "CRFT", LastSeenTick: 90},
		{StationID: "war_citadel", Level: 1, RentalFeePerRun: 60, OwnerFaction: "WAR", LastSeenTick: 80},
	}
	out := formatWhereFacility("ceramite_plating", rows, 120)
	for _, want := range []string{"ceramite_plating", "grand_exchange_station", "war_citadel", "50", "CRFT", "T2", "×3"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	// age = currentTick - LastSeenTick = 120-90 = 30 for the first row
	if !strings.Contains(out, "30") {
		t.Errorf("expected age 30 in:\n%s", out)
	}
}

func TestFormatWhereFacilityEmpty(t *testing.T) {
	out := formatWhereFacility("reactor_core", nil, 120)
	if !strings.Contains(out, "No public facility") {
		t.Errorf("expected empty note in:\n%s", out)
	}
}
