package worker

import (
	"context"
	"fmt"
	"strings"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// sweepHullFloor is the hull fraction below which a sweep ship repairs at a
// station in its system before launching. explorer-4 began the 11-14 bottom
// pass at 86/480 and one radiation POI finished it.
const sweepHullFloor = 0.6

// sweepUnroutable reports an explore step that cannot route to its target
// ("Target system not found"): retrying from here only burns time on
// whatever POI the ship sits at.
func sweepUnroutable(err error) bool {
	return err != nil && strings.Contains(err.Error(), "Target system not found")
}

// sweepHazardView is st with each POI's hazard filled from the KB where the
// live system read carries none, so the refuge plan sees every known hazard.
func (d *WorkerDispatch) sweepHazardView(ctx context.Context, st *game.State, sysID string) *game.State {
	v := st.Clone()
	v.System.POIs = append([]game.POI(nil), st.System.POIs...)
	if d.KB == nil {
		return v
	}
	kpois, err := d.KB.GetPOIs(ctx, sysID)
	if err != nil {
		return v
	}
	for i := range v.System.POIs {
		p := &v.System.POIs[i]
		if p.Hazard != nil {
			continue
		}
		for _, k := range kpois {
			if k.ID == p.ID && k.HazardType != "" {
				p.Hazard = &serverapi.POIHazard{Type: k.HazardType, Severity: k.HazardSeverity}
				break
			}
		}
	}
	return v
}

// sweepLeaveHazardPOI moves the ship off a landing POI that is known to dose
// a ship just sitting there (radiation, tidal shear, scar dust), before the
// tour and the explore steps spend minutes on it. The hazard_warning push
// cannot be relied on to trip the gate first: explorer-4 died at Cinderback
// without one. It reports false when the POI is hazardous and the system has
// nowhere safe to go, or the move failed.
func (d *WorkerDispatch) sweepLeaveHazardPOI(ctx context.Context, sysID, poiID string) bool {
	st := d.Client.GetState()
	if st == nil {
		return true
	}
	v := d.sweepHazardView(ctx, st, sysID)
	if v.CurrentPOI == "" {
		v.CurrentPOI = poiID
	}
	cur := systemPOI(v, v.CurrentPOI)
	if cur == nil || !cur.Hazard.Steady() {
		return true
	}
	plan := hazardRefuge(v)
	if len(plan) == 0 {
		fmt.Fprintf(d.Out, "pathfinder_sweep: landed on %s (%s %d) and %s has no safe POI\n", cur.Name, cur.Hazard.Type, cur.Hazard.Severity, sysID) //nolint:errcheck
		return false
	}
	fmt.Fprintf(d.Out, "pathfinder_sweep: landed on %s (%s %d, %d%% hull a dose); moving off: %v\n", //nolint:errcheck
		cur.Name, cur.Hazard.Type, cur.Hazard.Severity, cur.Hazard.DosePct(), plan)
	for _, step := range plan {
		waitOutHold(ctx, d.Out, d.Client)
		var err error
		switch step[0] {
		case "travel":
			err = waitOutPending(ctx, func() error { _, terr := d.Client.Travel(ctx, step[1]); return terr })
		case "dock":
			err = waitOutPending(ctx, func() error { return dockIdempotent(ctx, d.Client) })
		}
		if err != nil {
			fmt.Fprintf(d.Out, "pathfinder_sweep: %v failed: %v\n", step, err) //nolint:errcheck
			return false
		}
	}
	if s := d.Client.GetState(); s != nil && (s.Doc || s.Ship.DockedAtBase != "") {
		_ = exploreRefuelDocked(ctx, ExploreDeps{Client: d.Client, KB: d.KB}, d.Out)
	}
	return true
}

// sweepRepairBeforeLaunch tops the hull up at a station in sysID when it is
// below sweepHullFloor. With no station here it only warns: the lap goes on,
// since a ship stranded mid-ring is worse off than a thin hull.
func (d *WorkerDispatch) sweepRepairBeforeLaunch(ctx context.Context, sysID string) {
	st := d.Client.GetState()
	if st == nil || st.Ship.MaxHull <= 0 || float64(st.Ship.Hull) >= sweepHullFloor*float64(st.Ship.MaxHull) {
		return
	}
	hull, maxHull := st.Ship.Hull, st.Ship.MaxHull
	poi := d.sweepStationPOI(ctx, sysID)
	if poi == "" {
		if k := cargoQuantity(st, repairKitItem); k > 0 {
			if err := d.Client.Repair(ctx); err != nil {
				fmt.Fprintf(d.Out, "pathfinder_sweep: hull %v/%v, no station at %s, kit repair failed: %v; launching anyway\n", hull, maxHull, sysID, err) //nolint:errcheck
				return
			}
			_ = d.Client.GetStatus(ctx)
			if s2 := d.Client.GetState(); s2 != nil {
				fmt.Fprintf(d.Out, "pathfinder_sweep: no station at %s; repaired from %.0f spare kit(s): hull %v -> %v/%v\n", sysID, k, hull, s2.Ship.Hull, s2.Ship.MaxHull) //nolint:errcheck
			}
			return
		}
		fmt.Fprintf(d.Out, "pathfinder_sweep: hull %v/%v and no station at %s; launching anyway\n", hull, maxHull, sysID) //nolint:errcheck
		return
	}
	fmt.Fprintf(d.Out, "pathfinder_sweep: hull %v/%v; repairing at %s before launch\n", hull, maxHull, poi) //nolint:errcheck
	if !st.Doc && st.Ship.DockedAtBase == "" {
		if err := d.ensureHomeNav(ctx, sysID, poi); err != nil {
			fmt.Fprintf(d.Out, "pathfinder_sweep: reach %s for repair: %v; launching anyway\n", poi, err) //nolint:errcheck
			return
		}
		waitOutHold(ctx, d.Out, d.Client)
		if err := waitOutPending(ctx, func() error { return dockIdempotent(ctx, d.Client) }); err != nil {
			fmt.Fprintf(d.Out, "pathfinder_sweep: dock at %s for repair: %v; launching anyway\n", poi, err) //nolint:errcheck
			return
		}
	}
	_ = exploreRefuelDocked(ctx, ExploreDeps{Client: d.Client, KB: d.KB}, d.Out)
}
