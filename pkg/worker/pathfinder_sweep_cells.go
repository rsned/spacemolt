package worker

import (
	"context"
	"fmt"
	"strings"

	"github.com/rsned/spacemolt/pkg/handoff"
)

// sweepCellsPrefix marks the handoff records cmd/tools/sweep-cellgift queues:
// a depot holder gifts fuel cells to a sweep explorer docked at its station.
const sweepCellsPrefix = "sweepcells/"

// sweepCollectCells loads gifted cells into the hold while the explorer sits
// docked after its lap. A gift lands in the recipient's storage at the
// sender's station, so it is only reachable here, docked at that station.
// Best-effort: a failure is logged and the record stays done for the next pass.
func (d *WorkerDispatch) sweepCollectCells(ctx context.Context) {
	if d.Handoff == nil {
		return
	}
	st := d.Client.GetState()
	if st == nil || !st.Doc || st.Player.DockedAtBase == "" {
		return
	}
	recs, err := d.Handoff.List()
	if err != nil {
		fmt.Fprintf(d.Out, "pathfinder_sweep: read handoff queue: %v\n", err) //nolint:errcheck
		return
	}
	for _, r := range recs {
		if r.Status != handoff.StatusDone || r.Recipient != d.AgentID || r.MovedQty < 1 ||
			r.Station != st.Player.DockedAtBase || !strings.HasPrefix(r.ID, sweepCellsPrefix) {
			continue
		}
		free := int(st.Ship.CargoCapacity - st.Ship.CargoUsed)
		n := min(r.MovedQty, free)
		if n < 1 {
			fmt.Fprintf(d.Out, "pathfinder_sweep: hold full; %d %s left in storage\n", r.MovedQty, r.ItemID) //nolint:errcheck
			return
		}
		if werr := d.Client.WithdrawItems(ctx, r.ItemID, float64(n)); werr != nil {
			fmt.Fprintf(d.Out, "pathfinder_sweep: withdraw gifted %d %s: %v\n", n, r.ItemID, werr) //nolint:errcheck
			continue
		}
		if _, terr := d.Handoff.Transition(r.ID, handoff.StatusDone, handoff.StatusCollected, nil); terr != nil {
			fmt.Fprintf(d.Out, "pathfinder_sweep: mark %s collected: %v\n", r.ID, terr) //nolint:errcheck
		}
		fmt.Fprintf(d.Out, "pathfinder_sweep: loaded %d gifted %s from storage\n", n, r.ItemID) //nolint:errcheck
	}
}
