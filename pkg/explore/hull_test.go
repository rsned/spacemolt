package explore

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// hullFixture is a scanner-less ship at the first of pois with the given hull
// (of 480) and repair kits. Each repair uses one kit for KitHull hull.
func hullFixture(hull float64, kits int, pois ...game.POI) (*fakeClient, Deps, *logReporter) {
	c := newFakeClient(pois...)
	c.state.Ship.Hull = hull
	setKits(c, kits)
	c.onRepair = func() {
		setKits(c, kitCount(c.state)-1)
		c.state.Ship.Hull = min(c.state.Ship.Hull+KitHull, c.state.Ship.MaxHull)
	}
	d, rep := testDeps(c)
	d.HasScanner = func(*game.State) bool { return false }
	return c, d, rep
}

func setKits(c *fakeClient, n int) {
	c.state.Ship.Cargo = nil
	if n > 0 {
		c.state.Ship.Cargo = []game.CargoItem{{ItemID: KitItem, Quantity: float64(n)}}
	}
}

func hazardous(p game.POI, kind string, severity int) game.POI {
	p.Hazard = &serverapi.POIHazard{Type: kind, Severity: severity}
	return p
}

// dose makes arriving at poiID cost the ship hull, as a hazard_warning would.
func dose(c *fakeClient, poiID string, hullAfter float64) {
	c.onTravel = func(id string) {
		if id != poiID {
			return
		}
		c.state.Ship.Hull = hullAfter
		c.state.LastHazardDose = game.HazardDose{
			HazardWarning: serverapi.HazardWarning{POIID: poiID, Hull: int(hullAfter), MaxHull: 480},
			At:            time.Now(),
		}
	}
}

func result(t *testing.T, res Result, id string) POIResult {
	t.Helper()
	for _, p := range res.POIs {
		if p.ID == id {
			return p
		}
	}
	t.Fatalf("no result for POI %s in %+v", id, res.POIs)
	return POIResult{}
}

// At the floor with nothing to repair with, every POI is skipped and logged,
// and the tour says the hull stopped it.
func TestHullFloorSkipsAndLogs(t *testing.T) {
	c, d, rep := hullFixture(240, 0, poi("star", "star", 0), poi("belt", "asteroid_belt", 1), poi("moon", "planet", 2))

	res, err := System(context.Background(), d, Options{HullFloor: 0.5})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"belt", "moon"} {
		if p := result(t, res, id); p.Visited || p.Skipped != "hull 50%" {
			t.Errorf("%s: visited=%v skipped=%q, want skipped \"hull 50%%\"", id, p.Visited, p.Skipped)
		}
	}
	if len(c.travels()) != 0 {
		t.Errorf("travelled at the hull floor: %v", c.travels())
	}
	if !rep.has("skipped belt: hull 50%") || !rep.has("skipped moon: hull 50%") {
		t.Errorf("skips not logged; lines: %q", rep.lines)
	}
	if res.Stopped != StoppedHull {
		t.Errorf("Stopped=%q, want %q", res.Stopped, StoppedHull)
	}
}

// Kits bring the hull back above the floor, and the POI is visited.
func TestHullFloorRepairsFromKits(t *testing.T) {
	c, d, _ := hullFixture(216, 3, poi("star", "star", 0), poi("belt", "asteroid_belt", 1)) // 45% of 480

	res, err := System(context.Background(), d, Options{HullFloor: 0.5})
	if err != nil {
		t.Fatal(err)
	}
	if !result(t, res, "belt").Visited {
		t.Errorf("belt not visited after repair: %+v", res.POIs)
	}
	// 216 -> 266 is above 240 (50%): one kit is enough.
	if got := kitCount(c.state); got != 2 {
		t.Errorf("%d kits left, want 2", got)
	}
	if c.state.Ship.Hull != 266 {
		t.Errorf("hull %v, want 266", c.state.Ship.Hull)
	}
	if res.Stopped != "" {
		t.Errorf("Stopped=%q, want none", res.Stopped)
	}
}

// A POI skipped at the floor gets one more try once the route is done; a
// station is never skipped, since docking is how a hull gets repaired.
func TestFloorSkippedPOIRetriedAtEnd(t *testing.T) {
	c, d, _ := hullFixture(240, 0, poi("star", "star", 0), poi("belt", "asteroid_belt", 1), poi("depot", "station", 2))
	d.StationCapture = func(context.Context) error {
		c.state.Ship.Hull = 480 // repaired at the dock
		return nil
	}

	res, err := System(context.Background(), d, Options{HullFloor: 0.5})
	if err != nil {
		t.Fatal(err)
	}
	// star (where the ship started) and belt are both skipped at the floor;
	// after the dock they are retried in route order.
	if got, want := c.travels(), []string{"depot", "star", "belt"}; !slices.Equal(got, want) {
		t.Errorf("travelled %v, want %v", got, want)
	}
	if p := result(t, res, "belt"); !p.Visited || p.Skipped != "" {
		t.Errorf("belt after the retry: %+v", p)
	}
	if res.Stopped != "" {
		t.Errorf("Stopped=%q, want none", res.Stopped)
	}
}

// With EnterHazards a hazardous POI is visited, worked, and left.
func TestEnterHazardLeavesAfterOneDose(t *testing.T) {
	resetRelicsTried()
	c, d, _ := hullFixture(480, 0, poi("star", "star", 0), hazardous(poi("scar", "relic", 1), serverapi.HazardRadiation, 3))
	dose(c, "scar", 442)
	c.poiReplies = map[string]string{"scar": relicPrompt}
	c.rawCommand = func(string) (string, error) { return `{"message":"ok","data_copied":true}`, nil }
	updated := 0
	d.UpdatePOI = func(context.Context) error { updated++; return nil }

	res, err := System(context.Background(), d, Options{EnterHazards: true, HullFloor: 0.5, Relics: true})
	if err != nil {
		t.Fatal(err)
	}
	if p := result(t, res, "scar"); !p.Visited {
		t.Errorf("hazardous POI not visited: %+v", p)
	}
	if updated != 2 || res.Relics != 1 {
		t.Errorf("POI updates=%d relics=%d, want 2 and 1", updated, res.Relics)
	}
	// The hazard was the last POI of the route, so the ship must move off it.
	if got, want := c.travels(), []string{"scar", "star"}; !slices.Equal(got, want) {
		t.Errorf("travelled %v, want %v (leave the hazard)", got, want)
	}
}

// Without EnterHazards, a POI at or above SkipHazardSeverity is skipped.
func TestHazardSkippedWithoutEnterHazards(t *testing.T) {
	c, d, rep := hullFixture(480, 0,
		poi("star", "star", 0),
		hazardous(poi("scar", "planet", 1), serverapi.HazardRadiation, 2),
		hazardous(poi("haze", "nebula", 2), serverapi.HazardRadiation, 1))

	res, err := System(context.Background(), d, Options{SkipHazardSeverity: 2})
	if err != nil {
		t.Fatal(err)
	}
	if p := result(t, res, "scar"); p.Visited || p.Skipped != "radiation hazard severity 2" {
		t.Errorf("scar: %+v, want skipped for its hazard", p)
	}
	if !result(t, res, "haze").Visited {
		t.Error("a severity-1 hazard below the skip threshold was not visited")
	}
	if slices.Contains(c.travels(), "scar") {
		t.Errorf("travelled into the hazard: %v", c.travels())
	}
	if !rep.has("skipped scar: radiation hazard severity 2") {
		t.Errorf("hazard skip not logged; lines: %q", rep.lines)
	}
}

// After a dose, a kit is used only when a whole kit's worth of hull is missing.
func TestKitUsedAfterDoseWhenMissingAKit(t *testing.T) {
	for _, tc := range []struct {
		hullAfter float64
		wantKits  int
	}{
		{430, 2}, // missing 50: one kit
		{460, 3}, // missing 20: none
	} {
		c, d, _ := hullFixture(480, 3,
			poi("star", "star", 0),
			hazardous(poi("scar", "planet", 1), serverapi.HazardRadiation, 2),
			poi("moon", "planet", 2))
		dose(c, "scar", tc.hullAfter)

		if _, err := System(context.Background(), d, Options{EnterHazards: true, HullFloor: 0.5}); err != nil {
			t.Fatal(err)
		}
		if got := kitCount(c.state); got != tc.wantKits {
			t.Errorf("hull %v after the dose: %d kits left, want %d", tc.hullAfter, got, tc.wantKits)
		}
		// The kit is used once the ship is clear of the hazard, not on it.
		if i, j := slices.Index(c.calls, "repair"), slices.Index(c.calls, "travel moon"); i >= 0 && i < j {
			t.Errorf("repaired while still at the hazard; calls: %v", c.calls)
		}
	}
}
