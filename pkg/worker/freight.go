package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/market"
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

// freightInputs are the per-pass facts freightCandidate needs from the caller.
// Passing them in (rather than deriving them) keeps the gate testable without a
// live galaxy graph or fuel-price source.
type freightInputs struct {
	// CargoFree is the ship's remaining hold, in cargo units.
	CargoFree float64
	// FuelCostFor prices the fuel for a jump count (nil -> free).
	FuelCostFor func(jumps int) float64
	// HopsTo resolves jump distance to a destination base; ok=false means
	// unroutable, and the contract is skipped rather than guessed at.
	HopsTo func(destBaseID string) (int, bool)
}

// freightCandidate evaluates freight at the current dock and returns the best
// scored candidate, or nil plus a skip reason. Failure is always "skip the
// pass", never an error: the caller falls through to missions/exploration
// exactly as it does when the mission board is empty.
func freightCandidate(ctx context.Context, deps MissionDeps, in freightInputs, out io.Writer) (*freightCand, string) {
	if out == nil {
		out = io.Discard
	}
	// Capability precondition first: a ship that cannot hold a package has no
	// business talking to /shipping, so this costs zero server calls.
	if freightPackagesFit(in.CargoFree) < 1 {
		return nil, fmt.Sprintf("hold has %.0f free, a package needs %.0f", in.CargoFree, freightPackageFootprint)
	}

	// Debt guard. Freight debt blocks acceptance server-side, so listing the
	// board would be wasted. We never auto-pay: an operator settles debt, so a
	// systematic breach bug cannot buy back its own ability to keep breaching.
	if err := deps.Client.ShippingProfile(ctx); err != nil {
		return nil, fmt.Sprintf("shipping profile: %v", err)
	}
	var prof serverapi.ShippingProfileResponse
	if raw := deps.Client.GetRawJSON("shipping_profile"); len(raw) > 0 {
		if err := json.Unmarshal(raw, &prof); err != nil {
			return nil, fmt.Sprintf("decode shipping profile: %v", err)
		}
	}
	if prof.DebtBlocksAcceptance {
		reason := prof.DebtBlockReason
		if reason == "" {
			reason = "freight debt blocks acceptance"
		}
		fmt.Fprintf(out, "freight: skipping, %s (outstanding %d) — operator must settle\n", reason, prof.Profile.OutstandingDebt) //nolint:errcheck
		return nil, reason
	}

	if err := deps.Client.ShippingList(ctx, ""); err != nil {
		return nil, fmt.Sprintf("shipping list: %v", err)
	}
	var board serverapi.ShippingListResponse
	if raw := deps.Client.GetRawJSON("shipping_list"); len(raw) > 0 {
		if err := json.Unmarshal(raw, &board); err != nil {
			return nil, fmt.Sprintf("decode shipping list: %v", err)
		}
	}
	if len(board.Shipments) == 0 {
		reason := board.EmptyReason
		if reason == "" {
			reason = "no freight posted here"
		}
		return nil, reason
	}

	cands := make([]freightCand, 0, len(board.Shipments))
	for _, l := range board.Shipments {
		hops := l.Contract.RouteHops
		if in.HopsTo != nil {
			h, ok := in.HopsTo(l.Contract.DestinationBaseID)
			if !ok {
				fmt.Fprintf(out, "freight: skip %s: no route to %s\n", l.Contract.ID, l.Contract.DestinationBaseID) //nolint:errcheck
				continue
			}
			hops = h
		}
		c, reason := buildFreightCand(l, hops, in.FuelCostFor)
		if reason != "" {
			// Logged at every rejection on purpose: the net distribution here is
			// the only evidence for whether freightMinNet is set sanely against
			// real NPC rewards, which are unmeasured.
			fmt.Fprintf(out, "freight: skip %s: %s\n", l.Contract.ID, reason) //nolint:errcheck
			continue
		}
		cands = append(cands, c)
	}

	best := selectFreightCand(cands)
	if best == nil {
		return nil, "no freight cleared the gate"
	}
	fmt.Fprintf(out, "freight: best %s to %s, %d hops, net %.0f\n", best.Contract.ID, best.DestBaseID, best.Hops, best.Net) //nolint:errcheck
	return best, ""
}

// freightRecord persists one settled contract outcome. Telemetry failures are
// logged and swallowed: losing a metrics row must never abort a trip.
func freightRecord(ctx context.Context, deps MissionDeps, out io.Writer, c serverapi.ShipmentContract, cand *freightCand, payout float64, outcome, reason string) {
	if deps.Market == nil {
		return
	}
	now := missionNow(deps)
	fuel := 0.0
	hops := c.RouteHops
	if cand != nil {
		fuel = cand.FuelCost
		if hops == 0 {
			hops = cand.Hops
		}
	}
	r := market.FreightResult{
		AgentID:       deps.AgentID,
		ContractID:    c.ID,
		PackageID:     c.PackageID,
		FromBaseID:    c.OriginBaseID,
		ToBaseID:      c.DestinationBaseID,
		ServiceLevel:  c.ServiceLevel,
		RouteHops:     hops,
		BaseReward:    float64(c.BaseReward),
		MaxSpeedBonus: float64(c.MaxSpeedBonus),
		FuelCost:      fuel,
		CarrierPayout: payout,
		Outcome:       outcome,
		Reason:        reason,
		AcceptedAt:    c.AcceptedAt,
		FinishedAt:    rfc(now),
		AcceptedTick:  c.AcceptedTick,
		FinishedTick:  missionTick(deps),
		CreatedAt:     rfc(now),
	}
	if err := deps.Market.RecordFreightResult(ctx, r); err != nil {
		fmt.Fprintf(out, "freight: record %s result: %v\n", outcome, err) //nolint:errcheck
	}
}

// freightAccept takes the candidate and then verifies it — the server only sets
// deadline_tick at accept, so feasibility is unknowable beforehand. An infeasible
// contract is immediately returned, which the live smoke confirmed is debt-free
// (status returned_intact, full shipper_refund, liability released,
// outstanding_debt unchanged). ok=false means the candidate was released.
func freightAccept(ctx context.Context, deps MissionDeps, cand *freightCand, out io.Writer) (*serverapi.ShipmentContract, bool) {
	if out == nil {
		out = io.Discard
	}
	id := cand.Contract.ID
	if err := deps.Client.ShippingAccept(ctx, id, "player"); err != nil {
		fmt.Fprintf(out, "freight: accept %s: %v\n", id, err) //nolint:errcheck
		freightRecord(ctx, deps, out, cand.Contract, cand, 0, "accept_failed", err.Error())
		return nil, false
	}

	var resp serverapi.ShippingContractResponse
	if raw := deps.Client.GetRawJSON("shipping_accept"); len(raw) > 0 {
		if err := json.Unmarshal(raw, &resp); err != nil {
			fmt.Fprintf(out, "freight: decode accept %s: %v\n", id, err) //nolint:errcheck
		}
	}
	c := resp.Contract
	if c.ID == "" {
		// The accept reply did not decode. We may well be holding a contract we
		// cannot reason about, so release it rather than transit blind.
		fmt.Fprintf(out, "freight: accept %s returned no contract; returning it\n", id) //nolint:errcheck
		freightReturn(ctx, deps, out, cand.Contract, cand, "returned_infeasible", "accept reply did not decode")
		return nil, false
	}

	hops := c.RouteHops
	if hops == 0 {
		hops = cand.Hops
	}
	// missionTick(deps), not c.AcceptedTick: shipping mutations are
	// tick-deferred, so by the time this reply is in hand the current tick
	// is already >= AcceptedTick. Checking against AcceptedTick would
	// overstate the remaining window and bias this feasibility gate
	// optimistic — the opposite of what a fail-closed check needs.
	if ok, reason := freightDeadlineOK(hops, c.DeadlineTick, missionTick(deps)); !ok {
		fmt.Fprintf(out, "freight: %s infeasible (%s); returning\n", id, reason) //nolint:errcheck
		freightReturn(ctx, deps, out, c, cand, "returned_infeasible", reason)
		return nil, false
	}
	fmt.Fprintf(out, "freight: accepted %s to %s (deadline tick %d)\n", id, c.DestinationBaseID, c.DeadlineTick) //nolint:errcheck
	return &c, true
}

// missionTakeFreight accepts cand and runs its trip. It is the single place the
// accept -> publish -> run-trip sequence lives, shared by all three call sites in
// Missions (empty board, fully-gated board, and the co-equal net comparison).
//
// taken=false means no trip was started — either there was no candidate, or the
// contract was released by freightAccept (returned as infeasible, or the accept
// itself failed) — and the caller continues with whatever it would have done
// without freight. The returned error is freightRunTrip's, which is nil on every
// handled outcome: freight must never become a new way for the pass to fail.
func missionTakeFreight(ctx context.Context, deps MissionDeps, cand *freightCand, out io.Writer) (bool, error) {
	if cand == nil {
		return false, nil
	}
	accepted, ok := freightAccept(ctx, deps, cand, out)
	if !ok {
		return false, nil
	}
	publishActivity(deps.SetActivity, "Freight "+accepted.ID+" to "+accepted.DestinationBaseID)
	return true, freightRunTrip(ctx, deps, accepted, cand, func(ctx context.Context, baseID string) error {
		return missionNavToBase(ctx, deps, baseID)
	}, out)
}

// freightReturn hands a contract back and records the outcome. A failed return
// is logged loudly: it is the only situation in which a breach becomes possible
// despite the design, so it must be visible in the canary logs.
func freightReturn(ctx context.Context, deps MissionDeps, out io.Writer, c serverapi.ShipmentContract, cand *freightCand, outcome, reason string) {
	if err := deps.Client.ShippingReturn(ctx, c.ID); err != nil {
		fmt.Fprintf(out, "freight: RETURN FAILED for %s (%s): %v — breach now possible\n", c.ID, reason, err) //nolint:errcheck
		// Record return_failed, not the caller's outcome: the return itself
		// didn't happen, so this contract can still breach. Grouping by
		// Outcome must surface that as the alarm state it is, not as a clean
		// return.
		freightRecord(ctx, deps, out, c, cand, 0, "return_failed", "return failed: "+err.Error())
		return
	}
	freightRecord(ctx, deps, out, c, cand, 0, outcome, reason)
}

// freightPackageItemID is the cargo/storage item id for a contract's package.
// The contract carries the bare hash in package_id; storage and cargo address it
// with a "package:" prefix (confirmed live).
func freightPackageItemID(packageID string) string {
	return "package:" + packageID
}

// freightInFlightCheck re-verifies the deadline buffer while carrying. Called
// every pass: a long disconnect can burn the whole margin between passes, and a
// contract that has become unwinnable is worth more returned than breached.
// Returns false when the contract was released.
func freightInFlightCheck(ctx context.Context, deps MissionDeps, c *serverapi.ShipmentContract, cand *freightCand, remainingHops int, out io.Writer) bool {
	if out == nil {
		out = io.Discard
	}
	if ok, reason := freightDeadlineOK(remainingHops, c.DeadlineTick, missionTick(deps)); !ok {
		fmt.Fprintf(out, "freight: in-flight buffer collapsed for %s (%s); returning\n", c.ID, reason) //nolint:errcheck
		freightReturn(ctx, deps, out, *c, cand, "returned_inflight", reason)
		return false
	}
	return true
}

// freightReconcile recovers an in-flight contract from server state after a
// restart or reconnect. The worker's in-memory task does not survive a restart
// (no captains_log resume yet), and an orphaned package rides to a breach in
// silence, so this runs before taking any new work.
func freightReconcile(ctx context.Context, deps MissionDeps, out io.Writer) (*serverapi.ShipmentContract, bool) {
	if out == nil {
		out = io.Discard
	}
	if err := deps.Client.ShippingProfile(ctx); err != nil {
		fmt.Fprintf(out, "freight: reconcile profile: %v\n", err) //nolint:errcheck
		return nil, false
	}
	var prof serverapi.ShippingProfileResponse
	if raw := deps.Client.GetRawJSON("shipping_profile"); len(raw) > 0 {
		if err := json.Unmarshal(raw, &prof); err != nil {
			fmt.Fprintf(out, "freight: reconcile decode profile: %v\n", err) //nolint:errcheck
			return nil, false
		}
	}
	if prof.Profile.ActiveContracts == 0 {
		return nil, false
	}

	// The profile reports only a count, so the board read supplies the contract
	// itself — an in_transit contract we are contracted on comes back in list.
	if err := deps.Client.ShippingList(ctx, ""); err != nil {
		fmt.Fprintf(out, "freight: reconcile list: %v\n", err) //nolint:errcheck
		return nil, false
	}
	var board serverapi.ShippingListResponse
	if raw := deps.Client.GetRawJSON("shipping_list"); len(raw) > 0 {
		if err := json.Unmarshal(raw, &board); err != nil {
			fmt.Fprintf(out, "freight: reconcile decode list: %v\n", err) //nolint:errcheck
			return nil, false
		}
	}
	for _, l := range board.Shipments {
		if l.Contract.Status == "in_transit" {
			c := l.Contract
			fmt.Fprintf(out, "freight: reconciled held contract %s to %s (deadline tick %d)\n", c.ID, c.DestinationBaseID, c.DeadlineTick) //nolint:errcheck
			return &c, true
		}
	}
	fmt.Fprintf(out, "freight: profile reports %d active contract(s) but none found in the board read\n", prof.Profile.ActiveContracts) //nolint:errcheck
	return nil, false
}

// freightRunTrip carries an accepted contract to its destination and delivers it.
// Returns nil on every handled outcome — like Missions and Haul, a failed trip
// logs and idles rather than killing the worker loop.
func freightRunTrip(ctx context.Context, deps MissionDeps, c *serverapi.ShipmentContract, cand *freightCand, nav func(ctx context.Context, baseID string) error, out io.Writer) error {
	if out == nil {
		out = io.Discard
	}
	// Accept placed the sealed package in personal storage at origin; pull it
	// into the hold before leaving. If it will not load we are holding a contract
	// we cannot physically carry, which is a guaranteed breach — return instead.
	//
	// Conditional on the package not already being aboard: this function is
	// re-entered by the reconcile-resume path for a contract whose nav failed on
	// an earlier pass, and by then the package is in CARGO, not storage. An
	// unconditional withdraw fails there with "not in storage" and the error
	// branch below would return the contract — destroying the healthy contract
	// the nav-failure branch deliberately left in flight. Only an absent package
	// means "cannot physically carry".
	item := freightPackageItemID(c.PackageID)
	if cargoCount(deps.Client.GetState(), item) < 1 {
		if err := deps.Client.WithdrawItems(ctx, item, 1); err != nil {
			fmt.Fprintf(out, "freight: withdraw %s: %v; returning contract\n", item, err) //nolint:errcheck
			freightReturn(ctx, deps, out, *c, cand, "returned_infeasible", "package would not load: "+err.Error())
			return nil
		}
	}

	if nav != nil {
		if err := nav(ctx, c.DestinationBaseID); err != nil {
			// Navigation failed but the deadline may still hold; leave the
			// contract in flight and let the next pass re-check the buffer
			// (freightInFlightCheck) rather than returning on a transient error.
			fmt.Fprintf(out, "freight: navigate to %s: %v\n", c.DestinationBaseID, err) //nolint:errcheck
			return nil
		}
	}

	if err := deps.Client.ShippingDeliver(ctx, c.ID); err != nil {
		fmt.Fprintf(out, "freight: deliver %s: %v\n", c.ID, err) //nolint:errcheck
		return nil
	}
	var settle serverapi.ShippingSettlementResponse
	// The outcome stays "delivered" whatever happens here — a committed package
	// cannot be un-delivered the way an unaccepted one can be released. But a
	// payout of 0 from a decode failure must not read as a genuine zero payout in
	// freight_results, so the reason carries the distinction.
	settleReason := ""
	raw := deps.Client.GetRawJSON("shipping_deliver")
	switch {
	case len(raw) == 0:
		settleReason = "settlement decode failed: no reply"
		fmt.Fprintf(out, "freight: deliver %s returned no settlement reply\n", c.ID) //nolint:errcheck
	default:
		if err := json.Unmarshal(raw, &settle); err != nil {
			settleReason = "settlement decode failed: " + err.Error()
			fmt.Fprintf(out, "freight: decode deliver %s: %v\n", c.ID, err) //nolint:errcheck
		}
	}
	final := settle.Contract
	if final.ID == "" {
		final = *c
	}
	fmt.Fprintf(out, "freight: delivered %s, payout %d\n", c.ID, settle.CarrierPayout) //nolint:errcheck
	freightRecord(ctx, deps, out, final, cand, float64(settle.CarrierPayout), "delivered", settleReason)
	return nil
}

// missionHopsToBase resolves jump distance to a base id via the server's router.
// The KB cannot map base -> system (no POI-by-id lookup, and SpaceBase carries
// only POIID), and the contract addresses its destination by base id only, so
// the router is the authority — the same approach haulResolveSellSystem uses for
// the moving capital. ok=false means unroutable, and the caller skips the
// contract rather than guessing a distance.
func missionHopsToBase(ctx context.Context, deps MissionDeps, destBaseID string) (int, bool) {
	if destBaseID == "" {
		return 0, false
	}
	route, err := deps.Client.FindRoute(ctx, destBaseID)
	if err != nil || len(route) == 0 {
		return 0, false
	}
	// RouteStep.Jumps is cumulative, so the last hop carries the total. Fall back
	// to the step count when the server omits it.
	if hops := route[len(route)-1].Jumps; hops > 0 {
		return hops, true
	}
	return len(route), true
}

// missionNavToBase routes to a base id, resolving its system through the router
// and reusing the pass's existing navigation rather than adding a second path.
func missionNavToBase(ctx context.Context, deps MissionDeps, destBaseID string) error {
	route, err := deps.Client.FindRoute(ctx, destBaseID)
	if err != nil {
		return fmt.Errorf("route to %s: %w", destBaseID, err)
	}
	if len(route) == 0 {
		return fmt.Errorf("no route to %s", destBaseID)
	}
	destSystem := route[len(route)-1].SystemID
	if destSystem == "" {
		return fmt.Errorf("router returned no system for %s", destBaseID)
	}
	return deps.nav(ctx, destSystem, destBaseID)
}
