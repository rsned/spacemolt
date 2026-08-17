package main

import (
	"encoding/json"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// TestSurveyReplyCarriesWildlifeCensus guards the decode path the bare `survey`
// command now uses. survey_system terminates with an action_result frame nesting
// the payload under "result", so the census is invisible unless the frame is
// unwrapped first — the same trap that once made every other survey field read
// zero. Both shapes must yield the wildlife array and the bloom state.
func TestSurveyReplyCarriesWildlifeCensus(t *testing.T) {
	wrapped := []byte(`{"command":"survey_system","tick":1637900,"result":{
		"system_id":"kochab","system_name":"Kochab","survey_power":168,
		"bloom_intensity":1.4,"bloom_status":"rising",
		"wildlife":[
			{"species":"belt_grazer","name":"Belt-Grazer","role":"grazer","estimate":40,"abundance":"abundant","ranched":0},
			{"species":"slag_tortoise","name":"Slag-Tortoise","role":"grazer","estimate":6,"abundance":"scarce"}],
		"newly_revealed":[],"already_revealed":[],"faint_signatures":[],"xp_gained":{"scanning":5},
		"message":"Survey complete."}}`)

	flat := []byte(`{"system_id":"kochab","system_name":"Kochab","survey_power":168,
		"bloom_intensity":0.5,"bloom_status":"fading",
		"wildlife":[{"species":"inkwyrm","name":"Inkwyrm","role":"grazer","estimate":3,"abundance":"scarce"}],
		"message":"Survey complete."}`)

	for _, tc := range []struct {
		name        string
		raw         []byte
		wantSpecies int
		wantBloom   string
		wantIntens  float64
	}{
		{"action_result wrapped", wrapped, 2, "rising", 1.4},
		{"legacy flat", flat, 1, "fading", 0.5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var resp serverapi.SurveySystemResponse
			if err := json.Unmarshal(unwrapActionResult(tc.raw), &resp); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if len(resp.Wildlife) != tc.wantSpecies {
				t.Fatalf("wildlife = %d species, want %d", len(resp.Wildlife), tc.wantSpecies)
			}
			if resp.BloomStatus != tc.wantBloom || resp.BloomIntensity != tc.wantIntens {
				t.Errorf("bloom = %q/%v, want %q/%v",
					resp.BloomStatus, resp.BloomIntensity, tc.wantBloom, tc.wantIntens)
			}
			if resp.Wildlife[0].Estimate == 0 {
				t.Error("estimate decoded as 0; the census would record an empty system")
			}
		})
	}
}

// TestPOITypeFromState covers the habitat lookup the manual get_nearby path uses.
// An unknown POI must resolve to "" so the species records no habitat, rather
// than inheriting a wrong one — habitat is the evidence a diet is later inferred
// from, so a guess there is worse than a blank.
func TestPOITypeFromState(t *testing.T) {
	st := &game.State{}
	st.System.POIs = []game.POI{
		{ID: "kochab_belt", Type: "belt"},
		{ID: "gold_run_cryobelt", Type: "cryobelt"},
	}

	for _, tc := range []struct{ poiID, want string }{
		{"kochab_belt", "belt"},
		{"gold_run_cryobelt", "cryobelt"},
		{"unknown_poi", ""},
		{"", ""},
	} {
		if got := poiTypeFromState(st, tc.poiID); got != tc.want {
			t.Errorf("poiTypeFromState(%q) = %q, want %q", tc.poiID, got, tc.want)
		}
	}
	if got := poiTypeFromState(nil, "kochab_belt"); got != "" {
		t.Errorf("poiTypeFromState(nil) = %q, want empty", got)
	}
}
