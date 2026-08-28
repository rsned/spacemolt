package main

import (
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

func surveyReply() serverapi.SurveySystemResponse {
	return serverapi.SurveySystemResponse{
		SystemID: "ashford",
		AlreadyRevealed: []serverapi.RevealedPOI{{
			ID: "prismatic_gas_pocket", Name: "Prismatic Gas Pocket", Type: "gas_cloud",
			Description: "A pocket of gas that refracts light in impossible patterns.",
			Resources: []serverapi.SurveyResource{{
				ResourceID: "prismatic_nebulite", Richness: 22, Remaining: 4500, MaxRemaining: 4500,
			}},
		}},
		NewlyRevealed: []serverapi.RevealedPOI{{
			ID: "deep_vein", Name: "Deep Vein", Type: "asteroid_belt",
			Resources: []serverapi.SurveyResource{{ResourceID: "iron_ore", Richness: 40, Remaining: 900, MaxRemaining: 1000}},
		}},
	}
}

// A hidden POI is newly revealed exactly ONCE, ever. Every survey after that
// reports it under already_revealed, which saveSurveyPOIs ignored -- so its
// resources could only be refreshed by physically flying to it. Live evidence
// 2026-08-28: prismatic_gas_pocket's resource row sat at tick 1,020,715 while
// the POI row read 1,736,298, and the survey reply carrying the current numbers
// was discarded.
func TestSurveyPOIs_CapturesAlreadyRevealedToo(t *testing.T) {
	got := surveyPOIsToKB(surveyReply(), "craftsman-1", 1736323)
	if len(got) != 2 {
		t.Fatalf("captured %d POIs, want 2 (newly + already revealed)", len(got))
	}
	byID := map[string]bool{}
	for _, p := range got {
		byID[p.ID] = true
	}
	if !byID["prismatic_gas_pocket"] {
		t.Error("already_revealed POI dropped")
	}
	if !byID["deep_vein"] {
		t.Error("newly_revealed POI dropped")
	}
}

// hidden is INTRINSIC to the POI, not "not yet revealed to you": get_poi
// reports hidden:true for prismatic_gas_pocket, a POI already revealed to us.
// saveSurveyPOIs hardcoded Hidden:false with the comment "it is no longer
// hidden", which wrote false over a true value -- and the upsert is
// tick-guarded, so the newer survey write clobbered the correct value.
//
// SurveyedPOI carries no hidden field, but appearing in either revealed list
// means the POI needed a survey to see, which is exactly what hidden means.
func TestSurveyPOIs_MarkRevealedPOIsAsHidden(t *testing.T) {
	for _, p := range surveyPOIsToKB(surveyReply(), "craftsman-1", 1736323) {
		if !p.Hidden {
			t.Errorf("%s: Hidden = false; a POI a survey had to reveal IS hidden", p.ID)
		}
	}
}

func TestSurveyPOIs_CarriesResourceCapacity(t *testing.T) {
	for _, p := range surveyPOIsToKB(surveyReply(), "craftsman-1", 1736323) {
		if len(p.Resources) != 1 {
			t.Fatalf("%s: %d resources", p.ID, len(p.Resources))
		}
		if p.Resources[0].MaxRemaining == 0 {
			t.Errorf("%s: MaxRemaining dropped; the survey reply carries it for free", p.ID)
		}
	}
}

func TestSurveyPOIs_EmptyReplyYieldsNothing(t *testing.T) {
	if got := surveyPOIsToKB(serverapi.SurveySystemResponse{}, "a", 1); len(got) != 0 {
		t.Errorf("got %d POIs from an empty reply", len(got))
	}
}
