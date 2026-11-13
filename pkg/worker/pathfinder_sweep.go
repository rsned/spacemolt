package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"

	"github.com/rsned/spacemolt/pkg/galaxy"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/sweep"
)

const (
	// sweepPassBudget bounds one pass in ticks (25 min), inside the 30 min
	// SleepCommandMaxWait ceiling on a worker command. A 5° leg at 7,950 LY
	// is ~69 ticks, so a pass normally ends at its waypoint well before.
	sweepPassBudget = 150
	// sweepStormTolerance is how far (degrees) the live drift bearing may
	// differ from the one we plotted before it counts as a storm deflection.
	sweepStormTolerance = 0.5
)

// PathfinderSweep runs one bounded pass of the pathfinder frontier sweep:
// adrift, it follows the ring leg by leg; in a system, it explores, rejoins
// or finishes (sweepInSystem). State is saved after every change, so the
// idle loop can call it again and again and a restart resumes in place.
//
// Spec: docs/superpowers/specs/2026-11-12-pathfinder-frontier-sweep-design.md
func (d *WorkerDispatch) PathfinderSweep(ctx context.Context) error {
	if d.SweepRing <= 0 {
		return errors.New("pathfinder_sweep: sweep_ring not configured")
	}
	if d.SweepLaunchCost <= 0 {
		// The whole fuel reserve is a multiple of the launch cost: with no
		// estimate a ship would never burn its cells and would run dry.
		return errors.New("pathfinder_sweep: sweep_launch_cost not configured")
	}
	path := sweep.StatePath(d.agentsDir(), d.AgentID)
	s, ok, err := sweep.Load(path)
	if err != nil {
		return fmt.Errorf("pathfinder_sweep: %w", err)
	}
	if !ok {
		s = sweep.NewState(d.SweepRing, d.SweepStartDeg, d.SweepLaunchCost)
	}
	if s.Phase == sweep.PhaseDone {
		fmt.Fprintln(d.Out, "pathfinder_sweep: lap complete") //nolint:errcheck
		return nil
	}
	loc, err := d.sweepLocation(ctx)
	if err != nil {
		return err
	}
	if !loc.Location.Drifting() {
		return d.sweepInSystem(ctx, s, path, loc)
	}
	return d.sweepDrift(ctx, s, path, loc)
}

// sweepLocation reads get_location.
func (d *WorkerDispatch) sweepLocation(ctx context.Context) (serverapi.LocationResponse, error) {
	var loc serverapi.LocationResponse
	if err := d.Client.RawCommand(ctx, "get_location", nil); err != nil {
		return loc, fmt.Errorf("pathfinder_sweep: get_location: %w", err)
	}
	if err := json.Unmarshal(d.Client.GetRawJSON("location"), &loc); err != nil {
		return loc, fmt.Errorf("pathfinder_sweep: decode get_location: %w", err)
	}
	return loc, nil
}

// sweepDrift polls the drift once a tick until the leg's waypoint is reached
// (or passed), then plots the next leg; a storm deflection re-aims at once.
// It returns when the next leg is plotted, the ship arrives in a system, or
// the pass budget runs out.
func (d *WorkerDispatch) sweepDrift(ctx context.Context, s *sweep.State, path string, loc serverapi.LocationResponse) error {
	for range sweepPassBudget {
		l := loc.Location
		x, y := l.TransitX, l.TransitY
		s.LastX, s.LastY, s.Drifted, s.ReadSinceLaunch = x, y, true, true
		if s.Phase == sweep.PhaseHome && sweepHomeMissed(l, s) {
			tank, _, cells, premium := sweepFuel(d.Client.GetState())
			if sweep.Total(tank, cells, premium) >= s.LaunchCost {
				fmt.Fprintf(d.Out, "pathfinder_sweep: home drift missed %s (bearing %.1f, plotted %.1f); correcting\n", s.HomeSystem, l.TransitBearing, s.PlannedBearing) //nolint:errcheck
				return d.sweepGoHome(ctx, s, path, x, y, false)
			}
		}
		if s.Phase == sweep.PhaseLap {
			if bearingDiff(l.TransitBearing, s.PlannedBearing) > sweepStormTolerance {
				s.Gaps = append(s.Gaps, sweep.Gap{FromDeg: s.ProgressDeg, ToDeg: s.TargetDeg, Reason: "storm"})
				fmt.Fprintf(d.Out, "pathfinder_sweep: storm deflected the drift (bearing %.1f, plotted %.1f); gap %.1f°→%.1f° logged, re-aiming\n", //nolint:errcheck
					l.TransitBearing, s.PlannedBearing, s.ProgressDeg, s.TargetDeg)
				sweep.Advance(s, s.TargetDeg)
				return d.sweepNextLeg(ctx, s, path, x, y)
			}
			if sweep.Reached(x, y, s.TargetX, s.TargetY, l.TransitBearing) {
				sweep.Advance(s, s.TargetDeg)
				return d.sweepNextLeg(ctx, s, path, x, y)
			}
		}
		settle(ctx, game.SleepTick)
		if ctx.Err() != nil {
			break
		}
		var err error
		if loc, err = d.sweepLocation(ctx); err != nil {
			_ = sweep.Save(path, s)
			return err
		}
		if !loc.Location.Drifting() {
			if err = sweep.Save(path, s); err != nil {
				return err
			}
			return d.sweepInSystem(ctx, s, path, loc)
		}
	}
	return sweep.Save(path, s)
}

// sweepNextLeg plots the next ring leg from (x,y), or turns for home when
// the lap is done or the fuel budget no longer covers launch + one more +
// the final jump.
func (d *WorkerDispatch) sweepNextLeg(ctx context.Context, s *sweep.State, path string, x, y float64) error {
	tank, _, cells, premium := sweepFuel(d.Client.GetState())
	if sweep.LapDone(s) || !sweep.CanContinue(tank, cells, premium, s.LaunchCost) {
		return d.sweepGoHome(ctx, s, path, x, y, false)
	}
	tx, ty, deg := sweep.NextTarget(s, x, y)
	s.TargetX, s.TargetY, s.TargetDeg = tx, ty, deg
	return d.sweepLaunch(ctx, s, path, sweep.BearingTo(x, y, tx, ty))
}

// sweepGoHome switches to the home phase and launches toward the nearest
// accessible non-stronghold station system by straight-line distance.
// Adrift there is no choice but to launch. From inside a system the ship is
// safe (GSA tows it after 20 idle minutes), so it launches only with one
// spare launch in hand; otherwise it stays and waits for the tow.
func (d *WorkerDispatch) sweepGoHome(ctx context.Context, s *sweep.State, path string, x, y float64, fromSystem bool) error {
	s.Phase = sweep.PhaseHome
	tank, _, cells, premium := sweepFuel(d.Client.GetState())
	total := sweep.Total(tank, cells, premium)
	if fromSystem && total < 2*s.LaunchCost {
		fmt.Fprintf(d.Out, "pathfinder_sweep: %.0f fuel is under two launches (%.0f each); staying put, waiting for GSA tow\n", total, s.LaunchCost) //nolint:errcheck
		return sweep.Save(path, s)
	}
	fmt.Fprintf(d.Out, "pathfinder_sweep: heading home (swept %.1f°, fuel %.0f total)\n", s.SweptDeg, total) //nolint:errcheck
	id, ok, err := galaxy.NearestStationByDistance(ctx, d.KB, x, y)
	if err != nil || !ok {
		_ = sweep.Save(path, s)
		return fmt.Errorf("pathfinder_sweep: no station to head home to from (%.0f,%.0f): %v", x, y, err)
	}
	sys, err := d.KB.GetSystem(ctx, id)
	if err != nil || sys == nil {
		_ = sweep.Save(path, s)
		return fmt.Errorf("pathfinder_sweep: home system %s: %v", id, err)
	}
	s.HomeSystem = id
	s.TargetX, s.TargetY = sys.Position.X, sys.Position.Y
	return d.sweepLaunch(ctx, s, path, sweep.BearingTo(x, y, sys.Position.X, sys.Position.Y))
}

// sweepLaunch burns cells if the tank is low, launches (or redirects) on
// bearing, measures the first launch's real cost, and saves the state.
func (d *WorkerDispatch) sweepLaunch(ctx context.Context, s *sweep.State, path string, bearing float64) error {
	tank, maxTank, cells, premium := sweepFuel(d.Client.GetState())
	for _, r := range sweep.Refills(tank, maxTank, cells, premium, s.LaunchCost) {
		if err := d.Client.UseItem(ctx, r.Item, r.Qty); err != nil {
			fmt.Fprintf(d.Out, "pathfinder_sweep: use %d %s: %v\n", r.Qty, r.Item, err) //nolint:errcheck
		}
	}
	_ = d.Client.GetShip(ctx) // the cached tank lags use_item; measure from the real level
	before, _, _, _ := sweepFuel(d.Client.GetState())
	bearing = math.Round(bearing*10) / 10
	if bearing >= 360 {
		bearing -= 360
	}
	res, err := d.Client.Jump(ctx, strconv.FormatFloat(bearing, 'f', 1, 64))
	if err != nil {
		_ = sweep.Save(path, s)
		return fmt.Errorf("pathfinder_sweep: launch on %.1f: %w", bearing, err)
	}
	if res == nil || !res.Drifting {
		_ = sweep.Save(path, s)
		return fmt.Errorf("pathfinder_sweep: jump %.1f did not start a drift (is a pathfinder_drive fitted?)", bearing)
	}
	if err := d.Client.GetShip(ctx); err == nil && !s.LaunchCostMeasured {
		after, _, _, _ := sweepFuel(d.Client.GetState())
		if c, ok := sweep.MeasuredLaunchCost(before, after); ok {
			s.LaunchCost, s.LaunchCostMeasured = c, true
			fmt.Fprintf(d.Out, "pathfinder_sweep: measured launch cost %.0f fuel\n", c) //nolint:errcheck
		}
	}
	s.PlannedBearing, s.Drifted, s.ReadSinceLaunch, s.Landed = bearing, true, false, false
	s.PendingMeasureSystem = ""
	s.Launches++
	fmt.Fprintf(d.Out, "pathfinder_sweep: launch %d on %.1f° toward %.1f° (ring %.0f, swept %.1f°, phase %s)\n", //nolint:errcheck
		s.Launches, bearing, s.TargetDeg, s.Ring, s.SweptDeg, s.Phase)
	return sweep.Save(path, s)
}

// sweepHomeMissed reports whether a home drift has gone wrong: a storm
// turned it, or it has passed the station's position without arriving.
func sweepHomeMissed(l serverapi.LocationInfo, s *sweep.State) bool {
	if bearingDiff(l.TransitBearing, s.PlannedBearing) > sweepStormTolerance {
		return true
	}
	far := math.Hypot(s.TargetX-l.TransitX, s.TargetY-l.TransitY) > sweep.DriftPerTick
	return far && sweep.Reached(l.TransitX, l.TransitY, s.TargetX, s.TargetY, l.TransitBearing)
}

// sweepFuel is the tank, its capacity, and the fuel cells in the hold.
func sweepFuel(st *game.State) (tank, maxTank float64, cells, premium int) {
	if st == nil {
		return 0, 0, 0, 0
	}
	for _, c := range st.Ship.Cargo {
		switch c.ItemID {
		case "fuel_cell":
			cells += int(c.Quantity)
		case "premium_fuel_cell":
			premium += int(c.Quantity)
		}
	}
	return st.Ship.Fuel, st.Ship.MaxFuel, cells, premium
}

// bearingDiff is the smallest angle between two bearings, in [0, 180].
func bearingDiff(a, b float64) float64 {
	d := math.Mod(math.Abs(a-b), 360)
	if d > 180 {
		d = 360 - d
	}
	return d
}
