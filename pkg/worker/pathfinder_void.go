package worker

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/galaxy"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/sweep"
)

// Interior void sweep: the pathfinder_sweep verb in sweep_mode "void". The
// ship claims a void zone from the shared ledger, flies its square wave
// waypoint by waypoint, explores any system a drift lands in, refuels at
// stations, and claims the next zone when one is done. Drift, explore,
// safety, fuel-home and refuel-resume are the ring sweep's own code.
//
// Spec: docs/superpowers/specs/2026-11-14-interior-void-sweep-design.md

const (
	// voidDefaultLedger is the run ledger when sweep_void_run is unset.
	voidDefaultLedger = "data/sweep/void-run.json"
	// voidStationReach is how close (LY) a station must be to a zone's
	// next waypoint for an explorer to take the zone on.
	voidStationReach = 2500.0
)

func (d *WorkerDispatch) voidLedger() string {
	if d.SweepVoidRun != "" {
		return d.SweepVoidRun
	}
	return voidDefaultLedger
}

// voidSweep is one bounded pass of the void sweep.
func (d *WorkerDispatch) voidSweep(ctx context.Context) error {
	if d.SweepLaunchCost <= 0 {
		return errors.New("pathfinder_sweep: sweep_launch_cost not configured")
	}
	path := sweep.StatePath(d.agentsDir(), d.AgentID)
	s, ok, err := sweep.Load(path)
	if err != nil {
		return fmt.Errorf("pathfinder_sweep: %w", err)
	}
	if !ok || s.Mode != sweep.ModeVoid {
		ns := sweep.NewVoidState(d.SweepLaunchCost)
		if ok { // a finished ring lap: keep its measured launch cost
			ns.LaunchCost, ns.LaunchCostMeasured = s.LaunchCost, s.LaunchCostMeasured
		}
		s = ns
	}
	d.voidKeepLease(s)

	if s.Phase == sweep.PhaseDone {
		if s.ZoneID == "" {
			claimed, cerr := d.voidClaim(ctx, s, path)
			if cerr != nil || !claimed {
				return cerr
			}
		}
		return d.sweepAfterLap(ctx, s, path)
	}
	loc, err := d.sweepLocation(ctx)
	if err != nil {
		return err
	}
	if loc.Location.Drifting() {
		return d.sweepDrift(ctx, s, path, loc)
	}
	sysID := loc.Location.SystemID
	if st := d.Client.GetState(); sysID == "" && st != nil {
		sysID = st.System.ID
	}
	if s.Drifted && s.ZoneID != "" && sysID != "" && !d.sweepAPISystem(ctx, sysID) {
		d.voidFound(s, sysID)
	}
	if s.ZoneID == "" && (s.Phase == sweep.PhaseLap || s.Phase == sweep.PhaseStage) {
		claimed, err := d.voidClaim(ctx, s, path)
		if err != nil {
			return err
		}
		if !claimed {
			s.Phase = sweep.PhaseHome // dock at a station and wait there
			return d.sweepInSystem(ctx, s, path, loc)
		}
		if staged, err := d.voidStage(ctx, s, path, sysID); staged {
			return err
		}
	}
	return d.sweepInSystem(ctx, s, path, loc)
}

// voidKeepLease refreshes the claim on the ship's zone every pass, so only a
// dead worker's lease lapses. A zone another explorer has taken over (our
// lease did lapse) is dropped; the next in-system pass claims another.
func (d *WorkerDispatch) voidKeepLease(s *sweep.State) {
	if s.ZoneID == "" {
		return
	}
	err := sweep.SaveProgress(d.voidLedger(), d.AgentID, s.ZoneID, s.Waypoint, nil, time.Now())
	switch {
	case err == nil:
	case strings.Contains(err.Error(), "does not hold"):
		fmt.Fprintf(d.Out, "void sweep: lost %s (%v); dropping it\n", s.ZoneID, err) //nolint:errcheck
		s.ZoneID, s.Waypoints, s.Waypoint, s.ZoneFound = "", nil, 0, nil
	default:
		fmt.Fprintf(d.Out, "void sweep: lease on %s: %v\n", s.ZoneID, err) //nolint:errcheck
	}
}

// voidClaim takes the next zone from the ledger into s. False (and a "no
// zones left" line) when none is left for this ship.
func (d *WorkerDispatch) voidClaim(ctx context.Context, s *sweep.State, path string) (bool, error) {
	st := d.Client.GetState()
	_, maxTank, _, _ := sweepFuel(st)
	perTank := 0.0
	if s.LaunchCost > 0 {
		perTank = maxTank / s.LaunchCost
	}
	unlocked := smugglingUnlocked(st)
	reachable := func(z *sweep.VoidZone) bool {
		if z.Next >= len(z.Waypoints) {
			return false
		}
		wp := z.Waypoints[z.Next]
		id, found, err := galaxy.NearestStationByDistance(ctx, d.KB, wp.X, wp.Y, unlocked, nil)
		if err != nil || !found {
			return false
		}
		sys, err := d.KB.GetSystem(ctx, id)
		return err == nil && sys != nil && math.Hypot(sys.Position.X-wp.X, sys.Position.Y-wp.Y) <= voidStationReach
	}
	z, err := sweep.ClaimZone(d.voidLedger(), d.AgentID, perTank, reachable, time.Now())
	if err != nil {
		_ = sweep.Save(path, s)
		return false, fmt.Errorf("void sweep: claim: %w", err)
	}
	if z == nil {
		fmt.Fprintln(d.Out, "void sweep: no zones left") //nolint:errcheck
		return false, sweep.Save(path, s)
	}
	s.ZoneID, s.Waypoints, s.Waypoint, s.ZoneFound = z.ID, z.Waypoints, z.Next, nil
	fmt.Fprintf(d.Out, "void sweep: claimed %s (%.2fM LY², %d lanes) at leg %d/%d\n", //nolint:errcheck
		z.ID, z.Area/1e6, len(z.Waypoints)/2, z.Next, len(z.Waypoints))
	return true, sweep.Save(path, s)
}

// voidStage flies by jump lanes to the known system nearest the zone's next
// waypoint, when that is not the system the ship is in, so the launch into
// the zone is short. True when it moved (the next pass launches from there).
func (d *WorkerDispatch) voidStage(ctx context.Context, s *sweep.State, path, sysID string) (bool, error) {
	if s.Waypoint >= len(s.Waypoints) {
		return false, nil
	}
	wp := s.Waypoints[s.Waypoint]
	systems, err := d.KB.GetSystems(ctx)
	if err != nil {
		fmt.Fprintf(d.Out, "void sweep: staging: %v; launching from here\n", err) //nolint:errcheck
		return false, nil //nolint:nilerr // staging is optional; launch from here
	}
	unlocked := smugglingUnlocked(d.Client.GetState())
	best, bestD := "", math.Inf(1)
	for _, sys := range systems {
		if (sys.Position.X == 0 && sys.Position.Y == 0 && sys.ID != "sol") || (sys.IsStronghold && !unlocked) {
			continue
		}
		if dd := math.Hypot(sys.Position.X-wp.X, sys.Position.Y-wp.Y); dd < bestD {
			best, bestD = sys.ID, dd
		}
	}
	if best == "" || best == sysID {
		return false, nil
	}
	fmt.Fprintf(d.Out, "void sweep: staging at %s (%.0f LY from %s's leg %d) by lanes\n", best, bestD, s.ZoneID, s.Waypoint) //nolint:errcheck
	if err := d.ensureHomeNav(ctx, best, ""); err != nil {
		fmt.Fprintf(d.Out, "void sweep: staging at %s: %v; launching from here\n", best, err) //nolint:errcheck
		return false, nil
	}
	return true, sweep.Save(path, s)
}

// voidFound records a hidden system a drift landed in.
func (d *WorkerDispatch) voidFound(s *sweep.State, sysID string) {
	if slices.Contains(s.ZoneFound, sysID) {
		return
	}
	s.ZoneFound = append(s.ZoneFound, sysID)
	fmt.Fprintf(d.Out, "void sweep: found %s in %s\n", sysID, s.ZoneID) //nolint:errcheck
	if err := sweep.SaveProgress(d.voidLedger(), d.AgentID, s.ZoneID, s.Waypoint, []string{sysID}, time.Now()); err != nil {
		fmt.Fprintf(d.Out, "void sweep: record find: %v\n", err) //nolint:errcheck
	}
}

// voidDriftStep is the void sweep's half of a drift reading: a storm
// re-aims at the same waypoint (logged as a gap); reaching the waypoint
// records the leg and plots the next, or ends the zone. False when the
// drift should simply go on.
func (d *WorkerDispatch) voidDriftStep(ctx context.Context, s *sweep.State, path string, l serverapi.LocationInfo, x, y float64) (bool, error) {
	if s.Waypoint >= len(s.Waypoints) {
		return false, nil
	}
	if bearingDiff(l.TransitBearing, s.PlannedBearing) > sweepStormTolerance {
		s.Gaps = append(s.Gaps, sweep.Gap{FromDeg: float64(s.Waypoint), ToDeg: float64(s.Waypoint), Reason: "storm on " + s.ZoneID + " leg"})
		fmt.Fprintf(d.Out, "void sweep: storm deflected the drift (bearing %.1f, plotted %.1f) on %s leg %d; re-flying it from here\n", //nolint:errcheck
			l.TransitBearing, s.PlannedBearing, s.ZoneID, s.Waypoint)
		return true, d.voidLeg(ctx, s, path, x, y)
	}
	wp := s.Waypoints[s.Waypoint]
	if !sweep.Reached(x, y, wp.X, wp.Y, l.TransitBearing) {
		return false, nil
	}
	s.Waypoint++
	if err := sweep.SaveProgress(d.voidLedger(), d.AgentID, s.ZoneID, s.Waypoint, nil, time.Now()); err != nil {
		fmt.Fprintf(d.Out, "void sweep: save progress: %v\n", err) //nolint:errcheck
	}
	if s.Waypoint >= len(s.Waypoints) {
		fmt.Fprintf(d.Out, "void sweep: %s done (%d legs, found %v); heading for a station\n", s.ZoneID, len(s.Waypoints), s.ZoneFound) //nolint:errcheck
		s.ZoneID, s.Waypoints, s.Waypoint, s.ZoneFound = "", nil, 0, nil
		return true, d.sweepGoHome(ctx, s, path, x, y, false)
	}
	return true, d.voidLeg(ctx, s, path, x, y)
}

// voidLeg launches (or redirects) from (x,y) toward the current waypoint,
// or turns for a station when fuel no longer covers the leg and the trip in.
func (d *WorkerDispatch) voidLeg(ctx context.Context, s *sweep.State, path string, x, y float64) error {
	tank, _, cells, premium := sweepFuel(d.Client.GetState())
	if !sweep.CanContinue(tank, cells, premium, s.LaunchCost) {
		return d.sweepGoHome(ctx, s, path, x, y, false)
	}
	wp := s.Waypoints[s.Waypoint]
	s.TargetX, s.TargetY = wp.X, wp.Y
	return d.sweepLaunch(ctx, s, path, sweep.BearingTo(x, y, wp.X, wp.Y))
}
