package main

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// unscannedHalt is the typed error exploreSystem returns when auto-explore
// reaches a POI hosting a species no scan has ever characterised. It is a
// clean stop, not a failure: the tour parks the ship at the POI so the
// operator can decide whether to spend the tick on a scan — the danger
// bracket is exactly what is unknown before scanning, so automating the scan
// would walk into fights nobody chose (see reportUnscannedSpecies).
type unscannedHalt struct {
	Species []string
	POIID   string
	POIName string
}

func (h *unscannedHalt) Error() string {
	where := h.POIName
	if where == "" {
		where = h.POIID
	}
	return fmt.Sprintf("unscanned wildlife at %s: %s", where, strings.Join(h.Species, ", "))
}

// detectUnscanned reports which species in a get_nearby creature list have
// never been scanned (seen in the KB, but danger_scanned_utc unstamped),
// sorted for stable output. Best-effort: a nil or non-wildlife KB, or a KB
// error, yields nothing — detection must never break an exploration pass.
func detectUnscanned(ctx context.Context, kb knowledge.Base, creatures []serverapi.NearbyCreature) []string {
	if kb == nil || len(creatures) == 0 {
		return nil
	}
	ids := make([]string, 0, len(creatures))
	seen := make(map[string]bool, len(creatures))
	for _, c := range creatures {
		if c.Species == "" || seen[c.Species] {
			continue
		}
		seen[c.Species] = true
		ids = append(ids, c.Species)
	}
	unscanned, err := knowledge.UnscannedSpecies(ctx, kb, ids)
	if err != nil || len(unscanned) == 0 {
		return nil
	}
	slices.Sort(unscanned)
	return unscanned
}
