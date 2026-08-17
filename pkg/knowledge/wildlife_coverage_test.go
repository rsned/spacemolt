package knowledge

import (
	"context"
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// TestCaptureWildlifeNearby_EmptyPOIIsRecorded is the whole reason this table
// exists. On 2026-08-17 the operator ran get_nearby at every POI in Goldcrest;
// goldcrest_star held no creatures, so nothing was written, and the database was
// then read back as "2 of 5 POIs never checked". A look that found nothing is a
// measurement — it bounds the population there at zero.
func TestCaptureWildlifeNearby_EmptyPOIIsRecorded(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()

	n, err := CaptureWildlifeNearby(ctx, kb, nil, "goldcrest", "goldcrest_star", "sun", "databot", 1638200)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("sightings = %d, want 0 from an empty POI", n)
	}

	cov, err := kb.GetWildlifeCoverage(ctx, "goldcrest", "goldcrest_star", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(cov) != 1 {
		t.Fatalf("coverage rows = %d, want 1 — the empty look must be recorded", len(cov))
	}
	c := cov[0]
	if c.CreaturesSeen != 0 || c.SpeciesSeen != 0 {
		t.Errorf("coverage = %+v, want zero counts", c)
	}
	if c.POIType != "sun" || c.Source != WildlifeSourceNearby || c.AgentID != "databot" {
		t.Errorf("coverage lost its context: %+v", c)
	}
	if c.ObservedUTC == "" {
		t.Error("ObservedUTC is empty; the row cannot be placed in time")
	}

	// And no sighting was invented for a place that held nothing.
	s, err := kb.GetWildlifeSightings(ctx, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(s) != 0 {
		t.Errorf("sightings = %d, want none", len(s))
	}
}

// TestLatestWildlifeLooks_SeparatesEmptyFromUnvisited pins the distinction the
// whole table is for.
func TestLatestWildlifeLooks_SeparatesEmptyFromUnvisited(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()

	// A belt with creatures, and a star with none. A third POI is never touched.
	if _, err := CaptureWildlifeNearby(ctx, kb, []serverapi.NearbyCreature{
		{CreatureID: "crt_1", Species: "glitterback_crab", Name: "Glitterback Crab", Role: "grazer", Hull: 60, MaxHull: 60},
		{CreatureID: "crt_2", Species: "glitterback_crab", Name: "Glitterback Crab", Role: "grazer", Hull: 60, MaxHull: 60},
	}, "goldcrest", "goldcrest_belt", "asteroid_belt", "databot", 1638172); err != nil {
		t.Fatal(err)
	}
	if _, err := CaptureWildlifeNearby(ctx, kb, nil, "goldcrest", "goldcrest_star", "sun", "databot", 1638200); err != nil {
		t.Fatal(err)
	}

	looks, err := kb.LatestWildlifeLooks(ctx, "goldcrest")
	if err != nil {
		t.Fatal(err)
	}
	byPOI := map[string]WildlifeLook{}
	for _, l := range looks {
		byPOI[l.POIID] = l
	}
	if len(byPOI) != 2 {
		t.Fatalf("looks = %v, want exactly the 2 POIs checked", byPOI)
	}
	if got := byPOI["goldcrest_belt"].CreaturesSeen; got != 2 {
		t.Errorf("belt creatures = %d, want 2", got)
	}
	star, ok := byPOI["goldcrest_star"]
	if !ok {
		t.Fatal("the empty star is missing; 'checked and empty' is still invisible")
	}
	if star.CreaturesSeen != 0 {
		t.Errorf("star creatures = %d, want 0", star.CreaturesSeen)
	}
	if _, ok := byPOI["the_gold_crest"]; ok {
		t.Error("a POI nobody visited must not appear")
	}
}

// TestLatestWildlifeLooks_KeepsTheNewestPerPOI: repeated looks are the series
// that lets population change be measured at all, and the query must report the
// current state rather than the first visit.
func TestLatestWildlifeLooks_KeepsTheNewestPerPOI(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()

	crabs := func(n int) []serverapi.NearbyCreature {
		out := make([]serverapi.NearbyCreature, 0, n)
		for i := range n {
			out = append(out, serverapi.NearbyCreature{
				CreatureID: string(rune('a' + i)), Species: "glitterback_crab",
				Name: "Glitterback Crab", Role: "grazer", Hull: 60, MaxHull: 60,
			})
		}

		return out
	}
	if _, err := CaptureWildlifeNearby(ctx, kb, crabs(6), "goldcrest", "goldcrest_belt", "asteroid_belt", "databot", 1638100); err != nil {
		t.Fatal(err)
	}
	if _, err := CaptureWildlifeNearby(ctx, kb, crabs(2), "goldcrest", "goldcrest_belt", "asteroid_belt", "databot", 1638900); err != nil {
		t.Fatal(err)
	}

	looks, err := kb.LatestWildlifeLooks(ctx, "goldcrest")
	if err != nil {
		t.Fatal(err)
	}
	if len(looks) != 1 {
		t.Fatalf("looks = %d, want 1 POI", len(looks))
	}
	if looks[0].CreaturesSeen != 2 || looks[0].GameTick != 1638900 {
		t.Errorf("look = %+v, want the newer count of 2 at tick 1638900", looks[0])
	}
	// Both looks are retained as history.
	cov, err := kb.GetWildlifeCoverage(ctx, "goldcrest", "goldcrest_belt", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(cov) != 2 {
		t.Errorf("coverage history = %d rows, want both looks kept", len(cov))
	}
}

// TestCaptureWildlifeSurvey_EmptyCensusIsRecorded: a system survey that found no
// wildlife is a fact about the system, not an absence of data.
func TestCaptureWildlifeSurvey_EmptyCensusIsRecorded(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()

	if _, err := CaptureWildlifeSurvey(ctx, kb, serverapi.SurveySystemResponse{
		SystemID: "barren", BloomStatus: "dormant",
	}, "databot", 1638300); err != nil {
		t.Fatal(err)
	}
	cov, err := kb.GetWildlifeCoverage(ctx, "barren", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(cov) != 1 {
		t.Fatalf("coverage = %d rows, want the empty census recorded", len(cov))
	}
	if cov[0].POIID != "" {
		t.Errorf("survey coverage got a POI id %q; a census is system-wide", cov[0].POIID)
	}
	if cov[0].Source != WildlifeSourceSurvey {
		t.Errorf("source = %q", cov[0].Source)
	}
}
