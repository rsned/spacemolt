package worker

import (
	"context"
	"fmt"
	"io"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

// HaulDangerGateLevel is the minimum danger_zones.danger_level at which haul
// routing refuses a system outright. Unlike the stronghold guard this gate is
// unconditional — no standing or unlock makes a wildlife kill zone safe.
// Born from goldcrest: four wildlife hull losses through 2026-08-19 and three
// more 2026-08-22..24, while danger_zones sat at zero rows and no route
// decision ever read it.
const HaulDangerGateLevel = 5

// dangerRefsFor returns the system references (dual-keyed by id AND name,
// mirroring strongholdRefsFor — opportunity rows carry system NAMES while
// FindBestPrices rows carry ids) of every system at or above
// HaulDangerGateLevel. Best-effort: a nil KB or a read error yields an empty
// set so hauling is never blocked by the gate's own plumbing; the error case
// logs a warning because it silently widens the fleet's exposure.
func dangerRefsFor(ctx context.Context, kb knowledge.Base, out io.Writer) map[string]bool {
	if kb == nil {
		return nil
	}
	if out == nil {
		out = io.Discard
	}
	zones, err := kb.GetDangerZones(ctx, HaulDangerGateLevel)
	if err != nil {
		fmt.Fprintf(out, "haul: danger-zone read failed (gate disabled this pass): %v\n", err) //nolint:errcheck
		return nil
	}
	if len(zones) == 0 {
		return nil
	}
	refs := make(map[string]bool, len(zones)*2)
	for _, z := range zones {
		if z.SystemID != "" {
			refs[z.SystemID] = true
		}
		if z.SystemName != "" {
			refs[z.SystemName] = true
		}
	}
	fmt.Fprintf(out, "haul: avoiding %d danger-zone system(s) (level >= %d)\n", len(zones), HaulDangerGateLevel) //nolint:errcheck
	return refs
}

// unionRefs merges two reference sets without mutating either. Empty/nil
// sides short-circuit to the other set (shared, not copied — callers treat
// ref sets as read-only).
func unionRefs(a, b map[string]bool) map[string]bool {
	if len(b) == 0 {
		return a
	}
	if len(a) == 0 {
		return b
	}
	u := make(map[string]bool, len(a)+len(b))
	for k := range a {
		u[k] = true
	}
	for k := range b {
		u[k] = true
	}
	return u
}
