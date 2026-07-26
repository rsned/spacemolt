package worker

import (
	"fmt"
	"sort"
	"strings"

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
	missionTicksPerJump   = 12  // ~2 min/jump allowance (jump + transit + dock)
	// missionBuyBudgetFraction of current credits may be spent acquiring
	// mission cargo across the whole stacked set — never bet the full wallet.
	missionBuyBudgetFraction = 0.8
)

// missionCandidate is a deliver-shaped board entry with derived routing and
// economics, ready for stacking.
type missionCandidate struct {
	Entry      serverapi.MissionBoardEntry
	ItemID     string
	Qty        int // units to deliver
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
// Fallback (TemplateID missing/mismatched): Title plus the single objective's
// item/quantity/target-base tuple, mirroring missionResume's shape check.
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
				if used[j] || a.Title != c.Entry.Title || len(a.Objectives) != 1 {
					continue
				}
				o := a.Objectives[0]
				if o.ItemID == c.ItemID && o.Required == c.Qty && o.TargetBase == c.DestBaseID {
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

// missionObjectiveDeliver is the deliver-cargo objective type on the wire.
const missionObjectiveDeliver = "deliver_item"

// missionDeliverType reports whether a board type is one this worker will run
// as a plain delivery. Smuggling only qualifies with explicit opt-in.
func missionDeliverType(t string, allowSmuggling bool) bool {
	return t == missionTypeDelivery || (allowSmuggling && t == missionTypeSmuggling)
}

// deliverShape extracts the runnable core of a board entry. The server sends
// no requirements block (openapi: additionalProperties=false) — deliver
// details live in objectives. ok=false for anything but a single-leg
// deliver_item mission (multi-leg chains and compound objectives are
// v1-rejected), or when a module gate is present.
//
// allowSmuggling widens the type gate to contraband couriers. It is a
// parameter rather than a package rule so the gate stays inside this pure
// function: a caller that forgets to pass it gets the safe answer.
func deliverShape(e serverapi.MissionBoardEntry, allowSmuggling bool) (item string, qty int, destBase, destSystem string, ok bool) {
	if !missionDeliverType(e.Type, allowSmuggling) || len(e.RequiredModules) > 0 || len(e.Objectives) != 1 {
		return "", 0, "", "", false
	}
	o := e.Objectives[0]
	if o.Type != missionObjectiveDeliver || o.ItemID == "" || o.Quantity <= 0 || o.TargetBaseID == "" || o.SystemID == "" {
		return "", 0, "", "", false
	}
	return o.ItemID, o.Quantity, o.TargetBaseID, o.SystemID, true
}

// buildMissionCandidate prices and routes one board entry. dist maps system id
// -> jumps from the worker's current system (navigation.BFSJumps output); refAsk
// returns the sentinel-filtered best ask for an item (ok=false -> unpriceable);
// fuelCostFor prices the fuel for a jump count. A non-empty reason means the
// entry was filtered out (and why, for the worker log).
func buildMissionCandidate(e serverapi.MissionBoardEntry, dist map[string]int, refAsk func(itemID string) (float64, bool), fuelCostFor func(jumps int) float64, allowSmuggling bool) (missionCandidate, string) {
	item, qty, destBase, destSystem, ok := deliverShape(e, allowSmuggling)
	if !ok {
		return missionCandidate{}, "not a plain deliver mission"
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
	if e.ExpiresInTicks > 0 && e.ExpiresInTicks < missionMinExpiryTicks+jumps*missionTicksPerJump {
		return missionCandidate{}, fmt.Sprintf("expires in %d ticks (< %d needed for %d jumps)",
			e.ExpiresInTicks, missionMinExpiryTicks+jumps*missionTicksPerJump, jumps)
	}
	reward := 0.0
	if e.Rewards != nil {
		reward = float64(e.Rewards.Credits)
	}
	buyQty := max(qty-e.ProvidedItems[item], 0)
	itemCost := 0.0
	if buyQty > 0 {
		ask, priced := refAsk(item)
		if !priced || ask <= 0 {
			return missionCandidate{}, fmt.Sprintf("no reference ask for %s", item)
		}
		itemCost = float64(buyQty) * ask
	}
	fuelCost := fuelCostFor(jumps)
	net := reward - itemCost - fuelCost
	if net < missionMinNet {
		return missionCandidate{}, fmt.Sprintf("net %.0f below floor %.0f", net, missionMinNet)
	}
	return missionCandidate{
		Entry: e, ItemID: item, Qty: qty, BuyQty: buyQty,
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
