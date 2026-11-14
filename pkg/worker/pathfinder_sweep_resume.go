package worker

import (
	"context"
	"fmt"
	"time"

	"github.com/rsned/spacemolt/pkg/sweep"
)

// sweepRefuelRetryTicks spaces refuel attempts at a dry desk (1 hour).
const sweepRefuelRetryTicks = 360

// sweepAfterLap runs while the sweep is done and the ship docked: refuel,
// then go back out to the ring at the stopping angle with at least half a
// tank, or move to a station with fuel when this desk is dry (explorer-7
// docked at Longshore's Atlantis on 2026-11-13: no fuel, no market). A ship
// that swept the full circle stays done.
func (d *WorkerDispatch) sweepAfterLap(ctx context.Context, s *sweep.State, path string) error {
	st := d.Client.GetState()
	if sweep.LapDone(s) || st == nil || !st.Doc {
		fmt.Fprintln(d.Out, "pathfinder_sweep: lap complete") //nolint:errcheck
		return nil
	}
	sysID := st.System.ID
	tank, maxTank, cells, premium := sweepFuel(st)
	dry := false
	if tank < maxTank && (s.RefuelTriedTick == 0 || st.CurrentTick-s.RefuelTriedTick >= sweepRefuelRetryTicks) {
		s.RefuelTriedTick = st.CurrentTick
		if err := d.Client.Refuel(ctx); err != nil {
			fmt.Fprintf(d.Out, "pathfinder_sweep: refuel at %s: %v\n", sysID, err) //nolint:errcheck
			if deskIsDry(err) {
				dry = true
				if d.Market != nil {
					_ = d.Market.MarkDeskDry(ctx, st.Player.DockedAtBase, d.AgentID, time.Now())
				}
			}
		}
		_ = d.Client.GetStatus(ctx)
		if st = d.Client.GetState(); st == nil {
			return sweep.Save(path, s)
		}
		tank, maxTank, cells, premium = sweepFuel(st)
	}
	total := sweep.Total(tank, cells, premium)
	switch {
	case total >= sweepResumeFuel(maxTank, s.LaunchCost):
		fmt.Fprintf(d.Out, "pathfinder_sweep: fuel %.0f at %s; resuming the lap at %.1f° on ring %.0f\n", total, sysID, s.ProgressDeg, s.Ring) //nolint:errcheck
		s.Phase, s.HomeSystem, s.Landed = sweep.PhaseLap, "", true
		return d.sweepRejoin(ctx, s, path, sysID)
	case dry && total >= s.LaunchCost:
		x, y := d.sweepPosition(ctx, s, sysID)
		if x == 0 && y == 0 {
			return sweep.Save(path, s)
		}
		fmt.Fprintf(d.Out, "pathfinder_sweep: %s desk is dry (fuel %.0f); moving to a station with fuel\n", sysID, total) //nolint:errcheck
		return d.sweepHeadFor(ctx, s, path, x, y, sysID)
	}
	fmt.Fprintf(d.Out, "pathfinder_sweep: lap complete; docked at %s with %.0f fuel\n", sysID, total) //nolint:errcheck
	return sweep.Save(path, s)
}

// sweepResumeFuel is the fuel a docked ship needs before going back out:
// half its tank, and never less than two home thresholds.
func sweepResumeFuel(maxTank, launchCost float64) float64 {
	return max(maxTank/2, 2*sweep.HomeThreshold(launchCost))
}

// sweepFuelledFilter accepts systems with a station whose desk was last seen
// with fuel (and that also pass also). Nil when there is no fuel data, so the
// caller falls back to plain distance.
func (d *WorkerDispatch) sweepFuelledFilter(ctx context.Context, also func(string) bool) func(string) bool {
	if d.Market == nil {
		return nil
	}
	fuelled, err := d.Market.StationsWithFuel(ctx)
	if err != nil || len(fuelled) == 0 {
		return nil
	}
	return func(sysID string) bool {
		if also != nil && !also(sysID) {
			return false
		}
		if fuelled[sysID] {
			return true
		}
		pois, err := d.KB.GetPOIs(ctx, sysID)
		if err != nil {
			return false
		}
		for _, p := range pois {
			if p.Type != "station" {
				continue
			}
			if fuelled[p.ID] {
				return true
			}
			if b, err := d.KB.GetBaseByPOI(ctx, p.ID); err == nil && b != nil && fuelled[b.ID] {
				return true
			}
		}
		return false
	}
}
