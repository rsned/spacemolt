package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// craftsman-1's Concordance (2026-11-14) travelled to Crush Vector Star,
// radiation severity 3, with hull 450 and no warning; the KB already held the
// hazard. The banner names it and how long the hull lasts.
func TestHazardBannerSteady(t *testing.T) {
	h := &serverapi.POIHazard{Type: "radiation", Severity: 3}
	got := hazardBanner(h, "Crush Vector Star", 450, 450, 0, false)
	for _, want := range []string{"[HAZARD]", "Crush Vector Star", "radiation severity 3", "8%", "13 tick"} {
		if !strings.Contains(got, want) {
			t.Errorf("banner %q missing %q", got, want)
		}
	}
}

func TestHazardBannerMiningOnly(t *testing.T) {
	h := &serverapi.POIHazard{Type: "debris", Severity: 2}
	got := hazardBanner(h, "Rubble", 100, 100, 0, false)
	if !strings.Contains(got, "each mine") || strings.Contains(got, "tick(s) to") {
		t.Errorf("banner %q, want a per-mine note and no tick countdown", got)
	}
}

func TestHazardBannerNone(t *testing.T) {
	if got := hazardBanner(nil, "x", 1, 1, 0, false); got != "" {
		t.Errorf("banner for no hazard = %q, want empty", got)
	}
	if got := hazardBanner(&serverapi.POIHazard{Type: "ion_storm", Severity: 3}, "x", 1, 1, 0, false); got == "" {
		t.Error("ion storm banner empty; a scan-only hazard still deserves a note")
	}
}

func TestLookupPOIHazardFromState(t *testing.T) {
	st := &game.State{}
	st.System.POIs = []game.POI{
		{ID: "crush_vector_star", Name: "Crush Vector Star", Hazard: &serverapi.POIHazard{Type: "radiation", Severity: 3}},
		{ID: "wayfork_belt", Name: "Belt"},
	}
	h, name := lookupPOIHazard(nil, st, "Crush Vector Star")
	if h == nil || h.Severity != 3 || name != "Crush Vector Star" {
		t.Errorf("lookup by name = %+v %q", h, name)
	}
	if h, _ := lookupPOIHazard(nil, st, "wayfork_belt"); h != nil {
		t.Errorf("clean POI returned hazard %+v", h)
	}
}

// Auto-escape fires on the server's hazard_warning push and is a toggle:
// short visits (missions, wreck recovery) are allowed, the push is the line.
func TestArrivalHazardFromTravelReply(t *testing.T) {
	raw := []byte(`{"action":"arrived","poi":"Crush Vector Star","poi_hazard":{"severity":3,"type":"radiation"},"poi_id":"crush_vector_star"}`)
	h, name := arrivalHazard(raw)
	if h == nil || h.Type != "radiation" || name != "Crush Vector Star" {
		t.Errorf("arrivalHazard = %+v %q", h, name)
	}
	if h, _ := arrivalHazard([]byte(`{"action":"arrived","poi":"x"}`)); h != nil {
		t.Errorf("no poi_hazard gave %+v", h)
	}
}

// A route cannot say which POI a jump lands on, so autopilot/find_route flag
// every route system holding a known hazard POI (operator 2026-11-14).
func TestRouteHazardLines(t *testing.T) {
	kb := knowledge.NewMemoryKB()
	ctx := context.Background()
	for _, p := range []knowledge.POI{
		{ID: "crush_vector_star", SystemID: "crush_vector", Name: "Crush Vector Star", Type: "sun", HazardType: "radiation", HazardSeverity: 3},
		{ID: "cv_belt", SystemID: "crush_vector", Name: "CV Belt", Type: "asteroid_belt"},
		{ID: "wf_dust", SystemID: "wayfork", Name: "Dust", Type: "nebula", HazardType: "scar_dust", HazardSeverity: 1},
		{ID: "gr_belt", SystemID: "greenrust", Name: "Belt", Type: "asteroid_belt"},
	} {
		if err := kb.RememberPOI(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	lines := routeHazardLines(kb, []string{"greenrust", "crush_vector", "wayfork"}, false)
	if len(lines) != 2 {
		t.Fatalf("lines %q, want crush_vector and wayfork", lines)
	}
	if !strings.Contains(lines[0], "crush_vector") || !strings.Contains(lines[0], "Crush Vector Star radiation 3") {
		t.Errorf("line 0 %q", lines[0])
	}
	if !strings.Contains(lines[1], "Dust scar_dust 1") {
		t.Errorf("line 1 %q", lines[1])
	}
}

// escapeClient records the escape's moves.
type escapeClient struct {
	game.GameClient
	st    *game.State
	moves []string
}

func (e *escapeClient) GetState() *game.State { return e.st }
func (e *escapeClient) Travel(_ context.Context, poi string) (*game.TravelResult, error) {
	e.moves = append(e.moves, "travel:"+poi)
	return &game.TravelResult{}, nil
}
func (e *escapeClient) Dock(context.Context) error {
	e.moves = append(e.moves, "dock")
	return nil
}

func dosedAtStar() *escapeClient {
	st := &game.State{CurrentPOI: "crush_vector_star"}
	st.System.POIs = []game.POI{
		{ID: "crush_vector_star", Hazard: &serverapi.POIHazard{Type: "radiation", Severity: 3}},
		{ID: "cv_belt", Position: game.Position{X: 1}},
	}
	return &escapeClient{st: st}
}

func waitEscape(t *testing.T) {
	t.Helper()
	for range 200 {
		if !hazardEscaping.Load() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("escape still running")
}

func TestHazardAutoEscapeLeaves(t *testing.T) {
	hazardEscapeOn.Store(true)
	c := dosedAtStar()
	hazardAutoEscape(context.Background(), c)
	waitEscape(t)
	if len(c.moves) != 1 || c.moves[0] != "travel:cv_belt" {
		t.Errorf("moves %v, want travel to the clean belt", c.moves)
	}
}

func TestHazardAutoEscapeOff(t *testing.T) {
	hazardEscapeOn.Store(false)
	t.Cleanup(func() { hazardEscapeOn.Store(true) })
	c := dosedAtStar()
	hazardAutoEscape(context.Background(), c)
	waitEscape(t)
	if len(c.moves) != 0 {
		t.Errorf("moves %v with escape off, want none", c.moves)
	}
}
