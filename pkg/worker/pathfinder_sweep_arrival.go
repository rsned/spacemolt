package worker

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/rsned/spacemolt/pkg/galaxy"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/sweep"
)

const (
	// sweepExploreStale passed as StaleTicks means a stale system never
	// qualifies: an explore run visits only undiscovered and never-visited
	// systems, so a drift that lands in the old galaxy does not set off a
	// re-capture tour of the whole map.
	sweepExploreStale = math.MaxInt64 / 2
	// sweepExploreMaxJumps bounds one explore run; what is left is logged
	// as unfinished for a later run.
	sweepExploreMaxJumps = 30
)

// sweepInSystem handles a pass that finds the ship in a system: record an
// arrival, explore the cluster one step per pass, rejoin the ring, or finish
// at a station.
func (d *WorkerDispatch) sweepInSystem(ctx context.Context, s *sweep.State, path string, loc serverapi.LocationResponse) error {
	st := d.Client.GetState()
	sysID := loc.Location.SystemID
	if sysID == "" && st != nil {
		sysID = st.System.ID
	}
	deps := ExploreDeps{Client: d.Client, KB: d.KB, Out: d.Out, StaleTicks: sweepExploreStale, AgentID: d.AgentID, Market: d.Market}

	if s.Drifted {
		d.sweepArrival(ctx, s, sysID, loc)
	}

	switch s.Phase {
	case sweep.PhaseHome:
		return d.sweepHomeInSystem(ctx, s, path, sysID)
	case sweep.PhaseLap:
		// In a system without having drifted here: the sweep's first launch,
		// or a restart that found the ship parked. Head for the ring.
		return d.sweepRejoin(ctx, s, path, sysID)
	}

	// PhaseExplore.
	if len(loc.Location.NearbyRepeaters) > 0 {
		fmt.Fprintf(d.Out, "pathfinder_sweep: %d repeater(s) at %s; leaving\n", len(loc.Location.NearbyRepeaters), sysID) //nolint:errcheck
		s.AddUnfinished(sysID)
		return d.sweepRejoin(ctx, s, path, sysID)
	}
	if leave, why := hazardShouldLeave(st, time.Now()); leave {
		fmt.Fprintf(d.Out, "pathfinder_sweep: hazard at %s (%s); leaving\n", sysID, why) //nolint:errcheck
		s.AddUnfinished(sysID)
		return d.sweepRejoin(ctx, s, path, sysID)
	}
	if !s.ArrivalToured {
		s.ArrivalToured = true
		if err := sweep.Save(path, s); err != nil {
			return err
		}
		return d.sweepTourFn()(ctx, deps)
	}
	if s.ExploreJumps < sweepExploreMaxJumps {
		var tick int64
		if st != nil {
			tick = st.CurrentTick
		}
		target, ok, err := NextExploreTarget(ctx, d.KB, sysID, sweepExploreStale, tick)
		if err != nil {
			return err
		}
		if ok {
			s.ExploreJumps++
			if err := sweep.Save(path, s); err != nil {
				return err
			}
			fmt.Fprintf(d.Out, "pathfinder_sweep: explore run step %d: %s -> %s\n", s.ExploreJumps, sysID, target) //nolint:errcheck
			return d.sweepExploreStepFn()(ctx, deps)
		}
	} else {
		fmt.Fprintf(d.Out, "pathfinder_sweep: explore run hit %d jumps at %s; logged unfinished\n", sweepExploreMaxJumps, sysID) //nolint:errcheck
		s.AddUnfinished(sysID)
	}
	if st != nil && (st.Doc || st.Ship.DockedAtBase != "") {
		_ = exploreRefuelDocked(ctx, deps, d.Out)
	}
	return d.sweepRejoin(ctx, s, path, sysID)
}

// sweepArrival records a drift's landing: the measured position, any
// repeaters there, and the switch from lap to explore.
func (d *WorkerDispatch) sweepArrival(ctx context.Context, s *sweep.State, sysID string, loc serverapi.LocationResponse) {
	if rec, ok := d.KB.(knowledge.SystemPositionRecorder); ok && sysID != "" {
		wrote, err := rec.RecordMeasuredSystemPosition(ctx, sysID, s.LastX, s.LastY)
		switch {
		case err != nil:
			fmt.Fprintf(d.Out, "pathfinder_sweep: record position of %s: %v\n", sysID, err) //nolint:errcheck
		case wrote:
			fmt.Fprintf(d.Out, "pathfinder_sweep: %s measured at (%.1f, %.1f)\n", sysID, s.LastX, s.LastY) //nolint:errcheck
		}
	}
	var tick int64
	if gs := d.Client.GetState(); gs != nil {
		tick = gs.CurrentTick
	}
	if n, err := knowledge.CaptureHostilesNearby(ctx, d.KB, loc.Location.NearbyRepeaters, sysID, loc.Location.POIID, d.AgentID, tick); err != nil {
		fmt.Fprintf(d.Out, "pathfinder_sweep: hostile capture at %s: %v\n", sysID, err) //nolint:errcheck
	} else if n > 0 {
		fmt.Fprintf(d.Out, "pathfinder_sweep: %d repeater(s) recorded at %s\n", n, sysID) //nolint:errcheck
	}
	s.Drifted, s.ArrivalToured, s.ExploreJumps = false, false, 0
	if s.Phase == sweep.PhaseLap {
		s.Phase = sweep.PhaseExplore
	}
	fmt.Fprintf(d.Out, "pathfinder_sweep: drift landed in %s (phase %s)\n", sysID, s.Phase) //nolint:errcheck
}

// sweepPosition is the ship's map position in sysID: the KB position, or
// the last drift reading when the system has no coordinates yet.
func (d *WorkerDispatch) sweepPosition(ctx context.Context, s *sweep.State, sysID string) (x, y float64) {
	if sys, err := d.KB.GetSystem(ctx, sysID); err == nil && sys != nil && (sys.Position.X != 0 || sys.Position.Y != 0) {
		return sys.Position.X, sys.Position.Y
	}
	return s.LastX, s.LastY
}

// sweepRejoin launches back toward the ring at the saved angle (or heads
// home when the fuel budget no longer allows another lap launch).
func (d *WorkerDispatch) sweepRejoin(ctx context.Context, s *sweep.State, path, sysID string) error {
	x, y := d.sweepPosition(ctx, s, sysID)
	if x == 0 && y == 0 {
		_ = sweep.Save(path, s)
		return fmt.Errorf("pathfinder_sweep: position of %s unknown; cannot plot a bearing", sysID)
	}
	tank, _, cells, premium := sweepFuel(d.Client.GetState())
	if !sweep.CanContinue(tank, cells, premium, s.LaunchCost) {
		return d.sweepGoHome(ctx, s, path, x, y)
	}
	s.Phase = sweep.PhaseLap
	tx, ty, deg := sweep.NextTarget(s, x, y)
	s.TargetX, s.TargetY, s.TargetDeg = tx, ty, deg
	return d.sweepLaunch(ctx, s, path, sweep.BearingTo(x, y, tx, ty))
}

// sweepHomeInSystem finishes at a station in this system, else flies to one
// by lanes, else launches toward one, else waits for the GSA tow.
func (d *WorkerDispatch) sweepHomeInSystem(ctx context.Context, s *sweep.State, path, sysID string) error {
	if poi := d.sweepStationPOI(ctx, sysID); poi != "" {
		if err := d.ensureHomeNav(ctx, sysID, poi); err != nil {
			_ = sweep.Save(path, s)
			return fmt.Errorf("pathfinder_sweep: reach station at %s: %w", sysID, err)
		}
		if err := dockIdempotent(ctx, d.Client); err != nil {
			_ = sweep.Save(path, s)
			return fmt.Errorf("pathfinder_sweep: dock at %s: %w", sysID, err)
		}
		_ = exploreRefuelDocked(ctx, ExploreDeps{Client: d.Client, KB: d.KB}, d.Out)
		s.Phase = sweep.PhaseDone
		fmt.Fprintf(d.Out, "pathfinder_sweep: lap complete at %s; ring %.0f, swept %.1f°, launches %d, gaps %d, unfinished %v\n", //nolint:errcheck
			sysID, s.Ring, s.SweptDeg, s.Launches, len(s.Gaps), s.Unfinished)
		return sweep.Save(path, s)
	}
	graph := &galaxy.GalaxyGraph{}
	if err := graph.BuildFromDB(ctx, d.KB); err == nil {
		if near, err := galaxy.FindNearestStation(ctx, d.KB, graph, sysID, 1, false); err == nil && len(near) > 0 && near[0].SystemID != sysID {
			s.HomeSystem = near[0].SystemID
			if err := sweep.Save(path, s); err != nil {
				return err
			}
			fmt.Fprintf(d.Out, "pathfinder_sweep: home by lanes to %s\n", near[0].SystemID) //nolint:errcheck
			return d.ensureHomeNav(ctx, near[0].SystemID, "")
		}
	}
	tank, _, cells, premium := sweepFuel(d.Client.GetState())
	if sweep.Total(tank, cells, premium) >= s.LaunchCost {
		x, y := d.sweepPosition(ctx, s, sysID)
		if x != 0 || y != 0 {
			return d.sweepGoHome(ctx, s, path, x, y)
		}
	}
	fmt.Fprintf(d.Out, "pathfinder_sweep: stuck at %s with %.0f fuel and no station by lane; waiting for GSA tow\n", sysID, sweep.Total(tank, cells, premium)) //nolint:errcheck
	return sweep.Save(path, s)
}

// sweepStationPOI is the POI of an accessible, non-outpost station in
// sysID, or "" when it has none.
func (d *WorkerDispatch) sweepStationPOI(ctx context.Context, sysID string) string {
	if sys, err := d.KB.GetSystem(ctx, sysID); err != nil || sys == nil || sys.IsStronghold {
		return ""
	}
	pois, err := d.KB.GetPOIs(ctx, sysID)
	if err != nil {
		return ""
	}
	for _, p := range pois {
		if p.Type != "station" {
			continue
		}
		if b, err := d.KB.GetBaseByPOI(ctx, p.ID); err == nil && b != nil && b.PublicAccess && !b.IsOutpost() {
			return p.ID
		}
	}
	return ""
}

func (d *WorkerDispatch) sweepTourFn() func(context.Context, ExploreDeps) error {
	if d.sweepTour != nil {
		return d.sweepTour
	}
	return func(ctx context.Context, deps ExploreDeps) error { return systemTour(ctx, deps, deps.Out) }
}

func (d *WorkerDispatch) sweepExploreStepFn() func(context.Context, ExploreDeps) error {
	if d.sweepExploreStep != nil {
		return d.sweepExploreStep
	}
	return Explore
}
