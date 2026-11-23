package worker

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/navigation"
)

// huntRoamFile is the per-agent record of how far a hunter has had to look for
// creature work. It sits beside hunt-chain.json and follows its persistence
// idiom (atomic tmp+rename, a missing file is not an error).
const huntRoamFile = "hunt_roam.json"

// huntRoam is one hunter's search state across passes and restarts.
//
// DryPasses counts consecutive Station Need searches that found no qualifying
// ground; it drives the adaptive radius (huntNeedRadius) and resets to 0 when
// a pass finds a job. Visited is the systems whose boards this hunter has
// already found dry since its last job (huntNextMove skips them); it resets
// with DryPasses when a job is found.
//
// NeedDry is per held Station Need (keyed by active mission id): the dry
// count its own ground radius is computed from (huntNeedRadius). It starts at
// the board's DryPasses when the need is accepted and grows by one on every
// pass whose ground search for that need finds nothing, so a held need whose
// ground is depleted widens its search instead of retrying radius 6 until it
// expires. Entries for missions no longer active are pruned.
//
// NeedFails is per held Station Need (active mission id): how many of its own
// ground searches have found nothing since it was accepted. At
// huntNeedAbandonAfter the need is abandoned. NeedGaveUp is per board mission
// id: when (UTC, RFC 3339) this hunter abandoned it, so it is not taken again
// for huntNeedGaveUpFor.
type huntRoam struct {
	DryPasses  int               `json:"dry_passes"`
	Visited    []string          `json:"visited,omitempty"`
	NeedDry    map[string]int    `json:"need_dry,omitempty"`
	NeedFails  map[string]int    `json:"need_fails,omitempty"`
	NeedGaveUp map[string]string `json:"need_gave_up,omitempty"`
	UpdatedUTC string            `json:"updated_utc,omitempty"`
}

// huntRoamPath is <agentsDir>/<agentID>/hunt_roam.json, agentsDir defaulting
// to DefaultAgentsDir.
func huntRoamPath(agentsDir, agentID string) string {
	if agentsDir == "" {
		agentsDir = DefaultAgentsDir
	}
	return filepath.Join(agentsDir, agentID, huntRoamFile)
}

// loadHuntRoam reads the roam record. A missing file is a fresh hunter and
// returns the zero record with no error.
func loadHuntRoam(path string) (huntRoam, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return huntRoam{}, nil
		}
		return huntRoam{}, fmt.Errorf("hunt-roam read: %w", err)
	}
	var r huntRoam
	if err := json.Unmarshal(raw, &r); err != nil {
		return huntRoam{}, fmt.Errorf("hunt-roam decode: %w", err)
	}
	return r, nil
}

// saveHuntRoam writes the roam record atomically, creating the agent
// directory if needed.
func saveHuntRoam(path string, r huntRoam) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("hunt-roam marshal: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("hunt-roam mkdir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("hunt-roam write: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("hunt-roam replace: %w", err)
	}
	return nil
}

// huntLoadRoam reads this agent's roam record, logging and returning the zero
// record on any failure. With no AgentID there is no file.
func huntLoadRoam(deps HuntDeps, out io.Writer) huntRoam {
	if deps.AgentID == "" {
		return huntRoam{}
	}
	r, err := loadHuntRoam(huntRoamPath(deps.AgentsDir, deps.AgentID))
	if err != nil {
		fmt.Fprintf(out, "hunt: %v; starting the search radius over\n", err) //nolint:errcheck
		return huntRoam{}
	}
	return r
}

// huntSetDryPasses records the dry-pass count, writing only when it changed.
func huntSetDryPasses(deps HuntDeps, out io.Writer, n int) {
	if deps.AgentID == "" {
		return
	}
	path := huntRoamPath(deps.AgentsDir, deps.AgentID)
	r, err := loadHuntRoam(path)
	if err != nil {
		r = huntRoam{} // a corrupt record is replaced, not preserved
	}
	if err == nil && r.DryPasses == n {
		return
	}
	r.DryPasses = n
	r.UpdatedUTC = rfc(huntNow(deps))
	if err := saveHuntRoam(path, r); err != nil {
		fmt.Fprintf(out, "hunt: %v\n", err) //nolint:errcheck
	}
}

// Dry-board roaming: the operator's rule for a hunter whose board offered
// nothing. First try the other empire stations' boards nearby; when those are
// dry too, go home if Conjunction Junction is close enough and hunt the
// grounds around it, otherwise move to an empire capital not yet tried and
// start over there.
const (
	// huntHomeSystem / huntHomePOI are Conjunction Junction, the faction's
	// home station.
	huntHomeSystem = "cloverfield"
	huntHomePOI    = "862e07fa76571d0a76cab3cbaf4046e7"
	// huntHomeJumps is how close home must be for a dry hunter to go home
	// rather than to another capital.
	huntHomeJumps = 12
	// huntHomeGroundJumps bounds the home grounds: cloverfield itself and
	// empire-policed systems within this many jumps of it.
	huntHomeGroundJumps = 3

	// The kinds huntNextMove returns.
	huntMoveStation = "station"
	huntMoveHome    = "home"
	huntMoveCapital = "capital"
)

// huntNextMove chooses where a dry hunter goes next, in the operator's order:
//
//   - "station": the nearest of nearbyStations (systems with an empire
//     station) within huntNearbyJumps that is not current and not Visited;
//   - "home": huntHomeSystem, when it is within huntHomeJumps (including
//     standing in it);
//   - "capital": the nearest empire capital not Visited.
//
// kind is "" when none applies.
func huntNextMove(graph navigation.JumpGraph, current string, nearbyStations []string, roam huntRoam, systems []knowledge.System) (kind, target string) {
	if st, ok := huntNearestUnvisited(graph, current, nearbyStations, roam.Visited, huntNearbyJumps); ok {
		return huntMoveStation, st
	}
	if huntJumpsTo(graph, current, huntHomeSystem) <= huntHomeJumps {
		return huntMoveHome, huntHomeSystem
	}
	capitals := slices.DeleteFunc(capitalSystems(systems), func(c string) bool { return slices.Contains(roam.Visited, c) })
	if c, _, ok := nearestCapital(graph, current, capitals); ok {
		return huntMoveCapital, c
	}
	return "", ""
}

// huntJumpsTo is the hop count from src to dst over graph, navigation.RouteInf
// when unreachable.
func huntJumpsTo(graph navigation.JumpGraph, src, dst string) int {
	return navigation.BFSJumps(graph, src, []string{dst})[dst]
}

// huntNearestUnvisited returns the closest candidate system within maxJumps of
// current, skipping current and anything in seen; ties go to the lower id.
func huntNearestUnvisited(graph navigation.JumpGraph, current string, candidates, seen []string, maxJumps int) (string, bool) {
	dist := navigation.BFSJumps(graph, current, candidates)
	best, bestHops := "", 0
	for _, c := range candidates {
		d := dist[c]
		if c == current || slices.Contains(seen, c) || d <= 0 || d > maxJumps {
			continue
		}
		if best == "" || d < bestHops || (d == bestHops && c < best) {
			best, bestHops = c, d
		}
	}
	return best, best != ""
}

// huntNearbyEmpireStations lists the empire-policed systems within
// huntNearbyJumps of current that hold an empire (mission-board) station.
func huntNearbyEmpireStations(ctx context.Context, kb knowledge.Base, graph navigation.JumpGraph, current string, systems []knowledge.System, floor int) []string {
	policed := make([]string, 0, len(systems))
	for _, s := range systems {
		if s.ID != "" && s.ID != current && s.PoliceLevel >= floor {
			policed = append(policed, s.ID)
		}
	}
	dist := navigation.BFSJumps(graph, current, policed)
	var out []string
	for _, id := range policed {
		if d := dist[id]; d <= 0 || d > huntNearbyJumps {
			continue
		}
		if poi, err := missionBoardStationPOI(ctx, kb, id); err == nil && poi != "" {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out
}

// huntAddVisited appends id to list unless it is already there.
func huntAddVisited(list []string, id string) []string {
	if id == "" || slices.Contains(list, id) {
		return list
	}
	return append(list, id)
}

// huntSaveRoam writes r for this agent, logging a failure.
func huntSaveRoam(deps HuntDeps, out io.Writer, r huntRoam) {
	if deps.AgentID == "" {
		return
	}
	r.UpdatedUTC = rfc(huntNow(deps))
	if err := saveHuntRoam(huntRoamPath(deps.AgentsDir, deps.AgentID), r); err != nil {
		fmt.Fprintf(out, "hunt: %v\n", err) //nolint:errcheck
	}
}

// huntRoamFoundJob starts the search over after a pass found a job of any
// kind: the Station Need radius and the visited list both reset. It writes
// only when something changes.
func huntRoamFoundJob(deps HuntDeps, out io.Writer) {
	if deps.AgentID == "" {
		return
	}
	r, err := loadHuntRoam(huntRoamPath(deps.AgentsDir, deps.AgentID))
	if err == nil && r.DryPasses == 0 && len(r.Visited) == 0 {
		return
	}
	// Held needs keep their own radius.
	huntSaveRoam(deps, out, huntRoam{NeedDry: r.NeedDry, NeedFails: r.NeedFails, NeedGaveUp: r.NeedGaveUp})
}

// huntNeedDry is the dry count held need activeID searches at (0 when none
// is recorded: the starting radius).
func huntNeedDry(deps HuntDeps, out io.Writer, activeID string) int {
	return huntLoadRoam(deps, out).NeedDry[activeID]
}

// huntSetNeedDry records the dry count for held need activeID.
func huntSetNeedDry(deps HuntDeps, out io.Writer, activeID string, n int) {
	if deps.AgentID == "" || activeID == "" {
		return
	}
	r := huntLoadRoam(deps, out)
	if r.NeedDry == nil {
		r.NeedDry = map[string]int{}
	}
	if v, ok := r.NeedDry[activeID]; ok && v == n {
		return
	}
	r.NeedDry[activeID] = n
	huntSaveRoam(deps, out, r)
}

// huntPruneNeedDry drops per-need radius records for missions no longer in
// the active list.
func huntPruneNeedDry(deps HuntDeps, out io.Writer, active []string) {
	if deps.AgentID == "" {
		return
	}
	r := huntLoadRoam(deps, out)
	pruned := false
	for id := range r.NeedDry {
		if !slices.Contains(active, id) {
			delete(r.NeedDry, id)
			pruned = true
		}
	}
	for id := range r.NeedFails {
		if !slices.Contains(active, id) {
			delete(r.NeedFails, id)
			pruned = true
		}
	}
	if pruned {
		huntSaveRoam(deps, out, r)
	}
}

// huntNeedSearchFailed widens a need's own ground radius after a pass whose
// ground search for it came up empty, and abandons the need once
// huntNeedAbandonAfter of its searches have failed: a held need blocks every
// other job, and a hunter holding one with no ground does nothing until it
// expires. Observed live 2026-11-22, pirate-5 and pirate-10 both parked this
// way. Only Station Needs are abandoned; kill missions can be chain missions
// and are still held (see huntFindQuarry).
func huntNeedSearchFailed(ctx context.Context, deps HuntDeps, out io.Writer, who string, job huntJob) {
	if job.need == nil {
		return
	}
	dry := huntNeedDry(deps, out, job.activeID) + 1
	huntSetNeedDry(deps, out, job.activeID, dry)
	fmt.Fprintf(out, "hunt[%s]: no ground for %s this pass; its search radius is now %d jump(s) (was %d)\n", //nolint:errcheck
		who, job.boardID, huntNeedRadius(dry), job.need.Radius)

	if deps.AgentID == "" || job.activeID == "" {
		return
	}
	r := huntLoadRoam(deps, out)
	if r.NeedFails == nil {
		r.NeedFails = map[string]int{}
	}
	r.NeedFails[job.activeID]++
	fails := r.NeedFails[job.activeID]
	if fails < huntNeedAbandonAfter {
		huntSaveRoam(deps, out, r)
		return
	}
	if err := deps.Client.AbandonMission(ctx, job.activeID); err != nil {
		fmt.Fprintf(out, "hunt[%s]: abandon %s after %d dry search(es) failed: %v; held for next pass\n", who, job.boardID, fails, err) //nolint:errcheck
		huntSaveRoam(deps, out, r)
		return
	}
	fmt.Fprintf(out, "hunt[%s]: ABANDONED need %s (%s) after %d dry search(es); not taking it again for %s\n", //nolint:errcheck
		who, job.boardID, job.title, fails, huntNeedGaveUpFor)
	delete(r.NeedFails, job.activeID)
	delete(r.NeedDry, job.activeID)
	if r.NeedGaveUp == nil {
		r.NeedGaveUp = map[string]string{}
	}
	r.NeedGaveUp[job.boardID] = huntNow(deps).UTC().Format(time.RFC3339)
	huntSaveRoam(deps, out, r)
}

// huntNeedSearchFound clears a held need's failed-search count when a ground
// holds its quarry: only consecutive dry searches lead to abandoning it.
func huntNeedSearchFound(deps HuntDeps, out io.Writer, job huntJob) {
	if job.need == nil || deps.AgentID == "" || job.activeID == "" {
		return
	}
	r := huntLoadRoam(deps, out)
	if r.NeedFails[job.activeID] == 0 {
		return
	}
	delete(r.NeedFails, job.activeID)
	huntSaveRoam(deps, out, r)
}

// huntNeedGaveUp reports whether this hunter abandoned board need boardID
// within huntNeedGaveUpFor of now.
func huntNeedGaveUp(r huntRoam, boardID string, now time.Time) bool {
	at, err := time.Parse(time.RFC3339, r.NeedGaveUp[boardID])
	return err == nil && now.Sub(at) < huntNeedGaveUpFor
}

// huntRoamAfterDry makes this dry pass's one move. It marks the current system
// visited, asks huntNextMove where to go, and flies there: to a nearby empire
// station or a capital (docking), or home to Conjunction Junction. homeHunt is
// true when the hunter is already docked at home and should hunt the home
// grounds this pass instead of moving.
//
// Every route goes through Autopilot, so the stronghold gate applies to all of
// them; a move that fails is logged and retried on a later dry pass. Below
// huntRepairAtHull nothing happens at all ("held for repair").
func huntRoamAfterDry(ctx context.Context, deps HuntDeps, out io.Writer, who string) (homeHunt bool) {
	// Neither a move nor home hunting starts on a damaged hull: the dock
	// repair can fail and is swallowed, so re-read and gate here. The hunter
	// stays docked and the next pass repairs first.
	if err := deps.Client.GetStatus(ctx); err != nil {
		fmt.Fprintf(out, "hunt[%s]: dry board; get_status: %v; staying put\n", who, err) //nolint:errcheck
		return false
	}
	st := deps.Client.GetState()
	if frac, repaired := huntHullRepaired(st); !repaired {
		fmt.Fprintf(out, "hunt[%s]: dry board but hull %.0f%% below %.0f%%; roaming held for repair\n", //nolint:errcheck
			who, frac*100, huntRepairAtHull*100)
		return false
	}
	if st.System.ID == "" || deps.KB == nil {
		return false
	}
	current := st.System.ID
	systems, err := deps.KB.GetSystems(ctx)
	if err != nil {
		fmt.Fprintf(out, "hunt[%s]: dry board; systems unavailable: %v; staying put\n", who, err) //nolint:errcheck
		return false
	}
	conns, err := deps.KB.GetConnections(ctx)
	if err != nil {
		fmt.Fprintf(out, "hunt[%s]: dry board; connections unavailable: %v; staying put\n", who, err) //nolint:errcheck
		return false
	}
	// Distances on the routes a hunter may fly: policed, plus the final hop
	// into cloverfield for the home check (never through it).
	graph := huntPolicedGraph(conns, systems, current, true, huntPoliceFloor(deps))

	roam := huntLoadRoam(deps, out)
	roam.Visited = huntAddVisited(roam.Visited, current)
	stations := huntNearbyEmpireStations(ctx, deps.KB, graph, current, systems, huntPoliceFloor(deps))
	kind, target := huntNextMove(graph, current, stations, roam, systems)

	switch kind {
	case huntMoveStation, huntMoveCapital:
		roam.Visited = huntAddVisited(roam.Visited, target)
		huntSaveRoam(deps, out, roam)
		poi, _ := missionBoardStationPOI(ctx, deps.KB, target)
		fmt.Fprintf(out, "hunt[%s]: board at %s dry; trying the %s board at %s/%s (%d jump(s))\n", //nolint:errcheck
			who, current, kind, target, poi, huntJumpsTo(graph, current, target))
		if err := huntFlyTo(ctx, deps, out, false, target, poi); err != nil {
			fmt.Fprintf(out, "hunt[%s]: roam to %s failed: %v; retry next dry pass\n", who, target, err) //nolint:errcheck
			huntEscapeBattle(ctx, deps, out, who, "roaming", err)
		}
		return false
	case huntMoveHome:
		huntSaveRoam(deps, out, roam)
		if st.CurrentPOI == huntHomePOI && st.Doc {
			fmt.Fprintf(out, "hunt[%s]: boards nearby are dry; hunting the home grounds\n", who) //nolint:errcheck
			return true
		}
		fmt.Fprintf(out, "hunt[%s]: boards nearby are dry; going home to Conjunction Junction (%d jump(s))\n", //nolint:errcheck
			who, huntJumpsTo(graph, current, huntHomeSystem))
		if err := huntFlyTo(ctx, deps, out, true, huntHomeSystem, huntHomePOI); err != nil {
			fmt.Fprintf(out, "hunt[%s]: going home failed: %v; retry next dry pass\n", who, err) //nolint:errcheck
			huntEscapeBattle(ctx, deps, out, who, "going home", err)
		}
		return false
	default:
		// Every station and capital in reach has been tried since the last
		// job: start the list over rather than stand still for good.
		fmt.Fprintf(out, "hunt[%s]: no untried station or capital left from %s; starting the visited list over\n", who, current) //nolint:errcheck
		roam.Visited = nil
		huntSaveRoam(deps, out, roam)
		return false
	}
}

// huntFlyTo undocks if needed, autopilots to systemID (and poiID when known)
// on a policed route (home: the cloverfield waiver, huntAutopilot) and docks
// there. With no poiID it stops in the system; the next pass's station
// recovery finds a dock.
func huntFlyTo(ctx context.Context, deps HuntDeps, out io.Writer, home bool, systemID, poiID string) error {
	if st := deps.Client.GetState(); st != nil && st.Doc {
		if err := deps.Client.Undock(ctx); err != nil {
			return fmt.Errorf("undock: %w", err)
		}
	}
	if err := huntFlyPoliced(ctx, deps, out, home, systemID, poiID); err != nil {
		return fmt.Errorf("transit: %w", err)
	}
	if poiID == "" {
		return nil
	}
	if err := deps.Client.Dock(ctx); err != nil {
		return fmt.Errorf("dock at %s: %w", poiID, err)
	}
	return nil
}

// huntHomeLawlessWaiver is the ONE exception to the hunt fleet's Lawless gate:
// cloverfield, home, is Lawless (police_level 0), and the operator asked for
// hunting around our own station. It is consulted only by the home-grounds
// search (huntHomeGrounds), so it applies only in home mode and never to any
// other Lawless system.
func huntHomeLawlessWaiver(systemID string) bool { return systemID == huntHomeSystem }

// huntHomeGroundAllowed reports whether s may be a home hunting ground on
// policing: empire-policed, or home itself under the waiver.
func huntHomeGroundAllowed(s knowledge.System) bool {
	return s.PoliceLevel >= huntPolicedMinLevel || huntHomeLawlessWaiver(s.ID)
}

// huntHomeGrounds lists the home hunting-ground systems, nearest home first:
// cloverfield plus empire-policed systems within huntHomeGroundJumps of it.
func huntHomeGrounds(graph navigation.JumpGraph, systems []knowledge.System) []string {
	ids := make([]string, 0, len(systems))
	for _, s := range systems {
		if s.ID != "" && huntHomeGroundAllowed(s) {
			ids = append(ids, s.ID)
		}
	}
	dist := navigation.BFSJumps(graph, huntHomeSystem, ids)
	ids = slices.DeleteFunc(ids, func(id string) bool { return dist[id] > huntHomeGroundJumps })
	slices.SortFunc(ids, func(a, b string) int {
		if c := cmp.Compare(dist[a], dist[b]); c != 0 {
			return c
		}
		return strings.Compare(a, b)
	})
	return ids
}

// huntTravelToHomeGround flies to the best wildlife POI on the home grounds
// (nearest system first, huntLocalWildlifePOI's ranking within it), skipping
// exclude and known high-severity hazards.
func huntTravelToHomeGround(ctx context.Context, deps HuntDeps, out io.Writer, exclude []string) (string, error) {
	if deps.KB == nil {
		return "", fmt.Errorf("no knowledge base configured")
	}
	systems, err := deps.KB.GetSystems(ctx)
	if err != nil {
		return "", fmt.Errorf("systems: %w", err)
	}
	conns, err := deps.KB.GetConnections(ctx)
	if err != nil {
		return "", fmt.Errorf("connections: %w", err)
	}
	// Home grounds are counted out from cloverfield over policed jumps.
	graph := huntPolicedGraph(conns, systems, huntHomeSystem, true, huntPolicedMinLevel)
	for _, sys := range huntHomeGrounds(graph, systems) {
		skip := slices.Clone(exclude)
		for {
			poi, why := huntLocalWildlifePOI(ctx, deps.KB, sys, skip)
			if poi == "" {
				break
			}
			if huntPOIHazardous(ctx, deps.KB, sys, poi) {
				skip = append(skip, poi)
				continue
			}
			if st := deps.Client.GetState(); st != nil && st.CurrentPOI == poi && !st.Doc {
				return poi, nil
			}
			fmt.Fprintf(out, "hunt: heading to home ground %s/%s (%s)\n", sys, poi, why) //nolint:errcheck
			if st := deps.Client.GetState(); st != nil && st.Doc {
				if err := deps.Client.Undock(ctx); err != nil {
					return "", fmt.Errorf("undock: %w", err)
				}
			}
			if err := huntFlyPoliced(ctx, deps, out, true, sys, poi); err != nil {
				return "", fmt.Errorf("transit to %s: %w", poi, err)
			}
			return poi, nil
		}
	}
	return "", huntNoGround("no wildlife POI known on the home grounds")
}
