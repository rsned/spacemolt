package knowledge

import (
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// The three repeater classes seen live (2026-11-12) become three species with
// their hull constants; an unclassified one is dropped.
func TestNearbyWithRepeatersClassifies(t *testing.T) {
	creatures := []serverapi.NearbyCreature{{CreatureID: "crt_1", Species: "belt_grazer", MaxHull: 300}}
	reps := []serverapi.RepeaterInfo{
		{RepeaterID: "repeater_a", Name: "Repeater Monolith", ShipClass: "monolith", Hull: 6400, MaxHull: 6400, MaxShield: 9600},
		{RepeaterID: "repeater_b", Name: "Repeater Pane", ShipClass: "pane", Hull: 400, MaxHull: 400, MaxShield: 600},
		{RepeaterID: "repeater_c", Name: "Repeater Spire", ShipClass: "spire", Hull: 1600, MaxHull: 1600, InBattle: true},
		{RepeaterID: "repeater_d", Name: "Repeater ?"},
	}
	got := NearbyWithRepeaters(creatures, reps)
	if len(got) != 4 {
		t.Fatalf("len = %d, want 4 (1 creature + 3 classified repeaters)", len(got))
	}
	if len(creatures) != 1 {
		t.Fatal("input slice modified")
	}
	species, sightings := WildlifeFromNearby(got, "gloaming", "the_threshold", "", "craftsman-1", 1)
	want := map[string]int{"belt_grazer": 300, "repeater_monolith": 6400, "repeater_pane": 400, "repeater_spire": 1600}
	for _, s := range species {
		if want[s.Species] != s.MaxHull {
			t.Errorf("%s max_hull %d, want %d", s.Species, s.MaxHull, want[s.Species])
		}
		if s.Species != "belt_grazer" && s.Role != RepeaterRole {
			t.Errorf("%s role %q", s.Species, s.Role)
		}
	}
	if len(species) != 4 || len(sightings) != 4 {
		t.Fatalf("species %d sightings %d, want 4/4", len(species), len(sightings))
	}
	for _, s := range sightings {
		if s.Species == "repeater_spire" && s.InCombat != 1 {
			t.Errorf("spire in_combat %d, want 1", s.InCombat)
		}
	}
}

func TestNearbyWithRepeatersNoneReturnsInput(t *testing.T) {
	c := []serverapi.NearbyCreature{{CreatureID: "crt_1", Species: "x"}}
	if got := NearbyWithRepeaters(c, nil); len(got) != 1 {
		t.Fatalf("got %v", got)
	}
}
