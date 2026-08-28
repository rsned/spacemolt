package worker

import (
	"context"
	"io"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// wildlifeStateClient supplies just the state huntWhere and huntPOIType read.
type wildlifeStateClient struct {
	game.GameClient
	st *game.State
}

func (c wildlifeStateClient) GetState() *game.State { return c.st }

// coverageKB records only what the capture layer was asked to store.
type coverageKB struct {
	knowledge.Base
	coverage  int
	sightings int
}

func (k *coverageKB) UpsertWildlifeSpecies(context.Context, []knowledge.WildlifeSpecies) error {
	return nil
}

func (k *coverageKB) RecordWildlifeSightings(_ context.Context, rows []knowledge.WildlifeSighting) error {
	k.sightings += len(rows)
	return nil
}
func (k *coverageKB) RecordWildlifeKill(context.Context, knowledge.WildlifeKill) error { return nil }
func (k *coverageKB) RecordWildlifeCoverage(_ context.Context, rows []knowledge.WildlifeCoverage) error {
	k.coverage += len(rows)
	return nil
}

// A look that found nothing is data. CaptureWildlifeNearby writes its coverage
// row first and unconditionally so an empty POI still leaves a trace -- its own
// comment names the failure it prevents, "a fully surveyed system reading as
// half unvisited" -- but the caller returned before reaching it.
//
// Wildlife is transient: ashford_ice_shelf reported 0 creatures at 12:41:35 and
// 3 at 12:43:16 on 2026-08-28. Only the coverage row separates "looked, found
// none" from "never looked".
func TestHuntCaptureWildlife_RecordsAnEmptyLook(t *testing.T) {
	kb := &coverageKB{}
	deps := HuntDeps{KB: kb, AgentID: "pirate-7", Client: wildlifeStateClient{st: &game.State{}}}

	huntCaptureWildlife(context.Background(), deps, io.Discard,
		serverapi.GetNearbyResponse{POIID: "ashford_ice_shelf"}, "ashford_ice_shelf")

	if kb.coverage != 1 {
		t.Errorf("coverage rows = %d, want 1; an empty look must still be recorded", kb.coverage)
	}
	if kb.sightings != 0 {
		t.Errorf("sightings = %d, want 0", kb.sightings)
	}
}

func TestHuntCaptureWildlife_StillRecordsSightings(t *testing.T) {
	kb := &coverageKB{}
	deps := HuntDeps{KB: kb, AgentID: "pirate-7", Client: wildlifeStateClient{st: &game.State{}}}

	huntCaptureWildlife(context.Background(), deps, io.Discard, serverapi.GetNearbyResponse{
		POIID: "ashford_ice_shelf",
		Creatures: []serverapi.NearbyCreature{
			{Species: "rime_grazer", Name: "Rime-Grazer", Role: "grazer", MaxHull: 70},
		},
	}, "ashford_ice_shelf")

	if kb.coverage != 1 {
		t.Errorf("coverage rows = %d, want 1", kb.coverage)
	}
	if kb.sightings != 1 {
		t.Errorf("sightings = %d, want 1", kb.sightings)
	}
}
