package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// Creature hull stopped being a species constant on 2026-10-14: three Young
// Cloudwhales at yarrowfield_vapor_fields read 1368/1433/1408 and are expected
// to climb as they mature. wildlife_species keeps ONE max_hull per species
// (ratcheted up), and wildlife_sightings records neither hull nor creature_id,
// so every per-individual number is currently discarded.
//
// This collector is deliberately a temporary append-only JSONL dump, not a KB
// table: the stage vocabulary is unknown (these are the first ever sighted),
// and a migration cannot be edited once applied. Collect first, design after.
func TestAppendCreatureObservationsWritesOneLinePerCreature(t *testing.T) {
	dir := t.TempDir()
	creatures := []serverapi.NearbyCreature{
		{CreatureID: "lev_aaa", Species: "cloudwhale", Name: "Young Cloudwhale", Role: "grazer", Hull: 1368, MaxHull: 1368},
		{CreatureID: "lev_bbb", Species: "cloudwhale", Name: "Young Cloudwhale", Role: "grazer", Hull: 1433, MaxHull: 1433},
	}

	if err := appendCreatureObservations(dir, creatures, "yarrowfield", "yarrowfield_vapor_fields", "craftsman-1", 2137000); err != nil {
		t.Fatalf("appendCreatureObservations: %v", err)
	}

	path := filepath.Join(dir, "observations.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2 (one per creature)\n%s", len(lines), data)
	}

	var got creatureObservation
	if err := json.Unmarshal([]byte(lines[1]), &got); err != nil {
		t.Fatalf("line 2 is not valid JSON: %v", err)
	}
	// The per-individual hull and the stable creature id are the whole point:
	// without both, a growth curve cannot be reconstructed.
	if got.CreatureID != "lev_bbb" || got.MaxHull != 1433 || got.Hull != 1433 {
		t.Errorf("got %+v, want creature lev_bbb at hull 1433", got)
	}
	if got.Name != "Young Cloudwhale" {
		t.Errorf("name = %q; the stage-qualified name is how the vocabulary gets learned", got.Name)
	}
	if got.POIID != "yarrowfield_vapor_fields" || got.GameTick != 2137000 {
		t.Errorf("got %+v, want the POI and tick that locate the observation", got)
	}
	if got.ObservedUTC == "" {
		t.Error("ObservedUTC empty; a growth curve needs a wall-clock stamp")
	}
}

// Appending must accumulate across visits -- the same creature re-observed
// hours later is exactly the signal being collected, so a second call must not
// truncate the first.
func TestAppendCreatureObservationsAccumulates(t *testing.T) {
	dir := t.TempDir()
	c := []serverapi.NearbyCreature{{CreatureID: "lev_aaa", Species: "cloudwhale", MaxHull: 1368}}
	for range 3 {
		if err := appendCreatureObservations(dir, c, "yarrowfield", "yarrowfield_vapor_fields", "craftsman-1", 1); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	data, _ := os.ReadFile(filepath.Join(dir, "observations.jsonl"))
	if n := len(strings.Split(strings.TrimSpace(string(data)), "\n")); n != 3 {
		t.Errorf("got %d lines after 3 appends, want 3", n)
	}
}

// An empty dir disables collection: this is temporary scaffolding and must be
// switchable off without a rebuild.
func TestAppendCreatureObservationsDisabledByEmptyDir(t *testing.T) {
	if err := appendCreatureObservations("", []serverapi.NearbyCreature{{CreatureID: "x"}}, "s", "p", "a", 1); err != nil {
		t.Errorf("empty dir must be a silent no-op, got %v", err)
	}
}
