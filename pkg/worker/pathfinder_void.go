package worker

import (
	"cmp"
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
	// voidCaptureSkip: a ship in a system this close to its waypoint steps
	// on to the next one, since every relaunch at it would be captured
	// straight back (about the capture radius).
	voidCaptureSkip = 100.0
	// voidFindMargin is how far outside a zone's square wave a landing
	// still counts as a find for the zone.
	voidFindMargin = 300.0
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
		// Start as if docked after a zone: refuel first, then claim and go.
		ns.Phase = sweep.PhaseDone
		if ok { // a finished ring lap: keep its measured launch cost
			ns.LaunchCost, ns.LaunchCostMeasured = s.LaunchCost, s.LaunchCostMeasured
		}
		s = ns
	}
	d.voidKeepLease(s)

	if s.Phase == sweep.PhaseDone {
		if st := d.Client.GetState(); st != nil && (st.Doc || st.Ship.DockedAtBase != "") {
			if s.ZoneID == "" {
				claimed, cerr := d.voidClaim(ctx, s, path)
				if cerr != nil || !claimed {
					return cerr
				}
			}
			return d.sweepAfterLap(ctx, s, path)
		}
		// Done but undocked (play_as, a manual undock): back to a station,
		// or the lease, refreshed every pass, would hold the zone forever.
		fmt.Fprintln(d.Out, "void sweep: done but not docked; heading for a station") //nolint:errcheck
		s.Phase = sweep.PhaseHome
	}
	loc, err := d.sweepLocation(ctx)
	if err != nil {
		return err
	}
	if loc.Location.Drifting() {
		return d.sweepDrift(ctx, s, path, loc)
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
	s.ZoneID, s.Waypoints, s.Waypoint, s.ZoneFound, s.StagePending = z.ID, z.Waypoints, z.Next, nil, true
	fmt.Fprintf(d.Out, "void sweep: claimed %s (%.2fM LY², %d lanes) at leg %d/%d\n", //nolint:errcheck
		z.ID, z.Area/1e6, len(z.Waypoints)/2, z.Next, len(z.Waypoints))
	return true, sweep.Save(path, s)
}

// voidStageTries bounds how many staging systems voidStage tries, nearest
// first, before launching from where the ship is.
const voidStageTries = 4

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
		return false, nil                                                         //nolint:nilerr // staging is optional; launch from here
	}
	unlocked := smugglingUnlocked(d.Client.GetState())
	type cand struct {
		id string
		d  float64
	}
	var cands []cand
	for _, sys := range systems {
		if (sys.Position.X == 0 && sys.Position.Y == 0 && sys.ID != "sol") || (sys.IsStronghold && !unlocked) {
			continue
		}
		cands = append(cands, cand{sys.ID, math.Hypot(sys.Position.X-wp.X, sys.Position.Y-wp.Y)})
	}
	slices.SortFunc(cands, func(a, b cand) int { return cmp.Compare(a.d, b.d) })
	// The nearest system is not always reachable by lanes: a frontier system
	// has none (explorer-8 → Turnback) and an avoided one is refused
	// (explorer-9 → Taygeta). Try the next-nearest few before giving up.
	for i, c := range cands {
		if i >= voidStageTries || c.id == sysID {
			return false, nil
		}
		fmt.Fprintf(d.Out, "void sweep: staging at %s (%.0f LY from %s's leg %d) by lanes\n", c.id, c.d, s.ZoneID, s.Waypoint) //nolint:errcheck
		// A lane jump that lands while the previous action is still settling
		// answers "action pending"; wait it out rather than give up the staging
		// and launch from far away (explorer-5, 11-15).
		err := waitOutPending(ctx, func() error { return d.ensureHomeNav(ctx, c.id, "") })
		if err == nil {
			return true, sweep.Save(path, s)
		}
		if errors.Is(err, ErrRouteThroughAvoided) || strings.Contains(strings.ToLower(err.Error()), "no route") {
			fmt.Fprintf(d.Out, "void sweep: staging at %s: %v; trying the next nearest\n", c.id, err) //nolint:errcheck
			continue
		}
		fmt.Fprintf(d.Out, "void sweep: staging at %s: %v; launching from here\n", c.id, err) //nolint:errcheck
		return false, nil
	}
	fmt.Fprintln(d.Out, "void sweep: no staging system reachable by lanes; launching from here") //nolint:errcheck
	return false, nil
}

// voidLanded credits a landing to the zone when the system is hidden (not
// charted map ground) and lies within voidFindMargin of the zone's square
// wave: a long drift in from a station lands in systems that are not finds
// of this zone.
func (d *WorkerDispatch) voidLanded(ctx context.Context, s *sweep.State, sysID string) {
	if s.ZoneID == "" || sysID == "" || len(s.Waypoints) == 0 || d.sweepAPISystem(ctx, sysID) {
		return
	}
	x, y := d.sweepPosition(ctx, s, sysID)
	x0, y0, x1, y1 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, p := range s.Waypoints {
		x0, y0, x1, y1 = min(x0, p.X), min(y0, p.Y), max(x1, p.X), max(y1, p.Y)
	}
	if x < x0-voidFindMargin || x > x1+voidFindMargin || y < y0-voidFindMargin || y > y1+voidFindMargin {
		return
	}
	d.voidFound(s, sysID)
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
	if s.ZoneID == "" || s.Waypoint >= len(s.Waypoints) {
		// The zone went away mid-drift (lease lapsed while the worker was
		// stopped): take another and aim at it, or make for a station.
		claimed, err := d.voidClaim(ctx, s, path)
		if err != nil {
			return true, err
		}
		if !claimed {
			return true, d.sweepGoHome(ctx, s, path, x, y, false)
		}
		s.StagePending = false // adrift: no lanes to fly
		return true, d.voidLeg(ctx, s, path, x, y)
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
		return true, d.voidZoneDone(ctx, s, path, x, y, false)
	}
	return true, d.voidLeg(ctx, s, path, x, y)
}

// voidZoneDone closes the finished zone and heads for a station, where the
// next zone is claimed.
func (d *WorkerDispatch) voidZoneDone(ctx context.Context, s *sweep.State, path string, x, y float64, fromSystem bool) error {
	fmt.Fprintf(d.Out, "void sweep: %s done (%d legs, found %v); heading for a station\n", s.ZoneID, len(s.Waypoints), s.ZoneFound) //nolint:errcheck
	s.ZoneID, s.Waypoints, s.Waypoint, s.ZoneFound, s.StagePending = "", nil, 0, nil, false
	return d.sweepGoHome(ctx, s, path, x, y, fromSystem)
}

// voidRejoin launches from a system back into the zone: staged by lanes
// first after a claim, stepping past any waypoint the ship already sits on
// (relaunching at it would be captured straight back here).
func (d *WorkerDispatch) voidRejoin(ctx context.Context, s *sweep.State, path, sysID string, x, y float64) error {
	if s.ZoneID == "" {
		return sweep.Save(path, s)
	}
	if s.StagePending {
		s.StagePending = false
		if moved, err := d.voidStage(ctx, s, path, sysID); moved {
			return err
		}
	}
	for s.Waypoint < len(s.Waypoints) {
		wp := s.Waypoints[s.Waypoint]
		if math.Hypot(wp.X-x, wp.Y-y) >= voidCaptureSkip {
			break
		}
		s.Gaps = append(s.Gaps, sweep.Gap{FromDeg: float64(s.Waypoint), ToDeg: float64(s.Waypoint + 1), Reason: "waypoint inside " + sysID})
		fmt.Fprintf(d.Out, "void sweep: %s leg %d ends inside %s; stepping past it\n", s.ZoneID, s.Waypoint, sysID) //nolint:errcheck
		s.Waypoint++
		if err := sweep.SaveProgress(d.voidLedger(), d.AgentID, s.ZoneID, s.Waypoint, nil, time.Now()); err != nil {
			fmt.Fprintf(d.Out, "void sweep: save progress: %v\n", err) //nolint:errcheck
		}
	}
	if s.Waypoint >= len(s.Waypoints) {
		return d.voidZoneDone(ctx, s, path, x, y, true)
	}
	wp := s.Waypoints[s.Waypoint]
	s.TargetX, s.TargetY = wp.X, wp.Y
	return d.sweepLaunch(ctx, s, path, sweep.BearingTo(x, y, wp.X, wp.Y))
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
