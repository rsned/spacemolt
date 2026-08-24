package worker

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/rsned/spacemolt/pkg/galaxy"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/navigation"
)

// Exploration missions (spec: docs/superpowers/specs/
// 2026-07-17-exploration-missions-design.md): pure-navigation board entries
// whose objectives are all visit_system / dock_at_base. No cargo, no item
// budget — the whole cost is fuel and time, so selection gates on
// net-per-jump and the tour is order-optimized (probe-verified 2026-07-17:
// objectives complete in any order; complete_mission at the return dock pays
// instantly; abandons cost zero credits).

const (
	missionTypeExploration = "exploration"
	missionObjectiveVisit  = "visit_system"
	missionObjectiveDock   = "dock_at_base"

	// missionMinNetPerJump: exploration pays for pure travel, so an absolute
	// net floor isn't enough — the live board's diff-2 "Local Sector Survey"
	// clears missionMinNet at 78 cr/jump, a 32-jump trap. The floor sits above
	// the haul fleet's leftover tier (~220 cr/jump) so exploration only wins
	// when it genuinely pays (the Prospectus probe: 1,667 cr/jump).
	missionMinNetPerJump = 300.0
	// missionExploreMaxJumps caps a tour's total planned jumps; exploration
	// destinations may individually exceed the delivery MaxJumps radius.
	missionExploreMaxJumps = 20
	// missionExploreMaxPermLegs: exact-permutation tour search bound (7! =
	// 5040 orders over cached BFS distances); larger leg sets fall back to
	// nearest-neighbor.
	missionExploreMaxPermLegs = 7
	// missionExploreMaxStages bounds the staged-objective loop: completing an
	// objective can APPEND new objectives (operator-observed story missions),
	// so the executor re-reads actives and flies again — but never forever.
	missionExploreMaxStages = 5
)

// missionLeg is one navigation objective: a system to visit, plus the base to
// dock at for dock_at_base legs (empty BaseID = visit only).
type missionLeg struct {
	SystemID string
	BaseID   string
}

// exploreShape extracts the nav legs of a board entry, in wire order. ok=false
// unless the mission is exploration-typed, ungated (no required modules), and
// EVERY objective is a well-formed visit_system/dock_at_base — the type alone
// cannot be trusted (live board: exploration-typed missions carrying
// deliver_item or traverse_wormhole legs).
func exploreShape(e serverapi.MissionBoardEntry) ([]missionLeg, bool) {
	if e.Type != missionTypeExploration || len(e.RequiredModules) > 0 || len(e.Objectives) == 0 {
		return nil, false
	}
	legs := make([]missionLeg, 0, len(e.Objectives))
	for _, o := range e.Objectives {
		switch o.Type {
		case missionObjectiveVisit:
			if o.SystemID == "" {
				return nil, false
			}
			legs = append(legs, missionLeg{SystemID: o.SystemID})
		case missionObjectiveDock:
			if o.SystemID == "" || o.TargetBaseID == "" {
				return nil, false
			}
			legs = append(legs, missionLeg{SystemID: o.SystemID, BaseID: o.TargetBaseID})
		default:
			return nil, false
		}
	}
	return legs, true
}

// exploreNavLegsFromActive is exploreShape's active-mission analogue: the
// still-incomplete nav legs of an active exploration mission, in wire order.
// ok=false when any objective (complete or not) is outside the nav vocabulary.
func exploreNavLegsFromActive(m serverapi.ActiveMission) (remaining []missionLeg, ok bool) {
	for _, o := range m.Objectives {
		switch o.Type {
		case missionObjectiveVisit:
			if !o.Completed {
				remaining = append(remaining, missionLeg{SystemID: o.SystemID})
			}
		case missionObjectiveDock:
			if !o.Completed {
				remaining = append(remaining, missionLeg{SystemID: o.SystemID, BaseID: o.TargetBase})
			}
		default:
			return nil, false
		}
	}
	return remaining, true
}

// exploreTour orders legs for shortest total travel from current. The trailing
// wire leg is pinned last when it is a dock leg (the return-to-giver
// convention: completing there claims the reward in the same dock); the rest
// are ordered by exact permutation search up to missionExploreMaxPermLegs,
// nearest-neighbor beyond. Returns the ordered legs and total jumps;
// jumps >= navigation.RouteInf means some leg is unreachable.
func exploreTour(current string, legs []missionLeg, dist func(a, b string) int) ([]missionLeg, int) {
	if len(legs) == 0 {
		return nil, 0
	}
	var pinned *missionLeg
	free := legs
	if n := len(legs); n > 1 && legs[n-1].BaseID != "" {
		pinned = &legs[n-1]
		free = legs[:n-1]
	}
	chainCost := func(order []missionLeg) int {
		total, at := 0, current
		for _, l := range order {
			d := dist(at, l.SystemID)
			if d >= navigation.RouteInf {
				return navigation.RouteInf
			}
			total += d
			at = l.SystemID
		}
		if pinned != nil {
			d := dist(at, pinned.SystemID)
			if d >= navigation.RouteInf {
				return navigation.RouteInf
			}
			total += d
		}
		return total
	}

	var best []missionLeg
	bestCost := navigation.RouteInf
	if len(free) <= missionExploreMaxPermLegs {
		order := make([]missionLeg, len(free))
		copy(order, free)
		var permute func(k int)
		permute = func(k int) {
			if k == len(order) {
				if c := chainCost(order); c < bestCost {
					bestCost = c
					best = append([]missionLeg(nil), order...)
				}
				return
			}
			for i := k; i < len(order); i++ {
				order[k], order[i] = order[i], order[k]
				permute(k + 1)
				order[k], order[i] = order[i], order[k]
			}
		}
		permute(0)
	} else {
		// Nearest-neighbor for oversized leg sets.
		rest := append([]missionLeg(nil), free...)
		at := current
		for len(rest) > 0 {
			bi, bd := -1, navigation.RouteInf
			for i, l := range rest {
				if d := dist(at, l.SystemID); d < bd {
					bi, bd = i, d
				}
			}
			if bi < 0 {
				return legs, navigation.RouteInf
			}
			best = append(best, rest[bi])
			at = rest[bi].SystemID
			rest = append(rest[:bi], rest[bi+1:]...)
		}
		bestCost = chainCost(best)
	}
	if best == nil {
		best = free
	}
	if pinned != nil {
		best = append(best, *pinned)
	}
	return best, bestCost
}

// buildExploreCandidate gates and prices one exploration board entry. dist is
// a pairwise jump-distance function over {current} ∪ leg systems. A non-empty
// reason means the entry was filtered (and why, for the worker log).
func buildExploreCandidate(e serverapi.MissionBoardEntry, current string, dist func(a, b string) int, fuelCostFor func(jumps int) float64, jumpTicks int) (missionCandidate, string) {
	legs, ok := exploreShape(e)
	if !ok {
		return missionCandidate{}, "not a pure-navigation exploration mission"
	}
	if len(e.Warnings) > 0 {
		return missionCandidate{}, "has warnings"
	}
	ordered, jumps := exploreTour(current, legs, dist)
	if jumps >= navigation.RouteInf {
		return missionCandidate{}, "a tour leg is unreachable"
	}
	if jumps > missionExploreMaxJumps {
		return missionCandidate{}, fmt.Sprintf("tour is %d jumps (cap %d)", jumps, missionExploreMaxJumps)
	}
	if e.ExpiresInTicks > 0 && e.ExpiresInTicks < missionMinExpiryTicks+jumps*jumpTicks {
		return missionCandidate{}, fmt.Sprintf("expires in %d ticks (< %d needed for %d jumps at %d ticks/jump)",
			e.ExpiresInTicks, missionMinExpiryTicks+jumps*jumpTicks, jumps, jumpTicks)
	}
	reward := 0.0
	if e.Rewards != nil {
		reward = float64(e.Rewards.Credits)
	}
	fuelCost := fuelCostFor(jumps)
	net := reward - fuelCost
	if net < missionMinNet {
		return missionCandidate{}, fmt.Sprintf("net %.0f below floor %.0f", net, missionMinNet)
	}
	if net/float64(max(jumps, 1)) < missionMinNetPerJump {
		return missionCandidate{}, fmt.Sprintf("net %.0f over %d jumps is below the %.0f/jump floor", net, jumps, missionMinNetPerJump)
	}
	last := ordered[len(ordered)-1]
	return missionCandidate{
		Entry: e, DestBaseID: last.BaseID, DestSystem: last.SystemID,
		Reward: reward, FuelCost: fuelCost, Net: net, Jumps: jumps, Legs: ordered,
	}, ""
}

// explorePOIFor resolves a dock leg's base id to its POI id (they differ:
// grand_exchange_station's POI is grand_exchange). Falls back to the base id
// itself — identical for every other live station — on any KB gap.
func explorePOIFor(ctx context.Context, deps MissionDeps, baseID string) string {
	if base, err := deps.KB.GetBase(ctx, baseID); err == nil && base != nil && base.POIID != "" {
		return base.POIID
	}
	return baseID
}

// missionRunExplore flies candidate c's tour: nav each leg (dock legs dock and
// fire the market capture), then re-reads the active mission — completing an
// objective can APPEND new ones (staged story missions), so it loops
// plan→fly→re-read until nothing nav-shaped remains incomplete, a stage adds
// an objective outside the nav vocabulary (abandon: staged_non_nav), or the
// stage bound trips (abandon: stage_limit). Completion happens at the final
// dock leg when the tour ends on one, else back at the accept station.
func missionRunExplore(ctx context.Context, deps MissionDeps, out io.Writer, c missionCandidate, acceptSystem, acceptPOI, fromBase, acceptedAt string, acceptedTick int64, strongholds map[string]bool) {
	legs := c.Legs
	at := acceptSystem
	dockedAt := "" // base id of the most recent completed dock leg
	for stage := 0; ; stage++ {
		for _, leg := range legs {
			if strongholds[leg.SystemID] {
				fmt.Fprintf(out, "missions: explore leg %s is a pirate stronghold; abandoning %s\n", leg.SystemID, c.Entry.MissionID) //nolint:errcheck
				missionAbandon(ctx, deps, out, c, fromBase, acceptedAt, acceptedTick, "stronghold_destination")
				return
			}
			poi := ""
			if leg.BaseID != "" {
				poi = explorePOIFor(ctx, deps, leg.BaseID)
			}
			if nerr := deps.nav(ctx, leg.SystemID, poi); nerr != nil {
				fmt.Fprintf(out, "missions: explore transit to %s failed: %v; held for next pass\n", leg.SystemID, nerr) //nolint:errcheck
				return // resume picks the mission up next pass
			}
			at = leg.SystemID
			if leg.BaseID != "" {
				// Same reason as the return-dock below: a leg whose base is
				// where the ship already sits answers "Already docked", and
				// aborting there leaves dockedAt unset so the tour can never
				// advance past its own starting point.
				if derr := dockIdempotent(ctx, deps.Client); derr != nil {
					fmt.Fprintf(out, "missions: explore dock at %s failed: %v; held for next pass\n", leg.BaseID, derr) //nolint:errcheck
					return
				}
				dockedAt = leg.BaseID
				if deps.capture != nil {
					if cerr := deps.capture(ctx); cerr != nil {
						fmt.Fprintf(out, "missions: market capture at %s: %v\n", leg.BaseID, cerr) //nolint:errcheck
					}
				}
			}
		}

		// Re-read: did every objective land, and did the mission grow?
		actives, aerr := missionFetchActiveMissions(ctx, deps)
		if aerr != nil {
			fmt.Fprintf(out, "missions: explore re-read actives: %v; held for next pass\n", aerr) //nolint:errcheck
			return
		}
		var act *serverapi.ActiveMission
		for i := range actives {
			if actives[i].MissionID == c.ActiveID {
				act = &actives[i]
				break
			}
		}
		if act == nil {
			fmt.Fprintf(out, "missions: explore active %s vanished mid-tour; no result recorded\n", c.ActiveID) //nolint:errcheck
			return
		}
		remaining, navOnly := exploreNavLegsFromActive(*act)
		if !navOnly {
			fmt.Fprintf(out, "missions: %s (%s) grew a non-navigation objective; abandoning\n", c.Entry.MissionID, c.Entry.Title) //nolint:errcheck
			missionAbandon(ctx, deps, out, c, fromBase, acceptedAt, acceptedTick, "staged_non_nav")
			return
		}
		if len(remaining) == 0 {
			break // tour complete
		}
		if stage+1 >= missionExploreMaxStages {
			fmt.Fprintf(out, "missions: %s still has %d objective(s) after %d stages; abandoning\n", c.Entry.MissionID, len(remaining), missionExploreMaxStages) //nolint:errcheck
			missionAbandon(ctx, deps, out, c, fromBase, acceptedAt, acceptedTick, "stage_limit")
			return
		}
		legs, _ = exploreTour(at, remaining, missionPairDist(ctx, deps, at, remaining))
		fmt.Fprintf(out, "missions: %s staged %d more objective(s); continuing tour\n", c.Entry.MissionID, len(remaining)) //nolint:errcheck
	}

	// Claim. If the tour didn't end docked at the giver-return base, head back
	// to the accept station (probe verified the return-dock case pays in
	// place; visit-only missions' claim location is unproven, so the accept
	// station is the safe default).
	if dockedAt == "" || dockedAt != c.DestBaseID || at != c.DestSystem {
		if nerr := deps.nav(ctx, acceptSystem, acceptPOI); nerr != nil {
			fmt.Fprintf(out, "missions: explore return transit failed: %v; held for next pass\n", nerr) //nolint:errcheck
			return
		}
		// dockIdempotent, not Dock: a ship already sitting at the return base
		// answers "Already docked", and aborting on that stranded a FINISHED
		// mission ("0 leg(s) remaining") in a tick-cadence retry loop that
		// never reached missionComplete.
		if derr := dockIdempotent(ctx, deps.Client); derr != nil {
			fmt.Fprintf(out, "missions: explore return dock failed: %v; held for next pass\n", derr) //nolint:errcheck
			return
		}
	}
	missionComplete(ctx, deps, out, c, fromBase, acceptedAt, acceptedTick)
}

// missionStrongholdHop checks every system a candidate will touch: each leg
// endpoint (or the single delivery destination) must not be a stronghold, and
// each consecutive hop's route must be stronghold-clear. Returns the offending
// system and ok=false, or ("", true) when the whole trip is safe. A nil
// galGraph skips route checks (endpoint guard only), mirroring the delivery
// path's degraded mode.
func missionStrongholdHop(strongholds map[string]bool, galGraph *galaxy.GalaxyGraph, current string, c missionCandidate) (string, bool) {
	hops := []string{c.DestSystem}
	if c.Legs != nil {
		hops = make([]string, len(c.Legs))
		for i, l := range c.Legs {
			hops[i] = l.SystemID
		}
	}
	// A chain mission is granted one-time passage to its OWN destination — that
	// is how an_introduction reaches Voss Redoubt while pirate standing is
	// still hostile, and a blanket refusal permanently blocks the single
	// mission that grants stronghold access. The exemption is deliberately
	// narrow: only the final hop, only for a chain-marked entry. Procedural
	// couriers, and any stronghold merely on the route, stay refused — this
	// guard exists because a worker was destroyed flying to one.
	passage := missionCarriesPassage(c.Entry)
	at := current
	for i, h := range hops {
		final := i == len(hops)-1
		exempt := passage && final
		if strongholds[h] && !exempt {
			return h, false
		}
		if galGraph != nil && !missionRouteClear(galGraph.FindPath, strongholds, at, h, exempt) {
			return "route to " + h, false
		}
		at = h
	}
	return "", true
}

// bestExploreCandidate returns the highest-net exploration candidate (ties by
// mission id for determinism), or nil.
func bestExploreCandidate(cands []missionCandidate) *missionCandidate {
	var best *missionCandidate
	for i := range cands {
		c := &cands[i]
		if best == nil || c.Net > best.Net ||
			(c.Net == best.Net && c.Entry.MissionID < best.Entry.MissionID) {
			best = c
		}
	}
	return best
}

// missionAcceptAndExplore accepts one exploration candidate, resolves its
// active-instance id, and flies the tour. The delivery accept flow's id
// resolution applies unchanged (board ids 404 on complete/abandon).
func missionAcceptAndExplore(ctx context.Context, deps MissionDeps, out io.Writer, c missionCandidate, acceptSystem, acceptPOI, fromBase string, strongholds map[string]bool) error {
	acceptedAt, acceptedTick := rfc(missionNow(deps)), missionTick(deps)
	if aerr := deps.Client.AcceptMission(ctx, c.Entry.MissionID); aerr != nil {
		fmt.Fprintf(out, "missions: accept %s failed: %v\n", c.Entry.MissionID, aerr) //nolint:errcheck
		return missionDryPass(ctx, deps, out)
	}
	_ = deps.sleep(ctx, game.SleepTick)
	actives, aerr := missionFetchActiveMissions(ctx, deps)
	if aerr != nil {
		fmt.Fprintf(out, "missions: resolve active id: get_active_missions: %v; accepted exploration held for next pass\n", aerr) //nolint:errcheck
		return nil // missionResume picks it up next pass
	}
	resolved := resolveActiveMissionIDs([]missionCandidate{c}, actives)
	if resolved[0].ActiveID == "" {
		fmt.Fprintf(out, "missions: accepted %s but no active instance resolved; held for next pass\n", c.Entry.MissionID) //nolint:errcheck
		return nil
	}
	c = resolved[0]
	if deps.State != nil {
		// Executed real work: reset the dry streak and the circuit/park state.
		deps.State.dry = 0
		deps.State.hopsDry = 0
		deps.State.parkedUntil = time.Time{}
	}
	fmt.Fprintf(out, "missions: running exploration %s (%s): %d leg(s), %d jumps, est net %.0f\n", c.Entry.MissionID, c.Entry.Title, len(c.Legs), c.Jumps, c.Net) //nolint:errcheck
	publishActivity(deps.SetActivity, "Mission "+c.Entry.Title)
	missionRunExplore(ctx, deps, out, c, acceptSystem, acceptPOI, fromBase, acceptedAt, acceptedTick, strongholds)
	return nil
}

// missionResumeExplore continues an exploration mission held from a previous
// pass/process: remaining nav legs are re-toured from the current system and
// flown; an all-complete hold goes straight to the claim step inside
// missionRunExplore. Actives outside the nav vocabulary are left alone
// (manual/other origin). Returns true when it acted.
func missionResumeExplore(ctx context.Context, deps MissionDeps, out io.Writer, m serverapi.ActiveMission, current string, strongholds map[string]bool) bool {
	remaining, navOnly := exploreNavLegsFromActive(m)
	if !navOnly {
		return false
	}
	held := missionCandidate{
		Entry: serverapi.MissionBoardEntry{
			MissionID: m.MissionID, TemplateID: m.TemplateID, Type: m.Type, Title: m.Title,
		},
		// m.MissionID is already the real active-instance id here.
		ActiveID: m.MissionID,
	}
	if n := len(m.Objectives); n > 0 {
		if last := m.Objectives[n-1]; last.Type == missionObjectiveDock {
			held.DestBaseID, held.DestSystem = last.TargetBase, last.SystemID
		}
	}
	ordered, jumps := exploreTour(current, remaining, missionPairDist(ctx, deps, current, remaining))
	if len(remaining) > 0 && jumps >= navigation.RouteInf {
		fmt.Fprintf(out, "missions: abandoning held %s (%s): a remaining leg is unreachable\n", m.MissionID, m.Title) //nolint:errcheck
		missionAbandon(ctx, deps, out, held, "", rfc(missionNow(deps)), missionTick(deps), "leg_unreachable")
		return true
	}
	held.Legs, held.Jumps = ordered, jumps
	poi := ""
	if st := deps.Client.GetState(); st != nil {
		poi = st.CurrentPOI
	}
	fmt.Fprintf(out, "missions: resuming exploration %s (%s): %d leg(s) remaining\n", m.MissionID, m.Title, len(remaining)) //nolint:errcheck
	publishActivity(deps.SetActivity, "Mission "+m.Title)
	missionRunExplore(ctx, deps, out, held, current, poi, "", rfc(missionNow(deps)), missionTick(deps), strongholds)
	return true
}

// missionPairDist builds a pairwise jump-distance function over {current} ∪
// the legs' systems from the KB jump graph (one BFS per distinct source).
func missionPairDist(ctx context.Context, deps MissionDeps, current string, legs []missionLeg) func(a, b string) int {
	systems := map[string]bool{current: true}
	for _, l := range legs {
		systems[l.SystemID] = true
	}
	targets := make([]string, 0, len(systems))
	for s := range systems {
		targets = append(targets, s)
	}
	conns, err := deps.KB.GetConnections(ctx)
	if err != nil {
		return func(a, b string) int { return navigation.RouteInf }
	}
	graph := navigation.JumpGraphFromConnections(conns)
	table := make(map[string]map[string]int, len(targets))
	for _, src := range targets {
		table[src] = navigation.BFSJumps(graph, src, targets)
	}
	return func(a, b string) int {
		if a == b {
			return 0
		}
		if row, ok := table[a]; ok {
			if d, ok := row[b]; ok {
				return d
			}
		}
		return navigation.RouteInf
	}
}
