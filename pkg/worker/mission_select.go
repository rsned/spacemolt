package worker

import (
	"fmt"
	"maps"
	"sort"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/navigation"
)

const (
	// MissionMaxStack is the server's concurrent-mission cap per player (the
	// Mission Runner guide's "accept 5 simultaneous missions" stacking play).
	MissionMaxStack = 5
	// DefaultMissionMaxJumps is the OPT-IN distance cap for mission
	// destinations, in jumps. It is no longer applied by default: MaxJumps 0
	// (the only value dispatch ever produced) now means unlimited.
	//
	// It was a flat 5, borrowed from the haul fleet's reposition philosophy
	// ("several nearby runs net more than one distant payday"). That reasoning
	// does not transfer — a hauler chooses its own destination and can always
	// find another opportunity, while a mission board offers what it offers.
	// The cap silently hid every long-haul mission from the entire fleet,
	// including the 17-jump cross-border run that is the ONLY route to
	// smuggling level 2. Distance is priced by the expiry gate, the fuel term
	// in the net, and Autopilot's pre-route refuel; a flat radius on top of
	// those only removed work the economics would have taken.
	DefaultMissionMaxJumps = 5
	// missionMinNet is the minimum estimated profit (reward - item cost - fuel)
	// a mission must clear. Below this the accept isn't worth the slot.
	missionMinNet = 500.0
	// Expiry gate (the arbitrage-expiry lesson: never accept work that can time
	// out mid-route). A finite expiry must cover a base margin plus a per-jump
	// travel allowance. Ticks are ~10s wall.
	missionMinExpiryTicks = 180 // 30 min base margin
	// missionMaxJumpTicks is the slowest jump the game can produce, used when
	// the ship's speed is unknown so an unknown hull is never priced
	// optimistically. See missionJumpTicks.
	missionMaxJumpTicks = 6
	// missionSmugglingMinExpiryTicks replaces the base margin for smuggling.
	// The 180-tick margin is travel-blind, and black-market jobs board AT the
	// station the worker is already docked at — six of them were refused for
	// runway they never needed (140 ticks available, 180 "needed for 0 jumps").
	// 30 ticks still covers the fixed accept/deliver overhead, and the per-jump
	// allowance above is untouched, so anything with actual travel is priced the
	// same as before.
	missionSmugglingMinExpiryTicks = 30
	// missionSmugglingXPCreditValue prices one point of smuggling XP in credits.
	//
	// Smuggling couriers are taken for XP, not money: they pay 300-1400 cr
	// against real fuel, so on credits alone every one is a loss and the floor
	// above rejects all of them. That is what left engineer-1 parked at level 2
	// (88/340) with the category enabled and zero accepts. The XP is the actual
	// payload — smuggling level gates chain 2 (`an_introduction`, which grants
	// permanent pirate-stronghold docking) and chain 3 (the Crimson wormholes),
	// both fleet-wide routing wins.
	//
	// 25 cr/XP makes the 252 XP that engineer-1 needs for the chain-2 unlock
	// worth ~6300 credits — less than a single fat haul run, for a permanent
	// unlock. It is deliberately not high enough to excuse anything: at this
	// rate a 175-XP courier survives a 2000 cr loss while a 5-XP one does not,
	// so the gate still discriminates rather than waving the category through.
	missionSmugglingXPCreditValue = 25.0
	// missionSmugglingXPFloor is the net a smuggling courier may lose while a
	// worker is buying its way up the skill. Couriers price out at -680 to
	// -2312 on the boards we have watched, so the normal 500 floor rejects
	// EVERY one — which is why the 2026-07-26 canaries made zero accepts and
	// produced no evidence either way about the worker binary. Levels are the
	// point: L3 unlocks chain 2 and L5 chain 3, both permanent routing wins.
	missionSmugglingXPFloor = -2500.0
	// missionSmugglingXPBudget caps the cumulative smuggling LOSS one worker
	// eats at the relaxed floor before it reverts to missionMinNet. Sized from
	// live numbers: couriers pay 75-100 XP for ~2000-2300 cr, so a level costs
	// roughly 6500-7000 cr and this buys 3-4 of them. Per-worker and in-memory,
	// so a restart forgives it — the forgiving direction for a canary whose
	// whole job is to reach the next level.
	missionSmugglingXPBudget = 25000.0
	// missionPayoutRatioWindow is how far back the realized-payout sample
	// reaches. Long enough to survive a quiet fleet (a few hours of no
	// completions must not empty the window and snap the gate back to face
	// value), short enough to notice the empire refilling its treasury within
	// a day rather than averaging a recovery away against a week of drought.
	missionPayoutRatioWindow = 24 * time.Hour
	// missionBuyBudgetFraction of current credits may be spent acquiring
	// mission cargo across the whole stacked set — never bet the full wallet.
	missionBuyBudgetFraction = 0.8
)

// missionCandidate is a deliver-shaped board entry with derived routing and
// economics, ready for stacking.
type missionCandidate struct {
	Entry serverapi.MissionBoardEntry
	// Items is the authoritative deliverable list, one entry per distinct item
	// (objectives for the same item are merged). ItemID/Qty/BuyQty below are the
	// aggregate view every existing consumer reads: for a single-item mission
	// they are that item verbatim, and for a multi-item one Qty/BuyQty are the
	// totals and ItemID is a joined label.
	Items      []missionDeliverable
	ItemID     string
	Qty        int // total units to deliver, across every objective
	BuyQty     int // units we must acquire (Qty minus provided)
	DestBaseID string
	DestSystem string
	Reward     float64
	ItemCost   float64 // BuyQty x reference ask
	FuelCost   float64
	Net        float64 // Reward - ItemCost - FuelCost
	Jumps      int     // current system -> DestSystem
	// ActiveID is the server's real (hex) active-mission id, resolved after
	// accept via resolveActiveMissionIDs (or copied straight from
	// get_active_missions for resumed candidates). Entry.MissionID stays the
	// board/template-ish id used for telemetry; complete_mission and
	// abandon_mission calls must use ActiveID — the board id 404s with
	// mission_not_found once the server has created the active instance.
	ActiveID string
	// Legs is the tour-ordered navigation plan for exploration candidates
	// (nil for deliveries). DestSystem/DestBaseID mirror the final leg.
	Legs []missionLeg
}

// resolveActiveMissionIDs matches each freshly accepted candidate to its
// active-mission instance so complete/abandon calls use the server's real
// id instead of the board's template-ish MissionID (mission_not_found:
// "Use the mission_id from get_active_missions, not template_id").
//
// Primary match: active.TemplateID == candidate's board MissionID — the
// server stamps the template id it was accepted from onto the new instance.
// Fallback (TemplateID missing/mismatched): Title plus the mission's WHOLE
// item/quantity set and target base (see activeMatchesItems), mirroring
// missionResume's shape check. Matching on one leg would never resolve a
// multi-item mission, and an unresolved candidate is dropped without an
// abandon — it would sit on the books with no id to release it.
// Each active is consumed by at most one candidate (first match wins) so two
// identically-titled board entries never resolve to the same instance.
// Unresolved candidates get ActiveID == "" — callers must drop them without
// calling abandon (there is no valid id to abandon).
func resolveActiveMissionIDs(accepted []missionCandidate, actives []serverapi.ActiveMission) []missionCandidate {
	used := make([]bool, len(actives))
	resolved := make([]missionCandidate, len(accepted))
	for i, c := range accepted {
		idx := -1
		for j, a := range actives {
			if !used[j] && a.TemplateID != "" && a.TemplateID == c.Entry.MissionID {
				idx = j
				break
			}
		}
		if idx < 0 {
			for j, a := range actives {
				if used[j] || a.Title != c.Entry.Title {
					continue
				}
				if activeMatchesItems(a, c.deliverables(), c.DestBaseID) {
					idx = j
					break
				}
			}
		}
		if idx >= 0 {
			c.ActiveID = actives[idx].MissionID
			used[idx] = true
		}
		resolved[i] = c
	}
	return resolved
}

// missionTypeDelivery is the board category v1 runs by default. Smuggling
// missions also carry deliver_item objectives (often with provided contraband
// and no warnings), so the mission-level type allowlist — not objective shape —
// is what keeps them out.
const missionTypeDelivery = "delivery"

// missionTypeSmuggling is the contraband-courier board category. It is
// deliver-shaped and otherwise indistinguishable from freight, so it is gated
// on explicit operator opt-in (mission_categories) at every decision point:
// nothing here infers it from the objective.
const missionTypeSmuggling = "smuggling"

// skillSmuggling is the skill id the server credits smuggling XP under in
// MissionRewards.SkillXP. It matches the board `type`, but they are distinct
// namespaces (mission type vs skill id), so they are named separately.
const skillSmuggling = "smuggling"

// missionObjectiveDeliver is the deliver-cargo objective type on the wire.
const missionObjectiveDeliver = "deliver_item"

// missionDeliverType reports whether a board type is one this worker will run
// as a plain delivery. Smuggling only qualifies with explicit opt-in.
func missionDeliverType(t string, allowSmuggling bool) bool {
	return t == missionTypeDelivery || (allowSmuggling && t == missionTypeSmuggling)
}

// deliverables is the candidate's item list, falling back to the aggregate
// single-item view when Items is unset. Candidates built by hand — rather than
// through buildMissionCandidate or heldDeliveryShape — fill only ItemID/Qty,
// and silently returning nothing for them would stop them ever resolving to
// their active instance, stranding a mission with no id to abandon it by.
func (c missionCandidate) deliverables() []missionDeliverable {
	if len(c.Items) > 0 {
		return c.Items
	}
	if c.ItemID == "" {
		return nil
	}

	return []missionDeliverable{{ItemID: c.ItemID, Qty: c.Qty, BuyQty: c.BuyQty}}
}

// activeMatchesItems reports whether an active mission delivers exactly the
// goods, quantities and destination a candidate was built for. Objectives are
// merged by item id first — the same merge deliverShape applies to the board —
// so a mission listing one good across two objectives still matches, and a
// multi-item mission matches on its whole set rather than on a single leg.
func activeMatchesItems(a serverapi.ActiveMission, items []missionDeliverable, destBase string) bool {
	if len(a.Objectives) == 0 {
		return false
	}
	want := make(map[string]int, len(items))
	for _, it := range items {
		want[it.ItemID] = it.Qty
	}
	got := make(map[string]int, len(a.Objectives))
	for _, o := range a.Objectives {
		if o.TargetBase != destBase {
			return false
		}
		// A dock_at_base objective carries nothing. Folding it in would add an
		// empty-item key and stop a visit mission ever matching itself.
		if o.Type == missionObjectiveDock {
			continue
		}
		got[o.ItemID] += o.Required
	}

	return maps.Equal(want, got)
}

// missionDeliverable is one good a delivery candidate hands over at its
// destination. Ordinary board deliveries carry exactly one; the smuggling
// chain's `an_introduction` carries two to the same base.
type missionDeliverable struct {
	ItemID string
	Qty    int // units the mission requires delivered
	BuyQty int // units we must acquire (Qty minus what the mission provides)
}

// deliverShape extracts the runnable core of a board entry. The server sends
// no requirements block (openapi: additionalProperties=false) — deliver
// details live in objectives.
//
// Every objective must be a well-formed deliver_item and they must all target
// ONE base: the delivery executor flies a single destination, so a split
// mission would strand half its cargo. Multi-leg and non-deliver objectives
// stay rejected.
//
// A non-empty reason means the entry is not runnable, and says why — the
// reasons are distinct because they land in the worker's skip log, where a
// single catch-all message hid a runnable chain mission behind the same text
// as a wrong-type one.
//
// allowSmuggling widens the type gate to contraband couriers. It is a
// parameter rather than a package rule so the gate stays inside this pure
// function: a caller that forgets to pass it gets the safe answer.
func deliverShape(e serverapi.MissionBoardEntry, allowSmuggling bool) (items []missionDeliverable, destBase, destSystem, reason string) {
	if !missionDeliverType(e.Type, allowSmuggling) {
		return nil, "", "", fmt.Sprintf("not a delivery-type mission (type %q)", e.Type)
	}
	if len(e.RequiredModules) > 0 {
		return nil, "", "", fmt.Sprintf("requires module(s): %s", strings.Join(e.RequiredModules, ", "))
	}
	if len(e.Objectives) == 0 {
		return nil, "", "", "mission carries no objectives"
	}
	// Objectives are merged by item id, because provided_items is a POOL keyed
	// by item: subtracting the grant per objective would double-count it and
	// invent a purchase the mission already covers.
	order := make([]string, 0, len(e.Objectives))
	qty := make(map[string]int, len(e.Objectives))
	for i, o := range e.Objectives {
		// dock_at_base is a delivery that carries nothing: go there, dock, claim.
		// It matters far beyond its 500 credits — `a_word_in_private` is shaped
		// this way and is the ONLY smuggling XP a level-0 agent can earn, since
		// every smuggling-TYPED mission already requires level 1. Refusing it
		// left a fresh agent unable to begin the chain at all.
		isDock := o.Type == missionObjectiveDock
		isDeliver := o.Type == missionObjectiveDeliver && o.ItemID != "" && o.Quantity > 0
		if (!isDock && !isDeliver) || o.TargetBaseID == "" || o.SystemID == "" {
			return nil, "", "", fmt.Sprintf("objective %d of %d is neither a well-formed deliver_item nor dock_at_base", i+1, len(e.Objectives))
		}
		if destBase == "" {
			destBase, destSystem = o.TargetBaseID, o.SystemID
		} else if o.TargetBaseID != destBase {
			return nil, "", "", fmt.Sprintf("objectives split across %s and %s; multi-destination delivery unsupported", destBase, o.TargetBaseID)
		}
		if isDock {
			continue // nothing to carry; the dock itself is the objective
		}
		if _, seen := qty[o.ItemID]; !seen {
			order = append(order, o.ItemID)
		}
		qty[o.ItemID] += o.Quantity
	}
	items = make([]missionDeliverable, 0, len(order))
	for _, id := range order {
		items = append(items, missionDeliverable{ItemID: id, Qty: qty[id]})
	}

	return items, destBase, destSystem, ""
}

// heldDelivery is the outstanding state of an ACTIVE delivery mission — the
// resume-path analogue of deliverShape.
type heldDelivery struct {
	Items          []missionDeliverable // units STILL owed, per distinct item
	TotalRemaining int
	DestBase       string
	DestSystem     string
	Covered        bool    // every objective is complete or already satisfiable
	ShortItem      string  // first item we cannot cover (only when !Covered)
	ShortAboard    float64 // what we hold of it
	ShortNeed      int     // what it still needs
}

// heldDeliveryShape summarises an active mission's delivery objectives. ok is
// false for anything that is not an all-deliver_item mission to a single base;
// those are left alone for manual handling rather than resumed or abandoned.
//
// aboard reports units of an item in the hold. An objective can be satisfied
// with nothing aboard — storage at the target base counts, and the wire reports
// it as in_storage — so a Completed objective owes nothing regardless of cargo.
// Coverage is judged per item AFTER merging, so two objectives for the same
// good are checked against one hold rather than twice against the same units.
func heldDeliveryShape(m serverapi.ActiveMission, aboard func(itemID string) float64) (heldDelivery, bool) {
	if len(m.Objectives) == 0 {
		return heldDelivery{}, false
	}
	h := heldDelivery{Covered: true}
	order := make([]string, 0, len(m.Objectives))
	rem := make(map[string]int, len(m.Objectives))
	for _, o := range m.Objectives {
		isDock := o.Type == missionObjectiveDock
		if !isDock && (o.Type != missionObjectiveDeliver || o.ItemID == "") {
			return heldDelivery{}, false
		}
		if h.DestBase == "" {
			h.DestBase, h.DestSystem = o.TargetBase, o.SystemID
		} else if o.TargetBase != h.DestBase {
			return heldDelivery{}, false
		}
		if isDock {
			continue // owes no cargo; docking there is the whole objective
		}
		remaining := max(o.Required-o.Current, 0)
		if o.Completed {
			remaining = 0
		}
		if _, seen := rem[o.ItemID]; !seen {
			order = append(order, o.ItemID)
		}
		rem[o.ItemID] += remaining
	}
	h.Items = make([]missionDeliverable, 0, len(order))
	for _, id := range order {
		h.Items = append(h.Items, missionDeliverable{ItemID: id, Qty: rem[id]})
		h.TotalRemaining += rem[id]
	}
	for _, it := range h.Items {
		if it.Qty == 0 {
			continue
		}
		if have := aboard(it.ItemID); have < float64(it.Qty) {
			h.Covered = false
			h.ShortItem, h.ShortAboard, h.ShortNeed = it.ItemID, have, it.Qty

			break
		}
	}

	return h, true
}

// missionItemLabel is the single-item view that the acquisition, cargo and
// telemetry paths still read off the candidate. A multi-item mission gets a
// joined label for logs and the mission_results row; those missions carry
// BuyQty 0 (see buildMissionCandidate), so no sourcing code ever parses it back
// into an item id.
func missionItemLabel(items []missionDeliverable) string {
	if len(items) == 1 {
		return items[0].ItemID
	}
	ids := make([]string, len(items))
	for i, it := range items {
		ids[i] = it.ItemID
	}

	return strings.Join(ids, "+")
}

// missionCarriesPassage reports whether a candidate is a linked CHAIN mission,
// which the server grants one-time passage for (that is how `an_introduction`
// reaches Voss Redoubt while pirate standing is still hostile). chain_next is
// the board's own marker: procedural couriers leave it empty and every chain
// leg sets it. Used only to permit a stronghold DESTINATION — never a
// stronghold on the way there.
func missionCarriesPassage(e serverapi.MissionBoardEntry) bool {
	return e.ChainNext != ""
}

// buildMissionCandidate prices and routes one board entry. dist maps system id
// -> jumps from the worker's current system (navigation.BFSJumps output); refAsk
// returns the sentinel-filtered best ask for an item (ok=false -> unpriceable);
// fuelCostFor prices the fuel for a jump count. A non-empty reason means the
// entry was filtered out (and why, for the worker log).
// fuelShare is how many same-destination smuggling entries the board is
// offering (1 when unbatched). Smuggling couriers arrive 2-3 at a time to one
// destination and SelectMissionSet already stacks by DestSystem, so the trip
// fuel is paid ONCE for the whole cohort — charging each candidate the full
// bill rejects a collectively profitable batch before stacking ever sees it.
// It affects the gate only; recorded economics stay at full cost. Ignored for
// non-smuggling entries.
// effectiveMissionFloor is the net a candidate must clear. Smuggling relaxes to
// missionSmugglingXPFloor while the worker's XP-buying budget holds, because the
// XP — not the credits — is the reason the run is worth taking; every other
// category, and a smuggling worker that has spent its budget, uses missionMinNet.
// Pure: the caller supplies what has been spent so far.
func effectiveMissionFloor(smuggling bool, spent, budget float64) float64 {
	if smuggling && spent < budget {
		return missionSmugglingXPFloor
	}

	return missionMinNet
}

// missionJumpTicks is the game's jump time for a hull:
//
//	jumpTicks = max(1, 7 - shipSpeed)
//
// (spacemolt.com/docs/guides/fuel). A speed-6 hull jumps in 1 tick, a speed-1
// hull in 6 — so 6 is the slowest jump that can physically exist. The gate
// previously charged a flat 12 ticks per jump for every ship, i.e. double the
// worst case, and refused couriers a fast hull had ample runway for: one was
// declined at "197 ticks available < 198 needed for 14 jumps" that a speed-6
// hull crosses in 14.
//
// A non-positive speed means we have no ship data; fall back to the slowest
// jump rather than guess in the optimistic direction.
func missionJumpTicks(speed float64) int {
	if speed <= 0 {
		return missionMaxJumpTicks
	}

	return max(1, missionMaxJumpTicks+1-int(speed))
}

func buildMissionCandidate(e serverapi.MissionBoardEntry, dist map[string]int, refAsk func(itemID string) (float64, bool), fuelCostFor func(jumps int) float64, allowSmuggling bool, fuelShare int, floor float64, jumpTicks int, payoutRatio float64) (missionCandidate, string) {
	items, destBase, destSystem, shapeReason := deliverShape(e, allowSmuggling)
	if shapeReason != "" {
		return missionCandidate{}, shapeReason
	}
	// Deliver-shaped missions can carry warnings (e.g. contraband, insurance
	// voided); uninsured idle accounts must never run these by accident.
	//
	// Smuggling is the exception, and only because its warnings ARE the
	// category: a contraband courier that carried no warning would be the
	// surprising case. Allowlisting `smuggling` in mission_categories is the
	// operator saying yes to exactly this risk, so re-refusing it here would
	// make the category impossible to enable. Ordinary freight is unchanged —
	// a delivery mission with warnings is still refused on a smuggling-enabled
	// worker, because nothing opted THAT run into the risk.
	if len(e.Warnings) > 0 && e.Type != missionTypeSmuggling {
		return missionCandidate{}, fmt.Sprintf("has warnings: %s", strings.Join(e.Warnings, "; "))
	}
	jumps, reachable := dist[destSystem]
	// navigation.BFSJumps pre-seeds every requested target with RouteInf, so
	// the map-miss (!reachable) branch never fires in production; kept for
	// tests that build dist maps by hand without the sentinel.
	if !reachable || jumps >= navigation.RouteInf {
		return missionCandidate{}, fmt.Sprintf("destination system %s unreachable", destSystem)
	}
	smuggling := e.Type == missionTypeSmuggling
	minExpiry := missionMinExpiryTicks
	if smuggling {
		minExpiry = missionSmugglingMinExpiryTicks
	}
	if e.ExpiresInTicks > 0 && e.ExpiresInTicks < minExpiry+jumps*jumpTicks {
		return missionCandidate{}, fmt.Sprintf("expires in %d ticks (< %d needed for %d jumps at %d ticks/jump)",
			e.ExpiresInTicks, minExpiry+jumps*jumpTicks, jumps, jumpTicks)
	}
	reward := 0.0
	if e.Rewards != nil {
		reward = float64(e.Rewards.Credits)
	}
	totalQty, totalBuy := 0, 0
	itemCost := 0.0
	for i := range items {
		items[i].BuyQty = max(items[i].Qty-e.ProvidedItems[items[i].ItemID], 0)
		totalQty += items[i].Qty
		totalBuy += items[i].BuyQty
		// Smuggling comes in two shapes: the goods are handed over on accept, or
		// the mission tells you to source them yourself. The second is
		// UNCOMPLETABLE — contraband has no sell orders on any market — so a
		// short ProvidedItems is a hard reject, not an economics question.
		//
		// This deliberately does not lean on the refAsk lookup failing to catch
		// it. That happens to work today (an unpriceable item is rejected below)
		// but it is incidental: one stale ask for a contraband item would let a
		// run through that the worker can then never finish.
		if smuggling && items[i].BuyQty > 0 {
			return missionCandidate{}, fmt.Sprintf("smuggling run must source %d x %s itself; contraband is not sold on the market", items[i].BuyQty, items[i].ItemID)
		}
		if items[i].BuyQty > 0 {
			ask, priced := refAsk(items[i].ItemID)
			if !priced || ask <= 0 {
				return missionCandidate{}, fmt.Sprintf("no reference ask for %s", items[i].ItemID)
			}
			itemCost += float64(items[i].BuyQty) * ask
		}
	}
	// Multi-item SOURCING is deliberately out of scope. The acquisition path
	// prices ONE ask ladder per candidate and recovers the unit cost as
	// ItemCost/BuyQty, so a mission buying two different goods would mis-price
	// both. Multi-item missions are supported only when the mission provides
	// the goods — which is exactly how the smuggling chain ships them — so
	// nothing needs sourcing. Refused explicitly rather than half-run.
	if len(items) > 1 && totalBuy > 0 {
		return missionCandidate{}, fmt.Sprintf("multi-objective delivery would need to source %d unit(s); only fully provided multi-item missions are supported", totalBuy)
	}
	fuelCost := fuelCostFor(jumps)
	// The gate scores what the empire will ACTUALLY pay: advertised rewards have
	// been settling well under face value since the treasury stopped being
	// replenished (see Collector.MissionPayoutRatio). c.Reward deliberately keeps
	// the ADVERTISED figure — mission_results.expected_reward is the input the
	// ratio is computed from, so discounting it there would feed the ratio back
	// into itself and spiral.
	net := reward*payoutRatio - itemCost - fuelCost
	// Net stays the CREDIT number at FULL trip cost — it is what gets recorded
	// and reported, and it must not be flattered by either adjustment below.
	// The gate, for smuggling only, judges two things the credit net misses:
	// the skill XP (which is why the run is worth taking at all), and the fact
	// that a batch of couriers to one destination pays for the trip once.
	gateNet := net
	if smuggling {
		if share := min(max(fuelShare, 1), MissionMaxStack); share > 1 {
			// Give back the fuel the siblings will carry. Capped at the stack
			// limit: a board offering fifty identical couriers does not make
			// fuel free, because at most MissionMaxStack ride one trip.
			gateNet += fuelCost - fuelCost/float64(share)
		}
	}
	// The XP credit keys on what the mission PAYS, not on how the server typed
	// it. a_word_in_private is typed "delivery" and awards 50 smuggling XP;
	// keying this on the type discarded that XP and refused the run over a
	// 7-credit shortfall, which left three level-0 canaries with nothing
	// acceptable on the only board that offers the chain.
	//
	// For anything not typed smuggling the credit may only rescue a mission that
	// ALREADY turns a credit profit. Negative-net XP purchases are budgeted on
	// the smuggling branch alone (see noteSmugglingLoss in mission.go); a
	// delivery never reaches that accounting, so letting 1250 credits of XP
	// credit drag a loss over the floor would spend real money on XP with
	// nothing tracking the spend.
	if e.Rewards != nil && (smuggling || net >= 0) {
		if xp := e.Rewards.SkillXP[skillSmuggling]; xp > 0 {
			gateNet += float64(xp) * missionSmugglingXPCreditValue
		}
	}
	if gateNet < floor {
		if smuggling && gateNet != net {
			return missionCandidate{}, fmt.Sprintf("net %.0f (+XP = %.0f) below floor %.0f", net, gateNet, floor)
		}
		return missionCandidate{}, fmt.Sprintf("net %.0f below floor %.0f", net, floor)
	}
	return missionCandidate{
		Entry: e, Items: items,
		ItemID: missionItemLabel(items), Qty: totalQty, BuyQty: totalBuy,
		DestBaseID: destBase, DestSystem: destSystem,
		Reward: reward, ItemCost: itemCost, FuelCost: fuelCost, Net: net, Jumps: jumps,
	}, ""
}

// SelectMissionSet picks up to MissionMaxStack candidates to run as one trip:
// the best-net candidate anchors the trip, and only candidates sharing its
// destination system stack onto it (cross-system banding is a phase-2 upgrade).
// The greedy fill respects the buy budget (missionBuyBudgetFraction of credits)
// and free cargo space; anchors farther than maxJumps are skipped entirely.
func SelectMissionSet(cands []missionCandidate, credits, cargoFree float64, maxJumps int) []missionCandidate {
	sorted := make([]missionCandidate, 0, len(cands))
	for _, c := range cands {
		// maxJumps <= 0 means no cap. Distance is PRICED, not forbidden: the
		// expiry gate already scales with jumps, fuel cost is subtracted in the
		// net, and Autopilot tops the tank up before departing. A flat radius on
		// top of all three only hid work the economics would have accepted.
		if maxJumps > 0 && c.Jumps > maxJumps {
			continue
		}
		sorted = append(sorted, c)
	}
	if len(sorted) == 0 {
		return nil
	}
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Net != sorted[j].Net {
			return sorted[i].Net > sorted[j].Net
		}
		return sorted[i].Entry.MissionID < sorted[j].Entry.MissionID
	})

	anchor := sorted[0]
	budget := credits * missionBuyBudgetFraction
	var picked []missionCandidate
	var spent, cargo float64
	for _, c := range sorted {
		if c.DestSystem != anchor.DestSystem {
			continue
		}
		if len(picked) >= MissionMaxStack {
			break
		}
		if spent+c.ItemCost > budget {
			continue
		}
		// The whole delivery quantity rides in the hold at once (provided items
		// are granted into cargo on accept).
		if cargo+float64(c.Qty) > cargoFree {
			continue
		}
		picked = append(picked, c)
		spent += c.ItemCost
		cargo += float64(c.Qty)
	}
	return picked
}
