package worker

import (
	"context"
	"fmt"

	"github.com/rsned/spacemolt/pkg/sweep"
)

// sweepStageInSystem runs the pre-lap staging route one leg per pass: refuel
// at the station in stage.Refuel, fly by lanes to stage.Launch, then launch
// toward stage.Target's position. The landing (sweepArrival) or a missed
// drift (sweepDrift) ends the stage.
func (d *WorkerDispatch) sweepStageInSystem(ctx context.Context, s *sweep.State, path, sysID string) error {
	stage, err := sweep.ParseStage(d.SweepStage)
	if err != nil || !stage.Set() {
		// The stage was dropped from the config mid-way: start the lap.
		s.Phase = sweep.PhaseLap
		return d.sweepRejoin(ctx, s, path, sysID)
	}
	if !s.StageRefueled {
		poi := d.sweepStationPOI(ctx, stage.Refuel)
		if poi == "" {
			_ = sweep.Save(path, s)
			return fmt.Errorf("pathfinder_sweep: stage refuel system %s has no accessible station", stage.Refuel)
		}
		if err = d.ensureHomeNav(ctx, stage.Refuel, poi); err != nil {
			_ = sweep.Save(path, s)
			return fmt.Errorf("pathfinder_sweep: stage: reach %s: %w", stage.Refuel, err)
		}
		if err = dockIdempotent(ctx, d.Client); err != nil {
			_ = sweep.Save(path, s)
			return fmt.Errorf("pathfinder_sweep: stage: dock at %s: %w", stage.Refuel, err)
		}
		if err = exploreRefuelDocked(ctx, ExploreDeps{Client: d.Client, KB: d.KB}, d.Out); err != nil {
			_ = sweep.Save(path, s)
			return fmt.Errorf("pathfinder_sweep: stage: refuel at %s: %w", stage.Refuel, err)
		}
		s.StageRefueled = true
		fmt.Fprintf(d.Out, "pathfinder_sweep: stage: refueled at %s\n", stage.Refuel) //nolint:errcheck
		return sweep.Save(path, s)
	}
	if sysID != stage.Launch {
		fmt.Fprintf(d.Out, "pathfinder_sweep: stage: lanes to launch point %s\n", stage.Launch) //nolint:errcheck
		if err = d.ensureHomeNav(ctx, stage.Launch, ""); err != nil {
			_ = sweep.Save(path, s)
			return fmt.Errorf("pathfinder_sweep: stage: reach %s: %w", stage.Launch, err)
		}
		return sweep.Save(path, s)
	}
	x, y := d.sweepPosition(ctx, s, sysID)
	tgt, err := d.KB.GetSystem(ctx, stage.Target)
	if err != nil || tgt == nil || (tgt.Position.X == 0 && tgt.Position.Y == 0) || (x == 0 && y == 0) {
		fmt.Fprintf(d.Out, "pathfinder_sweep: stage: no position for %s or %s; starting the lap\n", stage.Launch, stage.Target) //nolint:errcheck
		s.Phase = sweep.PhaseLap
		return d.sweepRejoin(ctx, s, path, sysID)
	}
	s.TargetX, s.TargetY = tgt.Position.X, tgt.Position.Y
	fmt.Fprintf(d.Out, "pathfinder_sweep: stage: launching from %s toward %s\n", stage.Launch, stage.Target) //nolint:errcheck
	return d.sweepLaunch(ctx, s, path, sweep.BearingTo(x, y, tgt.Position.X, tgt.Position.Y))
}
