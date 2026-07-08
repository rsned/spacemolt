package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

func TestSuggestSystems(t *testing.T) {
	systems := []knowledge.System{
		{ID: "nexus_prime", Name: "Nexus Prime"},
		{ID: "nova_terra", Name: "Nova Terra"},
		{ID: "sol", Name: "Sol"},
	}
	tests := []struct {
		name  string
		query string
		want  []string
	}{
		{"typo within distance 2", "nexis_prime", []string{"nexus_prime"}},
		{"substring on id", "nova", []string{"nova_terra"}},
		{"no match", "zzzzzz", nil},
		{"caps insensitive substring", "SOL", []string{"sol"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := suggestSystems(tt.query, systems)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("suggestSystems(%q) = %v, want %v", tt.query, got, tt.want)
			}
		})
	}
}

func TestSuggestSystemsLimitsToThree(t *testing.T) {
	systems := []knowledge.System{
		{ID: "node_a"}, {ID: "node_b"}, {ID: "node_c"}, {ID: "node_d"},
	}
	if got := suggestSystems("node", systems); len(got) != 3 {
		t.Errorf("len(suggestSystems) = %d, want 3 (capped)", len(got))
	}
}

func TestRenderSystemVisited(t *testing.T) {
	sys := &knowledge.System{
		ID: "nexus_prime", Name: "Nexus Prime", Empire: "solarian",
		PoliceLevel: 3, SecurityStatus: "high_sec", Description: "A core hub.",
		LastVisitedTick: 1000,
		Connections: []knowledge.SystemConnection{
			{SystemID: "sol", Distance: 4},
			{SystemID: "procyon", Distance: 7},
		},
	}
	pois := []knowledge.POI{
		{ID: "nexus_stn", Name: "Nexus Station", Type: "station", Services: []string{"refuel", "market"}},
		{ID: "belt_a", Name: "Asteroid Belt", Type: "asteroid", Resources: []game.POIResource{{ResourceID: "iron", Richness: 0.8}}},
		{ID: "star_a", Name: "Alpha Star", Type: "star", Class: "G2 V"},
	}
	names := map[string]string{"sol": "Sol", "procyon": "Procyon"}

	got := renderSystem(sys, pois, names, 1360) // 360 ticks after visit

	for _, want := range []string{
		"Nexus Prime (nexus_prime)", "Solarian",
		"Security: 3 - high_sec", "Visited", "A core hub.",
		"Sol", "sol", "4 LY",
		"Nexus Station", "refuel, market",
		"iron(0.8)", "G2 V",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("renderSystem output missing %q\n---\n%s", want, got)
		}
	}
}

func TestRenderSystemUnexplored(t *testing.T) {
	sys := &knowledge.System{
		ID: "unknown_edge", Name: "Unknown Edge", PoliceLevel: 1,
		SecurityStatus: "low_sec", LastVisitedTick: 0,
	}
	got := renderSystem(sys, nil, nil, 500)
	if !strings.Contains(got, "Unexplored (map-import only)") {
		t.Errorf("expected unexplored marker, got:\n%s", got)
	}
	if !strings.Contains(got, "untrusted") {
		t.Errorf("expected untrusted security marker, got:\n%s", got)
	}
	if !strings.Contains(got, "POIs:\n  (none)") {
		t.Errorf("expected (none) POIs, got:\n%s", got)
	}
}

func TestRenderSystemHiddenPOIAndConnFallback(t *testing.T) {
	sys := &knowledge.System{
		ID: "s1", Name: "S1", LastVisitedTick: 10,
		Connections: []knowledge.SystemConnection{{SystemID: "ghost", Distance: 2}},
	}
	pois := []knowledge.POI{{ID: "wh1", Name: "Wormhole", Type: "wormhole", Hidden: true}}
	got := renderSystem(sys, pois, map[string]string{}, 20) // ghost not in map
	if !strings.Contains(got, "Wormhole (hidden)") {
		t.Errorf("expected hidden marker, got:\n%s", got)
	}
	if !strings.Contains(got, "ghost") { // falls back to id in name slot
		t.Errorf("expected connection id fallback, got:\n%s", got)
	}
}
