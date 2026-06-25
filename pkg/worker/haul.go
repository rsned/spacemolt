package worker

import (
	"context"
	"fmt"
	"io"
	"math"
	"sort"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/market"
	"github.com/rsned/spacemolt/pkg/navigation"
)

// DefaultHaulPoolLimit caps how many available opportunities a hauler considers.
const DefaultHaulPoolLimit = 50

// haulNearTieFraction: opportunities within this fraction of the top gross profit
// are reordered by proximity/chaining rather than raw profit.
const haulNearTieFraction = 0.10

// Pre-buy profit gate (see docs/superpowers/specs/2026-06-25-hauler-prebuy-profit-gate.md).
// At buy time the live spread must clear BOTH thresholds or the buy is skipped and the
// row is left claimed. Guards against acting on a stale/collapsed spread (the trader-3
// loss, 2026-06-24: bought 2654, sold 2631).
const (
	haulMinMargin    = 0.03   // (sellBid-liveAsk)/liveAsk
	haulMinNetProfit = 1000.0 // (sellBid-liveAsk)*qty
)

// haulGate decides whether to execute a claimed haul given freshly-read station
// prices. It takes the live ask at the buy station and the latest bid at the sell
// station, sizes the buy on the live ask, and requires the live spread to clear BOTH
// haulMinMargin and haulMinNetProfit. ok is false (with a human-readable reason) when
// a price is missing, the buy is unaffordable, or the spread is too thin.
func haulGate(opp market.ArbitrageOpportunity, prices []market.ItemStationPrice, cargoFree, credits float64) (qty, liveAsk, sellBid float64, ok bool, reason string) {
	var haveAsk, haveBid bool
	for _, p := range prices {
		if p.StationID == opp.FromStationID && p.HasSell {
			liveAsk, haveAsk = p.BestAsk, true
		}
		if p.StationID == opp.ToStationID && p.HasBuy {
			sellBid, haveBid = p.BestBid, true
		}
	}
	if !haveAsk || liveAsk <= 0 {
		return 0, liveAsk, sellBid, false, "no live ask at buy station"
	}
	if !haveBid {
		return 0, liveAsk, sellBid, false, "no live bid at sell station"
	}
	qty = sizeBuy(opp, cargoFree, credits, liveAsk)
	if qty < 1 {
		return qty, liveAsk, sellBid, false, fmt.Sprintf("unaffordable/no cargo (qty=%.0f)", qty)
	}
	margin := (sellBid - liveAsk) / liveAsk
	net := (sellBid - liveAsk) * qty
	if margin < haulMinMargin || net < haulMinNetProfit {
		return qty, liveAsk, sellBid, false, fmt.Sprintf("spread too thin (margin=%.1f%%, net=%.0f)", margin*100, net)
	}
	return qty, liveAsk, sellBid, true, ""
}

// buildNameToID maps a system reference (as carried by arbitrage rows) to a system
// id from the KB; the jump graph keys on ids. market.db's stations.system_name is
// inconsistent — some rows store the display name ("Sol"), others the id form
// ("alpha_centauri") — so every system is indexed under BOTH its display name and
// its id (id→id is identity), letting an opportunity resolve either way. Last write
// wins on the (rare) duplicate key.
func buildNameToID(systems []knowledge.System) map[string]string {
	m := make(map[string]string, len(systems)*2)
	for _, s := range systems {
		if s.ID != "" {
			m[s.ID] = s.ID
		}
		if s.Name != "" {
			m[s.Name] = s.ID
		}
	}
	return m
}

// rankedOpp pairs an opportunity with its resolved routing facts.
type rankedOpp struct {
	opp       market.ArbitrageOpportunity
	buySysID  string
	sellSysID string // "" if unresolved
	jumps     int    // current -> buySys
	chain     bool   // sellSys at/adjacent to another opp's buySys
}

// RankHaulOpportunities orders available opportunities best-first for a hauler at
// currentSystemID. Primary order is gross_profit descending; opportunities within
// haulNearTieFraction of the top gross are instead ordered by reposition cost
// (jumps current->buy), then a chaining bonus (sell at/adjacent to another opp's
// buy), then id. Opportunities whose buy-system name does not resolve to a known
// system id, or whose buy-system is unreachable, are dropped.
func RankHaulOpportunities(opps []market.ArbitrageOpportunity, currentSystemID string, nameToID map[string]string, graph navigation.JumpGraph) []market.ArbitrageOpportunity {
	resolved := make([]rankedOpp, 0, len(opps))
	buyTargets := make([]string, 0, len(opps))
	for _, o := range opps {
		buyID, ok := nameToID[o.FromSystemName]
		if !ok || buyID == "" {
			continue // can't route to the buy station
		}
		resolved = append(resolved, rankedOpp{opp: o, buySysID: buyID, sellSysID: nameToID[o.ToSystemName]})
		buyTargets = append(buyTargets, buyID)
	}
	if len(resolved) == 0 {
		return nil
	}

	dist := navigation.BFSJumps(graph, currentSystemID, buyTargets)

	reach := make([]rankedOpp, 0, len(resolved))
	for _, r := range resolved {
		d, ok := dist[r.buySysID]
		if !ok || d >= navigation.RouteInf {
			continue
		}
		r.jumps = d
		reach = append(reach, r)
	}
	if len(reach) == 0 {
		return nil
	}

	for i := range reach {
		reach[i].chain = sellChains(reach[i], reach, graph)
	}

	maxGross := 0.0
	for _, r := range reach {
		if r.opp.GrossProfit > maxGross {
			maxGross = r.opp.GrossProfit
		}
	}
	threshold := maxGross * (1 - haulNearTieFraction)

	band := make([]rankedOpp, 0, len(reach))
	rest := make([]rankedOpp, 0, len(reach))
	for _, r := range reach {
		if r.opp.GrossProfit >= threshold {
			band = append(band, r)
		} else {
			rest = append(rest, r)
		}
	}

	sort.SliceStable(band, func(i, j int) bool {
		if band[i].jumps != band[j].jumps {
			return band[i].jumps < band[j].jumps
		}
		if band[i].chain != band[j].chain {
			return band[i].chain // chaining opp sorts first
		}
		return band[i].opp.ID < band[j].opp.ID
	})
	sort.SliceStable(rest, func(i, j int) bool {
		if rest[i].opp.GrossProfit != rest[j].opp.GrossProfit {
			return rest[i].opp.GrossProfit > rest[j].opp.GrossProfit
		}
		if rest[i].jumps != rest[j].jumps {
			return rest[i].jumps < rest[j].jumps
		}
		return rest[i].opp.ID < rest[j].opp.ID
	})

	out := make([]market.ArbitrageOpportunity, 0, len(reach))
	for _, r := range band {
		out = append(out, r.opp)
	}
	for _, r := range rest {
		out = append(out, r.opp)
	}
	return out
}

// sellChains reports whether r's sell-system is at or within one jump of any OTHER
// opportunity's buy-system (so the next run starts near r's drop-off).
func sellChains(r rankedOpp, all []rankedOpp, graph navigation.JumpGraph) bool {
	if r.sellSysID == "" {
		return false
	}
	for _, other := range all {
		if other.opp.ID == r.opp.ID {
			continue
		}
		if other.buySysID == r.sellSysID {
			return true
		}
		for _, nb := range graph[r.sellSysID] {
			if nb == other.buySysID {
				return true
			}
		}
	}
	return false
}

// sizeBuy returns how many units to buy: the opportunity quantity, capped by free
// cargo space and by what credits afford at askEach. Returns 0 when nothing is
// affordable or askEach is non-positive.
// TODO(logistics): cargoFree is treated as a unit count; per-unit item volume
// is not modeled (deferred with the spec's logistics-realism phase).
func sizeBuy(opp market.ArbitrageOpportunity, cargoFree, credits, askEach float64) float64 {
	if askEach <= 0 {
		return 0
	}
	qty := opp.Quantity
	if cargoFree < qty {
		qty = cargoFree
	}
	if affordable := math.Floor(credits / askEach); affordable < qty {
		qty = affordable
	}
	if qty < 0 {
		qty = 0
	}
	return qty
}

// OpportunityStore is the subset of *market.Collector the hauler needs. Defining it
// here keeps the engine testable with a fake and leaves pkg/market unmodified.
type OpportunityStore interface {
	GetOpportunities(ctx context.Context, status string, limit int) ([]market.ArbitrageOpportunity, error)
	ClaimOpportunity(ctx context.Context, id int, agentID string) (bool, error)
	CompleteOpportunity(ctx context.Context, id int, agentID string) (bool, error)
	ScanArbitrage(ctx context.Context, opts market.ScanOptions) (market.ScanResult, error)
	GetItemStationPrices(ctx context.Context, itemID string) ([]market.ItemStationPrice, error)
}

// loadAvailable returns available opportunities, running one ScanArbitrage to
// refresh the pool when it is empty (haulers are the periodic scan trigger). Scan
// uses default options; it is idempotent under the write lock, so a redundant scan
// from concurrent haulers is harmless.
func loadAvailable(ctx context.Context, store OpportunityStore, limit int) ([]market.ArbitrageOpportunity, error) {
	opps, err := store.GetOpportunities(ctx, "available", limit)
	if err != nil {
		return nil, fmt.Errorf("haul: get opportunities: %w", err)
	}
	if len(opps) > 0 {
		return opps, nil
	}
	// Zero-value options inherit the scanner's defaults (MinProfit/MinPrice/Limit);
	// hauler thresholds track whatever the scanner team sets.
	if _, err := store.ScanArbitrage(ctx, market.ScanOptions{}); err != nil {
		return nil, fmt.Errorf("haul: scan arbitrage: %w", err)
	}
	opps, err = store.GetOpportunities(ctx, "available", limit)
	if err != nil {
		return nil, fmt.Errorf("haul: get opportunities (post-scan): %w", err)
	}
	return opps, nil
}

// claimBest claims the first opportunity in ranked order still available. ok=false
// means every candidate was taken by another hauler first.
func claimBest(ctx context.Context, store OpportunityStore, ranked []market.ArbitrageOpportunity, agentID string) (market.ArbitrageOpportunity, bool, error) {
	for _, o := range ranked {
		ok, err := store.ClaimOpportunity(ctx, o.ID, agentID)
		if err != nil {
			return market.ArbitrageOpportunity{}, false, fmt.Errorf("haul: claim %d: %w", o.ID, err)
		}
		if ok {
			return o, true, nil
		}
	}
	return market.ArbitrageOpportunity{}, false, nil
}

// compile-time check that the real collector satisfies the engine's store.
var _ OpportunityStore = (*market.Collector)(nil)

// HaulDeps are the injected collaborators for one Haul step.
type HaulDeps struct {
	Client    game.GameClient
	KB        knowledge.Base
	Market    OpportunityStore
	Out       io.Writer // nil -> io.Discard
	AgentID   string    // claim owner
	PoolLimit int       // 0 -> DefaultHaulPoolLimit
	// RecaptureBuyMarket refreshes the current (buy) station's market into the store
	// so the pre-buy gate prices against a real-time ask. nil skips the live capture
	// (tests seed prices directly via the store).
	RecaptureBuyMarket func(ctx context.Context) error
}

// Haul performs one hauling step: load available opportunities (scanning if the
// pool is empty), rank them for the current system, claim the best reachable one,
// and run it (buy -> transit -> sell -> complete). On any mid-run failure it logs
// and returns nil so the worker idles and retries; the claimed row is left claimed
// (harmless — the spread regenerates on the next scan).
func Haul(ctx context.Context, deps HaulDeps) error {
	out := deps.Out
	if out == nil {
		out = io.Discard
	}
	if deps.Market == nil {
		fmt.Fprintln(out, "haul: market collector not configured; skipping") //nolint:errcheck
		return nil
	}
	if deps.KB == nil {
		fmt.Fprintln(out, "haul: no knowledge base; skipping") //nolint:errcheck
		return nil
	}
	limit := deps.PoolLimit
	if limit <= 0 {
		limit = DefaultHaulPoolLimit
	}
	state := deps.Client.GetState()
	if state == nil || state.System.ID == "" {
		fmt.Fprintln(out, "haul: current system unknown; skipping") //nolint:errcheck
		return nil
	}
	current := state.System.ID

	opps, err := loadAvailable(ctx, deps.Market, limit)
	if err != nil {
		return err
	}
	if len(opps) == 0 {
		fmt.Fprintln(out, "haul: no opportunities available; idling") //nolint:errcheck
		return nil
	}

	systems, err := deps.KB.GetSystems(ctx)
	if err != nil {
		return fmt.Errorf("haul: get systems: %w", err)
	}
	conns, err := deps.KB.GetConnections(ctx)
	if err != nil {
		return fmt.Errorf("haul: get connections: %w", err)
	}
	nameToID := buildNameToID(systems)
	graph := navigation.JumpGraphFromConnections(conns)

	ranked := RankHaulOpportunities(opps, current, nameToID, graph)
	if len(ranked) == 0 {
		fmt.Fprintln(out, "haul: no reachable opportunities; idling") //nolint:errcheck
		return nil
	}

	opp, ok, err := claimBest(ctx, deps.Market, ranked, deps.AgentID)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintln(out, "haul: all candidates already claimed; idling") //nolint:errcheck
		return nil
	}

	return runClaimedHaul(ctx, deps, out, opp, nameToID)
}

// runClaimedHaul executes a claimed opportunity end to end. Any error is logged and
// swallowed (returns nil) so the worker stays alive; the row is left claimed.
// Before buying, it re-prices both legs against fresh data (a live recapture of the
// buy station + the latest sell-station bid) and applies haulGate: the buy is skipped
// unless the live spread clears both the margin and net-profit thresholds. This guards
// against acting on a stale/collapsed opportunity.
func runClaimedHaul(ctx context.Context, deps HaulDeps, out io.Writer, opp market.ArbitrageOpportunity, nameToID map[string]string) error {
	buySys := nameToID[opp.FromSystemName]
	sellSys := nameToID[opp.ToSystemName]
	if buySys == "" {
		fmt.Fprintf(out, "haul: opp %d buy system %q unresolved; leaving claimed\n", opp.ID, opp.FromSystemName) //nolint:errcheck
		return nil
	}
	if sellSys == "" {
		fmt.Fprintf(out, "haul: opp %d sell system %q unresolved; leaving claimed\n", opp.ID, opp.ToSystemName) //nolint:errcheck
		return nil
	}
	fmt.Fprintf(out, "haul: opp %d %s: buy %.0f @%s -> sell @%s\n", opp.ID, opp.ItemID, opp.Quantity, opp.FromStationName, opp.ToStationName) //nolint:errcheck

	if err := haulAutopilot(ctx, deps, out, buySys, opp.FromStationID); err != nil {
		fmt.Fprintf(out, "haul: opp %d transit to buy failed: %v; leaving claimed\n", opp.ID, err) //nolint:errcheck
		return nil
	}
	state := deps.Client.GetState()
	if state == nil {
		fmt.Fprintf(out, "haul: opp %d no state at buy station; leaving claimed\n", opp.ID) //nolint:errcheck
		return nil
	}
	cargoFree := state.Ship.CargoCapacity - state.Ship.CargoUsed

	// Re-price against fresh data before committing: refresh the buy station market
	// live (best-effort — the hauler is standing here, so this is real-time and does
	// not depend on marketbot cadence), then gate on the live spread.
	if deps.RecaptureBuyMarket != nil {
		if rerr := deps.RecaptureBuyMarket(ctx); rerr != nil {
			fmt.Fprintf(out, "haul: opp %d buy-market recapture failed: %v; pricing on last capture\n", opp.ID, rerr) //nolint:errcheck
		}
	}
	prices, perr := deps.Market.GetItemStationPrices(ctx, opp.ItemID)
	if perr != nil {
		fmt.Fprintf(out, "haul: opp %d price check failed: %v; leaving claimed\n", opp.ID, perr) //nolint:errcheck
		return nil
	}
	qty, _, _, pass, reason := haulGate(opp, prices, cargoFree, state.GetCredits())
	if !pass {
		fmt.Fprintf(out, "haul: opp %d %s; leaving claimed\n", opp.ID, reason) //nolint:errcheck
		return nil
	}
	if err := deps.Client.Buy(ctx, opp.ItemID, qty); err != nil {
		fmt.Fprintf(out, "haul: opp %d buy failed: %v; leaving claimed\n", opp.ID, err) //nolint:errcheck
		return nil
	}

	if err := haulAutopilot(ctx, deps, out, sellSys, opp.ToStationID); err != nil {
		fmt.Fprintf(out, "haul: opp %d transit to sell failed: %v; leaving claimed\n", opp.ID, err) //nolint:errcheck
		return nil
	}
	held := cargoQty(deps.Client.GetState(), opp.ItemID)
	if held <= 0 {
		fmt.Fprintf(out, "haul: opp %d nothing in cargo to sell; leaving claimed\n", opp.ID) //nolint:errcheck
		return nil
	}
	if err := deps.Client.Sell(ctx, opp.ItemID, held); err != nil {
		fmt.Fprintf(out, "haul: opp %d sell failed: %v; leaving claimed\n", opp.ID, err) //nolint:errcheck
		return nil
	}

	// Goods are already sold here; a CompleteOpportunity failure only leaves the
	// row 'claimed' (no success log). Harmless — the Phase-5b sweeper reclaims it.
	if _, err := deps.Market.CompleteOpportunity(ctx, opp.ID, deps.AgentID); err != nil {
		fmt.Fprintf(out, "haul: opp %d complete failed: %v\n", opp.ID, err) //nolint:errcheck
		return nil
	}
	fmt.Fprintf(out, "haul: opp %d complete (sold %.0f %s)\n", opp.ID, held, opp.ItemID) //nolint:errcheck
	return nil
}

// haulAutopilot routes to a station POI within a system, capturing each hop to the KB.
func haulAutopilot(ctx context.Context, deps HaulDeps, out io.Writer, system, poi string) error {
	return Autopilot(ctx, AutopilotDeps{
		Client: deps.Client,
		Out:    out,
		OnWaypoint: func(ctx context.Context) error {
			if deps.KB == nil {
				return nil
			}
			if err := KBUpdateSystem(ctx, deps.Client, deps.KB, ""); err != nil {
				return err
			}
			return KBUpdatePOI(ctx, deps.Client, deps.KB, "")
		},
	}, system, poi)
}

// cargoQty returns how many units of itemID are in the ship's cargo (0 if none).
func cargoQty(state *game.State, itemID string) float64 {
	if state == nil {
		return 0
	}
	for _, c := range state.Ship.Cargo {
		if c.ItemID == itemID {
			return c.Quantity
		}
	}
	return 0
}
