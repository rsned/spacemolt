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
	// DefaultMissionMaxJumps caps how far (jumps) a mission destination may be,
	// matching the haul fleet's reposition philosophy (DefaultHaulMaxJumps):
	// several nearby runs net more than one distant payday.
	DefaultMissionMaxJumps = 5
	// missionMinNet is the minimum estimated profit (reward - item cost - fuel)
	// a mission must clear. Below this the accept isn't worth the slot.
	missionMinNet = 500.0
	// Expiry gate (the arbitrage-expiry lesson: never accept work that can time
	// out mid-route). A finite expiry must cover a base margin plus a per-jump
	// travel allowance. Ticks are ~10s wall.
	missionMinExpiryTicks  = 180 // 30 min base margin
	missionTicksPerJump    = 12  // ~2 min/jump allowance (jump + transit + dock)
	// missionBuyBudgetFraction of current credits may be spent acquiring
	// mission cargo across the whole stacked set — never bet the full wallet.
	missionBuyBudgetFraction = 0.8
)

// missionCandidate is a deliver-shaped board entry with derived routing and
// economics, ready for stacking.
type missionCandidate struct {
	Entry      serverapi.MissionBoardEntry
	ItemID     string
	Qty        int     // units to deliver
	BuyQty     int     // units we must acquire (Qty minus provided)
	DestBaseID string
	DestSystem string
	Reward     float64
	ItemCost   float64 // BuyQty x reference ask
	FuelCost   float64
	Net        float64 // Reward - ItemCost - FuelCost
	Jumps      int     // current system -> DestSystem
}

// deliverShape extracts the deliver-mission core of e. ok=false when e is not a
// pure deliver mission v1 can run: kill/mine/visit components, module gates,
// and entries with no resolvable destination system are all skipped.
func deliverShape(e serverapi.MissionBoardEntry) (item string, qty int, destBase, destSystem string, ok bool) {
	r := e.Requirements
	if r == nil || r.DeliverItemID == "" || r.DeliverQuantity <= 0 || r.DeliverToBaseID == "" {
		return "", 0, "", "", false
	}
	// Any non-deliver component makes this a compound mission v1 skips.
	if r.KillCount > 0 || r.MineQuantity > 0 || r.VisitSystemCount > 0 || r.TargetPlayerID != "" {
		return "", 0, "", "", false
	}
	if len(e.RequiredModules) > 0 {
		return "", 0, "", "", false
	}
	// Destination system comes from the matching deliver objective; entries
	// without one cannot be routed and are skipped.
	for _, o := range e.Objectives {
		if o.TargetBaseID == r.DeliverToBaseID && o.SystemID != "" {
			return r.DeliverItemID, r.DeliverQuantity, r.DeliverToBaseID, o.SystemID, true
		}
	}
	return "", 0, "", "", false
}

// buildMissionCandidate prices and routes one board entry. dist maps system id
// -> jumps from the worker's current system (navigation.BFSJumps output); refAsk
// returns the sentinel-filtered best ask for an item (ok=false -> unpriceable);
// fuelCostFor prices the fuel for a jump count. A non-empty reason means the
// entry was filtered out (and why, for the worker log).
func buildMissionCandidate(e serverapi.MissionBoardEntry, dist map[string]int, refAsk func(itemID string) (float64, bool), fuelCostFor func(jumps int) float64) (missionCandidate, string) {
	item, qty, destBase, destSystem, ok := deliverShape(e)
	if !ok {
		return missionCandidate{}, "not a plain deliver mission"
	}
	// Deliver-shaped smuggling missions carry warnings (e.g. contraband,
	// insurance voided); uninsured idle accounts must never run these.
	if len(e.Warnings) > 0 {
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
	if maxJumps <= 0 {
		maxJumps = DefaultMissionMaxJumps
	}
	sorted := make([]missionCandidate, 0, len(cands))
	for _, c := range cands {
		if c.Jumps <= maxJumps {
			sorted = append(sorted, c)
		}
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
