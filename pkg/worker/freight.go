package worker

import (
	"fmt"
	"math"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

const (
	// freightPackageFootprint is the cargo a sealed shipping package occupies.
	// Measured live (2026-07-19): flat 100 regardless of contents — ten size-1
	// iron_ore still measured 100, because the container's 100-item capacity is
	// reserved whole. NOT contents-summed, and not the empty container's size (4).
	freightPackageFootprint = 100.0

	// freightTicksPerHop estimates travel cost per route hop. Measured live: a
	// 3-hop delivery took 56 ticks (~18.7/hop). Single-sample; re-tune from
	// freight_results after the canary.
	freightTicksPerHop = 19.0

	// freightDeadlineSlack is the safety multiplier on the estimated trip length.
	// The ~50% buffer absorbs GameClock forward-drift and reconnect stalls, both
	// of which cost real ticks we cannot predict at accept time.
	freightDeadlineSlack = 1.5

	// freightMinNet is the net floor a contract must clear, mirroring
	// missionMinNet so freight and missions are ranked on the same scale.
	//
	// WARNING: the only observed carrier_payout is 100, from a self-shipped
	// smoke contract; real NPC freight rewards are unmeasured. If they cluster
	// low, this floor rejects the whole board. freightCandidate logs the net of
	// every rejected candidate so one canary pass reveals the true distribution.
	freightMinNet = 500.0
)

// freightCand is an eligible freight listing with derived routing and economics,
// scored on the same scale as missionCandidate so the docked pass can rank them
// against each other.
type freightCand struct {
	Contract   serverapi.ShipmentContract
	DestBaseID string
	Hops       int
	Reward     float64
	FuelCost   float64
	Net        float64 // Reward - FuelCost
}

// freightPackagesFit reports how many whole sealed packages a hold can carry.
// The footprint is a constant, so this is knowable before any server call — it
// is the ship-capability precondition for freight. v1 acts only on >= 1; the
// count is what a future multi-package trip design will consume.
func freightPackagesFit(cargoFree float64) int {
	if cargoFree < freightPackageFootprint {
		return 0
	}
	return int(math.Floor(cargoFree / freightPackageFootprint))
}

// buildFreightCand derives economics for one listing. A non-empty reason means
// skip, and is logged verbatim so a canary pass shows why the board emptied out.
func buildFreightCand(l serverapi.ShippingListing, hops int, fuelCostFor func(jumps int) float64) (freightCand, string) {
	if !l.Eligible {
		reason := l.Reason
		if reason == "" {
			reason = "server marked ineligible"
		}
		return freightCand{}, reason
	}
	if l.Contract.DestinationBaseID == "" {
		return freightCand{}, "no destination_base_id"
	}
	reward := float64(l.Contract.BaseReward)
	fuel := 0.0
	if fuelCostFor != nil {
		fuel = fuelCostFor(hops)
	}
	// max_speed_bonus is deliberately excluded: it is upside, never a reason to
	// take a contract whose base reward does not stand on its own.
	net := reward - fuel
	if net < freightMinNet {
		return freightCand{}, fmt.Sprintf("net %.0f below floor %.0f (reward %.0f, fuel %.0f)", net, freightMinNet, reward, fuel)
	}
	return freightCand{
		Contract:   l.Contract,
		DestBaseID: l.Contract.DestinationBaseID,
		Hops:       hops,
		Reward:     reward,
		FuelCost:   fuel,
		Net:        net,
	}, ""
}

// selectFreightCand returns the highest-net candidate, or nil when there are none.
func selectFreightCand(cands []freightCand) *freightCand {
	var best *freightCand
	for i := range cands {
		if best == nil || cands[i].Net > best.Net {
			best = &cands[i]
		}
	}
	return best
}

// freightDeadlineOK reports whether the remaining tick window covers the
// estimated trip with slack. Runs POST-accept: a posted listing carries no
// deadline_tick — the server sets it at accept — so this cannot gate acceptance.
// Fails closed on a missing deadline: an unprovable deadline is a breach waiting
// to happen, and `return` is free.
func freightDeadlineOK(hops int, deadlineTick, nowTick int64) (bool, string) {
	if deadlineTick <= 0 {
		return false, "contract carries no deadline_tick"
	}
	remaining := deadlineTick - nowTick
	needed := float64(hops) * freightTicksPerHop * freightDeadlineSlack
	if float64(remaining) < needed {
		return false, fmt.Sprintf("deadline %d ticks < needed %.0f (%d hops)", remaining, needed, hops)
	}
	return true, ""
}
