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
		d.sweepCollectChat(ctx, sysID)
		d.sweepCaptureFn()(ctx, sysID)
	}
	d.sweepWritePendingMeasure(ctx, s)

	switch s.Phase {
	case sweep.PhaseStage:
		return d.sweepStageInSystem(ctx, s, path, sysID)
	case sweep.PhaseHome:
		return d.sweepHomeInSystem(ctx, s, path, sysID)
	case sweep.PhaseLap:
		// In a system without having drifted here: the sweep's first launch,
		// or a restart that found the ship parked. Head for the ring.
		return d.sweepRejoin(ctx, s, path, sysID)
	}

	// PhaseExplore. A leviathan here first: the first command is the way out.
	if lev := d.sweepLeviathanHere(ctx); lev != "" {
		fmt.Fprintf(d.Out, "pathfinder_sweep: LEVIATHAN at %s (%s); leaving without touring\n", sysID, lev) //nolint:errcheck
		s.AddUnfinished(sysID)
		return d.sweepRejoin(ctx, s, path, sysID)
	}
	if d.sweepAPISystem(ctx, sysID) {
		// Map coordinates mean old-galaxy ground, already charted: exploring
		// from here would tour the whole lane-connected old map.
		fmt.Fprintf(d.Out, "pathfinder_sweep: %s is charted (map coordinates); relaunching without exploring\n", sysID) //nolint:errcheck
		return d.sweepRejoin(ctx, s, path, sysID)
	}
	if len(loc.Location.NearbyRepeaters) > 0 {
		fmt.Fprintf(d.Out, "pathfinder_sweep: %d repeater(s) at %s; leaving\n", len(loc.Location.NearbyRepeaters), sysID) //nolint:errcheck
		s.AddUnfinished(sysID)
		return d.sweepRejoin(ctx, s, path, sysID)
	}
	if !s.ArrivalToured && !d.sweepLeaveHazardPOI(ctx, sysID, loc.Location.POIID) {
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
			err := d.sweepExploreStepFn()(ctx, deps)
			if !sweepUnroutable(err) {
				return err
			}
			// The target is in the KB but not on the server's lane map:
			// retrying parks the ship on whatever POI it landed at
			// (explorer-4, nine tries on a radiation POI). Give up here.
			fmt.Fprintf(d.Out, "pathfinder_sweep: %s is unroutable from %s (%v); logged unfinished, rejoining\n", target, sysID, err) //nolint:errcheck
			s.AddUnfinished(sysID)
			return d.sweepRejoin(ctx, s, path, sysID)
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

// sweepCollectChat pulls this system's chat history into the mbox on
// landing, before the ship moves on and the scoped history is out of reach.
func (d *WorkerDispatch) sweepCollectChat(ctx context.Context, sysID string) {
	if d.ChatBackfill == nil {
		return
	}
	n, err := d.ChatBackfill(ctx)
	if err != nil {
		fmt.Fprintf(d.Out, "pathfinder_sweep: chat history at %s: %v\n", sysID, err) //nolint:errcheck
		return
	}
	fmt.Fprintf(d.Out, "pathfinder_sweep: chat history at %s: %d message(s) walked\n", sysID, n) //nolint:errcheck
}

// sweepArrival records a drift's landing: the measured position, any
// repeaters there, and the switch from lap to explore.
func (d *WorkerDispatch) sweepArrival(ctx context.Context, s *sweep.State, sysID string, loc serverapi.LocationResponse) {
	// Only a reading taken during this drift measures where it ended; a
	// drift that landed before the first reading leaves LastX/LastY at the
	// previous system, which must not be written as this one's position.
	var tick int64
	if gs := d.Client.GetState(); gs != nil {
		tick = gs.CurrentTick
	}
	// ...and only a FRESH one: a worker stopped mid-drift and restarted after
	// the landing holds a reading from far back along the drift.
	fresh := s.LastReadTick > 0 && tick > 0 && tick-s.LastReadTick <= sweepMeasureMaxAgeTicks
	if s.ReadSinceLaunch && sysID != "" && !fresh {
		fmt.Fprintf(d.Out, "pathfinder_sweep: landed in %s with a reading %d tick(s) old; not measuring\n", sysID, tick-s.LastReadTick) //nolint:errcheck
	}
	if s.ReadSinceLaunch && sysID != "" {
		if fresh {
			s.PendingMeasureSystem, s.PendingX, s.PendingY = sysID, s.LastX, s.LastY
			s.PendingMeasureName = loc.Location.SystemName
		}
		if s.Phase == sweep.PhaseLap {
			// Count the arc up to the landing.
			if deg := unwrapNear(s.LastX, s.LastY, s.ProgressDeg); deg < s.ProgressDeg && deg >= s.TargetDeg-sweep.StepDeg {
				sweep.Advance(s, deg)
			}
		}
	}
	if s.Phase == sweep.PhaseLap && sysID != "" {
		// A landing even with no reading since the last launch (captured on
		// the tick after a replot): the rejoin must still step past it, or
		// it aims at the current angle and drifts back in (explorer-5 at
		// Strobe, live 2026-11-13).
		s.Landed = true
	}
	if n, err := knowledge.CaptureHostilesNearby(ctx, d.KB, loc.Location.NearbyRepeaters, sysID, loc.Location.POIID, d.AgentID, tick); err != nil {
		fmt.Fprintf(d.Out, "pathfinder_sweep: hostile capture at %s: %v\n", sysID, err) //nolint:errcheck
	} else if n > 0 {
		fmt.Fprintf(d.Out, "pathfinder_sweep: %d repeater(s) recorded at %s\n", n, sysID) //nolint:errcheck
	}
	s.Drifted, s.ArrivalToured, s.ExploreJumps = false, false, 0
	if s.Phase == sweep.PhaseLap || s.Phase == sweep.PhaseStage {
		// A staging landing explores like a lap landing; the lap then starts
		// at the start angle, since no arc has been swept yet.
		s.Phase = sweep.PhaseExplore
	}
	fmt.Fprintf(d.Out, "pathfinder_sweep: drift landed in %s (phase %s)\n", sysID, s.Phase) //nolint:errcheck
}

// sweepWritePendingMeasure writes a held arrival measurement. A frontier
// system has no KB row (the map capture skips systems without coordinates
// and the tour never makes one), so the write creates it; waiting for a row
// silently dropped Patient Span's position on 2026-11-13.
func (d *WorkerDispatch) sweepWritePendingMeasure(ctx context.Context, s *sweep.State) {
	id := s.PendingMeasureSystem
	rec, ok := d.KB.(knowledge.SystemPositionRecorder)
	if id == "" || !ok {
		return
	}
	wrote, err := rec.RecordMeasuredSystemPosition(ctx, id, s.PendingMeasureName, s.PendingX, s.PendingY)
	switch {
	case err != nil:
		fmt.Fprintf(d.Out, "pathfinder_sweep: record position of %s: %v\n", id, err) //nolint:errcheck
		return
	case wrote:
		fmt.Fprintf(d.Out, "pathfinder_sweep: %s measured at (%.1f, %.1f)\n", id, s.PendingX, s.PendingY) //nolint:errcheck
	}
	s.PendingMeasureSystem, s.PendingMeasureName = "", ""
}

// sweepAPISystem reports whether sysID has map (API) coordinates, i.e. is
// charted old-galaxy ground. A KB that cannot say counts as uncharted.
func (d *WorkerDispatch) sweepAPISystem(ctx context.Context, sysID string) bool {
	src, ok := d.KB.(knowledge.SystemPositionSourcer)
	if !ok {
		return false
	}
	s, err := src.SystemPositionSource(ctx, sysID)
	return err == nil && s == "api"
}

// unwrapNear is the polar angle of (x,y), unwrapped by whole turns to lie
// within 180° of ref (progress angles keep decreasing past -180).
func unwrapNear(x, y, ref float64) float64 {
	_, deg := sweep.Polar(x, y)
	return deg + 360*math.Round((ref-deg)/360)
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
		return d.sweepGoHome(ctx, s, path, x, y, true)
	}
	d.sweepRepairBeforeLaunch(ctx, sysID)
	s.Phase = sweep.PhaseLap
	if d.SweepRing > 0 && d.SweepRing != s.Ring {
		// The fleet re-spaced the rings (sweep_ring changed): take the new
		// ring on this departure; the leg out aims at it.
		fmt.Fprintf(d.Out, "pathfinder_sweep: ring %.0f -> %.0f (fleet config)\n", s.Ring, d.SweepRing) //nolint:errcheck
		s.Ring = d.SweepRing
	}
	tx, ty, deg := sweep.NextTarget(s, x, y)
	if s.Landed {
		// Back from a landing: aim at the next waypoint past it, not at the
		// landing angle, or the drift lands in the same system again. A
		// capture can drag the ship past the leg's waypoint (capture radius
		// ~50-90 LY), so count the arc up to the system's own angle first:
		// aiming at a waypoint behind it launches backwards and the next leg
		// drifts straight back in (explorer-1/-9, live 2026-11-13).
		if sd := unwrapNear(x, y, s.ProgressDeg); sd < s.ProgressDeg {
			sweep.Advance(s, sd)
		}
		deg = s.ProgressDeg - sweep.StepDeg
		tx, ty = sweep.RingPoint(s.Ring, deg)
	}
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
		if home := d.sweepLaneHome(ctx, graph, sysID); home != "" {
			s.HomeSystem = home
			if err := sweep.Save(path, s); err != nil {
				return err
			}
			fmt.Fprintf(d.Out, "pathfinder_sweep: home by lanes to %s\n", home) //nolint:errcheck
			return d.ensureHomeNav(ctx, home, "")
		}
	}
	if x, y := d.sweepPosition(ctx, s, sysID); x != 0 || y != 0 {
		return d.sweepGoHome(ctx, s, path, x, y, true)
	}
	tank, _, cells, premium := sweepFuel(d.Client.GetState())
	fmt.Fprintf(d.Out, "pathfinder_sweep: stuck at %s with %.0f fuel and no station by lane; waiting for GSA tow\n", sysID, sweep.Total(tank, cells, premium)) //nolint:errcheck
	return sweep.Save(path, s)
}

// sweepStationPOI is the POI of an accessible, non-outpost station in
// sysID, or "" when it has none.
func (d *WorkerDispatch) sweepStationPOI(ctx context.Context, sysID string) string {
	unlocked := smugglingUnlocked(d.Client.GetState())
	sys, err := d.KB.GetSystem(ctx, sysID)
	if err != nil || sys == nil || (sys.IsStronghold && !unlocked) {
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
		if b, err := d.KB.GetBaseByPOI(ctx, p.ID); err == nil && b != nil && (b.PublicAccess || sys.IsStronghold) && !b.IsOutpost() {
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
