package worker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/navigation"
)

// Station Need deliveries: a station asks for creature parts, the hunter takes
// the mission, hunts the named creature at a policed ground until the hold has
// the quantity, flies the parts to the mission's target base and completes it.

const (
	// huntNearbyJumps is the starting search radius for a Station Need's
	// hunting ground. It is a start, not a limit: many empire systems may be
	// hunted out, so each dry pass widens it (huntNeedRadius).
	huntNearbyJumps = 6
	// huntNeedDryPatience is how many dry Station Need searches the hunter
	// tolerates at one radius before doubling it. A hunt pass is long (it
	// reads a board and often flies), so this is far shorter than the haul
	// fleet's HaulDesertPatience, which counts ~11s passes.
	huntNeedDryPatience = 2
	// huntNeedAbandonAfter is how many of a held Station Need's own ground
	// searches may come up empty before the hunter abandons it (operator
	// decision 2026-11-23). huntNeedGaveUpFor is how long the hunter then
	// leaves that board entry alone.
	huntNeedAbandonAfter = 3
	huntNeedGaveUpFor    = 24 * time.Hour
	// huntDepletedLooks and huntDepletedFor define a hunted-out ground: its
	// last huntDepletedLooks looks all found none of the species, all within
	// huntDepletedFor. Empty looks older than that expire, so a ground that
	// has regenerated comes back into play.
	huntDepletedLooks = 2
	huntDepletedFor   = 6 * time.Hour
	// huntEmptyLookFor: one look that found none of the species keeps every
	// hunter off the ground this long, so a second hunter does not fly out to
	// make the second empty look itself (operator decision 2026-11-25; an
	// hour is a first guess, to be tuned). Regrowth follows the belt: a
	// mined-out belt has nothing for wildlife to grow from.
	huntEmptyLookFor = time.Hour
	// huntLookMatchWindow pairs a coverage row with the sightings the same
	// get_nearby wrote, when no game tick is available to pair them exactly.
	huntLookMatchWindow = time.Minute
	// huntNeedSightingLimit bounds the sightings read per species.
	huntNeedSightingLimit = 500
)

// errHuntNoNeedGround: no policed ground for the need within its radius.
var errHuntNoNeedGround = errors.New("no policed ground")

// huntNeedKB is the optional wildlife half of the KB that Station Need work
// needs. These methods live on *knowledge.SQLiteKB, not on knowledge.Base; a
// KB without them has no data to hunt a need by, so the need is refused
// rather than guessed at.
type huntNeedKB interface {
	SpeciesByDisplayName(ctx context.Context) (map[string]string, error)
	SpeciesDroppingItem(ctx context.Context, itemID string) ([]string, error)
	GetWildlifeSpecies(ctx context.Context) ([]knowledge.WildlifeSpecies, error)
	GetWildlifeSightings(ctx context.Context, species string, limit int) ([]knowledge.WildlifeSighting, error)
	GetWildlifeCoverage(ctx context.Context, systemID, poiID string, limit int) ([]knowledge.WildlifeCoverage, error)
}

var _ huntNeedKB = (*knowledge.SQLiteKB)(nil)

// huntNeedRadius is the ground search radius after dry consecutive dry
// passes. It reuses desertRadius's doubling and clamp, scaled so each
// huntNeedDryPatience dry passes count as one haul patience window:
// 6 -> 12 -> 24 -> HaulMaxHaulJumps.
func huntNeedRadius(dry int) int {
	return desertRadius(dry/huntNeedDryPatience*HaulDesertPatience, huntNearbyJumps)
}

// huntPickNeed chooses the need paying the most per jump:
// Reward / (jumpsTo(ground) + jumpsTo(target) + 1). Only needs whose ground is
// known and within radius jumps, and whose target is reachable, qualify.
// jumpsTo returns a negative number for an unreachable system.
func huntPickNeed(needs []huntNeed, jumpsTo func(systemID string) int, radius int) (huntNeed, bool) {
	var (
		best      huntNeed
		bestScore float64
		found     bool
	)
	for _, n := range needs {
		if n.GroundSystem == "" || n.SystemID == "" {
			continue
		}
		jg := jumpsTo(n.GroundSystem)
		if jg < 0 || jg > radius {
			continue
		}
		// The delivery target is a leg too: bounded by the same radius.
		jt := jumpsTo(n.SystemID)
		if jt < 0 || jt > radius {
			continue
		}
		score := float64(n.Reward) / float64(jg+jt+1)
		if !found || score > bestScore {
			best, bestScore, found = n, score, true
		}
	}
	return best, found
}

// huntGroundClass ranks a candidate POI by what our looks say about it.
type huntGroundClass int

const (
	// huntGroundSighted: one of the species was seen here within
	// huntDepletedFor. The best bet.
	huntGroundSighted huntGroundClass = iota
	// huntGroundUnlooked: never looked at (no coverage rows). Worth one look.
	huntGroundUnlooked
	// huntGroundStale: looked at, but neither recently sighted nor depleted.
	huntGroundStale
	// huntGroundDepleted: hunted out for now; skipped.
	huntGroundDepleted
)

// huntParseUTC reads an RFC3339 timestamp; ok is false for an empty or bad one.
func huntParseUTC(s string) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339, s)
	return t, err == nil
}

// huntLookFoundSpecies reports whether one look at a POI saw any of the
// species. The coverage row carries no species, so it is paired with the
// sightings the same get_nearby wrote: same game tick when both carry one,
// else observed within huntLookMatchWindow.
func huntLookFoundSpecies(look knowledge.WildlifeCoverage, sightings []knowledge.WildlifeSighting) bool {
	if look.CreaturesSeen == 0 {
		return false
	}
	lt, lok := huntParseUTC(look.ObservedUTC)
	for _, s := range sightings {
		if look.GameTick != 0 && s.GameTick != 0 {
			if s.GameTick == look.GameTick {
				return true
			}
			continue
		}
		st, sok := huntParseUTC(s.ObservedUTC)
		if lok && sok {
			d := st.Sub(lt)
			if d < 0 {
				d = -d
			}
			if d <= huntLookMatchWindow {
				return true
			}
		}
	}
	return false
}

// huntClassifyGround classes one POI from its newest looks (newest first) and
// the sightings of the wanted species at it.
func huntClassifyGround(looks []knowledge.WildlifeCoverage, sightings []knowledge.WildlifeSighting, now time.Time) huntGroundClass {
	if len(looks) > 0 {
		if t, ok := huntParseUTC(looks[0].ObservedUTC); ok && now.Sub(t) <= huntEmptyLookFor && !huntLookFoundSpecies(looks[0], sightings) {
			return huntGroundDepleted
		}
	}
	if len(looks) >= huntDepletedLooks {
		depleted := true
		for _, l := range looks[:huntDepletedLooks] {
			t, ok := huntParseUTC(l.ObservedUTC)
			if !ok || now.Sub(t) > huntDepletedFor || huntLookFoundSpecies(l, sightings) {
				depleted = false
				break
			}
		}
		if depleted {
			return huntGroundDepleted
		}
	}
	for _, s := range sightings {
		if t, ok := huntParseUTC(s.ObservedUTC); ok && now.Sub(t) <= huntDepletedFor {
			return huntGroundSighted
		}
	}
	if len(looks) == 0 {
		return huntGroundUnlooked
	}
	return huntGroundStale
}

var huntGroundClassNames = [...]string{"recently sighted", "never looked at", "sighted before", "hunted out"}

// huntNeedGround picks the hunting ground for a need: a POI where one of
// need.Species has been sighted, in an empire-policed system (never relaxed
// for need work), not a known high-severity hazard, not hunted out, and
// within radius jumps. Grounds beyond the radius are dropped BEFORE ranking, so
// a far one can never hide a near one. Best class first (huntClassifyGround),
// then fewest jumps. hops returns a negative number for an unreachable system.
func huntNeedGround(ctx context.Context, kb knowledge.Base, nk huntNeedKB, need huntNeed, hops func(systemID string) int, radius int, exclude []string, now time.Time, floor int) (systemID, poiID, why string) {
	type cand struct {
		system, poi string
		sightings   []knowledge.WildlifeSighting
	}
	byPOI := map[string]*cand{}
	var order []string
	for _, sp := range need.Species {
		rows, err := nk.GetWildlifeSightings(ctx, sp, huntNeedSightingLimit)
		if err != nil {
			continue
		}
		for _, s := range rows {
			if s.POIID == "" || s.SystemID == "" || s.Source == knowledge.WildlifeSourceSurvey || slices.Contains(exclude, s.POIID) {
				continue
			}
			c, ok := byPOI[s.POIID]
			if !ok {
				c = &cand{system: s.SystemID, poi: s.POIID}
				byPOI[s.POIID] = c
				order = append(order, s.POIID)
			}
			c.sightings = append(c.sightings, s)
		}
	}
	slices.Sort(order)

	var (
		bestSys, bestPOI string
		bestClass        huntGroundClass
		bestHops         int
		found            bool
	)
	policed := map[string]bool{}
	for _, id := range order {
		c := byPOI[id]
		ok, seen := policed[c.system]
		if !seen {
			ok = huntPolicedSystem(ctx, kb, c.system, floor)
			policed[c.system] = ok
		}
		if !ok || huntPOIHazardous(ctx, kb, c.system, c.poi) {
			continue
		}
		h := hops(c.system)
		if h < 0 || h > radius {
			continue
		}
		looks, err := nk.GetWildlifeCoverage(ctx, c.system, c.poi, huntDepletedLooks)
		if err != nil {
			looks = nil
		}
		class := huntClassifyGround(looks, c.sightings, now)
		if class == huntGroundDepleted {
			continue
		}
		if found && (class > bestClass || (class == bestClass && h >= bestHops)) {
			continue
		}
		bestSys, bestPOI, bestClass, bestHops, found = c.system, c.poi, class, h, true
	}
	if !found {
		return "", "", ""
	}
	return bestSys, bestPOI, fmt.Sprintf("policed ground, %s, %d jump(s)", huntGroundClassNames[bestClass], bestHops)
}

// huntSightedRadius is how far a kill mission flies to a ground where its
// quarry has been reported.
const huntSightedRadius = 12

// huntSightedGround is the best ground for a kill mission's named species
// from the fleet's shared wildlife reports: every hunter records what it sees
// at each POI (huntCaptureWildlife), and this reads those reports back with
// huntNeedGround's rules (recent sightings first, then fewest jumps; hunted
// out, hazardous and excluded grounds dropped) within huntSightedRadius of
// from. Empty when species is empty or nothing is reported in reach.
func huntSightedGround(ctx context.Context, kb knowledge.Base, from, species string, exclude []string, now time.Time, floor int) (systemID, poiID, why string) {
	nk, ok := kb.(huntNeedKB)
	if !ok || species == "" {
		return "", "", ""
	}
	jumps := huntJumpsFrom(ctx, kb, from, floor)
	if jumps == nil {
		return "", "", ""
	}
	sys, poi, class := huntNeedGround(ctx, kb, nk, huntNeed{Species: []string{species}}, jumps, huntSightedRadius, exclude, now, floor)
	if poi == "" {
		return "", "", ""
	}
	return sys, poi, fmt.Sprintf("%s reported here: %s", species, class)
}

// huntPolicedSystem reports whether the KB knows systemID as empire-policed
// (huntPolicedMinLevel). An unknown system is not.
func huntPolicedSystem(ctx context.Context, kb knowledge.Base, systemID string, floor int) bool {
	sys, err := kb.GetSystem(ctx, systemID)
	return err == nil && sys != nil && sys.PoliceLevel >= floor
}

// huntPOIHazardous reports whether the KB knows poiID as a hazard severe
// enough to refuse (hazardAvoidSeverity).
func huntPOIHazardous(ctx context.Context, kb knowledge.Base, systemID, poiID string) bool {
	pois, err := kb.GetPOIs(ctx, systemID)
	if err != nil {
		return false
	}
	i := slices.IndexFunc(pois, func(p knowledge.POI) bool { return p.ID == poiID })
	return i >= 0 && pois[i].HazardSeverity >= hazardAvoidSeverity
}

// huntJumpsFrom returns a hop counter from `from` over the policed jump graph
// (huntPolicedGraph: the routes a hunter may actually fly), or nil when the
// KB map cannot be read. A system reachable only through Lawless space is
// unreachable (-1).
func huntJumpsFrom(ctx context.Context, kb knowledge.Base, from string, floor int) func(string) int {
	systems, err := kb.GetSystems(ctx)
	if err != nil {
		return nil
	}
	conns, err := kb.GetConnections(ctx)
	if err != nil {
		return nil
	}
	graph := huntPolicedGraph(conns, systems, from, false, floor)
	ids := make([]string, 0, len(systems))
	for _, s := range systems {
		ids = append(ids, s.ID)
	}
	dist := navigation.BFSJumps(graph, from, ids)
	return func(to string) int {
		d, ok := dist[to]
		if !ok || d >= navigation.RouteInf {
			return -1
		}
		return d
	}
}

// huntNeedLookups resolves the KB data a need is parsed and admitted against.
type huntNeedLookups struct {
	byName  map[string]string
	dropsOf func(itemID string) []string
	roleOf  func(species string) string
}

func huntLoadNeedLookups(ctx context.Context, nk huntNeedKB) (huntNeedLookups, error) {
	byName, err := nk.SpeciesByDisplayName(ctx)
	if err != nil {
		return huntNeedLookups{}, fmt.Errorf("species names: %w", err)
	}
	guide, err := nk.GetWildlifeSpecies(ctx)
	if err != nil {
		return huntNeedLookups{}, fmt.Errorf("field guide: %w", err)
	}
	roles := make(map[string]string, len(guide))
	for _, s := range guide {
		roles[s.Species] = s.Role
	}
	return huntNeedLookups{
		byName: byName,
		dropsOf: func(itemID string) []string {
			sp, err := nk.SpeciesDroppingItem(ctx, itemID)
			if err != nil {
				return nil
			}
			return sp
		},
		roleOf: func(species string) string { return roles[species] },
	}, nil
}

// huntNeedHeld is how many of the need's item the cached hold carries.
func huntNeedHeld(st *game.State, need huntNeed) int {
	return int(cargoQty(st, need.ItemID))
}

// huntReadHeld re-reads the hold with get_cargo (loot replies are not trusted
// to update the cached cargo list) and returns the need's count. A failed read
// falls back to the cached state.
func huntReadHeld(ctx context.Context, deps HuntDeps, out io.Writer, need huntNeed) int {
	if err := deps.Client.GetCargo(ctx); err != nil {
		fmt.Fprintf(out, "hunt: get_cargo: %v; using the cached hold\n", err) //nolint:errcheck
	}
	return huntNeedHeld(deps.Client.GetState(), need)
}

// huntNeedJob takes the best nearby Station Need off the board. It runs only
// for a T2+ combat hull, and counts a pass that finds nothing as dry so the
// next search looks further out. dry is true only for that genuinely dry
// search (no Station Need with a ground in radius), never for a failure.
func huntNeedJob(ctx context.Context, deps HuntDeps, out io.Writer, who string, board serverapi.GetMissionsResponse) (job huntJob, ok, dry bool) {
	st := deps.Client.GetState()
	if st == nil {
		return huntJob{}, false, false
	}
	if !huntCreatureWorkHull(ctx, deps.KB, st.Ship.ClassID) {
		fmt.Fprintf(out, "hunt[%s]: hull %q does not take creature work; no Station Needs\n", who, st.Ship.ClassID) //nolint:errcheck
		return huntJob{}, false, false
	}
	nk, ok := deps.KB.(huntNeedKB)
	if !ok {
		fmt.Fprintf(out, "hunt[%s]: knowledge base has no wildlife data; no Station Needs\n", who) //nolint:errcheck
		return huntJob{}, false, false
	}
	lk, err := huntLoadNeedLookups(ctx, nk)
	if err != nil {
		fmt.Fprintf(out, "hunt[%s]: %v; no Station Needs this pass\n", who, err) //nolint:errcheck
		return huntJob{}, false, false
	}
	jumps := huntJumpsFrom(ctx, deps.KB, st.System.ID, huntPoliceFloor(deps))
	if jumps == nil {
		fmt.Fprintf(out, "hunt[%s]: galaxy graph unavailable; no Station Needs this pass\n", who) //nolint:errcheck
		return huntJob{}, false, false
	}
	roam := huntLoadRoam(deps, out)
	radius := huntNeedRadius(roam.DryPasses)
	free := int(st.Ship.CargoCapacity - st.Ship.CargoUsed)

	var needs []huntNeed
	entries := map[string]serverapi.MissionBoardEntry{}
	for _, e := range board.Missions {
		n, parsed := huntParseNeed(e, lk.byName, lk.dropsOf)
		if !parsed {
			if strings.HasPrefix(e.Title, "Station Need") {
				fmt.Fprintf(out, "hunt: skip need %s (%s): no known, fully resolved source creature\n", e.MissionID, e.Title) //nolint:errcheck
			}
			continue
		}
		if huntNeedGaveUp(roam, e.MissionID, huntNow(deps)) {
			fmt.Fprintf(out, "hunt: skip need %s (%s): abandoned after dry searches within the last %s\n", e.MissionID, e.Title, huntNeedGaveUpFor) //nolint:errcheck
			continue
		}
		if admit, reason := huntAdmissibleNeed(n, e.Difficulty, lk.roleOf); !admit {
			fmt.Fprintf(out, "hunt: skip need %s (%s): %s\n", e.MissionID, e.Title, reason) //nolint:errcheck
			continue
		}
		if n.TargetBaseID == "" || n.SystemID == "" {
			fmt.Fprintf(out, "hunt: skip need %s (%s): no delivery base named\n", e.MissionID, e.Title) //nolint:errcheck
			continue
		}
		if !huntPolicedSystem(ctx, deps.KB, n.SystemID, huntPoliceFloor(deps)) {
			fmt.Fprintf(out, "hunt: skip need %s (%s): delivery system %s is not empire-policed\n", e.MissionID, e.Title, n.SystemID) //nolint:errcheck
			continue
		}
		if room := n.Quantity - huntNeedHeld(st, n); room > free {
			fmt.Fprintf(out, "hunt: skip need %s (%s): needs %d free hold, have %d\n", e.MissionID, e.Title, room, free) //nolint:errcheck
			continue
		}
		n.MissionID = e.MissionID
		n.Radius = radius
		// A ground another hunter is working is not a ground for a new need.
		claimed := huntClaimedPOIs(deps, n.Species)
		n.GroundSystem, n.GroundPOI, _ = huntNeedGround(ctx, deps.KB, nk, n, jumps, radius, claimed, huntNow(deps), huntPoliceFloor(deps))
		if n.GroundPOI == "" {
			fmt.Fprintf(out, "hunt: skip need %s (%s): no policed ground within %d jump(s) where %s is sighted (%d claimed by other hunters)\n", e.MissionID, e.Title, radius, strings.Join(n.Species, "/"), len(claimed)) //nolint:errcheck
			continue
		}
		// One trip: out to the ground and on to the delivery base, both
		// counted from here as huntPickNeed counts them.
		if jg, jt := jumps(n.GroundSystem), jumps(n.SystemID); jg >= 0 && jt >= 0 {
			if cost := huntNeedFuelCost(deps, jg+jt); !huntNeedPays(n.Reward, cost) {
				fmt.Fprintf(out, "hunt: skip need %s (%s): %d cr does not clear %.1fx the ~%d cr of fuel for %d jump(s)\n", //nolint:errcheck
					e.MissionID, e.Title, n.Reward, huntNeedRewardMargin, cost, jg+jt)
				continue
			}
		}
		needs = append(needs, n)
		entries[e.MissionID] = e
	}

	chosen, ok := huntPickNeed(needs, jumps, radius)
	if !ok {
		huntSetDryPasses(deps, out, roam.DryPasses+1)
		fmt.Fprintf(out, "hunt[%s]: no Station Need with a ground within %d jump(s) (dry pass %d)\n", who, radius, roam.DryPasses+1) //nolint:errcheck
		return huntJob{}, false, true
	}
	e := entries[chosen.MissionID]
	activeID, _, accepted := huntAcceptMission(ctx, deps, out, e)
	if !accepted {
		return huntJob{}, false, false
	}
	// The need keeps the radius it was taken at; failed searches widen it.
	huntSetNeedDry(deps, out, activeID, roam.DryPasses)
	huntClaimGround(deps, out, chosen.GroundSystem, chosen.GroundPOI, chosen.Species)
	fmt.Fprintf(out, "hunt[%s]: accepted need %s (%s): %d %s from %s at %s/%s for %s, %d cr\n", //nolint:errcheck
		who, e.MissionID, e.Title, chosen.Quantity, chosen.ItemID, strings.Join(chosen.Species, "/"),
		chosen.GroundSystem, chosen.GroundPOI, chosen.TargetBaseID, chosen.Reward)
	return huntJob{
		boardID: e.MissionID, activeID: activeID, title: e.Title,
		target: chosen.Quantity, need: &chosen,
	}, true, false
}

// huntResumeNeed rebuilds a held Station Need from the active mission and
// re-runs admission. ok is false when m is not a creature need at all, and
// also when it no longer admits: that mission is logged and left held, never
// abandoned.
func huntResumeNeed(ctx context.Context, deps HuntDeps, out io.Writer, who string, m serverapi.ActiveMission) (huntJob, bool) {
	e := serverapi.MissionBoardEntry{
		MissionID: m.MissionID, TemplateID: m.TemplateID, Type: m.Type,
		Title: m.Title, Description: m.Description, Difficulty: m.Difficulty, Rewards: m.Rewards,
	}
	for _, o := range m.Objectives {
		e.Objectives = append(e.Objectives, serverapi.MissionObjective{
			Type: o.Type, Description: o.Description, ItemID: o.ItemID, Quantity: o.Required,
			TargetBaseID: o.TargetBase, SystemID: o.SystemID, Completed: o.Completed,
		})
	}
	if e.Type != "delivery" {
		return huntJob{}, false
	}
	nk, ok := deps.KB.(huntNeedKB)
	if !ok {
		return huntJob{}, false
	}
	lk, err := huntLoadNeedLookups(ctx, nk)
	if err != nil {
		fmt.Fprintf(out, "hunt[%s]: %v; held need %s not resumed this pass\n", who, err, m.MissionID) //nolint:errcheck
		return huntJob{}, false
	}
	n, ok := huntParseNeed(e, lk.byName, lk.dropsOf)
	if !ok {
		return huntJob{}, false // not a creature need
	}
	if admit, reason := huntAdmissibleNeed(n, m.Difficulty, lk.roleOf); !admit {
		fmt.Fprintf(out, "hunt[%s]: holding need %s (%s), not resuming: %s\n", who, m.MissionID, m.Title, reason) //nolint:errcheck
		return huntJob{}, false
	}
	if st := deps.Client.GetState(); st == nil || !huntCreatureWorkHull(ctx, deps.KB, st.Ship.ClassID) {
		fmt.Fprintf(out, "hunt[%s]: holding need %s (%s), not resuming: hull does not take creature work\n", who, m.MissionID, m.Title) //nolint:errcheck
		return huntJob{}, false
	}
	if n.TargetBaseID == "" || n.SystemID == "" {
		fmt.Fprintf(out, "hunt[%s]: holding need %s (%s), not resuming: no delivery base named\n", who, m.MissionID, m.Title) //nolint:errcheck
		return huntJob{}, false
	}
	// The accept-time guard, re-checked: policing can change, and a held
	// need must not be delivered into Lawless space.
	if !huntPolicedSystem(ctx, deps.KB, n.SystemID, huntPoliceFloor(deps)) {
		fmt.Fprintf(out, "hunt[%s]: holding need %s (%s), not resuming: delivery system %s is not empire-policed\n", who, m.MissionID, m.Title, n.SystemID) //nolint:errcheck
		return huntJob{}, false
	}
	boardID := m.TemplateID
	if boardID == "" {
		boardID = m.MissionID
	}
	n.MissionID = boardID
	// A resumed need searches at its OWN radius, widened by every pass whose
	// ground search for it failed (huntNeedSearchFailed).
	n.Radius = huntNeedRadius(huntNeedDry(deps, out, m.MissionID))
	fmt.Fprintf(out, "hunt[%s]: resuming need %s (%s): %d %s for %s\n", who, boardID, m.Title, n.Quantity, n.ItemID, n.TargetBaseID) //nolint:errcheck
	return huntJob{
		boardID: boardID, activeID: m.MissionID, title: m.Title,
		target: n.Quantity, need: &n, resumed: true,
	}, true
}

// huntTravelToNeedGround flies to the need's best ground not in exclude.
func huntTravelToNeedGround(ctx context.Context, deps HuntDeps, out io.Writer, need huntNeed, exclude []string) (string, error) {
	nk, ok := deps.KB.(huntNeedKB)
	if !ok {
		return "", fmt.Errorf("knowledge base has no wildlife data")
	}
	state := deps.Client.GetState()
	if state == nil {
		return "", fmt.Errorf("no cached state")
	}
	jumps := huntJumpsFrom(ctx, deps.KB, state.System.ID, huntPoliceFloor(deps))
	if jumps == nil {
		return "", fmt.Errorf("galaxy graph unavailable")
	}
	radius := need.Radius
	if radius <= 0 {
		radius = huntNearbyJumps
	}
	// Grounds other hunters are working come last: a held need shares one
	// only when it has no other, so a claim never strands a need.
	claimed := huntClaimedPOIs(deps, need.Species)
	sys, poi, why := huntNeedGround(ctx, deps.KB, nk, need, jumps, radius, slices.Concat(exclude, claimed), huntNow(deps), huntPoliceFloor(deps))
	if poi == "" && len(claimed) > 0 {
		if sys, poi, why = huntNeedGround(ctx, deps.KB, nk, need, jumps, radius, exclude, huntNow(deps), huntPoliceFloor(deps)); poi != "" {
			fmt.Fprintf(out, "hunt: every other ground for %s is out; sharing %s with the hunter that claimed it\n", need.ItemID, poi) //nolint:errcheck
		}
	}
	if poi == "" {
		return "", fmt.Errorf("%w within %d jump(s) where %s is sighted", errHuntNoNeedGround, radius, strings.Join(need.Species, "/"))
	}
	huntClaimGround(deps, out, sys, poi, need.Species)
	if state.CurrentPOI == poi && !state.Doc {
		return poi, nil
	}
	if state.Doc {
		if err := deps.Client.Undock(ctx); err != nil {
			return "", fmt.Errorf("undock: %w", err)
		}
	}
	fmt.Fprintf(out, "hunt: heading to %s/%s (%s) for %s\n", sys, poi, why, need.ItemID) //nolint:errcheck
	if err := huntFlyPoliced(ctx, deps, out, false, sys, poi); err != nil {
		return "", fmt.Errorf("transit to %s: %w", poi, err)
	}
	return poi, nil
}

// huntNeedQuarry is the quarry filter for a need: the standard admission with
// the role check forced on (huntAdmissibleQuarry, wildlife-only), then only
// the need's own species. Nothing else at the ground is quarry, whatever it
// drops.
func huntNeedQuarry(creatures []serverapi.NearbyCreature, species []string, out io.Writer) []serverapi.NearbyCreature {
	admitted := huntAdmissibleQuarry(creatures, true, "", out)
	keep := admitted[:0]
	for _, c := range admitted {
		if slices.Contains(species, c.Species) {
			keep = append(keep, c)
			continue
		}
		fmt.Fprintf(out, "hunt: skip %s: species %q is not a source for this need (%s)\n", c.CreatureID, c.Species, strings.Join(species, "/")) //nolint:errcheck
	}
	return keep
}

// huntDeliverNeed flies the parts to the mission's target base, docks and
// completes. Any failure holds the mission for the next pass, which resumes
// it and, holding the quantity already, comes straight back here.
//
// The delivery leg is flown only on a repaired hull (huntRepairAtHull): parts
// gathered at a damaged hull wait aboard, the next pass docks and repairs
// first (huntRepairAtDock), resumes the need, and delivers from there.
//
// delivered reports whether the mission was completed; the behaviour is the
// same either way, the caller only labels its sortie with it.
func huntDeliverNeed(ctx context.Context, deps HuntDeps, out io.Writer, who string, job huntJob) (delivered bool) {
	need := job.need
	if err := deps.Client.GetStatus(ctx); err != nil {
		fmt.Fprintf(out, "hunt: get_status before delivering %s: %v; held for next pass\n", job.boardID, err) //nolint:errcheck
		return false
	}
	st := deps.Client.GetState()
	if frac, repaired := huntHullRepaired(st); !repaired {
		fmt.Fprintf(out, "hunt[%s]: %s parts aboard but hull %.0f%% below %.0f%%; delivery waits for repair, held for next pass\n", //nolint:errcheck
			who, job.boardID, frac*100, huntRepairAtHull*100)
		return false
	}
	// Station ids are dual-named (POI id != base id at every capital): the
	// mission names the base, the ship's position is the station POI. Resolve
	// it as mission.go does, so a hunter docked at the target does not
	// undock and re-dock, and the leg travels to a POI id.
	targetPOI := need.TargetBaseID
	if deps.KB != nil {
		if base, err := deps.KB.GetBase(ctx, need.TargetBaseID); err == nil && base != nil && base.POIID != "" {
			targetPOI = base.POIID
		}
	}
	atTarget := st.Doc && (st.CurrentPOI == targetPOI || st.CurrentPOI == need.TargetBaseID)
	if !atTarget {
		if st.Doc {
			if err := deps.Client.Undock(ctx); err != nil {
				fmt.Fprintf(out, "hunt: undock to deliver %s: %v; held for next pass\n", job.boardID, err) //nolint:errcheck
				return false
			}
		}
		fmt.Fprintf(out, "hunt[%s]: delivering %d %s to %s/%s\n", who, need.Quantity, need.ItemID, need.SystemID, targetPOI) //nolint:errcheck
		if err := huntFlyPoliced(ctx, deps, out, false, need.SystemID, targetPOI); err != nil {
			fmt.Fprintf(out, "hunt: transit to %s: %v; %s held for next pass\n", targetPOI, err, job.boardID) //nolint:errcheck
			huntEscapeBattle(ctx, deps, out, who, "on the delivery leg", err)
			return false
		}
		if err := dockIdempotent(ctx, deps.Client); err != nil {
			fmt.Fprintf(out, "hunt: dock at %s: %v; %s held for next pass\n", targetPOI, err, job.boardID) //nolint:errcheck
			huntEscapeBattle(ctx, deps, out, who, "docking to deliver", err)
			return false
		}
	}
	if err := deps.Client.CompleteMission(ctx, job.activeID); err != nil {
		fmt.Fprintf(out, "hunt: complete %s failed: %v; held for next pass\n", job.activeID, err) //nolint:errcheck
		return false
	}
	fmt.Fprintf(out, "hunt[%s]: delivered %s (%s) at %s\n", who, job.boardID, huntJobName(job), need.TargetBaseID) //nolint:errcheck
	return true
}

// huntNeedActivityLabel is the status-page line for a need job.
func huntNeedActivityLabel(job huntJob, held int) string {
	return fmt.Sprintf("Hunt %s · %s %d/%d", huntJobName(job), job.need.ItemID, held, job.need.Quantity)
}
