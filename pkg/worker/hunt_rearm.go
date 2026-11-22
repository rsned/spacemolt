package worker

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/navigation"
	"github.com/rsned/spacemolt/pkg/reload"
)

// huntRearmCandidate is a station a hunter could rearm at.
type huntRearmCandidate struct {
	system, poi string
	jumps       int
	why         string
}

// huntRearmDestination picks where a hunter that needs ammoItems goes.
// Candidates: the home station, and every policed station deps.Sellers reports
// selling one of ammoItems. Nearest by policed jumps wins; home wins a tie.
// With no candidate reachable it returns home. ok=false only when home is unknown too.
func huntRearmDestination(ctx context.Context, deps HuntDeps, fromSystem string, ammoItems []string) (systemID, poiID, why string, ok bool) {
	systemID, poiID, why, _, ok = huntRearmRoute(ctx, deps, fromSystem, ammoItems)
	return systemID, poiID, why, ok
}

// huntRearmRoute is huntRearmDestination plus reachable: whether a policed
// route to the destination is known. It is false only for the home depot
// returned with no route known.
func huntRearmRoute(ctx context.Context, deps HuntDeps, fromSystem string, ammoItems []string) (systemID, poiID, why string, reachable, ok bool) {
	if deps.KB == nil || fromSystem == "" {
		return "", "", "", false, false
	}
	systems, err := deps.KB.GetSystems(ctx)
	if err != nil {
		return "", "", "", false, false
	}
	conns, err := deps.KB.GetConnections(ctx)
	if err != nil {
		return "", "", "", false, false
	}
	homeSys := huntHomeStationSystem(ctx, deps.KB, deps.HomeStation, systems)
	homePOI := ""
	if homeSys != "" {
		homePOI = huntStationPOI(ctx, deps.KB, deps.HomeStation)
	}
	graph := huntPolicedGraph(conns, systems, fromSystem, homeSys == huntHomeSystem)

	var cands []huntRearmCandidate
	if deps.Sellers != nil {
		cands = huntRearmSellerStations(ctx, deps, systems, ammoItems)
	}
	targets := make([]string, 0, len(cands)+1)
	if homeSys != "" {
		targets = append(targets, homeSys)
	}
	for _, c := range cands {
		targets = append(targets, c.system)
	}
	dist := navigation.BFSJumps(graph, fromSystem, targets)
	reach := func(sys string) int {
		if d, found := dist[sys]; found && d < navigation.RouteInf {
			return d
		}
		return -1
	}

	homeJumps := -1
	if homeSys != "" {
		homeJumps = reach(homeSys)
	}
	best := huntRearmCandidate{jumps: -1}
	for _, c := range cands {
		c.jumps = reach(c.system)
		if c.jumps < 0 {
			continue
		}
		if best.jumps < 0 || c.jumps < best.jumps {
			best = c
		}
	}
	switch {
	case best.jumps >= 0 && (homeJumps < 0 || best.jumps < homeJumps):
		if homeJumps < 0 {
			return best.system, best.poi, fmt.Sprintf("%s, %d jump(s); home depot unreachable", best.why, best.jumps), true, true
		}
		return best.system, best.poi, fmt.Sprintf("%s, %d jump(s), nearer than the home depot at %d", best.why, best.jumps, homeJumps), true, true
	case homeSys != "":
		if homeJumps < 0 {
			return homeSys, homePOI, "home depot (no policed route known)", false, true
		}
		return homeSys, homePOI, fmt.Sprintf("home depot, %d jump(s)", homeJumps), true, true
	}
	return "", "", "", false, false
}

// huntRearmSellerStations lists the policed, dockable stations the seller
// lookup reports for any of ammoItems, one per station, in the lookup's order.
// A seller's StationID may be a base id, so it is mapped to the POI id that
// huntFlyTo docks at; a station that maps to no known POI in its system is
// skipped, as is any seller in an unpoliced system.
func huntRearmSellerStations(ctx context.Context, deps HuntDeps, systems []knowledge.System, ammoItems []string) []huntRearmCandidate {
	policed := make(map[string]bool, len(systems))
	for _, s := range systems {
		policed[s.ID] = s.PoliceLevel >= huntPolicedMinLevel
	}
	var out []huntRearmCandidate
	seen := map[string]bool{}
	for _, item := range ammoItems {
		sellers, err := deps.Sellers(ctx, item, 1)
		if err != nil {
			continue
		}
		for _, s := range sellers {
			poi := huntStationPOI(ctx, deps.KB, s.StationID)
			if seen[poi] {
				continue
			}
			sysID := s.SystemID
			if sysID == "" {
				sysID = huntHomeStationSystem(ctx, deps.KB, poi, systems)
			}
			if !policed[sysID] || !huntPOIInSystem(ctx, deps.KB, sysID, poi) {
				continue
			}
			if base, berr := deps.KB.GetBaseByPOI(ctx, poi); berr == nil && base != nil && (!base.PublicAccess || base.IsOutpost()) {
				continue
			}
			seen[poi] = true
			name := s.StationName
			if name == "" {
				name = poi
			}
			out = append(out, huntRearmCandidate{system: sysID, poi: poi, why: fmt.Sprintf("%s sells %s", name, item)})
		}
	}
	return out
}

// huntPOIInSystem reports whether the KB lists poiID among systemID's POIs.
func huntPOIInSystem(ctx context.Context, kb knowledge.Base, systemID, poiID string) bool {
	pois, err := kb.GetPOIs(ctx, systemID)
	if err != nil {
		return false
	}
	return slices.ContainsFunc(pois, func(p knowledge.POI) bool { return p.ID == poiID })
}

// huntRearmAmmoItems is every ammo item the loadout needs: each ammo-fed
// weapon's loaded ammo, or, when none is loaded, every catalog ammo item of
// its ammo type.
func huntRearmAmmoItems(ctx context.Context, kb knowledge.Base, lo reload.Loadout) []string {
	var items []string
	add := func(id string) {
		if id != "" && !slices.Contains(items, id) {
			items = append(items, id)
		}
	}
	var catalog []knowledge.CatalogItem
	catalogRead := false
	for _, e := range lo.Plan {
		if e.LoadedAmmoID != "" {
			add(e.LoadedAmmoID)
			continue
		}
		if kb == nil {
			continue
		}
		if !catalogRead {
			catalog, _ = kb.GetItems(ctx)
			catalogRead = true
		}
		var ofType []string
		for _, it := range catalog {
			if it.Ammo != nil && it.Ammo.AmmoType == e.AmmoType {
				ofType = append(ofType, it.ID)
			}
		}
		slices.Sort(ofType)
		for _, id := range ofType {
			add(id)
		}
	}
	return items
}

// huntGoRearm flies there and docks. The next pass provisions.
func huntGoRearm(ctx context.Context, deps HuntDeps, out io.Writer, who string, lo reload.Loadout) error {
	st := deps.Client.GetState()
	from := ""
	if st != nil {
		from = st.System.ID
		if from == "" {
			from = st.CurrentSystem
		}
	}
	sys, poi, why, ok := huntRearmDestination(ctx, deps, from, huntRearmAmmoItems(ctx, deps.KB, lo))
	if !ok {
		return fmt.Errorf("no rearm destination known from %q (no home depot, no policed seller)", from)
	}
	if huntAtStation(ctx, deps.KB, st, poi) {
		fmt.Fprintf(out, "hunt[%s]: already docked at the rearm station %s\n", who, poi) //nolint:errcheck
		return nil
	}
	fmt.Fprintf(out, "hunt[%s]: out of ammo; going to rearm at %s/%s (%s)\n", who, sys, poi, strings.TrimSpace(why)) //nolint:errcheck
	return huntRearmLeg(ctx, deps, out, sys, poi)
}

// huntRearmLeg is the flight of huntGoRearm once the destination is chosen:
// policed routes only, then dock.
func huntRearmLeg(ctx context.Context, deps HuntDeps, out io.Writer, sys, poi string) error {
	publishActivity(deps.SetActivity, "Hunt · going to rearm")
	if err := huntFlyTo(ctx, deps, out, sys == huntHomeSystem, sys, poi); err != nil {
		return fmt.Errorf("rearm leg to %s: %w", poi, err)
	}
	return nil
}

// huntRearmElsewhere runs when dock-side provisioning left the ship unarmed.
// When huntRearmDestination names a station other than the one the ship is
// docked at (station ids compared as huntAtStation does, base id or POI id),
// it flies there and returns true: the caller drops its in-memory sortie and
// ends the pass. false (no destination known, no policed route to it known,
// or already docked at it) means the hunter is blocked unarmed where it
// stands; nothing was flown, not even an undock, so this never loops.
func huntRearmElsewhere(ctx context.Context, deps HuntDeps, out io.Writer, who string) bool {
	st := deps.Client.GetState()
	if st == nil {
		return false
	}
	from := cmp.Or(st.System.ID, st.CurrentSystem)
	sys, poi, why, reachable, ok := huntRearmRoute(ctx, deps, from, huntRearmAmmoItems(ctx, deps.KB, huntReadLoadout(ctx, deps)))
	if !ok || !reachable || huntAtStation(ctx, deps.KB, st, poi) {
		return false
	}
	fmt.Fprintf(out, "hunt[%s]: unarmed at %s; going to rearm at %s/%s (%s)\n", who, st.CurrentPOI, sys, poi, strings.TrimSpace(why)) //nolint:errcheck
	if err := huntRearmLeg(ctx, deps, out, sys, poi); err != nil {
		fmt.Fprintf(out, "hunt[%s]: going to rearm: %v; the next pass recovers to a station\n", who, err) //nolint:errcheck
		huntEscapeBattle(ctx, deps, out, who, "going to rearm", err)
	}
	return true
}
