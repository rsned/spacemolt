package worker

import (
	"sort"

	"github.com/rsned/spacemolt/pkg/market"
	"github.com/rsned/spacemolt/pkg/navigation"
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

// rerouteConfig tunes the find-a-market re-route decision. The zero value uses defaults.
type rerouteConfig struct {
	// MaxExtraJumps caps how far (in jumps from the current system) a re-route
	// destination may be. 0 -> default of 5.
	MaxExtraJumps int
	// FuelPerJumpCost is an approximate credit cost charged per extra jump to the
	// candidate, penalizing distant re-routes. 0 -> no fuel penalty (the margin guards).
	FuelPerJumpCost float64
	// MarginFrac and MarginFloor set how much a re-route must beat continuing by: at
	// least max(MarginFrac*buyCost, MarginFloor). MarginFrac 0 -> default 0.15.
	MarginFrac  float64
	MarginFloor float64
}

func (c rerouteConfig) defaulted() rerouteConfig {
	if c.MaxExtraJumps <= 0 {
		c.MaxExtraJumps = 5
	}
	if c.MarginFrac <= 0 {
		c.MarginFrac = 0.15
	}
	return c
}

// bestReroute picks the best alternative buyer for held units (bought at unitBuy each)
// among live buy-side prices, given precomputed jump distances from the current system.
// A candidate qualifies only when it is reachable within the jump budget and its projected
// net — proceeds (qty-capped) minus buy cost minus extra-fuel penalty — beats continuing to
// the original destination (continueNet) by at least max(MarginFrac*buyCost, MarginFloor).
// Returns the highest-net qualifying candidate, or ok=false when none clears the bar. Pure;
// also reusable for stale-cargo "find-a-market" liquidation.
func bestReroute(held, unitBuy float64, prices []market.BestPrice, jumps map[string]int, continueNet float64, cfg rerouteConfig) (market.BestPrice, bool) {
	cfg = cfg.defaulted()
	buyCost := unitBuy * held
	margin := cfg.MarginFrac * buyCost
	if cfg.MarginFloor > margin {
		margin = cfg.MarginFloor
	}
	threshold := continueNet + margin

	var best market.BestPrice
	bestNet := threshold // a candidate must strictly beat the threshold
	found := false
	for _, p := range prices {
		if p.ListingType != "buy" || p.Price <= 0 || p.Quantity <= 0 {
			continue
		}
		j, ok := jumps[p.SystemID]
		if !ok || j >= navigation.RouteInf || j > cfg.MaxExtraJumps {
			continue
		}
		proceeds := p.Price * min(held, p.Quantity)
		net := proceeds - buyCost - float64(j)*cfg.FuelPerJumpCost
		if net > bestNet {
			best, bestNet, found = p, net, true
		}
	}
	return best, found
}
