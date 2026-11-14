package worker

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/market"
	"github.com/rsned/spacemolt/pkg/sweep"
)

// lapDoneAtLongshore is explorer-7 on 2026-11-13: lap ended at Longshore
// (Atlantis, a dry desk with no market) with 67 fuel. Frontier Vault is
// closer but has no fuel on record; Kael Arsenal (Xamidimura, a stronghold
// the ship holds the unlock for) has.
func lapDoneAtLongshore(t *testing.T, fuel float64, refuelErr error) (*arrivalFakeClient, *WorkerDispatch, string) {
	t.Helper()
	kb := knowledge.NewMemoryKB()
	addSweepStation(t, kb, "longshore", -1495, -6833)
	addSweepStation(t, kb, "frontier_vault", -1200, -7000)
	addSweepStronghold(t, kb, "xamidimura", -2600, -6600)
	mc, err := market.Open(market.Config{DBPath: filepath.Join(t.TempDir(), "m.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mc.Close() })
	at := time.Now().UTC().Format(time.RFC3339)
	if err := mc.UpsertStationFuel(context.Background(), market.StationFuel{StationID: "xamidimura_keep_station", FuelReserve: 49931, ReserveObservedAt: at, CapturedAt: at}); err != nil {
		t.Fatal(err)
	}
	s := sweep.NewState(7350, 148, 45)
	s.Phase, s.HomeSystem, s.ProgressDeg, s.SweptDeg = sweep.PhaseDone, "longshore", -104.7, 257.7
	s.LaunchCostMeasured = true
	c := inSystem("longshore", fuel)
	c.state.Doc = true
	c.state.Player.DockedAtBase = "longshore_base"
	c.state.Player.Standings = map[string]game.EmpireStanding{"pirate_kael": {Baseline: 10}}
	c.refuelErr = refuelErr
	d, path, _ := newArrivalDispatch(t, c, kb, s)
	d.Market = mc
	return c, d, path
}

var errDryDesk = errors.New("station_fuel_empty: This station's fuel reserves are depleted")

func TestSweepResumesLapAfterRefuel(t *testing.T) {
	c, d, path := lapDoneAtLongshore(t, 67, nil)
	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _, _ := sweep.Load(path)
	if got.Phase != sweep.PhaseLap || got.HomeSystem != "" || len(c.jumps) != 1 {
		t.Fatalf("phase %q home %q jumps %v, want a lap launch back to the ring", got.Phase, got.HomeSystem, c.jumps)
	}
	if got.TargetDeg >= -104.7 {
		t.Errorf("target %.1f°, want past the stop angle", got.TargetDeg)
	}
}

func TestSweepDryDeskMovesToFuelledStation(t *testing.T) {
	c, d, path := lapDoneAtLongshore(t, 67, errDryDesk)
	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _, _ := sweep.Load(path)
	if got.Phase != sweep.PhaseHome || got.HomeSystem != "xamidimura" || len(c.jumps) != 1 {
		t.Fatalf("phase %q home %q jumps %v, want one launch to xamidimura", got.Phase, got.HomeSystem, c.jumps)
	}
}

func TestSweepDryDeskTooLowStays(t *testing.T) {
	c, d, path := lapDoneAtLongshore(t, 30, errDryDesk)
	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _, _ := sweep.Load(path)
	if got.Phase != sweep.PhaseDone || len(c.jumps) != 0 {
		t.Fatalf("phase %q jumps %v, want to stay docked", got.Phase, c.jumps)
	}
	// The next pass does not hammer the dry desk.
	c.calls = nil
	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(c.calls, "refuel") {
		t.Errorf("calls %v, want no refuel retry within the hour", c.calls)
	}
}

func TestSweepFullLapDoesNotResume(t *testing.T) {
	c, d, path := lapDoneAtLongshore(t, 67, nil)
	s, _, _ := sweep.Load(path)
	s.SweptDeg = 360
	if err := sweep.Save(path, s); err != nil {
		t.Fatal(err)
	}
	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _, _ := sweep.Load(path)
	if got.Phase != sweep.PhaseDone || len(c.jumps) != 0 {
		t.Fatalf("phase %q jumps %v, want the finished lap to stay finished", got.Phase, c.jumps)
	}
}

// laneStation is a lane-connected system with a public station.
func laneStation(t *testing.T, kb knowledge.Base, id string, x, y float64, conns ...string) {
	t.Helper()
	placeSystem(t, kb, id, x, y, 1, 1, conns...)
	ctx := context.Background()
	if err := kb.RememberPOI(ctx, knowledge.POI{ID: id + "_station", SystemID: id, Type: "station"}); err != nil {
		t.Fatal(err)
	}
	if err := kb.RememberBase(ctx, knowledge.SpaceBase{ID: id + "_base", POIID: id + "_station", PublicAccess: true}); err != nil {
		t.Fatal(err)
	}
}

// explorer-5 on 2026-11-13 homed by lanes to Winterhold, one jump away,
// twenty-five minutes after explorer-6 had found its desk dry. Lane homing
// must skip a station on record as dry when one with fuel is reachable.
func TestSweepHomeByLanesSkipsDryStation(t *testing.T) {
	kb := knowledge.NewMemoryKB()
	placeSystem(t, kb, "gap", 0, 0, 1, 1, "winterhold", "relay")
	laneStation(t, kb, "winterhold", 10, 0, "gap")
	placeSystem(t, kb, "relay", 0, 10, 1, 1, "gap", "last_light")
	laneStation(t, kb, "last_light", 0, 20, "relay")
	mc, err := market.Open(market.Config{DBPath: filepath.Join(t.TempDir(), "m.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mc.Close() })
	ctx := context.Background()
	at := time.Now().UTC().Format(time.RFC3339)
	if err := mc.UpsertStationFuel(ctx, market.StationFuel{StationID: "last_light_base", FuelReserve: 5000, ReserveObservedAt: at, CapturedAt: at}); err != nil {
		t.Fatal(err)
	}
	if err := mc.MarkDeskDry(ctx, "winterhold_base", "explorer-6", time.Now()); err != nil {
		t.Fatal(err)
	}
	s := sweep.NewState(7650, 148, 45)
	s.Phase, s.HomeSystem = sweep.PhaseHome, "gap"
	c := inSystem("gap", 66)
	d, _, seams := newArrivalDispatch(t, c, kb, s)
	d.Market = mc
	if err := d.PathfinderSweep(ctx); err != nil {
		t.Fatal(err)
	}
	if len(seams.navs) != 1 || seams.navs[0] != "last_light/" {
		t.Fatalf("navs %v, want lanes to last_light (winterhold is dry)", seams.navs)
	}
}
