package worker

import (
	"sort"

	"github.com/rsned/spacemolt/pkg/market"
)

// watchdogAction is the chosen reaction when a haul reaches its sell stop.
type watchdogAction int

const (
	sellAtMarket  watchdogAction = iota // proceed with the normal market sell
	postCostOrder                       // demand too thin: list at cost instead of dumping
)

// watchdogConfig holds the tunable thresholds; the zero value uses defaults.
type watchdogConfig struct {
	// MaxSellLossFrac tolerates a market sell losing up to this fraction of the
	// buy cost; beyond it, list the cargo at cost instead of realizing the loss.
	MaxSellLossFrac float64
}

func (c watchdogConfig) defaulted() watchdogConfig {
	if c.MaxSellLossFrac <= 0 {
		c.MaxSellLossFrac = 0.10
	}
	return c
}

// absorbableProceeds sums what a sale of up to heldQty units would earn against
// the destination's live BUY book: highest bid first, taking min(remaining, qty)
// at each price. Only side=="buy" orders (demand) count.
func absorbableProceeds(orders []market.Order, heldQty float64) float64 {
	bids := make([]market.Order, 0, len(orders))
	for _, o := range orders {
		if o.Side == "buy" && o.Quantity > 0 && o.PriceEach > 0 {
			bids = append(bids, o)
		}
	}
	sort.Slice(bids, func(i, j int) bool { return bids[i].PriceEach > bids[j].PriceEach })
	remaining, proceeds := heldQty, 0.0
	for _, b := range bids {
		if remaining <= 0 {
			break
		}
		take := b.Quantity
		if take > remaining {
			take = remaining
		}
		proceeds += take * b.PriceEach
		remaining -= take
	}
	return proceeds
}

// arrivalDecision picks the reaction when a hauler has arrived at its claimed sell
// station holding heldQty units bought for buyCostPaid total. If the live demand
// can't absorb the cargo without a loss beyond tolerance (or has no buyers), it
// returns postCostOrder; otherwise sellAtMarket.
func arrivalDecision(orders []market.Order, heldQty, buyCostPaid float64, cfg watchdogConfig) watchdogAction {
	cfg = cfg.defaulted()
	proceeds := absorbableProceeds(orders, heldQty)
	if proceeds <= 0 {
		return postCostOrder
	}
	if proceeds < buyCostPaid*(1-cfg.MaxSellLossFrac) {
		return postCostOrder
	}
	return sellAtMarket
}
