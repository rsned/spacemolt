package worker

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/navigation"
)

// huntHullRepaired reports whether st's hull is at or above huntRepairAtHull,
// the bar a hunter must meet before any leg that leaves the dock on purpose
// (a delivery, a roaming move, home hunting). frac is the hull fraction, 0
// when unknown; an unknown hull is not repaired.
func huntHullRepaired(st *game.State) (frac float64, ok bool) {
	if st == nil || st.Ship.MaxHull <= 0 {
		return 0, false
	}
	frac = st.Ship.Hull / st.Ship.MaxHull
	return frac, frac >= huntRepairAtHull
}

// huntCreatureWorkMinTier is the lowest hull tier allowed to take creature work.
const huntCreatureWorkMinTier = 2

// huntCreatureWorkCategory is the catalog category spelling for combat hulls.
// "Combat Support" is a distinct category and does not qualify.
const huntCreatureWorkCategory = "Combat"

// huntCreatureWorkHull reports whether classID is a T2+ combat hull. Any
// lookup error, an unknown class, or a class with no catalog category (legacy
// hulls such as the prospector and drillship) returns false, the safe failure.
func huntCreatureWorkHull(ctx context.Context, kb knowledge.Base, classID string) bool {
	if kb == nil || classID == "" {
		return false
	}
	sc, err := kb.GetShipClass(ctx, classID)
	if err != nil || sc == nil {
		return false
	}
	return sc.Tier >= huntCreatureWorkMinTier && sc.Category == huntCreatureWorkCategory
}

// huntRepairSearchJumps bounds the search for a repair station when the dock
// a hunter stands at cannot repair it.
const huntRepairSearchJumps = 12

// huntReadyToLeave is the repair gate in front of every creature-work leg out
// to a ground (Station Need, home hunting, wildlife kill mission). The dock
// repair (huntRepairAtDock) is best-effort and swallows failure, so the hull
// is re-read here: at or above huntRepairAtHull the leg may go. Below it the
// hunter never flies out to a ground. If the dock it stands at offers repair
// (the repair failed for another reason, e.g. credits) it holds there; if not,
// it goes to the nearest station known to repair and docks, and the next pass
// repairs there. With no repair station known it holds.
func huntReadyToLeave(ctx context.Context, deps HuntDeps, out io.Writer, who string) bool {
	if err := deps.Client.GetStatus(ctx); err != nil {
		fmt.Fprintf(out, "hunt[%s]: get_status before leaving for a ground: %v; held for next pass\n", who, err) //nolint:errcheck
		return false
	}
	st := deps.Client.GetState()
	frac, repaired := huntHullRepaired(st)
	if repaired {
		return true
	}
	if st == nil || deps.KB == nil {
		fmt.Fprintf(out, "hunt[%s]: hull %.0f%% below %.0f%%; held for repair\n", who, frac*100, huntRepairAtHull*100) //nolint:errcheck
		return false
	}
	if st.Doc && huntBaseRepairs(ctx, deps.KB, st.CurrentPOI) {
		fmt.Fprintf(out, "hunt[%s]: hull %.0f%% below %.0f%% and the repair at %s did not land; held for repair\n", //nolint:errcheck
			who, frac*100, huntRepairAtHull*100, st.CurrentPOI)
		return false
	}
	sys, poi := huntNearestRepairStation(ctx, deps.KB, st.System.ID, st.CurrentPOI)
	if poi == "" {
		fmt.Fprintf(out, "hunt[%s]: hull %.0f%% below %.0f%%, no repair at %s and no repair station known within %d jump(s); held for repair\n", //nolint:errcheck
			who, frac*100, huntRepairAtHull*100, st.CurrentPOI, huntRepairSearchJumps)
		return false
	}
	fmt.Fprintf(out, "hunt[%s]: hull %.0f%% below %.0f%% and %s has no repair; going to repair at %s/%s\n", //nolint:errcheck
		who, frac*100, huntRepairAtHull*100, st.CurrentPOI, sys, poi)
	publishActivity(deps.SetActivity, "Hunt · going to repair")
	if err := huntFlyTo(ctx, deps, out, false, sys, poi); err != nil {
		fmt.Fprintf(out, "hunt[%s]: repair leg to %s failed: %v; retry next pass\n", who, poi, err) //nolint:errcheck
		huntEscapeBattle(ctx, deps, out, who, "on the way to repair", err)
	}
	return false
}

// huntBaseRepairs reports whether the KB knows the base at poiID offers the
// repair service.
func huntBaseRepairs(ctx context.Context, kb knowledge.Base, poiID string) bool {
	if poiID == "" {
		return false
	}
	base, err := kb.GetBaseByPOI(ctx, poiID)
	return err == nil && base != nil && base.Services["repair"]
}

// huntNearestRepairStation is the nearest public, non-outpost station the KB
// knows offers repair, within huntRepairSearchJumps of from, other than
// exceptPOI. Empty when there is none.
func huntNearestRepairStation(ctx context.Context, kb knowledge.Base, from, exceptPOI string) (systemID, poiID string) {
	systems, err := kb.GetSystems(ctx)
	if err != nil {
		return "", ""
	}
	conns, err := kb.GetConnections(ctx)
	if err != nil {
		return "", ""
	}
	graph := huntPolicedGraph(conns, systems, from, false)
	ids := make([]string, 0, len(systems))
	for _, s := range systems {
		ids = append(ids, s.ID)
	}
	dist := navigation.BFSJumps(graph, from, ids)
	slices.SortFunc(ids, func(a, b string) int {
		if c := cmp.Compare(dist[a], dist[b]); c != 0 {
			return c
		}
		return strings.Compare(a, b)
	})
	for _, id := range ids {
		if dist[id] > huntRepairSearchJumps {
			break
		}
		pois, perr := kb.GetPOIs(ctx, id)
		if perr != nil {
			continue
		}
		for _, p := range pois {
			if p.Type != huntPOITypeStation || p.ID == exceptPOI {
				continue
			}
			base, berr := kb.GetBaseByPOI(ctx, p.ID)
			if berr == nil && base != nil && base.PublicAccess && !base.IsOutpost() && base.Services["repair"] {
				return id, p.ID
			}
		}
	}
	return "", ""
}
