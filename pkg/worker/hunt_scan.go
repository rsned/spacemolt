package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// huntScanUnknown scans one creature of each species the KB has never scanned
// and files what the scan revealed. It returns how many scans were made.
//
// A targeted scan of fauna always succeeds and reveals species danger, hull and
// lore, none of which get_nearby carries. One scan per species is enough because
// the data is per species. Failures are logged and skipped: scanning is
// opportunistic and must never stall a hunt.
func huntScanUnknown(ctx context.Context, deps HuntDeps, out io.Writer, creatures []serverapi.NearbyCreature) int {
	if out == nil {
		out = io.Discard
	}
	first := make(map[string]string, len(creatures))
	species := make([]string, 0, len(creatures))
	for _, c := range creatures {
		if c.Species == "" || c.CreatureID == "" {
			continue
		}
		if _, dup := first[c.Species]; dup {
			continue
		}
		first[c.Species] = c.CreatureID
		species = append(species, c.Species)
	}
	unscanned, err := knowledge.UnscannedSpecies(ctx, deps.KB, species)
	if err != nil || len(unscanned) == 0 {
		return 0
	}
	slices.Sort(unscanned)

	scans := 0
	for _, sp := range unscanned {
		id := first[sp]
		if id == "" {
			continue
		}
		if err := deps.Client.ScanTarget(ctx, id); err != nil {
			_, _ = fmt.Fprintf(out, "  scan %s (%s) failed: %v\n", id, sp, err)
			continue
		}
		scans++

		var resp serverapi.ScanResponse
		raw := deps.Client.GetRawJSON("scan")
		if len(raw) == 0 || json.Unmarshal(unwrapActionResult(raw), &resp) != nil || !resp.Success {
			continue
		}
		s := knowledge.ParseCreatureScan(resp.Username, resp.RevealedInfo, resp.Hull, resp.Description)
		if err := knowledge.CaptureWildlifeScan(ctx, deps.KB, sp, s, huntNow(deps)); err != nil {
			_, _ = fmt.Fprintf(out, "  scan of %s not recorded: %v\n", sp, err)
		}
	}
	return scans
}
