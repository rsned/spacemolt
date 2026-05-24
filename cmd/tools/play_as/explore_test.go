package main

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// TestSurveyResponseParse_ActionResultShape guards the regression where
// survey_system began terminating with an action_result frame that nests the
// payload under "result". surveySystem() must unwrap that before binding the
// flat SurveySystemResponse, otherwise every field reads zero and nothing
// prints. The frame below is the exact shape observed from the server.
func TestSurveyResponseParse_ActionResultShape(t *testing.T) {
	raw := []byte(`{"command":"survey_system","result":{"already_revealed":[],"anomaly_hint":"Spatial anomaly detected — faint readings toward Trader's Rest (4 jumps).","faint_signatures":[],"message":"Survey complete. No hidden deposits detected in this system.","newly_revealed":[],"survey_power":168,"system_id":"haven","system_name":"Haven","xp_gained":{"deep_core_mining":0,"scanning":5}},"tick":908613}`)

	var resp serverapi.SurveySystemResponse
	if err := json.Unmarshal(unwrapActionResult(raw), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp.SurveyPower != 168 {
		t.Errorf("SurveyPower = %d, want 168", resp.SurveyPower)
	}
	if resp.SystemName != "Haven" {
		t.Errorf("SystemName = %q, want %q", resp.SystemName, "Haven")
	}
	if resp.Message == "" {
		t.Error("Message is empty, want survey-complete text")
	}
	if resp.AnomalyHint == "" {
		t.Error("AnomalyHint is empty, want spatial-anomaly text")
	}
	if resp.XPGained["scanning"] != 5 {
		t.Errorf("XPGained[scanning] = %d, want 5", resp.XPGained["scanning"])
	}
}

// TestSurveyResponseParse_FlatShape confirms the legacy flat OK shape still
// binds unchanged (unwrapActionResult is a no-op when there is no "result").
func TestSurveyResponseParse_FlatShape(t *testing.T) {
	raw := []byte(`{"action":"survey_system","survey_power":42,"system_name":"Sol","message":"done"}`)

	var resp serverapi.SurveySystemResponse
	if err := json.Unmarshal(unwrapActionResult(raw), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.SurveyPower != 42 || resp.SystemName != "Sol" || resp.Message != "done" {
		t.Errorf("flat parse mismatch: power=%d name=%q msg=%q", resp.SurveyPower, resp.SystemName, resp.Message)
	}
}

func TestPlanExploreRoute_SinglePOI(t *testing.T) {
	pois := []game.POI{
		{ID: "a", Name: "Alpha", Position: game.Position{X: 1, Y: 2}},
	}
	route := planExploreRoute(pois, "a")
	if len(route) != 1 || route[0].ID != "a" {
		t.Fatalf("expected single POI 'a', got %v", ids(route))
	}
}

func TestPlanExploreRoute_StartPOIIsFirst(t *testing.T) {
	pois := []game.POI{
		{ID: "a", Position: game.Position{X: 0, Y: 0}},
		{ID: "b", Position: game.Position{X: 10, Y: 0}},
		{ID: "c", Position: game.Position{X: 5, Y: 0}},
	}
	route := planExploreRoute(pois, "b")
	if route[0].ID != "b" {
		t.Errorf("expected start at 'b', got %q", route[0].ID)
	}
}

func TestPlanExploreRoute_LinearLayout(t *testing.T) {
	// POIs in a line: a(0,0) c(2,0) b(5,0) d(9,0)
	// Starting at a, nearest-neighbor should visit in distance order: a → c → b → d
	pois := []game.POI{
		{ID: "a", Position: game.Position{X: 0, Y: 0}},
		{ID: "b", Position: game.Position{X: 5, Y: 0}},
		{ID: "c", Position: game.Position{X: 2, Y: 0}},
		{ID: "d", Position: game.Position{X: 9, Y: 0}},
	}
	route := planExploreRoute(pois, "a")
	expected := []string{"a", "c", "b", "d"}
	got := ids(route)
	for i := range expected {
		if got[i] != expected[i] {
			t.Errorf("position %d: expected %q, got %q (full: %v)", i, expected[i], got[i], got)
			break
		}
	}
}

func TestPlanExploreRoute_ClusterLayout(t *testing.T) {
	// Two clusters: start at 'a', nearby cluster (b,c) should be visited before far cluster (d,e)
	pois := []game.POI{
		{ID: "a", Position: game.Position{X: 0, Y: 0}},
		{ID: "b", Position: game.Position{X: 1, Y: 1}},
		{ID: "c", Position: game.Position{X: 2, Y: 0}},
		{ID: "d", Position: game.Position{X: 20, Y: 20}},
		{ID: "e", Position: game.Position{X: 21, Y: 19}},
	}
	route := planExploreRoute(pois, "a")
	got := ids(route)

	// a should be first, d and e should be last two (in either order)
	if got[0] != "a" {
		t.Errorf("expected start at 'a', got %q", got[0])
	}
	lastTwo := got[3] + "," + got[4]
	if lastTwo != "d,e" && lastTwo != "e,d" {
		t.Errorf("expected d,e as last two, got %v", got)
	}
}

func TestPlanExploreRoute_DifferentStartingPOIs(t *testing.T) {
	// Same system, different starting positions should give different routes.
	pois := []game.POI{
		{ID: "left", Position: game.Position{X: 0, Y: 0}},
		{ID: "center", Position: game.Position{X: 5, Y: 0}},
		{ID: "right", Position: game.Position{X: 10, Y: 0}},
	}

	routeFromLeft := planExploreRoute(pois, "left")
	routeFromRight := planExploreRoute(pois, "right")

	if routeFromLeft[0].ID != "left" {
		t.Errorf("route from left should start at 'left', got %q", routeFromLeft[0].ID)
	}
	if routeFromRight[0].ID != "right" {
		t.Errorf("route from right should start at 'right', got %q", routeFromRight[0].ID)
	}

	// From left: left → center → right
	// From right: right → center → left
	if routeFromLeft[1].ID != "center" {
		t.Errorf("from left, second should be 'center', got %q", routeFromLeft[1].ID)
	}
	if routeFromRight[1].ID != "center" {
		t.Errorf("from right, second should be 'center', got %q", routeFromRight[1].ID)
	}
}

func TestPlanExploreRoute_SolLikeSystem(t *testing.T) {
	// Simulates a Sol-like system with 13 POIs spread around.
	pois := []game.POI{
		{ID: "sol_star", Name: "Sol", Type: "star", Position: game.Position{X: 0, Y: 0}},
		{ID: "mercury", Name: "Mercury", Type: "planet", Position: game.Position{X: 0.4, Y: 0}},
		{ID: "venus", Name: "Venus", Type: "planet", Position: game.Position{X: 0.7, Y: 0.1}},
		{ID: "earth_station", Name: "Earth Station", Type: "station", Position: game.Position{X: 1, Y: 0}},
		{ID: "luna", Name: "Luna", Type: "planet", Position: game.Position{X: 1.1, Y: 0.1}},
		{ID: "mars", Name: "Mars", Type: "planet", Position: game.Position{X: 1.5, Y: -0.2}},
		{ID: "inner_belt", Name: "Inner Belt", Type: "asteroid_belt", Position: game.Position{X: 2.5, Y: 0.5}},
		{ID: "jupiter", Name: "Jupiter", Type: "planet", Position: game.Position{X: 5.2, Y: 1}},
		{ID: "saturn", Name: "Saturn", Type: "planet", Position: game.Position{X: 9.5, Y: -2}},
		{ID: "uranus", Name: "Uranus", Type: "planet", Position: game.Position{X: 19, Y: 3}},
		{ID: "neptune", Name: "Neptune", Type: "planet", Position: game.Position{X: 30, Y: -1}},
		{ID: "kuiper_belt", Name: "Kuiper Belt", Type: "ice_field", Position: game.Position{X: 40, Y: 0}},
		{ID: "oort_cloud", Name: "Oort Cloud", Type: "gas_cloud", Position: game.Position{X: 50, Y: 5}},
	}

	// Starting from Earth Station — should visit nearby POIs first, then move outward.
	route := planExploreRoute(pois, "earth_station")
	if route[0].ID != "earth_station" {
		t.Fatalf("expected start at earth_station, got %q", route[0].ID)
	}

	// Verify total route distance is reasonable (not worst case).
	totalDist := totalRouteDistance(route)
	// Worst case (random order) would be ~200+ AU of backtracking.
	// Nearest neighbor from earth should be ~55-60 AU (roughly the diameter).
	if totalDist > 80 {
		t.Errorf("route seems suboptimal: total distance %.1f AU (expected <80)", totalDist)
	}

	// The outer POIs (neptune, kuiper, oort) should be visited in sequence, not interleaved.
	outerStart := -1
	for i, poi := range route {
		if poi.ID == "neptune" || poi.ID == "kuiper_belt" || poi.ID == "oort_cloud" {
			if outerStart == -1 {
				outerStart = i
			}
		}
	}
	// All three outer POIs should be in the last 3 positions.
	if outerStart < len(route)-3 {
		t.Errorf("outer POIs should be visited last, first outer at position %d/%d (route: %v)",
			outerStart, len(route), ids(route))
	}
}

func TestPlanExploreRoute_AllSamePosition(t *testing.T) {
	pois := []game.POI{
		{ID: "a", Position: game.Position{X: 5, Y: 5}},
		{ID: "b", Position: game.Position{X: 5, Y: 5}},
		{ID: "c", Position: game.Position{X: 5, Y: 5}},
	}
	route := planExploreRoute(pois, "a")
	if len(route) != 3 {
		t.Fatalf("expected 3 POIs, got %d", len(route))
	}
	if route[0].ID != "a" {
		t.Errorf("expected start at 'a', got %q", route[0].ID)
	}
}

func TestPlanExploreRoute_UnknownStartPOI(t *testing.T) {
	pois := []game.POI{
		{ID: "a", Position: game.Position{X: 0, Y: 0}},
		{ID: "b", Position: game.Position{X: 10, Y: 0}},
	}
	route := planExploreRoute(pois, "nonexistent")
	if len(route) != 2 {
		t.Fatalf("expected 2 POIs, got %d", len(route))
	}
	// Falls back to first POI.
	if route[0].ID != "a" {
		t.Errorf("expected fallback to first POI 'a', got %q", route[0].ID)
	}
}

func TestPlanExploreRoute_EmptyList(t *testing.T) {
	route := planExploreRoute(nil, "a")
	if len(route) != 0 {
		t.Fatalf("expected empty route, got %d", len(route))
	}
}

func TestPoiDistance(t *testing.T) {
	a := game.Position{X: 0, Y: 0}
	b := game.Position{X: 3, Y: 4}
	dist := poiDistance(a, b)
	if math.Abs(dist-5.0) > 0.001 {
		t.Errorf("expected 5.0, got %f", dist)
	}
}

func TestPoiDistance_SamePoint(t *testing.T) {
	a := game.Position{X: 7, Y: 3}
	dist := poiDistance(a, a)
	if dist != 0 {
		t.Errorf("expected 0, got %f", dist)
	}
}

// --- Helpers ---

func ids(pois []game.POI) []string {
	result := make([]string, len(pois))
	for i, p := range pois {
		result[i] = p.ID
	}
	return result
}

func totalRouteDistance(route []game.POI) float64 {
	if len(route) < 2 {
		return 0
	}
	total := 0.0
	for i := 1; i < len(route); i++ {
		total += poiDistance(route[i-1].Position, route[i].Position)
	}
	return total
}
