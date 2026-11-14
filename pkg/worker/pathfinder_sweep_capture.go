package worker

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

func (d *WorkerDispatch) sweepCaptureFn() func(context.Context, string) {
	if d.sweepCapture != nil {
		return d.sweepCapture
	}
	return d.sweepLandingCapture
}

// sweepLandingCapture saves everything readable where a drift landed:
// get_system and get_poi into the KB, and get_nearby's wildlife and
// hostiles (repeaters). Player sightings flow through the client's
// observer. Runs on every landing whatever the sweep then does with the
// system -- charted, leviathan, repeaters, hazard or home -- because a
// frontier system may not be visited twice (operator 2026-11-14). Never
// fails the pass: a failed read is logged and the rest still run.
func (d *WorkerDispatch) sweepLandingCapture(ctx context.Context, sysID string) {
	if d.KB == nil {
		return
	}
	var failed []string
	if err := KBUpdateSystem(ctx, d.Client, d.KB, d.AgentID); err != nil {
		failed = append(failed, "system: "+err.Error())
	}
	if err := KBUpdatePOI(ctx, d.Client, d.KB, d.AgentID); err != nil {
		failed = append(failed, "poi: "+err.Error())
	}
	creatures, repeaters := 0, 0
	if err := d.Client.GetNearby(ctx); err != nil {
		failed = append(failed, "nearby: "+err.Error())
	} else {
		var nearby serverapi.GetNearbyResponse
		if raw := d.Client.GetRawJSON("nearby"); len(raw) > 0 {
			if err := json.Unmarshal(raw, &nearby); err != nil {
				failed = append(failed, "nearby parse: "+err.Error())
			}
		}
		poiID, poiType, tick := nearby.POIID, "", int64(0)
		if st := d.Client.GetState(); st != nil {
			tick = st.CurrentTick
			if poiID == "" {
				poiID = st.CurrentPOI
			}
			for _, p := range st.System.POIs {
				if p.ID == poiID {
					poiType = p.Type
				}
			}
		}
		if _, err := knowledge.CaptureHostilesNearby(ctx, d.KB, nearby.Repeaters, sysID, poiID, d.AgentID, tick); err != nil {
			failed = append(failed, "hostiles: "+err.Error())
		}
		if _, err := knowledge.CaptureWildlifeNearby(ctx, d.KB, nearby.Creatures, sysID, poiID, poiType, d.AgentID, tick); err != nil {
			failed = append(failed, "wildlife: "+err.Error())
		}
		creatures, repeaters = len(nearby.Creatures), len(nearby.Repeaters)
	}
	fmt.Fprintf(d.Out, "pathfinder_sweep: captured %s (system, poi, nearby: %d creature(s), %d repeater(s))", sysID, creatures, repeaters) //nolint:errcheck
	if len(failed) > 0 {
		fmt.Fprintf(d.Out, "; failed: %v", failed) //nolint:errcheck
	}
	fmt.Fprintln(d.Out) //nolint:errcheck
}
