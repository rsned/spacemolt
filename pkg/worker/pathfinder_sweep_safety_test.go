package worker

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/sweep"
)

// safetyFake adds in-system travel and station repair to the arrival fake.
type safetyFake struct {
	arrivalFakeClient
}

func (c *safetyFake) Travel(_ context.Context, poi string) (*game.TravelResult, error) {
	c.calls = append(c.calls, "travel "+poi)
	c.state.CurrentPOI = poi
	c.state.Doc = false
	return &game.TravelResult{}, nil
}

func (c *safetyFake) Repair(context.Context) error {
	c.calls = append(c.calls, "repair")
	if !c.state.Doc {
		return errors.New("not docked")
	}
	c.state.Ship.Hull = c.state.Ship.MaxHull
	return nil
}

// newSafety is a ship in sysID at hull/maxHull.
func newSafety(sysID string, hull, maxHull float64) *safetyFake {
	c := &safetyFake{*inSystem(sysID, 900)}
	c.state.Ship.Hull, c.state.Ship.MaxHull = hull, maxHull
	return c
}

func newSafetyDispatch(t *testing.T, c *safetyFake, kb knowledge.Base, s *sweep.State) (*WorkerDispatch, string, *sweepSeams, *bytes.Buffer) {
	t.Helper()
	d, path, seams := newArrivalDispatch(t, &c.arrivalFakeClient, kb, s)
	d.Client = c
	out := &bytes.Buffer{}
	d.Out = out
	return d, path, seams, out
}

// explorer-4, 2026-11-14: a drift landed it on Cinderback (radiation 2) in
// Old Clock. It toured and retried an explore target for three minutes on
// that POI, no dose push ever arrived to trip the hazard gate, and the ship
// died. A landing on a POI known to be hazardous moves off it first.
func TestSweepLeavesHazardPOIOnLanding(t *testing.T) {
	for name, kbHazard := range map[string]bool{"state knows": false, "only KB knows": true} {
		t.Run(name, func(t *testing.T) {
			kb := knowledge.NewMemoryKB()
			placeSystem(t, kb, "old_clock", 900, 7400, 1, 1, "last_lamp")
			c := newSafety("old_clock", 480, 480)
			rad := &serverapi.POIHazard{Type: "radiation", Severity: 2}
			c.state.System.POIs = []game.POI{
				{ID: "cinderback", Name: "Cinderback", Type: "planet"},
				{ID: "quiet_rock", Name: "Quiet Rock", Type: "asteroid_belt", Position: game.Position{X: 3, Y: 0}},
			}
			if kbHazard {
				if err := kb.RememberPOI(context.Background(), knowledge.POI{ID: "cinderback", SystemID: "old_clock", Type: "planet", HazardType: "radiation", HazardSeverity: 2}); err != nil {
					t.Fatal(err)
				}
			} else {
				c.state.System.POIs[0].Hazard = rad
			}
			c.state.CurrentPOI = "cinderback"
			s := sweep.NewState(7500, 148, 45)
			s.Drifted, s.LaunchCostMeasured = true, true
			d, _, seams, out := newSafetyDispatch(t, c, kb, s)
			if err := d.PathfinderSweep(context.Background()); err != nil {
				t.Fatal(err)
			}
			if !slices.Contains(c.calls, "travel quiet_rock") {
				t.Fatalf("calls %v, want a move off Cinderback to Quiet Rock", c.calls)
			}
			if !strings.Contains(out.String(), "radiation") {
				t.Errorf("output %q should say why it moved", out.String())
			}
			if len(seams.tours) != 1 {
				t.Errorf("tours %v, want the landing toured after moving to safety", seams.tours)
			}
		})
	}
}

// Docked at a station, a sweep (or explore) ship repairs as well as refuels.
func TestRefuelDockedAlsoRepairs(t *testing.T) {
	c := newSafety("frontier", 86, 480)
	c.state.Doc = true
	if err := exploreRefuelDocked(context.Background(), ExploreDeps{Client: c}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(c.calls, "repair") || c.state.Ship.Hull != 480 {
		t.Fatalf("calls %v hull %v, want a repair to 480", c.calls, c.state.Ship.Hull)
	}
}

// explorer-4 launched the bottom pass at 86/480 hull. Below the floor, a ship
// in a system with a station repairs there before it launches.
func TestSweepLowHullRepairsBeforeLaunch(t *testing.T) {
	kb := knowledge.NewMemoryKB()
	addSweepStation(t, kb, "node_alpha", -5800, 4100)
	c := newSafety("node_alpha", 150, 480)
	s := sweep.NewState(7500, 148, 45)
	s.Phase, s.LaunchCostMeasured = sweep.PhaseLap, true
	d, _, seams, _ := newSafetyDispatch(t, c, kb, s)
	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	r := slices.Index(c.calls, "repair")
	j := slices.IndexFunc(c.calls, func(x string) bool { return strings.HasPrefix(x, "jump ") })
	if r < 0 || j < 0 || r > j {
		t.Fatalf("calls %v navs %v, want a repair before the launch", c.calls, seams.navs)
	}
}

// Low hull with no station reachable: launch anyway (the lap must not
// strand), but say so.
func TestSweepLowHullNoStationLaunchesWithWarning(t *testing.T) {
	kb := knowledge.NewMemoryKB()
	placeSystem(t, kb, "far_shelf", -5000, 5600, 1, 1)
	c := newSafety("far_shelf", 150, 480)
	s := sweep.NewState(7500, 148, 45)
	s.Phase, s.LaunchCostMeasured = sweep.PhaseLap, true
	d, _, _, out := newSafetyDispatch(t, c, kb, s)
	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(c.jumps) != 1 {
		t.Fatalf("jumps %v, want the launch to go ahead", c.jumps)
	}
	if !strings.Contains(out.String(), "hull 150/480") {
		t.Errorf("output %q should warn about the hull", out.String())
	}
}

// explorer-4 retried an explore step to last_lamp nine times ("Target system
// not found") while parked on a radiation POI. An unroutable target ends the
// explore run here: log the system unfinished and rejoin the ring.
func TestSweepExploreUnknownTargetRejoins(t *testing.T) {
	kb := knowledge.NewMemoryKB()
	placeSystem(t, kb, "old_clock", 900, 7400, 1, 1, "last_lamp")
	c := newSafety("old_clock", 480, 480)
	s := exploringState(7500, 83)
	d, path, _, _ := newSafetyDispatch(t, c, kb, s)
	d.sweepExploreStep = func(context.Context, ExploreDeps) error {
		return errors.New("find_route failed: find_route failed: Target system not found")
	}
	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatalf("PathfinderSweep: %v, want the unroutable target absorbed", err)
	}
	if len(c.jumps) != 1 {
		t.Errorf("jumps %v, want a rejoin launch", c.jumps)
	}
	got, _, err := sweep.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(got.Unfinished, "old_clock") {
		t.Errorf("unfinished %v, want old_clock", got.Unfinished)
	}
}
