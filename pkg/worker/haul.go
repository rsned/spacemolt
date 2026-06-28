package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/market"
	"github.com/rsned/spacemolt/pkg/navigation"
)

// DefaultHaulPoolLimit caps how many available opportunities a hauler considers.
// GetOpportunities orders by gross_profit DESC, so a small limit returns only the
// highest-gross (and typically most distant) opportunities. With a reposition-distance
// cap (DefaultHaulMaxJumps), nearby-but-lower-gross opportunities must be in the pool or
// the cap would drop the whole pool and the hauler would idle — so the pool is generous.
const DefaultHaulPoolLimit = 500

// DefaultHaulMaxJumps caps how far (in jumps from the hauler's current system) a buy
// station may be for its opportunity to be considered. Repositioning halfway across the
// galaxy for one large payday wastes more time and fuel than several nearby hauls of
// comparable margin, so the fleet nets more from many short hops. 0 in a request means
// "use this default"; the pure ranker treats maxJumps<=0 as no cap (for tests).
const DefaultHaulMaxJumps = 5

// haulNearTieFraction: opportunities within this fraction of the top gross profit
// are reordered by proximity/chaining rather than raw profit.
const haulNearTieFraction = 0.10

// Stability boost: a route that has appeared in several consecutive scan cycles is
// durable supply/demand and far likelier to still be there on arrival, so it gets a
// bounded boost to its ranking gross. This is a BOOST, not a filter — a one-shot
// spread still competes, just from behind. Each consecutive cycle adds
// haulStabilityPerCycle, capped at haulStabilityCap cycles (so a streak can lift
// ranking gross by at most +50%, never dwarfing a much larger fresh spread).
const (
	haulStabilityPerCycle = 0.10
	haulStabilityCap      = 5
)

// stabilityBoost maps a cycles_seen streak to a ranking-gross multiplier (1.0 for a
// first sighting, up to 1.5 for a streak at or beyond the cap).
func stabilityBoost(cyclesSeen int) float64 {
	n := cyclesSeen - 1
	if n < 0 {
		n = 0
	}
	if n > haulStabilityCap {
		n = haulStabilityCap
	}
	return 1 + float64(n)*haulStabilityPerCycle
}

// effectiveGross is the gross profit used for ranking: raw gross lifted by the route's
// stability streak. Selection-time only — it never changes the credits actually earned.
func effectiveGross(o market.ArbitrageOpportunity) float64 {
	return o.GrossProfit * stabilityBoost(o.CyclesSeen)
}

// Pre-buy profit gate (see docs/superpowers/specs/2026-06-25-hauler-prebuy-profit-gate.md).
// At buy time the live spread must clear BOTH thresholds or the buy is skipped and the
// row is left claimed. Guards against acting on a stale/collapsed spread (the trader-3
// loss, 2026-06-24: bought 2654, sold 2631).
const (
	haulMinMargin    = 0.03   // (sellBid-liveAsk)/liveAsk
	haulMinNetProfit = 1000.0 // (sellBid-liveAsk)*qty
	// Small-hold ships (CargoCapacity below haulSmallHoldCap) can't move enough units to
	// clear haulMinNetProfit even on fat margins, so they idle on otherwise-good deals
	// (full-log gate analysis 2026-06-26: ~343 rejects were fat margin / net<1000). Until
	// cargo expanders raise their holds, they clear a reduced floor so they close trades.
	haulSmallHoldCap       = 100.0
	haulSmallHoldNetProfit = 250.0
)

// netProfitFloor returns the minimum net profit a haul must clear, reduced for
// small-hold ships that cannot physically move enough units to make haulMinNetProfit.
func netProfitFloor(cargoCap float64) float64 {
	if cargoCap > 0 && cargoCap < haulSmallHoldCap {
		return haulSmallHoldNetProfit
	}
	return haulMinNetProfit
}

// haulGate decides whether to execute a claimed haul given freshly-read station
// prices. It takes the live ask at the buy station and the latest bid at the sell
// station, sizes the buy on the live ask, and requires the live spread to clear BOTH
// haulMinMargin and haulMinNetProfit. ok is false (with a human-readable reason) when
// a price is missing, the buy is unaffordable, or the spread is too thin.
func haulGate(opp market.ArbitrageOpportunity, prices []market.ItemStationPrice, cargoFree, cargoCap, credits float64) (qty, liveAsk, sellBid float64, ok bool, reason string) {
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
	floor := netProfitFloor(cargoCap)
	if margin < haulMinMargin || net < floor {
		return qty, liveAsk, sellBid, false, fmt.Sprintf("spread too thin (margin=%.1f%%, net=%.0f, floor=%.0f)", margin*100, net, floor)
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
// system id, or whose buy-system is unreachable, are dropped. When maxJumps > 0, any
// opportunity whose buy-system is more than maxJumps from currentSystemID is also
// dropped (maxJumps <= 0 disables the cap).
func RankHaulOpportunities(opps []market.ArbitrageOpportunity, currentSystemID string, nameToID map[string]string, graph navigation.JumpGraph, maxJumps int) []market.ArbitrageOpportunity {
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
		if maxJumps > 0 && d > maxJumps {
			continue // too far to reposition for
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
		if g := effectiveGross(r.opp); g > maxGross {
			maxGross = g
		}
	}
	threshold := maxGross * (1 - haulNearTieFraction)

	band := make([]rankedOpp, 0, len(reach))
	rest := make([]rankedOpp, 0, len(reach))
	for _, r := range reach {
		if effectiveGross(r.opp) >= threshold {
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
		if band[i].opp.CyclesSeen != band[j].opp.CyclesSeen {
			return band[i].opp.CyclesSeen > band[j].opp.CyclesSeen // durable route first
		}
		return band[i].opp.ID < band[j].opp.ID
	})
	sort.SliceStable(rest, func(i, j int) bool {
		if gi, gj := effectiveGross(rest[i].opp), effectiveGross(rest[j].opp); gi != gj {
			return gi > gj
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
	GetClaimedByAgent(ctx context.Context, agentID string) ([]market.ArbitrageOpportunity, error)
	ClaimOpportunity(ctx context.Context, id int, agentID string) (bool, error)
	ReleaseOpportunity(ctx context.Context, id int, agentID string) (bool, error)
	CompleteOpportunity(ctx context.Context, id int, agentID string) (bool, error)
	ScanArbitrage(ctx context.Context, opts market.ScanOptions) (market.ScanResult, error)
	GetItemStationPrices(ctx context.Context, itemID string) ([]market.ItemStationPrice, error)
	GetStationOrders(ctx context.Context, stationID, itemID string) ([]market.Order, error)
	FindBestPrices(ctx context.Context, itemID, side string, limit int) ([]market.BestPrice, error)
	RecordHaulResult(ctx context.Context, r market.HaulResult) error
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
	MaxJumps  int       // 0 -> DefaultHaulMaxJumps; reposition-distance cap (jumps) to a buy station
	// RecaptureBuyMarket refreshes the current (buy) station's market into the store
	// so the pre-buy gate prices against a real-time ask. nil skips the live capture
	// (tests seed prices directly via the store).
	RecaptureBuyMarket func(ctx context.Context) error
	// Now returns the current wall-clock time (nil -> time.Now). Injected for deterministic tests.
	Now func() time.Time
	// Treasury rate-limits faction-treasury rescue withdrawals across haul passes
	// (held per worker process). nil disables the rescue path.
	Treasury *treasuryRescue
}

// haulMetrics accumulates per-leg stamps (wall + game tick) and pricing across one fresh
// haul, for a haul_results row written on completion. A nil *haulMetrics (resumed hauls,
// where earlier legs happened in a prior process) disables recording.
type haulMetrics struct {
	jumps                                   int
	buyPrice, sellPrice, qty                float64
	claimedAt, arrivedSrcAt, boughtAt       time.Time
	arrivedDstAt, soldAt                    time.Time
	claimedTick, arrivedSrcTick, boughtTick int64
	arrivedDstTick, soldTick                int64
}

func haulNow(deps HaulDeps) time.Time {
	if deps.Now != nil {
		return deps.Now()
	}
	return time.Now()
}

func haulTick(deps HaulDeps) int64 {
	if st := deps.Client.GetState(); st != nil {
		return st.GetTick()
	}
	return 0
}

// rfc formats t as a UTC RFC3339 string for a haul_results leg stamp.
func rfc(t time.Time) string { return t.UTC().Format(time.RFC3339) }

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
	maxJumps := deps.MaxJumps
	if maxJumps <= 0 {
		maxJumps = DefaultHaulMaxJumps
	}
	state := deps.Client.GetState()
	if state == nil || state.System.ID == "" {
		fmt.Fprintln(out, "haul: current system unknown; skipping") //nolint:errcheck
		return nil
	}
	current := state.System.ID

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

	// Resume before claiming anything new: if this agent already holds a claim (its
	// process restarted mid-haul, leaving the in-memory active opp behind but the claim
	// row intact), finish that haul first. runClaimedHaul skips the buy leg when the
	// goods are already aboard, so a resumed haul continues to its sell leg. The
	// distance cap does not apply here — once claimed (and possibly bought) the haul is
	// committed regardless of how far the legs are.
	held, err := deps.Market.GetClaimedByAgent(ctx, deps.AgentID)
	if err != nil {
		return fmt.Errorf("haul: get claimed: %w", err)
	}
	if len(held) > 0 {
		fmt.Fprintf(out, "haul: resuming claimed opp %d (%s)\n", held[0].ID, held[0].ItemID) //nolint:errcheck
		return runClaimedHaul(ctx, deps, out, held[0], nameToID, nil)
	}

	// With no active haul, any cargo in the hold is leftover (a prior run's goods) and
	// blocks every buy. Liquidate it first; if it acted this pass, idle and re-evaluate
	// next pass with a freer hold.
	if liquidateCargo(ctx, deps, out) {
		return nil
	}

	// Idle with an empty hold: a low wallet now is genuine fumes (not capital tied
	// up in cargo), so this is the safe point to pull a treasury rescue before the
	// next buy leg.
	deps.Treasury.maybe(ctx, deps.Client, out, haulNow(deps))

	opps, err := loadAvailable(ctx, deps.Market, limit)
	if err != nil {
		return err
	}
	if len(opps) == 0 {
		fmt.Fprintln(out, "haul: no opportunities available; idling") //nolint:errcheck
		return nil
	}

	ranked := RankHaulOpportunities(opps, current, nameToID, graph, maxJumps)
	if len(ranked) == 0 {
		fmt.Fprintf(out, "haul: no opportunities within %d jumps; idling\n", maxJumps) //nolint:errcheck
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

	m := &haulMetrics{claimedAt: haulNow(deps), claimedTick: haulTick(deps)}
	if buySys := nameToID[opp.FromSystemName]; buySys != "" {
		if sellSys := nameToID[opp.ToSystemName]; sellSys != "" {
			toBuy := navigation.BFSJumps(graph, current, []string{buySys})
			toSell := navigation.BFSJumps(graph, buySys, []string{sellSys})
			m.jumps = toBuy[buySys] + toSell[sellSys]
		}
	}
	return runClaimedHaul(ctx, deps, out, opp, nameToID, m)
}

// abandonClaim releases opp's claim back to the available pool and logs reason. Used on
// PRE-buy abandons so an opportunity the hauler decides not to act on (unroutable, gate
// rejected, dock failed, …) does not leak its claim out of the pool. Post-buy abandons
// instead KEEP the claim (see haulSellLeg) so the goods-bearing haul can be resumed.
func abandonClaim(ctx context.Context, deps HaulDeps, out io.Writer, opp market.ArbitrageOpportunity, reason string) error {
	if _, err := deps.Market.ReleaseOpportunity(ctx, opp.ID, deps.AgentID); err != nil {
		fmt.Fprintf(out, "haul: opp %d %s; release failed: %v\n", opp.ID, reason, err) //nolint:errcheck
		return nil
	}
	fmt.Fprintf(out, "haul: opp %d %s; released\n", opp.ID, reason) //nolint:errcheck
	return nil
}

// runClaimedHaul executes a claimed opportunity. Any error is logged and swallowed
// (returns nil) so the worker stays alive. If the goods are already aboard (a haul
// resumed after its process restarted post-buy), it skips the buy leg and goes straight
// to selling. Otherwise it transits to the buy station, re-prices both legs against
// fresh data and applies haulGate (skipping unless the live spread clears both the
// margin and net-profit thresholds), buys, then sells. PRE-buy abandons release the
// claim back to the pool; POST-buy abandons keep it claimed for resumption.
func runClaimedHaul(ctx context.Context, deps HaulDeps, out io.Writer, opp market.ArbitrageOpportunity, nameToID map[string]string, m *haulMetrics) error {
	buySys := nameToID[opp.FromSystemName]
	sellSys := nameToID[opp.ToSystemName]
	if buySys == "" {
		return abandonClaim(ctx, deps, out, opp, fmt.Sprintf("buy system %q unresolved", opp.FromSystemName))
	}
	if sellSys == "" {
		return abandonClaim(ctx, deps, out, opp, fmt.Sprintf("sell system %q unresolved", opp.ToSystemName))
	}

	// Resume: goods already aboard (interrupted after a successful buy) -> skip the buy
	// leg and go straight to delivery, instead of buying a second load.
	if aboard := cargoQty(deps.Client.GetState(), opp.ItemID); aboard > 0 {
		fmt.Fprintf(out, "haul: opp %d %s: resuming with %.0f aboard -> sell @%s\n", opp.ID, opp.ItemID, aboard, opp.ToStationName) //nolint:errcheck
		return haulSellLeg(ctx, deps, out, opp, sellSys, nil)
	}

	fmt.Fprintf(out, "haul: opp %d %s: buy %.0f @%s -> sell @%s\n", opp.ID, opp.ItemID, opp.Quantity, opp.FromStationName, opp.ToStationName) //nolint:errcheck

	if err := haulAutopilot(ctx, deps, out, buySys, opp.FromStationID, nil); err != nil {
		return abandonClaim(ctx, deps, out, opp, fmt.Sprintf("transit to buy failed: %v", err))
	}
	// Autopilot leaves the ship at the station POI but undocked. Dock explicitly so the
	// live recapture (view_market) can run — unlike buy/sell, view_market does not
	// auto-dock, so without this the recapture fails and the gate falls back to stale
	// prices. A station whose POI has no dockable base releases the claim.
	if err := deps.Client.Dock(ctx); err != nil {
		return abandonClaim(ctx, deps, out, opp, fmt.Sprintf("cannot dock at buy station %s: %v", opp.FromStationName, err))
	}
	state := deps.Client.GetState()
	if state == nil {
		return abandonClaim(ctx, deps, out, opp, "no state at buy station")
	}
	if m != nil {
		m.arrivedSrcAt, m.arrivedSrcTick = haulNow(deps), haulTick(deps)
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
		return abandonClaim(ctx, deps, out, opp, fmt.Sprintf("price check failed: %v", perr))
	}
	qty, liveAsk, sellBid, pass, reason := haulGate(opp, prices, cargoFree, state.Ship.CargoCapacity, state.GetCredits())
	if !pass {
		return abandonClaim(ctx, deps, out, opp, reason)
	}
	if err := deps.Client.Buy(ctx, opp.ItemID, qty); err != nil {
		return abandonClaim(ctx, deps, out, opp, fmt.Sprintf("buy failed: %v", err))
	}
	if m != nil {
		m.boughtAt, m.boughtTick = haulNow(deps), haulTick(deps)
		m.buyPrice, m.sellPrice, m.qty = liveAsk, sellBid, qty
		// Prefer the ACTUAL buy fill over the gate's quoted ask when the response carries it.
		if px, ok := actualUnitPrice(deps.Client.GetRawJSON("buy"), "total_cost", "quantity"); ok {
			m.buyPrice = px
		}
	}

	return haulSellLeg(ctx, deps, out, opp, sellSys, m)
}

// haulSellLeg transits to the sell station and sells the goods, completing the
// opportunity. The buy has already happened, so on any failure here the claim is KEPT
// (logged "leaving claimed") — the goods are aboard and the haul can be resumed to retry
// the sell leg on a later pass or after a restart.
func haulSellLeg(ctx context.Context, deps HaulDeps, out io.Writer, opp market.ArbitrageOpportunity, sellSys string, m *haulMetrics) error {
	// Transit to the sell station. A per-jump watchdog checks the claimed destination's live
	// demand en route; if it has thinned below break-even, autopilot stops early and we
	// re-route once to a better live market before riding the rest out (Step 2 reaction).
	rerouted := false
	for {
		var check WaypointCheckFunc
		if !rerouted {
			check = func(ctx context.Context) (bool, error) {
				return haulSellWatchdog(ctx, deps, opp, m), nil
			}
		}
		err := haulAutopilot(ctx, deps, out, sellSys, opp.ToStationID, check)
		if errors.Is(err, errAutopilotStopped) {
			rerouted = true // one re-route attempt max, then ride it out to avoid thrashing
			if newSys, newStn, ok := haulFindReroute(ctx, deps, opp, m); ok {
				fmt.Fprintf(out, "haul: opp %d demand thinned mid-route; re-routing to %s @%s\n", opp.ID, newSys, newStn) //nolint:errcheck
				sellSys, opp.ToStationID = newSys, newStn
			} else {
				fmt.Fprintf(out, "haul: opp %d demand thinned mid-route; no better market, continuing to %s\n", opp.ID, opp.ToStationID) //nolint:errcheck
			}
			continue
		}
		if err != nil {
			fmt.Fprintf(out, "haul: opp %d transit to sell failed: %v; leaving claimed\n", opp.ID, err) //nolint:errcheck
			return nil
		}
		break
	}
	if m != nil {
		m.arrivedDstAt, m.arrivedDstTick = haulNow(deps), haulTick(deps)
	}
	held := cargoQty(deps.Client.GetState(), opp.ItemID)
	if held <= 0 {
		fmt.Fprintf(out, "haul: opp %d nothing in cargo to sell; leaving claimed\n", opp.ID) //nolint:errcheck
		return nil
	}

	// On-arrival demand watchdog: if the destination's live buy book can no longer
	// absorb this cargo near break-even, list it at cost instead of dumping into a
	// market that evaporated mid-transit. Absent order data (nil) is not evidence of
	// dead demand — fall through to the normal sell (spec freshness rule).
	unitBuy := opp.BuyPrice
	if m != nil && m.buyPrice > 0 {
		unitBuy = m.buyPrice
	}
	if orders, oerr := deps.Market.GetStationOrders(ctx, opp.ToStationID, opp.ItemID); oerr != nil {
		fmt.Fprintf(out, "haul: opp %d demand check failed (%v); selling normally\n", opp.ID, oerr) //nolint:errcheck
	} else if haulDestCaptured(ctx, deps, opp.ToStationID, orders) && arrivalDecision(orders, held, unitBuy*held, watchdogConfig{}) == postCostOrder {
		fmt.Fprintf(out, "haul: opp %d demand too thin at %s; listing %.0f %s @cost %.0f\n", opp.ID, opp.ToStationID, held, opp.ItemID, unitBuy) //nolint:errcheck
		return haulPostCostOrder(ctx, deps, out, opp, held, unitBuy)
	}

	if err := deps.Client.Sell(ctx, opp.ItemID, held); err != nil {
		fmt.Fprintf(out, "haul: opp %d sell failed: %v; leaving claimed\n", opp.ID, err) //nolint:errcheck
		return nil
	}
	if m != nil {
		m.soldAt, m.soldTick = haulNow(deps), haulTick(deps)
		// Record the ACTUAL sell fill, not the pre-trade quote: the market may have moved
		// between the gate's price check and this sell, turning a quoted gain into a real
		// loss. Falls back to the quoted sellPrice when the fill is unavailable.
		if px, ok := actualUnitPrice(deps.Client.GetRawJSON("sell"), "total_earned", "quantity_sold"); ok {
			m.sellPrice = px
		}
	}

	// Goods are already sold here; a CompleteOpportunity failure only leaves the
	// row 'claimed' (no success log). Harmless — the Phase-5b sweeper reclaims it.
	if _, err := deps.Market.CompleteOpportunity(ctx, opp.ID, deps.AgentID); err != nil {
		fmt.Fprintf(out, "haul: opp %d complete failed: %v\n", opp.ID, err) //nolint:errcheck
		return nil
	}
	recordHaulResult(ctx, deps, out, opp, held, m)
	fmt.Fprintf(out, "haul: opp %d complete (sold %.0f %s)\n", opp.ID, held, opp.ItemID) //nolint:errcheck
	return nil
}

// haulPostCostOrder lists held cargo at the buy price instead of dumping it into thin
// demand (watchdog Tier 3), then completes the claim so it is not re-hauled. The eventual
// fill is captured by the server action log; no haul_result is recorded here (no sale yet).
// A CreateSellOrder failure leaves the claim in place (best-effort, never strands cargo).
func haulPostCostOrder(ctx context.Context, deps HaulDeps, out io.Writer, opp market.ArbitrageOpportunity, held, unitBuy float64) error {
	payload := map[string]any{
		"item_id":    opp.ItemID,
		"quantity":   int(held),
		"price_each": int(math.Round(unitBuy)),
	}
	if err := deps.Client.CreateSellOrder(ctx, payload); err != nil {
		fmt.Fprintf(out, "haul: opp %d cost-order failed: %v; leaving claimed\n", opp.ID, err) //nolint:errcheck
		return nil
	}
	if _, err := deps.Market.CompleteOpportunity(ctx, opp.ID, deps.AgentID); err != nil {
		fmt.Fprintf(out, "haul: opp %d complete-after-cost-order failed: %v\n", opp.ID, err) //nolint:errcheck
	}
	fmt.Fprintf(out, "haul: opp %d listed %.0f %s @cost %.0f (thin demand)\n", opp.ID, held, opp.ItemID, unitBuy) //nolint:errcheck
	return nil
}

// haulRerouteCandidates caps how many alternative buyer stations the re-route finder
// considers (FindBestPrices limit).
const haulRerouteCandidates = 10

// haulSellWatchdog reports whether the sell-leg autopilot should stop early because the
// claimed destination's live demand can no longer absorb the cargo aboard near break-even.
// Best-effort: missing cargo or order data returns false (do not stop) — matching the
// on-arrival watchdog's freshness rule.
func haulSellWatchdog(ctx context.Context, deps HaulDeps, opp market.ArbitrageOpportunity, m *haulMetrics) bool {
	held := cargoQty(deps.Client.GetState(), opp.ItemID)
	if held <= 0 {
		return false
	}
	orders, err := deps.Market.GetStationOrders(ctx, opp.ToStationID, opp.ItemID)
	if err != nil || !haulDestCaptured(ctx, deps, opp.ToStationID, orders) {
		return false
	}
	unitBuy := opp.BuyPrice
	if m != nil && m.buyPrice > 0 {
		unitBuy = m.buyPrice
	}
	return arrivalDecision(orders, held, unitBuy*held, watchdogConfig{}) == postCostOrder
}

// haulDestCaptured reports whether we have recent market data for stationID: directly (the
// item's book came back non-empty) or, when the item book is empty, by checking the station
// appears in the latest capture at all (a station-wide read). This distinguishes a captured
// station with zero buyers — genuine dead demand, so the watchdog acts — from a station we
// simply have no recent capture for, where the freshness rule says keep selling normally.
func haulDestCaptured(ctx context.Context, deps HaulDeps, stationID string, itemOrders []market.Order) bool {
	if len(itemOrders) > 0 {
		return true
	}
	all, err := deps.Market.GetStationOrders(ctx, stationID, "")
	return err == nil && len(all) > 0
}

// haulFindReroute looks for a better live destination for the cargo aboard when the claimed
// sell market has gone thin mid-transit. It compares candidate buyers (reachable within the
// jump budget) against continuing to the original destination, returning the new (system,
// station) when one clearly wins. ok=false means stay the course. Best-effort: any missing
// input (no KB, no state, empty cargo, no candidates) returns ok=false and never errors.
func haulFindReroute(ctx context.Context, deps HaulDeps, opp market.ArbitrageOpportunity, m *haulMetrics) (sysID, stationID string, ok bool) {
	if deps.KB == nil {
		return "", "", false
	}
	state := deps.Client.GetState()
	if state == nil || state.System.ID == "" {
		return "", "", false
	}
	held := cargoQty(state, opp.ItemID)
	if held <= 0 {
		return "", "", false
	}
	unitBuy := opp.BuyPrice
	if m != nil && m.buyPrice > 0 {
		unitBuy = m.buyPrice
	}
	// What continuing to the original destination would net at its current demand.
	origOrders, _ := deps.Market.GetStationOrders(ctx, opp.ToStationID, opp.ItemID)
	continueNet := absorbableProceeds(origOrders, held) - unitBuy*held

	prices, err := deps.Market.FindBestPrices(ctx, opp.ItemID, "buy", haulRerouteCandidates)
	if err != nil || len(prices) == 0 {
		return "", "", false
	}
	conns, err := deps.KB.GetConnections(ctx)
	if err != nil {
		return "", "", false
	}
	graph := navigation.JumpGraphFromConnections(conns)
	targets := make([]string, 0, len(prices))
	for _, p := range prices {
		targets = append(targets, p.SystemID)
	}
	jumps := navigation.BFSJumps(graph, state.System.ID, targets)
	target, found := bestReroute(held, unitBuy, prices, jumps, continueNet, rerouteConfig{})
	if !found || target.StationID == opp.ToStationID {
		return "", "", false
	}
	return target.SystemID, target.StationID, true
}

// actualUnitPrice extracts the true per-unit fill from a cached buy/sell response: the total
// credits moved (totalKey) divided by the quantity transacted (qtyKey). Returns ok=false when
// the response is absent or unparseable, so callers keep their pre-trade quote. This is what
// makes recorded prices — and therefore realized profit — reflect real fills instead of the
// gate's quote, letting a haul that the market moved against record as the loss it was.
func actualUnitPrice(raw []byte, totalKey, qtyKey string) (float64, bool) {
	if len(raw) == 0 {
		return 0, false
	}
	var resp map[string]any
	if err := json.Unmarshal(raw, &resp); err != nil {
		return 0, false
	}
	total, ok1 := resp[totalKey].(float64)
	qty, ok2 := resp[qtyKey].(float64)
	if !ok1 || !ok2 || qty <= 0 {
		return 0, false
	}
	return total / qty, true
}

// recordHaulResult writes the real, cargo-capped outcome of a just-completed haul (with
// per-leg timing) to the store. A nil *haulMetrics (resumed haul) records nothing. soldQty
// is the quantity actually delivered; it falls back to the gate-sized qty when the metrics
// captured a different figure. A write failure is logged, not fatal — the haul is done.
func recordHaulResult(ctx context.Context, deps HaulDeps, out io.Writer, opp market.ArbitrageOpportunity, soldQty float64, m *haulMetrics) {
	if m == nil {
		return
	}
	qty := soldQty
	if qty <= 0 {
		qty = m.qty
	}
	rec := market.HaulResult{
		OppID: opp.ID, AgentID: deps.AgentID, ItemID: opp.ItemID, Qty: qty,
		BuyPricePaid: m.buyPrice, SellPriceGot: m.sellPrice,
		RealizedProfit: (m.sellPrice - m.buyPrice) * qty, JumpsTraveled: m.jumps,
		ClaimedAt: rfc(m.claimedAt), ArrivedSrcAt: rfc(m.arrivedSrcAt), BoughtAt: rfc(m.boughtAt),
		ArrivedDstAt: rfc(m.arrivedDstAt), SoldAt: rfc(m.soldAt),
		ClaimedTick: m.claimedTick, ArrivedSrcTick: m.arrivedSrcTick, BoughtTick: m.boughtTick,
		ArrivedDstTick: m.arrivedDstTick, SoldTick: m.soldTick, CreatedAt: rfc(haulNow(deps)),
	}
	if err := deps.Market.RecordHaulResult(ctx, rec); err != nil {
		fmt.Fprintf(out, "haul: opp %d record result failed: %v\n", opp.ID, err) //nolint:errcheck
	}
	// Contribute the treasury's cut of this haul's realized profit to the shared fund.
	depositProfitShare(ctx, deps.Client, out, rec.RealizedProfit)
}

// haulAutopilot routes to a station POI within a system, capturing each hop to the KB.
// An optional check (sell leg only) can request an early stop for mid-route re-routing;
// nil disables it.
func haulAutopilot(ctx context.Context, deps HaulDeps, out io.Writer, system, poi string, check WaypointCheckFunc) error {
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
		WaypointCheck: check,
	}, system, poi)
}

// haulSellUndercut is the fraction of the best ask at which a hauler lists leftover
// cargo when no buyer exists at the station — a 10% undercut to move stale inventory.
const haulSellUndercut = 0.90

// haulReservedCargo are item ids a hauler never liquidates (operational, not trade goods).
var haulReservedCargo = map[string]bool{"fuel_cell": true}

// stationPrice returns the price entry for stationID, or nil if the station is absent.
func stationPrice(prices []market.ItemStationPrice, stationID string) *market.ItemStationPrice {
	for i := range prices {
		if prices[i].StationID == stationID {
			return &prices[i]
		}
	}
	return nil
}

// bestAskAnywhere returns the lowest sell-order price (best ask) for an item across all
// stations that list it, used to price a leftover-cargo sell order even when the item is
// not traded at the station the hauler happens to be sitting at. ok is false when no
// station anywhere has a sell order to reference.
func bestAskAnywhere(prices []market.ItemStationPrice) (best float64, ok bool) {
	for _, p := range prices {
		if p.HasSell && (!ok || p.BestAsk < best) {
			best, ok = p.BestAsk, true
		}
	}
	return best, ok
}

// liquidateCargo clears leftover (non-haul) cargo from the hold so it stops blocking
// buys. It docks at the current station, re-prices live, and for each non-reserved cargo
// item either sells into an existing buy order at the station (a ready market) or, if
// none, posts a sell order 10% below the station's best ask. Best-effort: not at a
// dockable station, or an item with no local price reference, is logged and left in the
// hold. Returns true if it took any sell / sell-order action this pass.
func liquidateCargo(ctx context.Context, deps HaulDeps, out io.Writer) bool {
	state := deps.Client.GetState()
	if state == nil {
		return false
	}
	var items []game.CargoItem
	for _, c := range state.Ship.Cargo {
		if c.Quantity > 0 && !haulReservedCargo[c.ItemID] {
			items = append(items, c)
		}
	}
	if len(items) == 0 {
		return false
	}
	// Dock only if not already docked — a ship sitting at its station returns
	// "Already docked" from Dock(), which must not abort the liquidation.
	if !state.IsDocked() {
		if err := deps.Client.Dock(ctx); err != nil {
			fmt.Fprintf(out, "haul: cannot dock to liquidate %d stale cargo item(s): %v\n", len(items), err) //nolint:errcheck
			return false
		}
	}
	if deps.RecaptureBuyMarket != nil {
		if err := deps.RecaptureBuyMarket(ctx); err != nil {
			fmt.Fprintf(out, "haul: liquidate recapture failed: %v; pricing on last capture\n", err) //nolint:errcheck
		}
	}
	station := ""
	if s := deps.Client.GetState(); s != nil {
		station = s.CurrentPOI
	}
	acted := false
	for _, item := range items {
		prices, err := deps.Market.GetItemStationPrices(ctx, item.ItemID)
		if err != nil {
			fmt.Fprintf(out, "haul: liquidate price check %s failed: %v\n", item.ItemID, err) //nolint:errcheck
			continue
		}
		// Prefer selling into a buyer at the current station (a ready market); otherwise
		// post a sell order here, priced 10% below the best ask anywhere for the item.
		if here := stationPrice(prices, station); here != nil && here.HasBuy {
			if err := deps.Client.Sell(ctx, item.ItemID, item.Quantity); err != nil {
				fmt.Fprintf(out, "haul: liquidate sell %.0f %s failed: %v\n", item.Quantity, item.ItemID, err) //nolint:errcheck
				continue
			}
			fmt.Fprintf(out, "haul: liquidated %.0f %s into local buy order @%.0f\n", item.Quantity, item.ItemID, here.BestBid) //nolint:errcheck
			acted = true
			continue
		}
		ask, ok := bestAskAnywhere(prices)
		if !ok {
			fmt.Fprintf(out, "haul: no market price for %.0f %s; leaving in hold\n", item.Quantity, item.ItemID) //nolint:errcheck
			continue
		}
		price := math.Round(ask * haulSellUndercut)
		if err := deps.Client.CreateSellOrder(ctx, map[string]any{
			"item_id":    item.ItemID,
			"quantity":   int(item.Quantity),
			"price_each": int(price),
		}); err != nil {
			fmt.Fprintf(out, "haul: liquidate sell-order %.0f %s failed: %v\n", item.Quantity, item.ItemID, err) //nolint:errcheck
			continue
		}
		fmt.Fprintf(out, "haul: posted sell order %.0f %s @%.0f (10%% under best ask %.0f)\n", item.Quantity, item.ItemID, price, ask) //nolint:errcheck
		acted = true
	}
	return acted
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
