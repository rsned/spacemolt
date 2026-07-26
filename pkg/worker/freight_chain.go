package worker

// freight_chain.go — pure chain math for multi-package freight trips
// (sub-project C). No I/O and no injected deps: everything here is
// unit-testable with literals.

import (
	"fmt"
	"sort"
)

// chainStop is one destination in a (prospective) delivery chain, priced
// from the CURRENT dock. DeadlineTick 0 means "not known yet" — a board
// candidate whose deadline the server only sets at accept time.
type chainStop struct {
	ContractID   string
	DestBaseID   string
	Hops         int
	DeadlineTick int64
}

// chainOrder returns the visiting order the feasibility bound assumes:
// nearest-first by hops, then earliest deadline, then contract id — fully
// deterministic so repeated passes and tests agree.
func chainOrder(stops []chainStop) []chainStop {
	out := make([]chainStop, len(stops))
	copy(out, stops)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Hops != out[j].Hops {
			return out[i].Hops < out[j].Hops
		}
		if out[i].DeadlineTick != out[j].DeadlineTick {
			return out[i].DeadlineTick < out[j].DeadlineTick
		}
		return out[i].ContractID < out[j].ContractID
	})
	return out
}

// chainCumulative returns worst-case cumulative hops to each stop of an
// ORDERED chain. The router prices destinations only from the current dock,
// so a leg between successive stops is bounded by the round trip through
// here: hops(d_i, d_{i+1}) <= h_i + h_{i+1}. Cumulative to stop i is then
// 2*(h_1+...+h_{i-1}) + h_i. Sound (never under-estimates) — accepts fail
// closed, and every later dock re-prices with fresh h values, so the bound
// only tightens as the chain progresses.
func chainCumulative(ordered []chainStop) []int {
	cum := make([]int, len(ordered))
	prefix := 0
	for i, s := range ordered {
		cum[i] = 2*prefix + s.Hops
		prefix += s.Hops
	}
	return cum
}

// chainFeasible reports whether every stop with a known deadline clears the
// worst-case bound at its chain position. Stops with DeadlineTick <= 0 are
// skipped: freightAccept re-runs this with the server-assigned deadline the
// moment it exists.
func chainFeasible(stops []chainStop, nowTick int64) (bool, string) {
	return chainFeasibleAfterDetour(stops, nowTick, 0)
}

// chainFeasibleAfterDetour is chainFeasible with a detour of detourHops flown
// FIRST, before any delivery — the fly-home-and-return recovery for a package
// the server will only take back at its origin. Inserting the detour as stop
// zero of the ordered chain adds exactly 2*detourHops to every cumulative
// bound (same round-trip-through-here argument as chainCumulative), so a
// detour of 0 reduces to chainFeasible identically.
//
// This is what stops a doomed package from taking healthy ones with it: the
// trip home is not free, and on a multi-package carrier it can push the rest
// of the hold past their own deadlines.
func chainFeasibleAfterDetour(stops []chainStop, nowTick int64, detourHops int) (bool, string) {
	ordered := chainOrder(stops)
	cum := chainCumulative(ordered)
	for i, s := range ordered {
		if s.DeadlineTick <= 0 {
			continue
		}
		needed := float64(2*detourHops+cum[i]) * freightTicksPerHop * freightDeadlineSlack
		if float64(s.DeadlineTick-nowTick) < needed {
			if detourHops > 0 {
				return false, fmt.Sprintf("a %d-hop detour leaves %s at position %d needing %.0f ticks, has %d",
					detourHops, s.ContractID, i+1, needed, s.DeadlineTick-nowTick)
			}
			return false, fmt.Sprintf("chain bound: %s at position %d needs %.0f ticks, has %d",
				s.ContractID, i+1, needed, s.DeadlineTick-nowTick)
		}
	}
	return true, ""
}

// chainTotalBound is the worst-case total hops to clear the whole set.
func chainTotalBound(stops []chainStop) int {
	cum := chainCumulative(chainOrder(stops))
	if len(cum) == 0 {
		return 0
	}
	return cum[len(cum)-1]
}

// chainMarginalHops prices a candidate by the hops it ADDS to the chain —
// the fuel a bundled contract actually costs, replacing v1's flat
// origin->destination pricing. An empty held set degenerates to cand.Hops,
// which is exactly the v1 number.
func chainMarginalHops(held []chainStop, cand chainStop) int {
	with := make([]chainStop, 0, len(held)+1)
	with = append(with, held...)
	with = append(with, cand)
	return chainTotalBound(with) - chainTotalBound(held)
}
