package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
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

	// carrierTierProbationary is the lowest carrier standing. Bootstrap relaxes
	// the freight net floor ONLY at this tier; every higher tier's cargo already
	// clears the normal floor. Matches ShippingProfileResponse.Progression.CurrentTier.
	carrierTierProbationary = "probationary"

	// freightProbationFloor is the relaxed net floor a probationary carrier
	// accepts down to while bootstrapping out of the tier. Negative: the
	// probationary board is mostly small POSITIVE-net contracts the 500 floor
	// wrongly rejects, plus a few small losses worth eating to reach the
	// ~4x-paying licensed tier. Aggressive-profile value; tunable.
	freightProbationFloor = -400.0

	// freightProbationBudget caps cumulative bootstrap LOSSES per worker
	// (positive-net accepts are free). Once spent, the floor reverts to 500
	// until the server flips the tier. Aggressive-profile value; tunable.
	freightProbationBudget = 3000.0

	// freightLoadMaxParks bounds how many times ONE contract may park on an
	// unconfirmed load before we give up and hand it back. Parking beats
	// returning because the package is usually aboard already (see
	// freightWithdrawConfirmed), but an unbounded park would pin the worker to
	// a contract it genuinely cannot carry.
	freightLoadMaxParks = 2
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

// freightEffectiveMax normalizes the configured package cap: anything below
// 1 (unset, or nonsense negatives) is the v1 single-contract behavior.
func freightEffectiveMax(n int) int {
	if n < 1 {
		return 1
	}
	return n
}

// effectiveFreightFloor is the net floor a contract must clear this pass. It
// relaxes from freightMinNet (500) to freightProbationFloor (-400) only while a
// worker is bootstrapping out of the probationary tier: bootstrap enabled, tier
// is exactly probationary, and its per-worker loss budget is not yet spent. Any
// other tier, an unknown/empty tier, a spent budget, or a disabled toggle all
// yield the normal 500 floor. Pure — the caller supplies the live tier and the
// per-worker spend so this needs no I/O and is table-testable.
func effectiveFreightFloor(tier string, bootstrapEnabled bool, spent, budget float64) float64 {
	if bootstrapEnabled && tier == carrierTierProbationary && spent < budget {
		return freightProbationFloor
	}
	return freightMinNet
}

// logTierIfChanged writes the carrier's tier the first time it is observed this
// session and again whenever it changes (a promotion). This gives the tagged
// worker log an authoritative per-worker carrier tier — captured on the next
// worker update without any per-agent shipping-profile query — while avoiding a
// line every freight pass. No-op on a nil receiver (tests) or empty tier.
func (s *missionRunState) logTierIfChanged(out io.Writer, p serverapi.CarrierTierProgress) {
	if s == nil || p.CurrentTier == "" || p.CurrentTier == s.lastLoggedTier {
		return
	}
	s.lastLoggedTier = p.CurrentTier
	if p.AtMaximumTier {
		fmt.Fprintf(out, "freight: carrier tier %s (max tier)\n", p.CurrentTier) //nolint:errcheck
		return
	}
	fmt.Fprintf(out, "freight: carrier tier %s (%d/%d deliveries, %d/%d value to %s)\n", //nolint:errcheck
		p.CurrentTier, p.SuccessfulDeliveries, p.RequiredSuccessfulDeliveries,
		p.DeliveredValue, p.RequiredDeliveredValue, p.NextTier)
}

// carrierTierAboveProbationary reports whether a KNOWN tier outranks
// probationary. An empty/unknown tier returns false: never fence a carrier that
// might itself be probationary (that would starve it of the hauls it needs).
func carrierTierAboveProbationary(tier string) bool {
	return tier != "" && tier != carrierTierProbationary
}

// freightBandExcluded reports whether to skip a board listing to reserve
// probationary-band cargo for carriers still climbing out of the probationary
// tier. Gated on the bootstrap switch (the fleet-wide "help the probationary
// climb" policy): with bootstrap off, selection is unchanged. Pure — the caller
// supplies the live carrier tier, the contract's required band, and the toggle.
func freightBandExcluded(carrierTier, contractBand string, bootstrapEnabled bool) bool {
	return bootstrapEnabled &&
		contractBand == carrierTierProbationary &&
		carrierTierAboveProbationary(carrierTier)
}

// buildFreightCand derives economics for one listing given the chain we
// already hold. A non-empty reason means skip, and is logged verbatim so a
// canary pass shows why the board emptied out.
func buildFreightCand(l serverapi.ShippingListing, hops int, held []chainStop, nowTick int64, fuelCostFor func(jumps int) float64, floor float64) (freightCand, string) {
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
	stop := chainStop{ContractID: l.Contract.ID, DestBaseID: l.Contract.DestinationBaseID, Hops: hops}
	// Inserting this stop must not break any HELD contract's deadline under
	// the chain bound (the candidate's own deadline does not exist until
	// accept; freightAccept checks it then, chain-aware).
	withCand := make([]chainStop, 0, len(held)+1)
	withCand = append(withCand, held...)
	withCand = append(withCand, stop)
	if ok, reason := chainFeasible(withCand, nowTick); !ok {
		return freightCand{}, "would break held deadline: " + reason
	}
	reward := float64(l.Contract.BaseReward)
	fuel := 0.0
	if fuelCostFor != nil {
		fuel = fuelCostFor(chainMarginalHops(held, stop))
	}
	// max_speed_bonus is deliberately excluded: it is upside, never a reason to
	// take a contract whose base reward does not stand on its own.
	net := reward - fuel
	if net < floor {
		return freightCand{}, fmt.Sprintf("net %.0f below floor %.0f (reward %.0f, fuel %.0f)", net, floor, reward, fuel)
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
	// Held is the current chain (one stop per held contract), priced from
	// this dock. Empty on a v1-equivalent pass.
	Held []chainStop
	// NowTick anchors chain-deadline feasibility (missionTick at pass start).
	NowTick int64
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
	deps.State.logTierIfChanged(out, prof.Progression)

	if prof.DebtBlocksAcceptance {
		reason := prof.DebtBlockReason
		if reason == "" {
			reason = "freight debt blocks acceptance"
		}
		fmt.Fprintf(out, "freight: skipping, %s (outstanding %d) — operator must settle\n", reason, prof.Profile.OutstandingDebt) //nolint:errcheck
		return nil, reason
	}

	// Headroom gates (sub-project C). The v1 "one contract at a time" rule
	// is the maxPk==1 special case of these.
	maxPk := freightEffectiveMax(deps.FreightMaxPackages)
	if len(in.Held) >= maxPk {
		reason := fmt.Sprintf("at max packages (%d/%d)", len(in.Held), maxPk)
		fmt.Fprintf(out, "freight: skipping, %s\n", reason) //nolint:errcheck
		return nil, reason
	}
	// Reconcile-gap guard: the server counting more actives than we remember
	// means a contract we cannot see (post-restart amnesia). Accepting more
	// while any contract is unaccounted for is how orphans breach — fail
	// closed exactly as v1 did with ActiveContracts>0 on empty memory.
	if prof.Profile.ActiveContracts > len(in.Held) {
		reason := fmt.Sprintf("%d active contract(s) unaccounted for (holding %d)", prof.Profile.ActiveContracts, len(in.Held))
		fmt.Fprintf(out, "freight: skipping, %s\n", reason) //nolint:errcheck
		return nil, reason
	}
	if !prof.Capacity.ActiveContractsUnlimited && prof.Capacity.ActiveContractLimit > 0 &&
		prof.Profile.ActiveContracts >= prof.Capacity.ActiveContractLimit {
		reason := fmt.Sprintf("server contract limit reached (%d/%d)", prof.Profile.ActiveContracts, prof.Capacity.ActiveContractLimit)
		fmt.Fprintf(out, "freight: skipping, %s\n", reason) //nolint:errcheck
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

	// Bootstrap floor: relaxed to freightProbationFloor only while this worker
	// is probationary and its loss budget holds; otherwise the normal 500 floor.
	// Read from the profile just fetched — no extra server call.
	floor := effectiveFreightFloor(prof.Progression.CurrentTier, deps.FreightBootstrap, deps.State.bootstrapSpent(), freightProbationBudget)

	cands := make([]freightCand, 0, len(board.Shipments))
	for _, l := range board.Shipments {
		if freightBandExcluded(prof.Progression.CurrentTier, l.Contract.RiskBand, deps.FreightBootstrap) {
			fmt.Fprintf(out, "freight: skip %s: probationary cargo reserved for climbing carriers (carrier tier %s)\n", //nolint:errcheck
				l.Contract.ID, prof.Progression.CurrentTier)
			continue
		}
		if !prof.Capacity.LiabilityUnlimited && prof.Capacity.AggregateLiabilityLimit > 0 &&
			l.Contract.ReservedExposure > prof.Capacity.RemainingAggregateLiability {
			fmt.Fprintf(out, "freight: skip %s: exposure %d over remaining liability %d\n", l.Contract.ID, l.Contract.ReservedExposure, prof.Capacity.RemainingAggregateLiability) //nolint:errcheck
			continue
		}
		hops := l.Contract.RouteHops
		if in.HopsTo != nil {
			h, ok := in.HopsTo(l.Contract.DestinationBaseID)
			if !ok {
				fmt.Fprintf(out, "freight: skip %s: no route to %s\n", l.Contract.ID, l.Contract.DestinationBaseID) //nolint:errcheck
				continue
			}
			hops = h
		}
		c, reason := buildFreightCand(l, hops, in.Held, in.NowTick, in.FuelCostFor, floor)
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

// freightStep is how a freight step ended. It exists because "released" and
// "return failed" must NOT be treated alike: a clean release leaves us holding
// nothing, while a failed return leaves a live, undischarged contract against
// us. Collapsing the two into a bool is what let the pass continue into the
// mission accept loop still on the hook for a package — the exact breach the
// fail-closed design exists to prevent.
type freightStep int

const (
	// freightStepProceed: contract in hand, carry on.
	freightStepProceed freightStep = iota
	// freightStepReleased: cleanly handed back, nothing owed; the pass
	// continues without freight.
	freightStepReleased
	// freightStepStuck: the return itself failed. The contract is still live
	// and we cannot discharge it, so the pass must abort and park rather than
	// fly elsewhere on other work.
	freightStepStuck
	// freightStepUndischargeable: the return was refused for a reason retrying
	// cannot fix — the package is only returnable at its origin station and we
	// could not get back there. Unlike Stuck this must NOT park the pass:
	// parking retries the same impossible call every ~10s while the deadline
	// runs out (fighter-1, 2026-07-25: 28 attempts in 4.5 minutes, then a
	// breach). The contract stays held and gets flown to its destination — a
	// late delivery may still settle, sitting still cannot.
	freightStepUndischargeable
)

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
func freightAccept(ctx context.Context, deps MissionDeps, cand *freightCand, held []chainStop, out io.Writer) (*serverapi.ShipmentContract, freightStep) {
	if out == nil {
		out = io.Discard
	}
	id := cand.Contract.ID
	if err := deps.Client.ShippingAccept(ctx, id, "player"); err != nil {
		fmt.Fprintf(out, "freight: accept %s: %v\n", id, err) //nolint:errcheck
		freightRecord(ctx, deps, out, cand.Contract, cand, 0, "accept_failed", err.Error())
		// A failed accept means we never took it on — nothing to discharge.
		return nil, freightStepReleased
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
		return nil, freightReturn(ctx, deps, out, cand.Contract, cand, "returned_infeasible", "accept reply did not decode")
	}

	hops := c.RouteHops
	if hops == 0 {
		hops = cand.Hops
	}
	// missionTick(deps), not c.AcceptedTick: shipping mutations are
	// tick-deferred, so by the time this reply is in hand the current tick is
	// already >= AcceptedTick; AcceptedTick would bias the gate optimistic.
	// Chain-aware: the accepted contract is checked at its position in the
	// held chain, not solo — its deadline exists only now.
	withNew := make([]chainStop, 0, len(held)+1)
	withNew = append(withNew, held...)
	withNew = append(withNew, chainStop{ContractID: c.ID, DestBaseID: c.DestinationBaseID, Hops: hops, DeadlineTick: c.DeadlineTick, RecoveryDeadlineTick: c.RecoveryDeadlineTick})
	if c.DeadlineTick <= 0 {
		// Fail closed on a missing deadline, as v1's freightDeadlineOK did.
		fmt.Fprintf(out, "freight: %s infeasible (contract carries no deadline_tick); returning\n", id) //nolint:errcheck
		return nil, freightReturn(ctx, deps, out, c, cand, "returned_infeasible", "contract carries no deadline_tick")
	}
	if ok, reason := chainFeasible(withNew, missionTick(deps)); !ok {
		fmt.Fprintf(out, "freight: %s infeasible (%s); returning\n", id, reason) //nolint:errcheck
		return nil, freightReturn(ctx, deps, out, c, cand, "returned_infeasible", reason)
	}
	fmt.Fprintf(out, "freight: accepted %s to %s (reward deadline tick %d, deliverable until %d)\n", id, c.DestinationBaseID, c.DeadlineTick, withNew[len(withNew)-1].deliverableUntil()) //nolint:errcheck
	// Remember the held contract in cross-pass memory: the board read never
	// shows our own in_transit contracts, so this is what reconcile resumes
	// from if the trip is interrupted mid-session.
	deps.State.addHeldFreight(&c)
	// A negative-net accept only happens under the relaxed probation floor;
	// charge the loss against the per-worker budget so it eventually reverts.
	if cand.Net < 0 {
		deps.State.addBootstrapSpent(-cand.Net)
	}
	return &c, freightStepProceed
}

// missionTakeFreight accepts cand, loads its package, and enters the chain. It
// is the single place the accept -> load -> publish -> chain sequence lives,
// shared by all three call sites in Missions (empty board, fully-gated board,
// and the co-equal net comparison).
//
// freightStepReleased means no chain was entered — either there was no
// candidate, or the contract was released by freightAccept/freightLoadPackage
// (returned as infeasible, unloadable, or the accept itself failed) — and the
// caller continues with whatever it would have done without freight.
// freightStepStuck means a return FAILED and an undischarged contract is
// still live: the caller must abort the pass rather than fly elsewhere on
// other work. The returned error is freightChainRun's, which is nil on every
// handled outcome: freight must never become a new way for the pass to fail.
// fuelModel is built at most once per pass (ensureFuelModel memoizes it) and
// building it costs a KB GetConnections plus a FindRoute server probe
// (haulFuelPerJump). Taking it as a builder rather than an already-built
// func(int) float64 keeps that cost lazy: it must run only once a candidate
// is actually being taken, not merely evaluated as a call argument at a dry
// path's call site (Go evaluates arguments before the call, so passing the
// built model there would probe the server on every board-empty pass even
// for EnableFreight=false workers, which take no freight path at all).
func missionTakeFreight(ctx context.Context, deps MissionDeps, cand *freightCand, held []chainStop, hopsTo func(string) (int, bool), fuelModel func() func(int) float64, out io.Writer) (freightStep, error) {
	if cand == nil {
		return freightStepReleased, nil
	}
	accepted, step := freightAccept(ctx, deps, cand, held, out)
	if step != freightStepProceed {
		return step, nil
	}
	if s := freightLoadPackage(ctx, deps, accepted, cand, out); s != freightStepProceed {
		return s, nil
	}
	publishActivity(deps.SetActivity, "Freight "+accepted.ID+" to "+accepted.DestinationBaseID)
	// The chain run's refill step bundles any further contracts on this
	// board that clear the gate, then walks the chain — with
	// FreightMaxPackages 1 this is exactly the v1 accept->trip sequence.
	return freightChainRun(ctx, deps, func(ctx context.Context, baseID string) error {
		return missionNavToBase(ctx, deps, baseID)
	}, hopsTo, fuelModel(), out)
}

// missionFreightOrDry is the tail of a dry path: take the freight candidate if
// there is one, otherwise count the dry pass. The board-empty and
// gate-empties-the-set paths both end this way. Ranking does NOT belong here —
// there is nothing left to rank against on a dry path, which is why this is a
// fallback rather than a second copy of the ranking switch in Missions.
//
// A stuck return (live undischarged contract) parks the pass instead of falling
// into missionDryPass, whose reposition logic would fly the worker away from the
// contract it still owes.
func missionFreightOrDry(ctx context.Context, deps MissionDeps, cand *freightCand, held []chainStop, hopsTo func(string) (int, bool), fuelModel func() func(int) float64, out io.Writer) error {
	step, ferr := missionTakeFreight(ctx, deps, cand, held, hopsTo, fuelModel, out)
	switch step {
	case freightStepProceed:
		return ferr
	case freightStepStuck:
		return nil
	}
	return missionDryPass(ctx, deps, out)
}

// freightReturn hands a contract back and records the outcome. A failed return
// is logged loudly: it is the only situation in which a breach becomes possible
// despite the design, so it must be visible in the canary logs.
func freightReturn(ctx context.Context, deps MissionDeps, out io.Writer, c serverapi.ShipmentContract, cand *freightCand, outcome, reason string) freightStep {
	return freightReturnVia(ctx, deps, out, nil, c, cand, outcome, reason)
}

// freightErrWrongOrigin is the server's machine-readable refusal code when a
// return is attempted away from the package's origin station. Live text:
// "wrong_origin: Return the intact package at its origin station." Only the
// code is matched; the prose after it is not part of any contract.
const freightErrWrongOrigin = "wrong_origin"

// freightIsWrongOrigin reports whether a return failure means the call was
// illegal HERE but legal at the contract's origin. It is the one return error
// that retrying from the same place can never resolve.
func freightIsWrongOrigin(err error) bool {
	return err != nil && strings.Contains(err.Error(), freightErrWrongOrigin)
}

// freightReturnAtOrigin flies back to a contract's origin station and retries
// the return there, since that is the only place the server accepts one.
// Returns the residual error, nil on success. Every failure string here keeps
// the wrong_origin code so the caller still classifies the contract as
// undischargeable rather than as a retryable blip.
func freightReturnAtOrigin(ctx context.Context, deps MissionDeps, out io.Writer, nav func(context.Context, string) error, c serverapi.ShipmentContract) error {
	if c.OriginBaseID == "" {
		return fmt.Errorf("%s and the contract carries no origin_base_id", freightErrWrongOrigin)
	}
	fmt.Fprintf(out, "freight: %s is only returnable at its origin %s; flying back to discharge it\n", c.ID, c.OriginBaseID) //nolint:errcheck
	if err := nav(ctx, c.OriginBaseID); err != nil {
		return fmt.Errorf("%s: navigate to origin %s: %w", freightErrWrongOrigin, c.OriginBaseID, err)
	}
	if err := freightSettleDock(ctx, deps, out); err != nil {
		return fmt.Errorf("%s: dock at origin %s: %w", freightErrWrongOrigin, c.OriginBaseID, err)
	}
	return deps.Client.ShippingReturn(ctx, c.ID)
}

// freightReturnVia is freightReturn with an origin-recovery hook. originNav
// non-nil offers a way to fly the ship home and return the package where the
// server will actually take it; nil means the caller either cannot navigate
// (accept, load — both already standing at the origin, so wrong_origin cannot
// arise for them) or has decided the trip home costs more than it saves.
//
// A wrong_origin refusal is Undischargeable either way: whatever the caller,
// retrying it from here is the one thing that is always wrong.
func freightReturnVia(ctx context.Context, deps MissionDeps, out io.Writer, originNav func(context.Context, string) error, c serverapi.ShipmentContract, cand *freightCand, outcome, reason string) freightStep {
	err := deps.Client.ShippingReturn(ctx, c.ID)
	if originNav != nil && freightIsWrongOrigin(err) {
		err = freightReturnAtOrigin(ctx, deps, out, originNav, c)
	}
	if err != nil {
		if freightIsWrongOrigin(err) {
			fmt.Fprintf(out, "freight: %s CANNOT be returned from here and its origin is out of reach (%v); flying it to its destination instead — a late delivery may still settle, retrying this return cannot\n", c.ID, err) //nolint:errcheck
			freightRecord(ctx, deps, out, c, cand, 0, "return_failed", "return refused away from origin: "+err.Error())
			deps.State.addHeldFreight(&c)
			return freightStepUndischargeable
		}
		fmt.Fprintf(out, "freight: RETURN FAILED for %s (%s): %v — breach now possible; parking this pass\n", c.ID, reason, err) //nolint:errcheck
		// Record return_failed, not the caller's outcome: the return itself
		// didn't happen, so this contract can still breach. Grouping by
		// Outcome must surface that as the alarm state it is, not as a clean
		// return.
		freightRecord(ctx, deps, out, c, cand, 0, "return_failed", "return failed: "+err.Error())
		// The contract is still live and still ours — keep (or establish) the
		// in-memory hold so the next pass's reconcile finds it.
		deps.State.addHeldFreight(&c)
		return freightStepStuck
	}
	freightRecord(ctx, deps, out, c, cand, 0, outcome, reason)
	deps.State.removeHeldFreight(c.ID)
	return freightStepReleased
}

// freightPackageItemID is the cargo/storage item id for a contract's package.
// The contract carries the bare hash in package_id; storage and cargo address it
// with a "package:" prefix (confirmed live).
func freightPackageItemID(packageID string) string {
	return "package:" + packageID
}

// freightReconcileSet verifies every remembered in-flight contract against
// the server before the pass takes any new work. An orphaned package rides
// to a default in silence (proven live 2026-07-20), so this runs before
// every pass. Per contract it applies exactly the v1 reconcile rules:
// fail-open on read trouble (memory wins; worst case is one clean deliver
// error), refresh on in_transit, record-and-drop on terminal statuses —
// "defaulted" records outcome "breached" because nothing else will.
// Server-side actives we do NOT remember are unrecoverable from here (own
// contracts never list on the board); the gate's unaccounted-for skip keeps
// us from accepting on top of them, and the loud log line below is the
// operator's rescue cue.
func freightReconcileSet(ctx context.Context, deps MissionDeps, out io.Writer) []*serverapi.ShipmentContract {
	if out == nil {
		out = io.Discard
	}
	for _, held := range deps.State.heldFreightAll() {
		freightVerifyHeld(ctx, deps, held, out)
	}
	survivors := deps.State.heldFreightAll()
	// Loud mismatch detection, log-only: the gate refuses new accepts on a
	// mismatch, so this cannot orphan anything further.
	if err := deps.Client.ShippingProfile(ctx); err == nil {
		var prof serverapi.ShippingProfileResponse
		if raw := deps.Client.GetRawJSON("shipping_profile"); len(raw) > 0 && json.Unmarshal(raw, &prof) == nil {
			if prof.Profile.ActiveContracts > len(survivors) {
				fmt.Fprintf(out, "freight: profile reports %d active contract(s) but memory holds %d — the held-freight file did not cover them (lost/corrupted or accepted elsewhere); operator rescue needed (own contracts never list)\n", //nolint:errcheck
					prof.Profile.ActiveContracts, len(survivors))
			}
		}
	}
	return survivors
}

// freightVerifyHeld re-reads one remembered contract via the synchronous
// `get` and updates held-set state per its status. See freightReconcileSet
// for the rules; this is the v1 freightReconcileHeld body, set-based.
func freightVerifyHeld(ctx context.Context, deps MissionDeps, held *serverapi.ShipmentContract, out io.Writer) {
	if err := deps.Client.ShippingGet(ctx, held.ID); err != nil {
		fmt.Fprintf(out, "freight: reconcile get %s: %v; resuming from memory\n", held.ID, err) //nolint:errcheck
		return
	}
	var resp serverapi.ShippingContractResponse
	if raw := deps.Client.GetRawJSON("shipping_get"); len(raw) > 0 {
		if err := json.Unmarshal(raw, &resp); err != nil {
			fmt.Fprintf(out, "freight: reconcile decode get %s: %v; resuming from memory\n", held.ID, err) //nolint:errcheck
			return
		}
	}
	c := resp.Contract
	if c.ID == "" {
		fmt.Fprintf(out, "freight: reconcile get %s returned no contract; resuming from memory\n", held.ID) //nolint:errcheck
		return
	}
	switch c.Status {
	case "in_transit":
		deps.State.addHeldFreight(&c) // refresh: server deadline etc. authoritative
	case "defaulted":
		fmt.Fprintf(out, "freight: held contract %s DEFAULTED server-side (flat debt; operator settles via pay_debt, package is keepable/unpackable)\n", c.ID) //nolint:errcheck
		freightRecord(ctx, deps, out, c, nil, 0, "breached", "reconciled: server status defaulted")
		deps.State.removeHeldFreight(c.ID)
	case "delivered":
		fmt.Fprintf(out, "freight: held contract %s already delivered server-side; recording without payout\n", c.ID) //nolint:errcheck
		freightRecord(ctx, deps, out, c, nil, 0, "delivered", "reconciled: settlement reply unseen; payout unrecorded")
		deps.State.removeHeldFreight(c.ID)
	default:
		fmt.Fprintf(out, "freight: held contract %s no longer in transit (status %q); releasing\n", c.ID, c.Status) //nolint:errcheck
		deps.State.removeHeldFreight(c.ID)
	}
}

// freightSettleDock waits until the ship's docked state has actually settled
// before a /shipping mutation is issued. Docking is tick-deferred, so a dock
// issued by nav may not be reflected in State yet; poll for up to three
// ticks, and nudge with one explicit Dock if a full tick passes without the
// pending dock landing (covers nav variants that stop short of docking).
// Uses deps.sleep so tests run instantly.
func freightSettleDock(ctx context.Context, deps MissionDeps, out io.Writer) error {
	if st := deps.Client.GetState(); st != nil && st.IsDocked() {
		return nil
	}
	sl := deps.sleep
	if sl == nil {
		sl = craftPollSleepFunc
	}
	const budget = 3 * game.SleepTick
	dockIssued := false
	for waited := time.Duration(0); waited < budget; waited += game.SleepQuick {
		if !dockIssued && waited >= game.SleepTick {
			// A pending dock had a full tick to land and didn't; ask
			// explicitly once. An error here is logged, not fatal — the
			// poll continues in case the original dock resolves anyway.
			dockIssued = true
			if err := deps.Client.Dock(ctx); err != nil {
				fmt.Fprintf(out, "freight: dock: %v\n", err) //nolint:errcheck
			}
		}
		if err := sl(ctx, game.SleepQuick); err != nil {
			return err
		}
		if st := deps.Client.GetState(); st != nil && st.IsDocked() {
			return nil
		}
	}
	return fmt.Errorf("still undocked after %s", budget)
}

// freightChainMaxLegs bounds the chain-run loop. Deliveries and returns
// strictly shrink the held set; refills are bounded by headroom — this
// guard exists only to make an unforeseen no-progress cycle exit loudly
// instead of spinning the pass forever.
const freightChainMaxLegs = 25

// freightChainStops prices the held set from the current dock. A held
// contract the router cannot reach right now prices at hops 0 with a log
// line — unroutable is a transient (mobile capitals, KB gaps), not proof of
// infeasibility, and pricing it 0 keeps its deadline check maximally
// conservative for the OTHER stops while its own deliver attempt resolves
// the truth.
func freightChainStops(ctx context.Context, deps MissionDeps, hopsTo func(string) (int, bool), out io.Writer) []chainStop {
	held := deps.State.heldFreightAll()
	stops := make([]chainStop, 0, len(held))
	for _, c := range held {
		hops := 0
		if hopsTo != nil {
			if h, ok := hopsTo(c.DestinationBaseID); ok {
				hops = h
			} else {
				fmt.Fprintf(out, "freight: no route to %s for held %s from here; pricing leg at 0\n", c.DestinationBaseID, c.ID) //nolint:errcheck
			}
		}
		stops = append(stops, chainStop{ContractID: c.ID, DestBaseID: c.DestinationBaseID, Hops: hops, DeadlineTick: c.DeadlineTick, RecoveryDeadlineTick: c.RecoveryDeadlineTick})
	}
	return stops
}

// freightPollLoaded waits until a withdrawn package actually lands in the hold.
// WithdrawItems is tick-deferred (client_commands.go acks the request, not the
// storage->cargo transfer), so returning Proceed the instant the ack lands can
// navigate a multi-package chain away with the package still in origin storage —
// the strand that loops forever on package_not_present (engineer-3, 2026-07-23).
// Mirrors freightSettleDock: poll up to three ticks at SleepQuick cadence, using
// deps.sleep so tests run instantly. Returns (true, nil) once aboard, (false, nil)
// on timeout, (false, err) if the sleep is cancelled.
func freightPollLoaded(ctx context.Context, deps MissionDeps, item string, out io.Writer) (bool, error) {
	if cargoCount(deps.Client.GetState(), item) >= 1 {
		return true, nil
	}
	sl := deps.sleep
	if sl == nil {
		sl = craftPollSleepFunc
	}
	const budget = 3 * game.SleepTick
	refreshErrs, refreshes := 0, 0
	var lastErr error
	for waited := time.Duration(0); waited < budget; waited += game.SleepQuick {
		if err := sl(ctx, game.SleepQuick); err != nil {
			return false, err
		}
		// Ask for cargo rather than re-reading the snapshot. The transfer
		// completes server-side, but Ship.Cargo only reflects it once a reply
		// updates state — nothing pushes cargo spontaneously, and the worker
		// heartbeat only reads local state. Without this the poll times out on
		// a package already aboard and returns a healthy contract: the live
		// 2026-07-24 regression that took the fleet to 0 deliveries.
		// Best-effort — a failed refresh is not proof of absence, so keep polling.
		// The error is still COUNTED: a refresh that never lands is
		// indistinguishable from an empty hold, and silently discarding it is
		// what made this failure mode unreadable for weeks (see the diagnostic
		// below).
		refreshes++
		if err := deps.Client.GetCargo(ctx); err != nil {
			refreshErrs++
			lastErr = err
		}
		if cargoCount(deps.Client.GetState(), item) >= 1 {
			return true, nil
		}
	}
	freightLogPollMiss(out, deps, item, refreshes, refreshErrs, lastErr)

	return false, nil
}

// freightLogPollMiss dumps what the poll could actually see when it gives up.
// A bare timeout cannot distinguish the three explanations for a package that
// was withdrawn successfully and later returned successfully (a return requires
// the sealed package IN the active ship, so it WAS aboard):
//
//	refresh never landed      -> refreshErrs > 0, or 0 entries with 0 errors
//	hold genuinely empty      -> 0 entries, refreshes all succeeded
//	aboard under another id   -> entries present, none matching item
//
// One occurrence of this line settles which. Instrumentation only — the caller's
// behavior is unchanged.
func freightLogPollMiss(out io.Writer, deps MissionDeps, item string, refreshes, refreshErrs int, lastErr error) {
	if out == nil {
		return
	}
	st := deps.Client.GetState()
	if st == nil {
		fmt.Fprintf(out, "freight: DIAG load poll missed %s: no client state; %d/%d cargo refreshes failed (last %v)\n", //nolint:errcheck
			item, refreshErrs, refreshes, lastErr)

		return
	}
	aboard := make([]string, 0, len(st.Ship.Cargo))
	for _, it := range st.Ship.Cargo {
		aboard = append(aboard, fmt.Sprintf("%s x%.0f", it.ItemID, it.Quantity))
	}
	fmt.Fprintf(out, "freight: DIAG load poll missed %s: %d/%d cargo refreshes failed (last %v); hold reads %.0f/%.0f used, %d entries [%s]\n", //nolint:errcheck
		item, refreshErrs, refreshes, lastErr, st.Ship.CargoUsed, st.Ship.CargoCapacity, len(aboard), strings.Join(aboard, ", "))
}

// freightLoadPackage pulls a contract's sealed package from station storage
// into the hold, if it is not already aboard (the reconcile-resume path
// re-enters with the package in CARGO — an unconditional withdraw would
// fail there and destroy a healthy contract). An absent, unloadable package
// means "cannot physically carry" and returns THAT contract.
func freightLoadPackage(ctx context.Context, deps MissionDeps, c *serverapi.ShipmentContract, cand *freightCand, out io.Writer) freightStep {
	item := freightPackageItemID(c.PackageID)
	if cargoCount(deps.Client.GetState(), item) >= 1 {
		return freightStepProceed
	}
	if err := deps.Client.WithdrawItems(ctx, item, 1); err != nil {
		fmt.Fprintf(out, "freight: withdraw %s: %v; returning contract\n", item, err) //nolint:errcheck
		return freightReturn(ctx, deps, out, *c, cand, "returned_infeasible", "package would not load: "+err.Error())
	}
	// Prefer the withdraw's OWN correlated reply over watching cargo: it is the
	// only load signal a concurrent state write cannot erase.
	if confirmed, known := freightWithdrawConfirmed(deps.Client.GetRawJSON("withdraw_items"), item); known {
		if confirmed {
			return freightStepProceed
		}
		return freightLoadUnconfirmed(ctx, deps, c, cand, item, "server reported no transfer", out)
	}
	loaded, err := freightPollLoaded(ctx, deps, item, out)
	if err != nil {
		// ctx cancelled mid-poll: the load is unconfirmed but the contract is still
		// held (freightAccept added it), so park rather than transit blind — the next
		// session's reconcile settles it.
		fmt.Fprintf(out, "freight: load poll for %s cancelled: %v; parking\n", item, err) //nolint:errcheck
		return freightStepStuck
	}
	if !loaded {
		return freightLoadUnconfirmed(ctx, deps, c, cand, item, "not visible in cargo after withdraw", out)
	}
	return freightStepProceed
}

// freightWithdrawConfirmed reads the withdraw's OWN action_result — the frame
// the server correlates to our request by request_id — and reports whether it
// says the package reached the hold. known=false means there was no such reply,
// or it was about a different item, so the caller must fall back to observation.
//
// This exists because Ship.Cargo is shared mutable state: parseShipData replaces
// the WHOLE Ship struct on any reply carrying a ship object — the 10s heartbeat's
// get_status among them — so a package that IS aboard can vanish from the local
// snapshot mid-poll. The correlated reply is immune to that race; a cargo poll is
// not. Trusting the poll is what returned 52 healthy contracts across 18 agents
// (traced 2026-07-27), each one costing a delivery that would have counted toward
// carrier tier. Live payload shape, operator-captured:
//
//	{"command":"withdraw_items","result":{"action":"withdraw_items","cargo_space":344,
//	 "cargo_total":1,"item_id":"xenon_gas","quantity":1,"storage_remaining":296}}
func freightWithdrawConfirmed(raw []byte, item string) (confirmed, known bool) {
	if len(raw) == 0 {
		return false, false
	}
	var res struct {
		ItemID     string   `json:"item_id"`
		Quantity   float64  `json:"quantity"`
		CargoTotal *float64 `json:"cargo_total"`
		DestTotal  *float64 `json:"dest_total"`
	}
	if err := json.Unmarshal(unwrapActionResult(raw), &res); err != nil {
		return false, false
	}
	// The raw store is keyed by command, so a leftover entry from an earlier
	// withdraw would lie about this one. Only a reply naming OUR item counts.
	if res.ItemID != item {
		return false, false
	}
	if res.Quantity >= 1 {
		return true, true
	}
	for _, total := range []*float64{res.CargoTotal, res.DestTotal} {
		if total != nil && *total >= 1 {
			return true, true
		}
	}
	return false, true
}

// freightLoadUnconfirmed parks a contract whose load could not be confirmed
// instead of handing it straight back. The package is usually ABOARD — what
// failed is the confirmation, not the transfer — and a return forfeits a
// delivery that would have counted toward carrier tier, which is precisely what
// stalled the probationary fleet. Parking keeps the contract held so the next
// pass's reconcile carries it to its destination; freightLoadMaxParks stops a
// genuinely uncarriable contract from pinning the worker forever.
//
// Without run state there is nowhere to count, so we do NOT park: an uncounted
// park could never end. That path keeps the pre-2026-07-27 return behavior.
func freightLoadUnconfirmed(ctx context.Context, deps MissionDeps, c *serverapi.ShipmentContract, cand *freightCand, item, why string, out io.Writer) freightStep {
	if parks, counted := deps.State.noteLoadPark(c.ID); counted && parks <= freightLoadMaxParks {
		fmt.Fprintf(out, "freight: load of %s unconfirmed (%s); parking %d/%d — it may already be aboard, and the next pass re-checks before flying it\n", //nolint:errcheck
			item, why, parks, freightLoadMaxParks)
		return freightStepStuck
	}
	fmt.Fprintf(out, "freight: package %s did not load after %d parks (%s); returning contract\n", item, freightLoadMaxParks, why) //nolint:errcheck
	return freightReturn(ctx, deps, out, *c, cand, "returned_infeasible", "package did not load into cargo after withdraw")
}

// freightDeliverDueHere delivers every held contract whose destination is
// baseID. Per contract this is the v1 runTrip deliver+record sequence; a
// deliver failure leaves that contract in flight (failed++) and the loop
// continues with the rest.
func freightDeliverDueHere(ctx context.Context, deps MissionDeps, baseID string, out io.Writer) (delivered, failed int) {
	for _, c := range deps.State.heldFreightAll() {
		if c.DestinationBaseID != baseID {
			continue
		}
		if err := deps.Client.ShippingDeliver(ctx, c.ID); err != nil {
			fmt.Fprintf(out, "freight: deliver %s: %v\n", c.ID, err) //nolint:errcheck
			failed++
			continue
		}
		var settle serverapi.ShippingSettlementResponse
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
		freightRecord(ctx, deps, out, final, nil, float64(settle.CarrierPayout), "delivered", settleReason)
		deps.State.removeHeldFreight(c.ID)
		delivered++
	}
	return delivered, failed
}

// freightChainRun is the sub-project C dock-pass loop: deliver what is due
// here, return what can no longer make its deadline, refill while headroom
// lasts (cap > 1 only), fly to the nearest held destination, repeat. At the
// default cap of 1, refill is skipped entirely: deliver, no refill, pass
// ends — the exact v1 trip shape (Plan Global Constraint #1).
//
// freightStepProceed covers both "set empty, pass done" and "contracts
// intentionally left in flight" (nav/deliver trouble — next pass reconciles
// and resumes). freightStepStuck means a return FAILED (live undischarged
// contract): the caller must park this pass. A return the server refuses
// outright because we are away from the package's origin is NOT stuck: the
// chain flies home to discharge it, or, failing that, carries it to its
// destination — see freightStepUndischargeable.
func freightChainRun(ctx context.Context, deps MissionDeps, nav func(ctx context.Context, baseID string) error, hopsTo func(string) (int, bool), fuelCostFor func(int) float64, out io.Writer) (freightStep, error) {
	if out == nil {
		out = io.Discard
	}
	attempted := map[string]bool{} // destinations that failed nav/deliver this pass
	// Contracts whose return the server refuses from here and whose origin we
	// could not reach. They stay held and get flown; they must never be
	// nominated for return again this pass.
	undischargeable := map[string]bool{}
	for leg := 0; leg < freightChainMaxLegs; leg++ {
		stops := freightChainStops(ctx, deps, hopsTo, out)
		if len(stops) == 0 {
			return freightStepProceed, nil
		}
		maxPk := freightEffectiveMax(deps.FreightMaxPackages)
		fmt.Fprintf(out, "freight: holding %d/%d packages\n", len(stops), maxPk) //nolint:errcheck

		// Return contracts whose deadline no longer clears the chain bound
		// from here — one at a time, re-pricing after each removal, so a
		// single degraded contract never takes healthy ones down with it.
		for {
			ok, reason := chainFeasible(stops, missionTick(deps))
			if ok {
				break
			}
			victim, returnable := freightWorstReturnableStop(stops, missionTick(deps), undischargeable)
			if !returnable {
				// Every blown stop is one we cannot hand back from here. Stop
				// pricing returns and fall through to the delivery legs below:
				// flying a doomed package at least gives it a chance to settle,
				// and it clears the hold either way.
				fmt.Fprintf(out, "freight: chain still infeasible (%s) but nothing left is returnable from here; flying the remainder anyway\n", reason) //nolint:errcheck
				break
			}
			c := freightHeldByID(deps, victim.ContractID)
			if c == nil {
				// Defensive: stop pricing a contract we no longer hold. This
				// leaves `stops` in a still-infeasible state by design — a
				// degradation, not a bug — the next leg re-prices from
				// scratch and either resolves it or picks a new victim.
				break
			}
			fmt.Fprintf(out, "freight: in-flight buffer collapsed for %s (%s); returning\n", c.ID, reason) //nolint:errcheck
			switch freightReturnVia(ctx, deps, out, freightOriginNav(deps, nav, hopsTo, stops, *c, out), *c, nil, "returned_inflight", reason) {
			case freightStepStuck:
				return freightStepStuck, nil
			case freightStepUndischargeable:
				// Held, unreturnable, and now excluded from victim selection so
				// the next iteration prices the rest of the chain instead of
				// re-nominating this one forever.
				undischargeable[c.ID] = true
				continue
			}
			stops = freightChainStops(ctx, deps, hopsTo, out)
			if len(stops) == 0 {
				return freightStepProceed, nil
			}
		}

		// Nav to the nearest held destination not already attempted this pass.
		target := ""
		for _, s := range chainOrder(stops) {
			if !attempted[s.DestBaseID] {
				target = s.DestBaseID
				break
			}
		}
		if target == "" {
			// Every remaining destination already failed once this pass;
			// leave the rest in flight for the next pass.
			return freightStepProceed, nil
		}
		if err := nav(ctx, target); err != nil {
			fmt.Fprintf(out, "freight: navigate to %s: %v; leaving in flight\n", target, err) //nolint:errcheck
			attempted[target] = true
			continue
		}
		if err := freightSettleDock(ctx, deps, out); err != nil {
			fmt.Fprintf(out, "freight: dock settle at %s: %v; leaving in flight\n", target, err) //nolint:errcheck
			attempted[target] = true
			continue
		}
		_, failed := freightDeliverDueHere(ctx, deps, target, out)
		if failed > 0 {
			// Headroom cannot be trusted mid-failure: no refill at this dock
			// (spec, Error handling). Try the next destination instead.
			attempted[target] = true
			continue
		}

		// Refill: accept while the gate still clears (headroom + chain
		// feasibility are all inside freightCandidate/freightAccept). A Stuck
		// result means a return FAILED inside the refill loop (live
		// undischarged contract) and must propagate as the pass's own
		// park-now signal, not be swallowed.
		//
		// At the default cap (1), refill must NOT run at all: v1 ended the
		// trip after one delivery, and Plan Global Constraint #1 requires the
		// default to reproduce that exactly (no new accept at this dock, pass
		// ends so the next pass re-ranks freight against missions/exploration
		// from here).
		if maxPk == 1 {
			fmt.Fprintln(out, "freight: cap 1, no refill (v1 trip shape)") //nolint:errcheck
		} else if freightChainRefill(ctx, deps, hopsTo, fuelCostFor, out) == freightStepStuck {
			return freightStepStuck, nil
		}
	}
	fmt.Fprintf(out, "freight: chain-run leg guard (%d) hit; leaving remainder in flight\n", freightChainMaxLegs) //nolint:errcheck
	return freightStepProceed, nil
}

// freightOriginNav decides whether a doomed contract is worth flying home to
// discharge, returning nav when it is and nil when it is not.
//
// Returning at the origin is the better outcome — it avoids the failure debt,
// which silently blocks every later accept. But the trip is not free: on a
// multi-package carrier it delays everything else in the hold, and a detour
// that breaches two live contracts to save one dead one is a bad trade. So the
// remaining chain is re-priced with the detour charged up front, and the trip
// is declined outright if that pushes any survivor past its deadline.
//
// An unroutable origin is NOT a refusal: hops are unknown, not infinite
// (mobile capitals, KB gaps), so the attempt proceeds and a failing nav
// settles it cheaply — same fail-open reading as freightChainStops.
func freightOriginNav(deps MissionDeps, nav func(context.Context, string) error, hopsTo func(string) (int, bool), stops []chainStop, c serverapi.ShipmentContract, out io.Writer) func(context.Context, string) error {
	if nav == nil || hopsTo == nil || c.OriginBaseID == "" {
		return nav
	}
	survivors := make([]chainStop, 0, len(stops))
	for _, s := range stops {
		if s.ContractID != c.ID {
			survivors = append(survivors, s)
		}
	}
	if len(survivors) == 0 {
		return nav // nothing else aboard to protect
	}
	h, ok := hopsTo(c.OriginBaseID)
	if !ok {
		return nav
	}
	if affordable, why := chainFeasibleAfterDetour(survivors, missionTick(deps), h); !affordable {
		fmt.Fprintf(out, "freight: NOT flying %s home to %s (%d hops) — %s; carrying it instead so the rest of the hold survives\n", //nolint:errcheck
			c.ID, c.OriginBaseID, h, why)
		return nil
	}
	return nav
}

// freightWorstReturnableStop picks the stop to sacrifice when the chain bound
// fails: the blown stop with the least deadline slack at its chain position,
// ignoring any contract in skip. ok is false when nothing eligible remains.
//
// Only stops that miss their OWN bound (negative slack) are eligible. Without
// that, excluding an undischargeable contract promotes the next-worst stop —
// which may be perfectly healthy — and the loop hands back a contract it could
// still have delivered. Before the skip set existed the distinction was
// invisible: whenever the chain failed, the least-slack stop was always a
// blown one.
//
// Skipped contracts stay in the chain MATH — they are still aboard and still
// cost hops — they are only barred from nomination.
func freightWorstReturnableStop(stops []chainStop, nowTick int64, skip map[string]bool) (chainStop, bool) {
	ordered := chainOrder(stops)
	cum := chainCumulative(ordered)
	var worst chainStop
	worstSlack, found := 0.0, false
	for i, s := range ordered {
		limit := s.deliverableUntil()
		if limit <= 0 || skip[s.ContractID] {
			continue
		}
		slack := float64(limit-nowTick) - float64(cum[i])*freightTicksPerHop*freightDeadlineSlack
		if slack < 0 && slack < worstSlack {
			worst, worstSlack, found = s, slack, true
		}
	}
	return worst, found
}

// freightHeldByID fetches one held contract, nil when absent.
func freightHeldByID(deps MissionDeps, id string) *serverapi.ShipmentContract {
	for _, c := range deps.State.heldFreightAll() {
		if c.ID == id {
			return c
		}
	}
	return nil
}

// freightChainRefill accepts additional contracts at the current dock while
// the gate clears. Each accept immediately loads its package so hold-space
// headroom self-updates for the next iteration; a load failure has already
// returned that contract inside freightLoadPackage, and refill stops for
// this dock rather than retrying — an unbounded retry here is how a
// systemic load failure (persistent WithdrawItems trouble, or the server
// re-listing a just-returned contract) turns into an infinite
// accept/withdraw-fail/return loop.
//
// Returns freightStepStuck the moment either freightAccept or
// freightLoadPackage reports Stuck: a FAILED return means a live
// undischarged contract, which is the ONLY global park condition, and it
// must propagate to the caller rather than be swallowed here. Returns
// freightStepProceed for every other outcome: the gate simply ran dry (no
// candidate), an accept came back Released (accept itself failed, or the
// contract proved infeasible and was returned cleanly), or a load came back
// Released (package would not load, contract returned) — refill stops for
// this dock either way, but the pass as a whole is free to continue.
func freightChainRefill(ctx context.Context, deps MissionDeps, hopsTo func(string) (int, bool), fuelCostFor func(int) float64, out io.Writer) freightStep {
	for {
		heldStops := freightChainStops(ctx, deps, hopsTo, out)
		in := freightInputs{
			CargoFree:   float64(cargoFreeSpace(deps.Client.GetState())),
			FuelCostFor: fuelCostFor,
			HopsTo:      hopsTo,
			Held:        heldStops,
			NowTick:     missionTick(deps),
		}
		cand, skip := freightCandidate(ctx, deps, in, out)
		if cand == nil {
			fmt.Fprintf(out, "freight: refill done (%s)\n", skip) //nolint:errcheck
			return freightStepProceed
		}
		accepted, step := freightAccept(ctx, deps, cand, heldStops, out)
		if step == freightStepStuck {
			return freightStepStuck
		}
		if step != freightStepProceed {
			return freightStepProceed // released; refill stops here, pass continues
		}
		if step := freightLoadPackage(ctx, deps, accepted, cand, out); step != freightStepProceed {
			if step == freightStepStuck {
				return freightStepStuck
			}
			// Released: the contract was returned (unloadable package), same as
			// an accept-release above. Stop refilling at this dock rather than
			// looping — an unbounded retry here is how a systemic load failure
			// (e.g. persistent WithdrawItems trouble) churns accept+return
			// forever if the board keeps re-offering. The pass continues
			// without further refill; the next dock re-evaluates from scratch.
			return freightStepProceed
		}
	}
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
