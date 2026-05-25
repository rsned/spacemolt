package spar

import (
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

func TestIsNonEmpireArena(t *testing.T) {
	tests := []struct {
		name string
		sys  game.SystemData
		want bool
	}{
		{"empire policed", game.SystemData{ID: "sol", Empire: "terran", SecurityStatus: "High Security", PoliceLevel: 5}, false},
		{"lawless", game.SystemData{ID: "ross_128", Empire: "", SecurityStatus: "Lawless", PoliceLevel: 0}, true},
		{"stronghold", game.SystemData{ID: "den", IsStronghold: true, PoliceLevel: 3}, true},
		{"unowned no police", game.SystemData{ID: "void", Empire: "", PoliceLevel: 0}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsNonEmpireArena(tt.sys); got != tt.want {
				t.Fatalf("IsNonEmpireArena(%s) = %v, want %v", tt.sys.ID, got, tt.want)
			}
		})
	}
}

func TestSelectArena(t *testing.T) {
	treasure := game.SystemData{ID: "treasure_cache", Empire: "terran", SecurityStatus: "High Security", PoliceLevel: 5}
	ross := game.SystemData{
		ID: "ross_128", Empire: "", SecurityStatus: "Lawless", PoliceLevel: 0,
		POIs:        []game.POI{{ID: "ross_128-belt", Type: "asteroid_belt"}},
		Connections: []game.ConnectionInfo{{SystemID: "treasure_cache"}},
	}
	// Lawless but no safe-adjacent neighbor and no POI: should be rejected.
	deepVoid := game.SystemData{ID: "deep_void", Empire: "", PoliceLevel: 0}
	// Lawless, has a POI, but all neighbors are also lawless — no safe adjacency.
	isolated := game.SystemData{
		ID: "pirate_pocket", Empire: "", PoliceLevel: 0,
		POIs:        []game.POI{{ID: "pirate_pocket-wreck", Type: "wreck"}},
		Connections: []game.ConnectionInfo{{SystemID: "deep_void"}},
	}

	byID := map[string]game.SystemData{"treasure_cache": treasure, "ross_128": ross, "deep_void": deepVoid, "pirate_pocket": isolated}
	all := []game.SystemData{treasure, ross, isolated, deepVoid}
	reachable := func(string) bool { return true }

	got, err := SelectArena(all, byID, reachable)
	if err != nil {
		t.Fatalf("SelectArena error: %v", err)
	}
	// pirate_pocket has a POI but no safe-adjacent neighbor, so it must be
	// rejected; ross_128 (the only fully-qualifying arena) must win.
	if got != "ross_128" {
		t.Fatalf("SelectArena = %q, want ross_128", got)
	}
}

func TestSelectArena_NoneFound(t *testing.T) {
	sol := game.SystemData{ID: "sol", Empire: "terran", PoliceLevel: 5}
	byID := map[string]game.SystemData{"sol": sol}
	if _, err := SelectArena([]game.SystemData{sol}, byID, func(string) bool { return true }); err == nil {
		t.Fatal("SelectArena should error when no non-empire arena exists")
	}
}
