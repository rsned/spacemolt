package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/market"
)

// noFuel prices every route at zero, isolating reward arithmetic.
func noFuel(_ int) float64 { return 0 }

// flatFuel charges 10 credits per jump.
func flatFuel(jumps int) float64 { return float64(jumps) * 10 }

func listing(id string, eligible bool, reward int64, hops int) serverapi.ShippingListing {
	return serverapi.ShippingListing{
		Eligible: eligible,
		Contract: serverapi.ShipmentContract{
			ID:                id,
			DestinationBaseID: "sol_central",
			BaseReward:        reward,
			RouteHops:         hops,
			ServiceLevel:      "standard",
		},
	}
}

// The sealed package is a flat 100 units, so capacity maps to a whole-package
// count. A hold that cannot take one package must yield zero.
func TestFreightPackagesFit(t *testing.T) {
	cases := []struct {
		name      string
		cargoFree float64
		want      int
	}{
		{"empty hold", 0, 0},
		{"just under one package", 99, 0},
		{"exactly one package", 100, 1},
		{"one and a half", 150, 1},
		{"six packages", 600, 6},
		{"negative is clamped", -5, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := freightPackagesFit(tc.cargoFree); got != tc.want {
				t.Fatalf("freightPackagesFit(%v) = %d, want %d", tc.cargoFree, got, tc.want)
			}
		})
	}
}

func TestBuildFreightCandRejects(t *testing.T) {
	// Ineligible listings never become candidates — the server's flag already
	// encodes carrier tier, liability and debt, and we do not second-guess it.
	if _, reason := buildFreightCand(listing("a", false, 5000, 2), 2, nil, 0, noFuel, freightMinNet); reason == "" {
		t.Fatal("an ineligible listing must be rejected")
	}
	// Below the net floor.
	if _, reason := buildFreightCand(listing("b", true, 100, 2), 2, nil, 0, noFuel, freightMinNet); reason == "" {
		t.Fatal("a reward below freightMinNet must be rejected")
	}
	// Fuel can push an otherwise-acceptable reward under the floor.
	if _, reason := buildFreightCand(listing("c", true, 520, 5), 5, nil, 0, flatFuel, freightMinNet); reason == "" {
		t.Fatal("net after fuel below freightMinNet must be rejected")
	}
	// No destination is unroutable.
	bad := listing("d", true, 5000, 2)
	bad.Contract.DestinationBaseID = ""
	if _, reason := buildFreightCand(bad, 2, nil, 0, noFuel, freightMinNet); reason == "" {
		t.Fatal("a listing with no destination must be rejected")
	}
}

func TestBuildFreightCandAccepts(t *testing.T) {
	c, reason := buildFreightCand(listing("e", true, 5000, 3), 3, nil, 0, flatFuel, freightMinNet)
	if reason != "" {
		t.Fatalf("want acceptance, got skip reason %q", reason)
	}
	if c.Net != 5000-30 {
		t.Fatalf("net = %v, want %v (reward 5000 - fuel 30)", c.Net, 5000-30)
	}
	if c.Hops != 3 || c.DestBaseID != "sol_central" {
		t.Fatalf("routing fields wrong: %+v", c)
	}
	// max_speed_bonus is upside only; it must never lift a candidate over the floor.
	low := listing("f", true, 100, 1)
	low.Contract.MaxSpeedBonus = 10000
	if _, reason := buildFreightCand(low, 1, nil, 0, noFuel, freightMinNet); reason == "" {
		t.Fatal("max_speed_bonus must not count toward the net floor")
	}
}

func TestSelectFreightCandPicksHighestNet(t *testing.T) {
	if got := selectFreightCand(nil); got != nil {
		t.Fatal("no candidates must select nothing")
	}
	a, _ := buildFreightCand(listing("a", true, 1000, 1), 1, nil, 0, noFuel, freightMinNet)
	b, _ := buildFreightCand(listing("b", true, 9000, 1), 1, nil, 0, noFuel, freightMinNet)
	c, _ := buildFreightCand(listing("c", true, 3000, 1), 1, nil, 0, noFuel, freightMinNet)
	got := selectFreightCand([]freightCand{a, b, c})
	if got == nil || got.Contract.ID != "b" {
		t.Fatalf("want highest-net candidate b, got %+v", got)
	}
}

// The deadline is only knowable after accept, so this gate runs post-accept.
// The smoke's own contract (3 hops, 180 ticks granted) must clear it.
// Migrated from v1's freightDeadlineOK (Task 6 retired it): chainFeasible's
// single-stop case reduces to exactly the same worst-case bound (cum[0] ==
// hops for a lone stop), so these scenarios move here unchanged.
func TestFreightChainFeasibleSingleStopDeadlineGate(t *testing.T) {
	// 3 hops * 19 ticks * 1.5 slack = 85.5 needed.
	live := []chainStop{{ContractID: "high", Hops: 3, DeadlineTick: 1380}}
	if ok, reason := chainFeasible(live, 1200); !ok {
		t.Fatalf("the live smoke contract must clear the gate, got %q", reason)
	}
	tight := []chainStop{{ContractID: "high", Hops: 3, DeadlineTick: 1240}}
	if ok, _ := chainFeasible(tight, 1200); ok {
		t.Fatal("40 ticks for a 3-hop trip must fail the gate")
	}
	// Zero hops (same-base contract) still needs a positive deadline window.
	zeroHop := []chainStop{{ContractID: "high", Hops: 0, DeadlineTick: 1201}}
	if ok, _ := chainFeasible(zeroHop, 1200); !ok {
		t.Fatal("a zero-hop contract with a live deadline must pass")
	}
}

// A missing deadline (DeadlineTick <= 0) is NOT gated by chainFeasible, which
// treats it as "not yet known" and skips it — that fail-closed rule lives
// solely in freightAccept, applied the instant the server assigns a real
// deadline. Migrated from v1's freightDeadlineOK zero-deadline case.
func TestFreightAcceptRejectsMissingDeadline(t *testing.T) {
	store := &fakeFreightStore{}
	noDeadline := acceptedContract(1200, 0) // server assigned no deadline_tick
	f := &fakeClient{
		state: &game.State{CurrentTick: 1200},
		raw:   map[string][]byte{"shipping_accept": shippingContractJSON(t, "accept", noDeadline)},
	}
	deps := MissionDeps{Client: f, AgentID: "fighter-4", Market: store}
	cand := &freightCand{Contract: serverapi.ShipmentContract{ID: "high"}, Hops: 3, Net: 6000}

	got, step := freightAccept(context.Background(), deps, cand, nil, io.Discard)
	if step == freightStepProceed || got != nil {
		t.Fatal("a contract with no deadline_tick must be released, not carried")
	}
	if !slices.Contains(f.shippingCalls, "return") {
		t.Fatalf("must return the contract, calls were %v", f.shippingCalls)
	}
	if len(store.results) != 1 || store.results[0].Outcome != "returned_infeasible" {
		t.Fatalf("want returned_infeasible, got %+v", store.results)
	}
	if !strings.Contains(store.results[0].Reason, "no deadline_tick") {
		t.Fatalf("reason must say why, got %q", store.results[0].Reason)
	}
}

func shippingListJSON(t *testing.T, listings ...serverapi.ShippingListing) []byte {
	t.Helper()
	b, err := json.Marshal(serverapi.ShippingListResponse{
		Action: "list", Shipments: listings, Total: len(listings),
	})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func shippingProfileJSON(t *testing.T, blocked bool, reason string) []byte {
	t.Helper()
	b, err := json.Marshal(serverapi.ShippingProfileResponse{
		Action:               "profile",
		Profile:              serverapi.CarrierProfile{Tier: "probationary"},
		DebtBlocksAcceptance: blocked,
		DebtBlockReason:      reason,
	})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// shippingProfileActiveJSON is a clean (debt-free) profile that already reports
// n live contracts.
func shippingProfileActiveJSON(t *testing.T, n int) []byte {
	t.Helper()
	b, err := json.Marshal(serverapi.ShippingProfileResponse{
		Action:  "profile",
		Profile: serverapi.CarrierProfile{Tier: "probationary", ActiveContracts: n},
	})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// freightTestInputs routes every destination at 2 hops with free fuel.
func freightTestInputs(cargoFree float64) freightInputs {
	return freightInputs{
		CargoFree:   cargoFree,
		FuelCostFor: noFuel,
		HopsTo:      func(string) (int, bool) { return 2, true },
	}
}

// A hold too small for a package must short-circuit before ANY server call.
// This is the cheapest possible rejection and must stay that way.
func TestFreightCandidateSkipsWhenHoldTooSmall(t *testing.T) {
	f := &fakeClient{state: &game.State{}}
	deps := MissionDeps{Client: f, AgentID: "fighter-4"}

	cand, reason := freightCandidate(context.Background(), deps, freightTestInputs(99), io.Discard)
	if cand != nil {
		t.Fatal("a 99-unit hold cannot carry a 100-unit package")
	}
	if reason == "" {
		t.Fatal("want a skip reason")
	}
	if len(f.shippingCalls) != 0 {
		t.Fatalf("must make zero shipping calls, made %v", f.shippingCalls)
	}
}

// Freight debt blocks acceptance server-side; we skip freight without spending
// a list call, and never auto-pay.
func TestFreightCandidateSkipsWhenDebtBlocked(t *testing.T) {
	f := &fakeClient{
		state: &game.State{},
		raw:   map[string][]byte{"shipping_profile": shippingProfileJSON(t, true, "unpaid failure debt")},
	}
	deps := MissionDeps{Client: f, AgentID: "fighter-4"}
	var log strings.Builder

	cand, reason := freightCandidate(context.Background(), deps, freightTestInputs(500), &log)
	if cand != nil {
		t.Fatal("debt-blocked carriers must not take freight")
	}
	if !strings.Contains(reason, "unpaid failure debt") {
		t.Fatalf("skip reason must carry the server's debt_block_reason, got %q", reason)
	}
	for _, c := range f.shippingCalls {
		if c == "list" {
			t.Fatal("must not list the board while debt-blocked")
		}
		if c == "pay_debt" {
			t.Fatal("must never auto-pay debt")
		}
	}
}

// One contract at a time. freightReconcile gives up — (nil, false) — when the
// profile reports actives but the board read shows none, and the pass then falls
// straight through to freightCandidate. Without this guard that path accepts a
// SECOND contract while the first is still undischarged, and the orphaned first
// is the one that breaches.
func TestFreightCandidateSkipsWhenContractAlreadyHeld(t *testing.T) {
	f := &fakeClient{
		state: &game.State{},
		raw: map[string][]byte{
			"shipping_profile": shippingProfileActiveJSON(t, 1),
			"shipping_list":    shippingListJSON(t, listing("high", true, 9000, 2)),
		},
	}
	deps := MissionDeps{Client: f, AgentID: "fighter-4"}
	var log strings.Builder

	cand, reason := freightCandidate(context.Background(), deps, freightTestInputs(500), &log)
	if cand != nil {
		t.Fatalf("must not take a second contract while one is held, got %s", cand.Contract.ID)
	}
	if !strings.Contains(reason, "unaccounted") {
		t.Fatalf("want the unaccounted-contract skip reason, got %q", reason)
	}
	if !strings.Contains(log.String(), "unaccounted") {
		t.Fatalf("the decline must show in the pass log, got %q", log.String())
	}
	for _, c := range f.shippingCalls {
		if c == "accept" {
			t.Fatal("must never accept a second contract")
		}
	}
}

func TestFreightCandidatePicksBestEligible(t *testing.T) {
	f := &fakeClient{
		state: &game.State{},
		raw: map[string][]byte{
			"shipping_profile": shippingProfileJSON(t, false, ""),
			"shipping_list": shippingListJSON(t,
				listing("low", true, 800, 2),
				listing("high", true, 6000, 2),
				listing("ineligible", false, 99000, 2),
			),
		},
	}
	deps := MissionDeps{Client: f, AgentID: "fighter-4"}

	cand, reason := freightCandidate(context.Background(), deps, freightTestInputs(500), io.Discard)
	if cand == nil {
		t.Fatalf("want a candidate, got skip: %s", reason)
	}
	if cand.Contract.ID != "high" {
		t.Fatalf("want the highest-net eligible contract, got %q", cand.Contract.ID)
	}
}

// Rejected candidates must have their net logged. This is how the canary
// measures the real NPC reward distribution against freightMinNet.
func TestFreightCandidateLogsRejectedNets(t *testing.T) {
	f := &fakeClient{
		state: &game.State{},
		raw: map[string][]byte{
			"shipping_profile": shippingProfileJSON(t, false, ""),
			"shipping_list":    shippingListJSON(t, listing("cheap", true, 100, 2)),
		},
	}
	deps := MissionDeps{Client: f, AgentID: "fighter-4"}
	var log strings.Builder

	cand, _ := freightCandidate(context.Background(), deps, freightTestInputs(500), &log)
	if cand != nil {
		t.Fatal("a 100-credit reward is below the floor")
	}
	if !strings.Contains(log.String(), "100") {
		t.Fatalf("the rejected reward must appear in the log; got %q", log.String())
	}
}

// An unroutable destination is skipped, not guessed at.
func TestFreightCandidateSkipsUnroutableDestination(t *testing.T) {
	f := &fakeClient{
		state: &game.State{},
		raw: map[string][]byte{
			"shipping_profile": shippingProfileJSON(t, false, ""),
			"shipping_list":    shippingListJSON(t, listing("far", true, 9000, 2)),
		},
	}
	deps := MissionDeps{Client: f, AgentID: "fighter-4"}
	in := freightTestInputs(500)
	in.HopsTo = func(string) (int, bool) { return 0, false }

	if cand, _ := freightCandidate(context.Background(), deps, in, io.Discard); cand != nil {
		t.Fatal("an unroutable destination must not become a candidate")
	}
}

func shippingContractJSON(t *testing.T, action string, c serverapi.ShipmentContract) []byte {
	t.Helper()
	b, err := json.Marshal(serverapi.ShippingContractResponse{Action: action, Contract: c})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// acceptedContract is the smoke's real shape: deadline set AT accept.
func acceptedContract(acceptedTick, deadlineTick int64) serverapi.ShipmentContract {
	return serverapi.ShipmentContract{
		ID:                "high",
		PackageID:         "pkg_hash",
		OriginBaseID:      "haven_station",
		DestinationBaseID: "sol_central",
		ServiceLevel:      "standard",
		Status:            "in_transit",
		AcceptedTick:      acceptedTick,
		TargetTick:        1290,
		DeadlineTick:      deadlineTick,
		BaseReward:        6000,
		RouteHops:         3,
	}
}

func TestFreightAcceptProceedsWhenDeadlineFeasible(t *testing.T) {
	f := &fakeClient{
		state: &game.State{CurrentTick: 1200},
		raw:   map[string][]byte{"shipping_accept": shippingContractJSON(t, "accept", acceptedContract(1200, 1380))},
	}
	deps := MissionDeps{Client: f, AgentID: "fighter-4", Market: &fakeMissionStore{}}
	cand := &freightCand{Contract: serverapi.ShipmentContract{ID: "high"}, Hops: 3, Net: 6000}

	got, step := freightAccept(context.Background(), deps, cand, nil, io.Discard)
	if step != freightStepProceed || got == nil {
		t.Fatal("a feasible contract must proceed")
	}
	if got.DeadlineTick != 1380 {
		t.Fatalf("the accepted contract's real deadline must be read back, got %d", got.DeadlineTick)
	}
	for _, c := range f.shippingCalls {
		if c == "return" {
			t.Fatal("a feasible contract must not be returned")
		}
	}
}

// The whole point of accept-then-verify: an infeasible deadline is discovered
// after committing, and `return` is the debt-free escape. AcceptedTick (1100)
// is deliberately earlier than the client's current tick (1200) — the accept
// reply is tick-deferred, so by the time it's in hand the game clock has
// moved on. Deadline 1210 read against AcceptedTick looks feasible (110
// ticks, needs 85.5); read against the current tick it's not (10 ticks).
// This gap is what makes the check meaningful: see step 3 in the commit
// message / task report for proof that checking against AcceptedTick lets
// this contract wrongly proceed.
func TestFreightAcceptReturnsWhenDeadlineInfeasible(t *testing.T) {
	store := &fakeFreightStore{}
	f := &fakeClient{
		state: &game.State{CurrentTick: 1200},
		raw:   map[string][]byte{"shipping_accept": shippingContractJSON(t, "accept", acceptedContract(1100, 1210))},
	}
	deps := MissionDeps{Client: f, AgentID: "fighter-4", Market: store}
	cand := &freightCand{Contract: serverapi.ShipmentContract{ID: "high"}, Hops: 3, Net: 6000}

	got, step := freightAccept(context.Background(), deps, cand, nil, io.Discard)
	if step == freightStepProceed || got != nil {
		t.Fatal("an infeasible contract must be released")
	}
	if !slices.Contains(f.shippingCalls, "return") {
		t.Fatalf("must return the contract, calls were %v", f.shippingCalls)
	}
	if len(store.results) != 1 || store.results[0].Outcome != "returned_infeasible" {
		t.Fatalf("must record returned_infeasible, got %+v", store.results)
	}
}

// A lost race (someone else took it) is a normal skip, recorded for the canary.
func TestFreightAcceptRecordsAcceptFailure(t *testing.T) {
	store := &fakeFreightStore{}
	f := &fakeClient{
		state:       &game.State{},
		shippingErr: map[string]error{"accept": errors.New("contract already accepted")},
	}
	deps := MissionDeps{Client: f, AgentID: "fighter-4", Market: store}
	cand := &freightCand{Contract: serverapi.ShipmentContract{ID: "high"}, Hops: 3, Net: 6000}

	if _, step := freightAccept(context.Background(), deps, cand, nil, io.Discard); step == freightStepProceed {
		t.Fatal("a failed accept must not proceed")
	}
	if len(store.results) != 1 || store.results[0].Outcome != "accept_failed" {
		t.Fatalf("must record accept_failed, got %+v", store.results)
	}
}

// If the return call itself fails, the contract was never actually handed
// back and can still breach — the one path the design doesn't cover. That
// must be recorded as its own return_failed outcome, not as a clean
// returned_infeasible, or a dashboard grouping by Outcome would hide the
// canary's stop signal.
func TestFreightAcceptRecordsReturnFailure(t *testing.T) {
	store := &fakeFreightStore{}
	f := &fakeClient{
		state:       &game.State{CurrentTick: 1200},
		raw:         map[string][]byte{"shipping_accept": shippingContractJSON(t, "accept", acceptedContract(1100, 1210))},
		shippingErr: map[string]error{"return": errors.New("connection reset")},
	}
	deps := MissionDeps{Client: f, AgentID: "fighter-4", Market: store}
	cand := &freightCand{Contract: serverapi.ShipmentContract{ID: "high"}, Hops: 3, Net: 6000}

	got, step := freightAccept(context.Background(), deps, cand, nil, io.Discard)
	if step == freightStepProceed || got != nil {
		t.Fatal("an infeasible contract must be released")
	}
	if len(store.results) != 1 || store.results[0].Outcome != "return_failed" {
		t.Fatalf("must record return_failed, got %+v", store.results)
	}
	if !strings.Contains(store.results[0].Reason, "connection reset") {
		t.Fatalf("reason must carry the underlying error, got %q", store.results[0].Reason)
	}
}

func shippingSettlementJSON(t *testing.T, action string, c serverapi.ShipmentContract, payout int64) []byte {
	t.Helper()
	b, err := json.Marshal(serverapi.ShippingSettlementResponse{
		Action: action, Contract: c, CarrierPayout: payout,
	})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Migrated from v1's freightRunTrip to freightChainRun (Task 6 retired
// freightRunTrip): a single held stop walks exactly the same
// nav->settle->deliver->record sequence. The withdraw-before-transit
// assertion moved to the freightLoadPackage-focused tests below — the chain
// loop itself no longer withdraws (that invariant is now established once, at
// accept time, before a contract ever enters the held set).
func TestFreightChainRunDeliversAndRecordsPayout(t *testing.T) {
	store := &fakeFreightStore{}
	delivered := acceptedContract(1200, 1380)
	delivered.Status = "delivered"
	f := &fakeClient{
		// Doc: the ship is docked at the destination — freightSettleDock
		// requires it before deliver is issued (dock is tick-deferred live).
		state: &game.State{Doc: true},
		raw:   map[string][]byte{"shipping_deliver": shippingSettlementJSON(t, "deliver", delivered, 6000)},
	}
	deps := MissionDeps{Client: f, AgentID: "fighter-4", Market: store, State: &missionRunState{}}
	c := acceptedContract(1200, 1380)
	deps.State.addHeldFreight(&c)
	var navigated []string
	nav := func(ctx context.Context, baseID string) error { navigated = append(navigated, baseID); return nil }
	hopsTo := func(string) (int, bool) { return 3, true }

	step, err := freightChainRun(context.Background(), deps, nav, hopsTo, nil, io.Discard)
	if err != nil || step != freightStepProceed {
		t.Fatalf("step=%v err=%v", step, err)
	}
	if len(navigated) != 1 || navigated[0] != "sol_central" {
		t.Fatalf("must navigate to destination_base_id, went to %v", navigated)
	}
	if len(store.results) != 1 || store.results[0].Outcome != "delivered" {
		t.Fatalf("want a delivered result, got %+v", store.results)
	}
	if store.results[0].CarrierPayout != 6000 {
		t.Fatalf("payout must come from the settlement reply, got %v", store.results[0].CarrierPayout)
	}
	if deps.State.heldFreightCount() != 0 {
		t.Fatal("a delivered contract must be cleared from the held set")
	}
}

// After a restart the in-memory held set is gone, and the board read never
// lists our own in_transit contracts (proven live 2026-07-20), so board-scan
// recovery is impossible: reconcile cannot resume anything from the server,
// it can only detect the mismatch and log it loudly for an operator rescue.
// (v1's TestFreightReconcileFindsHeldContract asserted board-scan recovery;
// that behavior was proven dead and is intentionally dropped — see Task 4
// brief. What this test still protects is the loud-log/no-accept guarantee;
// the gate's unaccounted-for skip, covered elsewhere, is what actually stops
// a second accept from stacking on top of the orphaned contract.)
func TestFreightReconcileLogsLoudMismatchWhenMemoryEmpty(t *testing.T) {
	f := &fakeClient{
		state: &game.State{},
		raw: map[string][]byte{
			"shipping_profile": func() []byte {
				b, _ := json.Marshal(serverapi.ShippingProfileResponse{
					Action:  "profile",
					Profile: serverapi.CarrierProfile{ActiveContracts: 1},
				})
				return b
			}(),
		},
	}
	deps := MissionDeps{Client: f, AgentID: "fighter-4", Market: &fakeFreightStore{}}
	var log strings.Builder

	held := freightReconcileSet(context.Background(), deps, &log)
	if len(held) != 0 {
		t.Fatalf("empty memory must never resume anything from the board, got %+v", held)
	}
	if slices.Contains(f.shippingCalls, "list") {
		t.Fatal("board-scan recovery is dropped; must not read the board")
	}
	if !strings.Contains(log.String(), "operator rescue") {
		t.Fatalf("a profile/memory mismatch must log loudly for operator rescue, got %q", log.String())
	}
}

func TestFreightReconcileNoActiveContracts(t *testing.T) {
	f := &fakeClient{
		state: &game.State{},
		raw:   map[string][]byte{"shipping_profile": shippingProfileJSON(t, false, "")},
	}
	deps := MissionDeps{Client: f, AgentID: "fighter-4", Market: &fakeFreightStore{}}

	if held := freightReconcileSet(context.Background(), deps, io.Discard); len(held) != 0 {
		t.Fatalf("no active contracts must reconcile to nothing, got %+v", held)
	}
}

// If the package cannot be pulled into the hold we must not transit — a
// contract we cannot physically carry is a guaranteed breach. Migrated from
// v1's freightRunTrip directly onto freightLoadPackage, which now owns this
// check: Task 6's chain loop no longer withdraws per leg, so "must not
// transit" is enforced by freightLoadPackage's caller never entering the
// chain on a non-Proceed step (covered at the missionTakeFreight level).
func TestFreightLoadPackageReturnsWhenWithdrawFails(t *testing.T) {
	store := &fakeFreightStore{}
	f := &fakeClient{
		state:       &game.State{},
		withdrawErr: errors.New("no such item in storage"),
	}
	deps := MissionDeps{Client: f, AgentID: "fighter-4", Market: store}
	c := acceptedContract(1200, 1380)

	if step := freightLoadPackage(context.Background(), deps, &c, &freightCand{Hops: 3}, io.Discard); step == freightStepProceed {
		t.Fatal("a package that will not load must not proceed")
	}
	if !slices.Contains(f.shippingCalls, "return") {
		t.Fatalf("must return the contract, calls were %v", f.shippingCalls)
	}
	if len(store.results) != 1 || store.results[0].Outcome != "returned_infeasible" {
		t.Fatalf("want returned_infeasible, got %+v", store.results)
	}
}

// A withdraw that succeeds but whose package never lands in cargo (tick-deferred
// transfer that silently fails) must NOT proceed — proceeding is the multi-package
// strand bug. The contract is returned instead.
func TestFreightLoadPackageReturnsWhenPackageNeverLoads(t *testing.T) {
	store := &fakeFreightStore{}
	f := &fakeClient{state: &game.State{}} // withdraw succeeds; cargo stays empty
	deps := MissionDeps{
		Client: f, AgentID: "engineer-3", Market: store,
		sleep: func(ctx context.Context, d time.Duration) error { return nil }, // instant, cargo never changes
	}
	c := acceptedContract(1200, 1380)

	step := freightLoadPackage(context.Background(), deps, &c, &freightCand{Hops: 3}, io.Discard)
	if step == freightStepProceed {
		t.Fatal("a package that never lands in cargo must not proceed")
	}
	if !slices.Contains(f.shippingCalls, "return") {
		t.Fatalf("must return the un-loaded contract, calls were %v", f.shippingCalls)
	}
	if len(store.results) != 1 || store.results[0].Outcome != "returned_infeasible" {
		t.Fatalf("want returned_infeasible, got %+v", store.results)
	}
}

// When the tick-deferred withdraw lands within the poll budget, the pass proceeds.
// The injected sleep drops the package into cargo on its first call, standing in for
// the transfer completing a tick after the withdraw ack.
func TestFreightLoadPackageProceedsOncePackageLands(t *testing.T) {
	f := &fakeClient{state: &game.State{}}
	c := acceptedContract(1200, 1380)
	item := freightPackageItemID(c.PackageID)
	landed := false
	deps := MissionDeps{
		Client: f, AgentID: "engineer-3", Market: &fakeFreightStore{},
		sleep: func(ctx context.Context, d time.Duration) error {
			if !landed {
				landed = true
				f.state.Ship.Cargo = append(f.state.Ship.Cargo, game.CargoItem{ItemID: item, Quantity: 1})
			}
			return nil
		},
	}

	step := freightLoadPackage(context.Background(), deps, &c, &freightCand{Hops: 3}, io.Discard)
	if step != freightStepProceed {
		t.Fatalf("a package that lands within budget must proceed, got %v", step)
	}
	if slices.Contains(f.shippingCalls, "return") {
		t.Fatalf("must not return a contract whose package loaded, calls were %v", f.shippingCalls)
	}
}

// A ctx cancellation mid-poll must park the pass (Stuck), NOT return the contract:
// the load is unconfirmed, the contract is still held, and the next session reconciles.
func TestFreightLoadPackageParksOnContextCancelMidPoll(t *testing.T) {
	f := &fakeClient{state: &game.State{}}
	deps := MissionDeps{
		Client: f, AgentID: "engineer-3", Market: &fakeFreightStore{},
		sleep: func(ctx context.Context, d time.Duration) error { return context.Canceled },
	}
	c := acceptedContract(1200, 1380)

	step := freightLoadPackage(context.Background(), deps, &c, &freightCand{Hops: 3}, io.Discard)
	if step != freightStepStuck {
		t.Fatalf("a ctx cancel mid-load must park (Stuck), got %v", step)
	}
	if slices.Contains(f.shippingCalls, "return") {
		t.Fatalf("must not return the contract on a ctx cancel, calls were %v", f.shippingCalls)
	}
}

// Freight must not run unless a fleet opts in, so the pool is unaffected until
// the canary flips the flag.
func TestMissionsSkipsFreightWhenDisabled(t *testing.T) {
	// A REAL docked state: with a bare &game.State{} this test was vacuous —
	// Missions() returns at the "current system unknown" guard before any
	// freight code, so "no shipping calls" was true of a function that exited
	// after three lines and never read the fixture below.
	f := freightBoardClient(t, boardJSON(t), nil)
	deps := missionDeps(f, &fakeMissionStore{}, missionKB())
	deps.State = &missionRunState{}
	// EnableFreight deliberately left false.

	_ = Missions(context.Background(), deps)
	if len(f.shippingCalls) != 0 {
		t.Fatalf("freight must be inert when disabled, but issued %v", f.shippingCalls)
	}
	// Reachability guard: proves the pass really ran the board read rather than
	// bailing early, so the assertion above means something.
	if !slices.Contains(f.calls, "get_missions") {
		t.Fatalf("test is vacuous — the pass never reached the board read: %v", f.calls)
	}
}

// TestMissionsFreightDisabledEmptyBoardNoFuelProbe is the I1 regression test.
// With EnableFreight false and an empty board, Missions() must not build the
// freight fuel model at all — ensureFuelModel's first call issues a KB
// GetConnections plus a FindRoute server probe (haul_fuel.go's
// haulFuelPerJump), and that probe must stay gated behind an actual freight
// candidate, not fire merely because ensureFuelModel() was evaluated as a Go
// call argument. Pre-fix, mission.go's board-empty branch passed
// ensureFuelModel() (called, not the builder) to missionFreightOrDry, which
// Go evaluates before the call happens regardless of EnableFreight or
// freightBest being nil — so a disabled worker still paid a FindRoute call on
// every board-empty pass. Post-fix the builder itself is passed and
// missionTakeFreight only invokes it once cand is confirmed non-nil, which
// never happens here since EnableFreight is false.
func TestMissionsFreightDisabledEmptyBoardNoFuelProbe(t *testing.T) {
	f := freightBoardClient(t, boardJSON(t), nil) // empty board
	deps := missionDeps(f, &fakeMissionStore{}, missionKB())
	deps.State = &missionRunState{}
	// EnableFreight deliberately left false.

	if err := Missions(context.Background(), deps); err != nil {
		t.Fatalf("Missions: %v", err)
	}
	// Reachability guard: proves the pass really reached the board-empty
	// branch this test targets, rather than bailing out earlier.
	if !slices.Contains(f.calls, "get_missions") {
		t.Fatalf("test is vacuous — the pass never reached the board read: %v", f.calls)
	}
	for _, c := range f.calls {
		if strings.HasPrefix(c, "find_route:") {
			t.Fatalf("EnableFreight=false must not probe FindRoute on an empty board, calls were %v", f.calls)
		}
	}
}

// With freight enabled and a hold too small, the pass must still complete
// normally — freight is additive, never a new way for a pass to fail.
func TestMissionsWithFreightEnabledStillCompletes(t *testing.T) {
	// Hold too small for a package (capacity 100, 95 used), so freight declines
	// on its capability precondition and the pass must still finish cleanly.
	f := freightBoardClient(t, boardJSON(t), nil)
	f.state = missionState(true, 5000, 95)
	store := &fakeFreightStore{}
	deps := missionDeps(f, &store.fakeMissionStore, missionKB())
	deps.Market, deps.EnableFreight = store, true
	deps.State = &missionRunState{}

	if err := Missions(context.Background(), deps); err != nil {
		t.Fatalf("a freight-enabled pass must not error: %v", err)
	}
	// Reachability guard: reconcile runs early and always reads the profile, so
	// its absence would mean the pass never got as far as the freight code.
	if !slices.Contains(f.shippingCalls, "profile") {
		t.Fatalf("test is vacuous — the pass never reached freight: %v", f.shippingCalls)
	}
	// A hold that cannot take a package must not accept one.
	if slices.Contains(f.shippingCalls, "accept") {
		t.Fatalf("a too-small hold must not accept freight: %v", f.shippingCalls)
	}
}

func TestMissionHopsToBaseUnroutable(t *testing.T) {
	f := &fakeClient{state: &game.State{}, routeErr: errors.New("no route")}
	deps := MissionDeps{Client: f, AgentID: "fighter-4"}
	if _, ok := missionHopsToBase(context.Background(), deps, "sol_central"); ok {
		t.Fatal("a router error must report unroutable, not a guessed distance")
	}
	if _, ok := missionHopsToBase(context.Background(), deps, ""); ok {
		t.Fatal("an empty base id must report unroutable")
	}
}

func TestMissionHopsToBaseUsesCumulativeJumps(t *testing.T) {
	f := &fakeClient{
		state: &game.State{},
		route: []game.RouteStep{{SystemID: "a", Jumps: 1}, {SystemID: "sol", Jumps: 3}},
	}
	deps := MissionDeps{Client: f, AgentID: "fighter-4"}
	hops, ok := missionHopsToBase(context.Background(), deps, "sol_central")
	if !ok || hops != 3 {
		t.Fatalf("want 3 cumulative hops, got %d (ok=%v)", hops, ok)
	}
}

// freightLoadPackage must not re-withdraw a package already aboard — the
// reconcile-resume path re-enters the chain for a contract whose package is
// ALREADY in the hold (a nav-failure leg deliberately leaves it in flight).
// Withdrawing again fails with "not in storage", and treating that as "cannot
// carry" would destroy the very contract the nav-failure branch went out of
// its way to preserve. Migrated from v1's freightRunTrip: the chain loop
// itself never withdraws (see TestFreightChainRunDeliversAndRecordsPayout's
// doc comment), so this behavior now belongs to freightLoadPackage alone.
func TestFreightLoadPackageSkipsWithdrawWhenAboard(t *testing.T) {
	f := &fakeClient{
		state: &game.State{Ship: game.Ship{
			Cargo: []game.CargoItem{{ItemID: "package:pkg_hash", Quantity: 1}},
		}},
		// A withdraw would fail here, exactly as the live server behaves once the
		// package has left storage — the test fails loudly if one is attempted.
		withdrawErr: errors.New("no such item in storage"),
	}
	deps := MissionDeps{Client: f, AgentID: "fighter-4", Market: &fakeFreightStore{}}
	c := acceptedContract(1200, 1380)

	if step := freightLoadPackage(context.Background(), deps, &c, &freightCand{Hops: 3}, io.Discard); step != freightStepProceed {
		t.Fatalf("a package already aboard must proceed, got %v", step)
	}
	for _, call := range f.calls {
		if strings.HasPrefix(call, "withdraw:") {
			t.Fatalf("must not re-withdraw a package already aboard, calls were %v", f.calls)
		}
	}
}

// A settlement that never decoded records payout 0. Without a reason a canary
// reading freight_results cannot tell that from a genuinely zero-payout
// contract, so the outcome stays "delivered" but the reason must say why.
// Migrated from v1's freightRunTrip to freightChainRun.
func TestFreightChainRunFlagsUndecodableSettlement(t *testing.T) {
	store := &fakeFreightStore{}
	f := &fakeClient{state: &game.State{Doc: true}} // no shipping_deliver raw reply at all
	deps := MissionDeps{Client: f, AgentID: "fighter-4", Market: store, State: &missionRunState{}}
	c := acceptedContract(1200, 1380)
	deps.State.addHeldFreight(&c)
	nav := func(ctx context.Context, baseID string) error { return nil }
	hopsTo := func(string) (int, bool) { return 3, true }

	step, err := freightChainRun(context.Background(), deps, nav, hopsTo, nil, io.Discard)
	if err != nil || step != freightStepProceed {
		t.Fatalf("step=%v err=%v", step, err)
	}
	if len(store.results) != 1 {
		t.Fatalf("want 1 result row, got %+v", store.results)
	}
	r := store.results[0]
	if r.Outcome != "delivered" {
		t.Fatalf("a committed package stays delivered, got %q", r.Outcome)
	}
	if r.Reason == "" {
		t.Fatal("an undecodable settlement must be distinguishable from a zero payout")
	}
}

// freightSimulatePackageLoad returns a MissionDeps.sleep func that drops the
// canned acceptedContract package into fc's cargo on its first call, standing
// in for the tick-deferred withdraw landing a tick after the ack.
// freightBoardClient's canned shipping_accept reply always resolves to
// acceptedContract, so any Missions()-level test that must get past
// freightLoadPackage's confirm poll (freightPollLoaded) to reach delivery
// needs this — the fixture's fakeClient.WithdrawItems never mutates cargo on
// its own.
func freightSimulatePackageLoad(fc *fakeClient) func(ctx context.Context, d time.Duration) error {
	landed := false
	item := freightPackageItemID(acceptedContract(0, 1380).PackageID)
	return func(ctx context.Context, d time.Duration) error {
		if !landed {
			landed = true
			fc.state.Ship.Cargo = append(fc.state.Ship.Cargo, game.CargoItem{ItemID: item, Quantity: 1})
		}
		return nil
	}
}

// freightBoardClient builds a docked fake whose /shipping board carries one
// good contract, wired end to end (profile -> list -> accept -> deliver) plus
// the router replies missionHopsToBase/missionNavToBase need.
func freightBoardClient(t *testing.T, missionsRaw []byte, extraRaw map[string][]byte) *fakeClient {
	t.Helper()
	delivered := acceptedContract(0, 1380)
	delivered.Status = "delivered"
	raw := map[string][]byte{
		"missions":         missionsRaw,
		"shipping_profile": shippingProfileJSON(t, false, ""),
		"shipping_list":    shippingListJSON(t, listing("high", true, 9000, 2)),
		"shipping_accept":  shippingContractJSON(t, "accept", acceptedContract(0, 1380)),
		"shipping_deliver": shippingSettlementJSON(t, "deliver", delivered, 9000),
	}
	for k, v := range extraRaw {
		raw[k] = v
	}
	f := &fakeClient{
		state: missionState(true, 5000, 0),
		route: []game.RouteStep{{SystemID: "sol", Jumps: 2}},
		raw:   raw,
	}
	// Task 6 wires every freight call site through freightChainRun's refill
	// loop, which re-lists the board after each delivery within the SAME
	// pass. The real /shipping board drops a listing once it is accepted
	// (it's now in_transit, owned by us) — model that so a static canned
	// reply doesn't let refill re-accept "high" every leg up to the chain's
	// leg-count guard.
	f.onShippingAccept = func(string) {
		f.raw["shipping_list"] = shippingListJSON(t)
	}
	return f
}

// boardFatalClient wraps fakeClient and fails the test the instant the
// mission board is read. Used to prove a held freight chain owns the whole
// pass end to end and never falls through to board-driven mission work — the
// discrimination check TestMissionsResumesHeldChainBeforeBoardWork depends on.
type boardFatalClient struct {
	fakeClient
	t *testing.T
}

func (f *boardFatalClient) GetMissions(ctx context.Context) error {
	f.t.Fatal("missions: board must not be read while a held freight chain owns the pass")
	return nil
}

// missionsFreightFixture builds a Missions()-level fixture for the held-chain
// resume test: a docked, freight-enabled worker whose profile already
// reflects one active contract, and whose /shipping `get`/`deliver` replies
// resolve contract "x" (destination baseA) as in_transit then delivered.
// Built from the same pieces as freightBoardClient/missionDeps — the
// Missions-level freight fixtures elsewhere in this file — except the
// mission board itself is wired to t.Fatal if ever read: the caller adds a
// held contract (or doesn't, for the discrimination check) and the pass must
// resolve it via the chain without ever reaching missionReadBoard.
func missionsFreightFixture(t *testing.T) MissionDeps {
	t.Helper()
	held := serverapi.ShipmentContract{ID: "x", DestinationBaseID: "baseA", Status: "in_transit", DeadlineTick: 99999}
	delivered := held
	delivered.Status = "delivered"
	bf := &boardFatalClient{
		fakeClient: fakeClient{
			state: missionState(true, 5000, 0),
			route: []game.RouteStep{{SystemID: "sol", Jumps: 1}},
			raw: map[string][]byte{
				"shipping_profile": shippingProfileActiveJSON(t, 1),
				"shipping_get":     shippingContractJSON(t, "get", held),
				"shipping_deliver": shippingSettlementJSON(t, "deliver", delivered, 100),
			},
		},
		t: t,
	}
	store := &fakeFreightStore{}
	deps := missionDeps(&bf.fakeClient, &store.fakeMissionStore, missionKB())
	deps.Client = bf
	deps.Market = store
	deps.EnableFreight = true
	deps.State = &missionRunState{}
	return deps
}

// TestMissionsResumesHeldChainBeforeBoardWork is the Task 6 Step 1 test: a
// held in_transit contract must own the pass end to end via the chain run —
// Missions() must deliver it and must NEVER read the mission board. This is
// the exact vacuous-test shape flagged by reference_missions_vacuous_test_trap
// (a bare &game.State{} would make Missions() early-return before reaching
// any of this code), so the discrimination proof matters: neutering by
// removing the addHeldFreight line below must make the test fail on
// boardFatalClient's t.Fatal, not pass trivially. See the Task 6 report for
// the recorded red output.
func TestMissionsResumesHeldChainBeforeBoardWork(t *testing.T) {
	deps := missionsFreightFixture(t)
	deps.State.addHeldFreight(&serverapi.ShipmentContract{ID: "x", DestinationBaseID: "baseA", DeadlineTick: 99999, Status: "in_transit"})

	if err := Missions(context.Background(), deps); err != nil {
		t.Fatalf("Missions: %v", err)
	}
	if deps.State.heldFreightCount() != 0 {
		t.Fatal("held contract not delivered by the pass")
	}
}

// brokenConnsKB always fails GetConnections, simulating the transient KB
// outage the reviewer used to prove the Critical finding: a held in-flight
// package went unreconciled because deps.KB.GetConnections was hoisted above
// the freight reconcile block and its error short-circuited the pass.
type brokenConnsKB struct {
	*fakeKB
}

func (b *brokenConnsKB) GetConnections(context.Context) ([]knowledge.Connection, error) {
	return nil, errors.New("kb: connections unavailable")
}

// TestMissionsReconcilesFreightDespiteBrokenConnectionsKB is the Critical-finding
// regression test. Freight reconcile must run before EVERY early return in the
// pass, including the one from a failed connections/graph fetch that the
// candidate-evaluation BFS table needs further down. Reusing
// missionsFreightFixture's boardFatalClient keeps the discrimination proof
// intact: reconcile must both fire (held count reaches 0) and own the pass
// without ever touching the mission board (which would t.Fatal).
//
// With the pre-fix ordering (GetConnections hoisted above the freight
// reconcile block) this test fails: Missions returns
// "missions: get connections: kb: connections unavailable" and the held
// contract is never reconciled. Recorded red output is in the Task 6 report.
func TestMissionsReconcilesFreightDespiteBrokenConnectionsKB(t *testing.T) {
	deps := missionsFreightFixture(t)
	deps.KB = &brokenConnsKB{fakeKB: missionKB()}
	deps.State.addHeldFreight(&serverapi.ShipmentContract{ID: "x", DestinationBaseID: "baseA", DeadlineTick: 99999, Status: "in_transit"})

	if err := Missions(context.Background(), deps); err != nil {
		t.Fatalf("Missions: %v", err)
	}
	if deps.State.heldFreightCount() != 0 {
		t.Fatal("held contract not reconciled/delivered despite a broken connections KB")
	}
}

// An empty mission board is exactly where freight has the most to add: it turns
// a dry pass into a paying one. Before the fix the board-empty early return
// fired before freight was ever considered.
func TestMissionsTakesFreightOnEmptyBoard(t *testing.T) {
	fc := freightBoardClient(t, boardJSON(t), nil)
	store := &fakeFreightStore{}
	deps := missionDeps(fc, &store.fakeMissionStore, missionKB())
	deps.Market, deps.EnableFreight = store, true
	deps.State = &missionRunState{}
	deps.sleep = freightSimulatePackageLoad(fc)

	if err := Missions(context.Background(), deps); err != nil {
		t.Fatalf("Missions: %v", err)
	}
	if !slices.Contains(fc.shippingCalls, "accept") || !slices.Contains(fc.shippingCalls, "deliver") {
		t.Fatalf("an empty board must fall through to freight, calls were %v", fc.shippingCalls)
	}
	if len(store.results) != 1 || store.results[0].Outcome != "delivered" {
		t.Fatalf("want a delivered freight result, got %+v", store.results)
	}
}

// Same board, freight disabled: the pass must behave exactly as it does today —
// no /shipping traffic at all, straight to the dry pass.
func TestMissionsEmptyBoardStaysDryWhenFreightDisabled(t *testing.T) {
	fc := freightBoardClient(t, boardJSON(t), nil)
	deps := missionDeps(fc, &fakeMissionStore{}, missionKB())
	deps.State = &missionRunState{}
	// EnableFreight deliberately left false.

	if err := Missions(context.Background(), deps); err != nil {
		t.Fatalf("Missions: %v", err)
	}
	if len(fc.shippingCalls) != 0 {
		t.Fatalf("freight must stay inert on the dry path when disabled, issued %v", fc.shippingCalls)
	}
	if deps.State.dry != 1 {
		t.Fatalf("an empty board must still count one dry pass, got %d", deps.State.dry)
	}
}

// The availability gate emptying `set` is the worse of the two dry paths: the
// freight candidate has already been computed and was then silently discarded.
//
// The reward is deliberately far ABOVE the 9000-credit freight net: the ranking
// switch compares freight against the PRE-gate mission net, so a cheaper mission
// would let freight win there instead and this test would never reach the
// gate-empty fallthrough it exists to cover.
func TestMissionsTakesFreightWhenAvailabilityGateEmptiesSet(t *testing.T) {
	rare := boardEntry("m_rare", "phase_matrix", 8, "haven_station2", "haven", 20000, 0)
	fc := freightBoardClient(t, boardJSON(t, rare), map[string][]byte{
		"storage": storageJSON(t, map[string]float64{}),
		"market":  marketJSON(t, map[string]float64{"steel": 100}), // no phase_matrix here
	})
	fc.activeMissionsSeq = [][]byte{activeJSON(t), activeJSON(t)}
	store := &fakeFreightStore{}
	store.asks = map[string]float64{"phase_matrix": 10}
	deps := missionDeps(fc, &store.fakeMissionStore, missionKB())
	deps.Market, deps.EnableFreight = store, true
	deps.State = &missionRunState{}
	deps.sleep = freightSimulatePackageLoad(fc)

	if err := Missions(context.Background(), deps); err != nil {
		t.Fatalf("Missions: %v", err)
	}
	if strings.Contains(strings.Join(fc.calls, " "), "accept:m_rare") {
		t.Fatalf("the unacquirable mission must still be gated out: %v", fc.calls)
	}
	if !slices.Contains(fc.shippingCalls, "accept") || !slices.Contains(fc.shippingCalls, "deliver") {
		t.Fatalf("a fully-gated board must fall through to freight, calls were %v", fc.shippingCalls)
	}
	if len(store.results) != 1 || store.results[0].Outcome != "delivered" {
		t.Fatalf("want a delivered freight result, got %+v", store.results)
	}
}

// Same fully-gated board, freight disabled: unchanged dry pass.
func TestMissionsGatedBoardStaysDryWhenFreightDisabled(t *testing.T) {
	rare := boardEntry("m_rare", "phase_matrix", 8, "haven_station2", "haven", 5500, 0)
	fc := freightBoardClient(t, boardJSON(t, rare), map[string][]byte{
		"storage": storageJSON(t, map[string]float64{}),
		"market":  marketJSON(t, map[string]float64{"steel": 100}),
	})
	fc.activeMissionsSeq = [][]byte{activeJSON(t), activeJSON(t)}
	store := &fakeMissionStore{asks: map[string]float64{"phase_matrix": 10}}
	deps := missionDeps(fc, store, missionKB())
	deps.State = &missionRunState{}

	if err := Missions(context.Background(), deps); err != nil {
		t.Fatalf("Missions: %v", err)
	}
	if len(fc.shippingCalls) != 0 {
		t.Fatalf("freight must stay inert on the gated path when disabled, issued %v", fc.shippingCalls)
	}
	if deps.State.dry != 1 {
		t.Fatalf("a fully-gated board must still count one dry pass, got %d", deps.State.dry)
	}
}

// Exploration is the FALLBACK, not a preemptor. The pass used to compare
// exploration against the mission trip alone and return immediately, so a
// high-net freight contract silently lost to a low-net exploration tour.
func TestMissionsFreightOutranksExploration(t *testing.T) {
	// Exploration nets far less than the 9000-credit freight contract.
	tour := exploreEntry("tour1", 600, visitObj("sol"), dockObj("haven", "haven_station"))
	fc := freightBoardClient(t, boardJSON(t, tour), nil)
	fc.activeMissionsSeq = [][]byte{activeJSON(t), activeJSON(t)}
	store := &fakeFreightStore{}
	deps := missionDeps(fc, &store.fakeMissionStore, missionKB())
	deps.Market, deps.EnableFreight = store, true
	deps.Categories = []string{"delivery", "exploration"}
	deps.State = &missionRunState{}
	deps.sleep = freightSimulatePackageLoad(fc)

	if err := Missions(context.Background(), deps); err != nil {
		t.Fatalf("Missions: %v", err)
	}
	if strings.Contains(strings.Join(fc.calls, " "), "accept:tour1") {
		t.Fatalf("a 600-net tour must not preempt a 9000-net contract: %v", fc.calls)
	}
	if !slices.Contains(fc.shippingCalls, "accept") || !slices.Contains(fc.shippingCalls, "deliver") {
		t.Fatalf("freight must win the ranking, calls were %v", fc.shippingCalls)
	}
}

// The mirror: exploration still wins when it genuinely out-nets freight, so the
// fix reorders the ranking rather than simply demoting exploration.
func TestMissionsExplorationStillWinsWhenItOutNets(t *testing.T) {
	tour := exploreEntry("tour1", 50000, visitObj("sol"), dockObj("haven", "haven_station"))
	fc := freightBoardClient(t, boardJSON(t, tour), nil)
	fc.activeMissionsSeq = [][]byte{activeJSON(t), activeJSON(t)}
	store := &fakeFreightStore{}
	deps := missionDeps(fc, &store.fakeMissionStore, missionKB())
	deps.Market, deps.EnableFreight = store, true
	deps.Categories = []string{"delivery", "exploration"}
	deps.State = &missionRunState{}

	if err := Missions(context.Background(), deps); err != nil {
		t.Fatalf("Missions: %v", err)
	}
	if !strings.Contains(strings.Join(fc.calls, " "), "accept:tour1") {
		t.Fatalf("a 50000-net tour must beat a 9000-net contract: %v", fc.calls)
	}
	if slices.Contains(fc.shippingCalls, "accept") {
		t.Fatalf("freight must not be accepted when exploration out-nets it: %v", fc.shippingCalls)
	}
}

// The boundary itself: the ranking uses `freightNet >= exploreNet`, so an exact
// tie must go to freight ("exploration is the fallback"). Only the two
// away-from-boundary directions were pinned, which leaves `>=` free to weaken to
// `>` unnoticed.
func TestMissionsFreightTakesTiesWithExploration(t *testing.T) {
	// 9000 credits, no fuel on this route: exactly the freight contract's net.
	tour := exploreEntry("tour1", 9000, visitObj("sol"), dockObj("haven", "haven_station"))
	fc := freightBoardClient(t, boardJSON(t, tour), nil)
	fc.activeMissionsSeq = [][]byte{activeJSON(t), activeJSON(t)}
	store := &fakeFreightStore{}
	deps := missionDeps(fc, &store.fakeMissionStore, missionKB())
	deps.Market, deps.EnableFreight = store, true
	deps.Categories = []string{"delivery", "exploration"}
	deps.State = &missionRunState{}
	deps.sleep = freightSimulatePackageLoad(fc)

	if err := Missions(context.Background(), deps); err != nil {
		t.Fatalf("Missions: %v", err)
	}
	if strings.Contains(strings.Join(fc.calls, " "), "accept:tour1") {
		t.Fatalf("freight must take an exact tie, exploration was accepted: %v", fc.calls)
	}
	if !slices.Contains(fc.shippingCalls, "accept") || !slices.Contains(fc.shippingCalls, "deliver") {
		t.Fatalf("freight must take an exact tie, calls were %v", fc.shippingCalls)
	}
}

// A failed RETURN leaves a live, undischarged contract. Continuing into the
// mission accept loop would buy cargo and fly elsewhere while still on the hook
// for a package — the exact breach the fail-closed design exists to prevent.
// This is the one case where "degrade to no freight this pass" is wrong.
func TestMissionsAbortsPassWhenFreightReturnFails(t *testing.T) {
	// A deliverable mission sits on the board, so a degrade would be visible as
	// an accept. The freight contract is infeasible (deadline already past), so
	// the pass tries to return it — and the return itself fails.
	entry := boardEntry("m1", "steel", 20, "sol_station", "sol", 3000, 0)
	fc := freightBoardClient(t, boardJSON(t, entry), nil)
	fc.state.CurrentTick = 9999 // deadline 1380 is long gone -> infeasible on accept
	fc.shippingErr = map[string]error{"return": errors.New("server refused the return")}
	fc.activeMissionsSeq = [][]byte{activeJSON(t), activeJSON(t)}
	store := &fakeFreightStore{}
	store.asks = map[string]float64{"steel": 20}
	deps := missionDeps(fc, &store.fakeMissionStore, missionKB())
	deps.Market, deps.EnableFreight = store, true
	deps.State = &missionRunState{}

	if err := Missions(context.Background(), deps); err != nil {
		t.Fatalf("a stuck return must park the pass, not error: %v", err)
	}
	if !slices.Contains(fc.shippingCalls, "return") {
		t.Fatalf("the infeasible contract must be returned: %v", fc.shippingCalls)
	}
	if strings.Contains(strings.Join(fc.calls, " "), "accept:m1") {
		t.Fatalf("must not take on mission work while holding an undischarged contract: %v", fc.calls)
	}
	if len(store.results) != 1 || store.results[0].Outcome != "return_failed" {
		t.Fatalf("want a return_failed row for the operator, got %+v", store.results)
	}
}

// The pass snapshots state at the top, but missionResume and
// missionUnloadAtHomeBase both free hold space afterwards. Judging freight
// against the stale snapshot means a worker that just shed its ore is still
// seen as full, and freight is skipped on the very pass it finally has room.
//
// cloneState makes the fake behave like the real client (which returns
// state.Clone()), so the early snapshot and a fresh read are distinguishable;
// the hold is freed during missionResume's active-missions read.
func TestMissionsFreightUsesFreshCargoReadNotStaleSnapshot(t *testing.T) {
	fc := freightBoardClient(t, boardJSON(t), nil)
	fc.state = missionState(true, 5000, 95) // snapshot: only 5 units free
	fc.cloneState = true
	fc.onGetActiveMissions = func() { fc.state.Ship.CargoUsed = 0 } // ore shed -> 100 free
	store := &fakeFreightStore{}
	deps := missionDeps(fc, &store.fakeMissionStore, missionKB())
	deps.Market, deps.EnableFreight = store, true
	deps.State = &missionRunState{}

	if err := Missions(context.Background(), deps); err != nil {
		t.Fatalf("Missions: %v", err)
	}
	if !slices.Contains(fc.shippingCalls, "accept") {
		t.Fatalf("freight must be judged against the freed hold, not the stale snapshot: %v", fc.shippingCalls)
	}
}

// The highest-stakes stuck path: reconcile finds a package already aboard, the
// deadline has collapsed, and the return FAILS. The worker is physically
// carrying an undischarged contract, so it must park — taking mission work here
// would fly it away mid-contract with the package still in the hold.
// Reconcile has no board fallback (Task 4: dropped, own contracts never
// list), so a contract held in memory from earlier this session — not one
// "discovered" from the server after a restart — is what exercises this
// path; freightVerifyHeld's synchronous get still refreshes/verifies it.
func TestMissionsParksWhenReconciledContractCannotBeReturned(t *testing.T) {
	held := acceptedContract(1100, 1210) // 10 ticks left at tick 1200 -> infeasible
	entry := boardEntry("m1", "steel", 20, "sol_station", "sol", 3000, 0)
	fc := freightBoardClient(t, boardJSON(t, entry), map[string][]byte{
		"shipping_get": shippingContractJSON(t, "get", held),
	})
	fc.state.CurrentTick = 1200
	fc.shippingErr = map[string]error{"return": errors.New("server refused the return")}
	fc.activeMissionsSeq = [][]byte{activeJSON(t), activeJSON(t)}
	store := &fakeFreightStore{}
	store.asks = map[string]float64{"steel": 20}
	deps := missionDeps(fc, &store.fakeMissionStore, missionKB())
	deps.Market, deps.EnableFreight = store, true
	deps.State = &missionRunState{}
	deps.State.addHeldFreight(&held)

	if err := Missions(context.Background(), deps); err != nil {
		t.Fatalf("a stuck reconciled contract must park the pass, not error: %v", err)
	}
	if strings.Contains(strings.Join(fc.calls, " "), "accept:m1") {
		t.Fatalf("must not take mission work while carrying an undischarged package: %v", fc.calls)
	}
	if len(store.results) != 1 || store.results[0].Outcome != "return_failed" {
		t.Fatalf("want a return_failed row for the operator, got %+v", store.results)
	}
	// Parked, not repositioned: missionDryPass would fly it away from the
	// destination it still owes a package to.
	if deps.State.dry != 0 {
		t.Fatalf("a stuck contract must park, not count a dry pass: dry=%d", deps.State.dry)
	}
}

// The dry-path variant of the stuck-return abort. Here freight is reached via
// missionFreightOrDry (empty board), the contract proves infeasible on accept,
// and the return fails. Falling through to missionDryPass would be wrong twice
// over: it counts a dry pass and, on the third, REPOSITIONS the worker — flying
// it away while it still owes an undischarged contract.
func TestMissionsParksWhenDryPathFreightReturnFails(t *testing.T) {
	fc := freightBoardClient(t, boardJSON(t), nil) // empty mission board
	fc.state.CurrentTick = 9999                    // accept reply's deadline 1380 is long gone
	fc.shippingErr = map[string]error{"return": errors.New("server refused the return")}
	store := &fakeFreightStore{}
	deps := missionDeps(fc, &store.fakeMissionStore, missionKB())
	deps.Market, deps.EnableFreight = store, true
	deps.State = &missionRunState{}

	if err := Missions(context.Background(), deps); err != nil {
		t.Fatalf("a stuck return must park the pass, not error: %v", err)
	}
	if !slices.Contains(fc.shippingCalls, "return") {
		t.Fatalf("the infeasible contract must be returned: %v", fc.shippingCalls)
	}
	if deps.State.dry != 0 {
		t.Fatalf("a stuck contract must park, not count a dry pass toward repositioning: dry=%d", deps.State.dry)
	}
	if len(store.results) != 1 || store.results[0].Outcome != "return_failed" {
		t.Fatalf("want a return_failed row for the operator, got %+v", store.results)
	}
}

// The live canary (2026-07-20) delivered 14 seconds before its tick-deferred
// dock resolved and got not_docked back — /shipping does not auto-dock. The
// trip must wait for State to actually report docked before issuing deliver.
// Migrated from v1's freightRunTrip to freightChainRun.
func TestFreightChainRunSettlesDockBeforeDeliver(t *testing.T) {
	store := &fakeFreightStore{}
	delivered := acceptedContract(1200, 1380)
	delivered.Status = "delivered"
	f := &fakeClient{
		state: &game.State{}, // undocked: nav's dock is still pending
		raw:   map[string][]byte{"shipping_deliver": shippingSettlementJSON(t, "deliver", delivered, 6000)},
	}
	deps := MissionDeps{Client: f, AgentID: "fighter-4", Market: store, State: &missionRunState{}}
	// The injected sleep stands in for the pending dock landing on the next
	// tick: the first poll wait flips the state to docked.
	deps.sleep = func(ctx context.Context, d time.Duration) error {
		f.state.Doc = true
		return nil
	}
	c := acceptedContract(1200, 1380)
	deps.State.addHeldFreight(&c)
	nav := func(ctx context.Context, baseID string) error { return nil }
	hopsTo := func(string) (int, bool) { return 3, true }

	step, err := freightChainRun(context.Background(), deps, nav, hopsTo, nil, io.Discard)
	if err != nil || step != freightStepProceed {
		t.Fatalf("step=%v err=%v", step, err)
	}
	if !slices.Contains(f.shippingCalls, "deliver") {
		t.Fatalf("deliver must run once the dock settles, calls were %v", f.shippingCalls)
	}
	// The pending dock landed within the first tick, so no explicit Dock
	// nudge may be issued — the nudge is reserved for a dock that never came.
	if slices.Contains(f.calls, "dock") {
		t.Fatalf("must not nudge Dock while the pending dock is settling, calls were %v", f.calls)
	}
	if len(store.results) != 1 || store.results[0].Outcome != "delivered" {
		t.Fatalf("want a delivered result, got %+v", store.results)
	}
}

// A dock that never settles must leave the contract in flight (next pass
// re-checks the buffer and retries) — NOT deliver into a not_docked error and
// NOT record any outcome. One explicit Dock nudge is expected after the
// pending dock has had a full tick to land. Migrated from v1's freightRunTrip
// to freightChainRun.
func TestFreightChainRunLeavesInFlightWhenDockNeverSettles(t *testing.T) {
	store := &fakeFreightStore{}
	f := &fakeClient{state: &game.State{}} // undocked forever
	deps := MissionDeps{Client: f, AgentID: "fighter-4", Market: store, State: &missionRunState{}}
	deps.sleep = func(ctx context.Context, d time.Duration) error { return nil }
	c := acceptedContract(1200, 1380)
	deps.State.addHeldFreight(&c)
	nav := func(ctx context.Context, baseID string) error { return nil }
	hopsTo := func(string) (int, bool) { return 3, true }

	step, err := freightChainRun(context.Background(), deps, nav, hopsTo, nil, io.Discard)
	if err != nil {
		t.Fatalf("a dock-settle failure must not fail the pass: %v", err)
	}
	if step != freightStepProceed {
		t.Fatalf("step = %v, want Proceed (contract left in flight for the next pass)", step)
	}
	if slices.Contains(f.shippingCalls, "deliver") {
		t.Fatalf("must not deliver while undocked, calls were %v", f.shippingCalls)
	}
	if slices.Contains(f.shippingCalls, "return") {
		t.Fatalf("a transient dock problem must not return the contract, calls were %v", f.shippingCalls)
	}
	if n := 0; true {
		for _, call := range f.calls {
			if call == "dock" {
				n++
			}
		}
		if n != 1 {
			t.Fatalf("want exactly one explicit Dock nudge, got %d in %v", n, f.calls)
		}
	}
	if len(store.results) != 0 {
		t.Fatalf("no terminal outcome may be recorded for an in-flight contract, got %+v", store.results)
	}
	if deps.State.heldFreightCount() != 1 {
		t.Fatal("contract must remain held for the next pass")
	}
}

// The board read never lists our own in_transit contracts (proven live), so
// reconcile must resume from the in-memory held contract, verified via the
// synchronous get — never the board. (Task 4: freightReconcileSet does call
// ShippingProfile unconditionally now, but only for the loud-mismatch log;
// it never falls back to it for recovery, and must never touch the board.)
func TestFreightReconcileUsesHeldMemoryFirst(t *testing.T) {
	held := acceptedContract(1200, 1380)
	f := &fakeClient{
		state: &game.State{},
		raw:   map[string][]byte{"shipping_get": shippingContractJSON(t, "get", held)},
	}
	st := &missionRunState{}
	st.addHeldFreight(&held)
	deps := MissionDeps{Client: f, AgentID: "fighter-4", Market: &fakeFreightStore{}, State: st}

	got := freightReconcileSet(context.Background(), deps, io.Discard)
	if len(got) != 1 || got[0].ID != "high" {
		t.Fatalf("must resume the held contract from memory, got %+v", got)
	}
	if !slices.Contains(f.calls, "shipping_get:high") {
		t.Fatalf("must verify the held contract via get, calls were %v", f.calls)
	}
	if slices.Contains(f.shippingCalls, "list") {
		t.Fatalf("memory-first reconcile must never read the board, calls were %v", f.shippingCalls)
	}
}

// A deadline that expires between passes surfaces as server status
// "defaulted" with a flat debt — and nothing else records it (the live
// canary's defaulted contract produced NO freight_results row). Reconcile
// must record it as breached, clear the memory, and free the worker for
// other work (the operator settles the debt).
func TestFreightReconcileRecordsDefaultedHeldContract(t *testing.T) {
	held := acceptedContract(1200, 1380)
	defaulted := held
	defaulted.Status = "defaulted"
	store := &fakeFreightStore{}
	f := &fakeClient{
		state: &game.State{},
		raw:   map[string][]byte{"shipping_get": shippingContractJSON(t, "get", defaulted)},
	}
	st := &missionRunState{}
	st.addHeldFreight(&held)
	deps := MissionDeps{Client: f, AgentID: "fighter-4", Market: store, State: st}

	got := freightReconcileSet(context.Background(), deps, io.Discard)
	if len(got) != 0 {
		t.Fatalf("a defaulted contract must not be resumed, got %+v", got)
	}
	if len(store.results) != 1 || store.results[0].Outcome != "breached" {
		t.Fatalf("want a breached row for the stop-query, got %+v", store.results)
	}
	if !strings.Contains(store.results[0].Reason, "defaulted") {
		t.Fatalf("reason must carry the server status, got %q", store.results[0].Reason)
	}
	if all := st.heldFreightAll(); len(all) != 0 {
		t.Fatalf("a terminal status must clear the held-freight memory, got %+v", all)
	}
}

// A transient get failure must not orphan a healthy contract: memory wins and
// the trip resumes (worst case is one deliver attempt that errors cleanly).
func TestFreightReconcileHeldSurvivesGetFailure(t *testing.T) {
	held := acceptedContract(1200, 1380)
	f := &fakeClient{
		state:       &game.State{},
		shippingErr: map[string]error{"get": errors.New("connection reset")},
	}
	st := &missionRunState{}
	st.addHeldFreight(&held)
	deps := MissionDeps{Client: f, AgentID: "fighter-4", Market: &fakeFreightStore{}, State: st}

	got := freightReconcileSet(context.Background(), deps, io.Discard)
	if len(got) != 1 || got[0].ID != "high" {
		t.Fatalf("memory must win over a transient get failure, got %+v", got)
	}
	if all := st.heldFreightAll(); len(all) == 0 {
		t.Fatal("the held-freight memory must survive a transient get failure")
	}
}

// Held-freight memory lifecycle: accept sets it, a delivered trip clears it,
// and a FAILED return keeps it (the contract is still live and still ours).
func TestFreightHeldMemoryLifecycle(t *testing.T) {
	st := &missionRunState{}
	delivered := acceptedContract(1200, 1380)
	delivered.Status = "delivered"
	f := &fakeClient{
		state: &game.State{CurrentTick: 1200, Doc: true},
		raw: map[string][]byte{
			"shipping_accept":  shippingContractJSON(t, "accept", acceptedContract(1200, 1380)),
			"shipping_deliver": shippingSettlementJSON(t, "deliver", delivered, 6000),
		},
	}
	deps := MissionDeps{Client: f, AgentID: "fighter-4", Market: &fakeFreightStore{}, State: st}
	cand := &freightCand{Contract: serverapi.ShipmentContract{ID: "high"}, Hops: 3, Net: 6000}

	_, step := freightAccept(context.Background(), deps, cand, nil, io.Discard)
	if step != freightStepProceed {
		t.Fatalf("accept must proceed, got step %v", step)
	}
	if all := st.heldFreightAll(); len(all) == 0 || all[0].ID != "high" {
		t.Fatalf("accept must set the held-freight memory, got %+v", all)
	}

	nav := func(ctx context.Context, baseID string) error { return nil }
	hopsTo := func(string) (int, bool) { return 3, true }
	if _, err := freightChainRun(context.Background(), deps, nav, hopsTo, nil, io.Discard); err != nil {
		t.Fatalf("freightChainRun: %v", err)
	}
	if all := st.heldFreightAll(); len(all) != 0 {
		t.Fatalf("a delivered trip must clear the held-freight memory, got %+v", all)
	}

	// A failed return keeps (or re-establishes) the hold.
	f.shippingErr = map[string]error{"return": errors.New("connection reset")}
	c := acceptedContract(1200, 1380)
	if step := freightReturn(context.Background(), deps, io.Discard, c, cand, "returned_inflight", "test"); step != freightStepStuck {
		t.Fatalf("a failed return must report stuck, got %v", step)
	}
	if all := st.heldFreightAll(); len(all) == 0 || all[0].ID != "high" {
		t.Fatal("a failed return must keep the held-freight memory for the next pass")
	}
}

// freightGateDeps builds a MissionDeps backed by a fake client that answers
// shipping_profile (and, when non-empty, shipping_list) with the given raw
// JSON verbatim — factored out of the inline mock construction in
// TestFreightCandidateSkipsWhenContractAlreadyHeld / TestFreightCandidatePicksBestEligible
// for gate tests that need direct control over profile/capacity fields those
// builders don't expose (active_contracts_unlimited, liability, exposure).
func freightGateDeps(t *testing.T, profileJSON, boardJSON string) MissionDeps {
	t.Helper()
	raw := map[string][]byte{"shipping_profile": []byte(profileJSON)}
	if boardJSON != "" {
		raw["shipping_list"] = []byte(boardJSON)
	}
	f := &fakeClient{state: &game.State{}, raw: raw}
	return MissionDeps{Client: f, AgentID: "fighter-4"}
}

func TestFreightEffectiveMax(t *testing.T) {
	for in, want := range map[int]int{-1: 1, 0: 1, 1: 1, 3: 3, 7: 7} {
		if got := freightEffectiveMax(in); got != want {
			t.Fatalf("freightEffectiveMax(%d) = %d, want %d", in, got, want)
		}
	}
}

func TestBuildFreightCandMarginalPricing(t *testing.T) {
	// Held stop at 5 hops; candidate at 2 hops adds marginal 4 hops
	// (order [2,5]: total 2*2+5=9, minus 5 held-only). Fuel 100/hop.
	held := []chainStop{{ContractID: "h", DestBaseID: "hb", Hops: 5, DeadlineTick: 100000}}
	l := serverapi.ShippingListing{Eligible: true, Contract: serverapi.ShipmentContract{
		ID: "c", DestinationBaseID: "cb", BaseReward: 1000,
	}}
	cand, reason := buildFreightCand(l, 2, held, 0, func(j int) float64 { return float64(j) * 100 }, freightMinNet)
	if reason != "" {
		t.Fatalf("unexpected reject: %s", reason)
	}
	if cand.FuelCost != 400 { // marginal 4 hops * 100, NOT 2*100
		t.Fatalf("FuelCost = %.0f, want 400 (marginal-hop pricing)", cand.FuelCost)
	}
	if cand.Net != 600 {
		t.Fatalf("Net = %.0f, want 600", cand.Net)
	}
}

func TestBuildFreightCandRejectsWhenHeldDeadlineBreaks(t *testing.T) {
	// Held contract barely feasible alone (5 hops -> needs 142.5 ticks,
	// has 150). Inserting a nearer candidate (2 hops) pushes it to
	// cumulative 9 -> needs 256.5, breaking it. Must reject the CANDIDATE.
	held := []chainStop{{ContractID: "h", DestBaseID: "hb", Hops: 5, DeadlineTick: 150}}
	l := serverapi.ShippingListing{Eligible: true, Contract: serverapi.ShipmentContract{
		ID: "c", DestinationBaseID: "cb", BaseReward: 100000,
	}}
	_, reason := buildFreightCand(l, 2, held, 0, nil, freightMinNet)
	if reason == "" {
		t.Fatal("candidate that breaks a held deadline must be rejected")
	}
}

func TestFreightCandidateSkipsAtMaxPackages(t *testing.T) {
	// Gate must refuse to list the board at all when held >= cap.
	// Build deps/in as in TestFreightCandidateSkipsWhenContractAlreadyHeld,
	// with profile JSON reporting active_contracts=1 to match Held, and:
	in := freightInputs{
		CargoFree: 500,
		Held:      []chainStop{{ContractID: "h", DestBaseID: "hb", Hops: 3, DeadlineTick: 100000}},
	}
	deps := freightGateDeps(t, `{"profile":{"active_contracts":1},"capacity":{"active_contracts":1,"active_contracts_unlimited":true,"liability_unlimited":true}}`, "")
	deps.FreightMaxPackages = 1
	cand, skip := freightCandidate(context.Background(), deps, in, io.Discard)
	if cand != nil || !strings.Contains(skip, "at max packages") {
		t.Fatalf("cand=%v skip=%q; want nil + at-max-packages skip", cand, skip)
	}
}

func TestFreightCandidateSkipsWhenProfileExceedsHeld(t *testing.T) {
	// Server says 2 active, we hold 1 -> reconcile gap; fail closed exactly
	// like v1's ActiveContracts>0-with-empty-memory skip.
	in := freightInputs{
		CargoFree: 500,
		Held:      []chainStop{{ContractID: "h", DestBaseID: "hb", Hops: 3, DeadlineTick: 100000}},
	}
	deps := freightGateDeps(t, `{"profile":{"active_contracts":2},"capacity":{"active_contracts":2,"active_contracts_unlimited":true,"liability_unlimited":true}}`, "")
	deps.FreightMaxPackages = 4
	cand, skip := freightCandidate(context.Background(), deps, in, io.Discard)
	if cand != nil || !strings.Contains(skip, "unaccounted") {
		t.Fatalf("cand=%v skip=%q; want nil + unaccounted skip", cand, skip)
	}
}

func TestFreightCandidateSkipsCandidateOverLiability(t *testing.T) {
	// Candidate's ReservedExposure exceeds remaining aggregate liability ->
	// that candidate is skipped (not the whole pass).
	board := `{"shipments":[{"eligible":true,"contract":{"id":"big","destination_base_id":"d1","base_reward":5000,"reserved_exposure":9000}},
	                        {"eligible":true,"contract":{"id":"ok","destination_base_id":"d2","base_reward":4000,"reserved_exposure":100}}]}`
	deps := freightGateDeps(t, `{"profile":{"active_contracts":0},"capacity":{"active_contracts":0,"active_contracts_unlimited":true,"aggregate_liability_limit":10000,"remaining_aggregate_liability":500}}`, board)
	deps.FreightMaxPackages = 4
	in := freightInputs{CargoFree: 500, HopsTo: func(string) (int, bool) { return 1, true }}
	cand, _ := freightCandidate(context.Background(), deps, in, io.Discard)
	if cand == nil || cand.Contract.ID != "ok" {
		t.Fatalf("cand = %+v, want contract ok (big skipped on liability)", cand)
	}
}

// reconcileFakeClient answers ShippingGet per contract ID, unlike the shared
// fakeClient (which only has one static "shipping_get" slot) — needed
// because freightReconcileSet verifies a whole set of held contracts in one
// pass, each with its own server-side answer.
type reconcileFakeClient struct {
	fakeClient
	byID    map[string]string // contractID -> raw shipping_get JSON; absent = get error
	lastGet []byte
}

func (f *reconcileFakeClient) ShippingGet(ctx context.Context, shipmentID string) error {
	f.calls = append(f.calls, "shipping_get:"+shipmentID)
	f.shippingCalls = append(f.shippingCalls, "get")
	raw, ok := f.byID[shipmentID]
	if !ok {
		return errors.New("no such contract")
	}
	f.lastGet = []byte(raw)
	return nil
}

func (f *reconcileFakeClient) GetRawJSON(key string) []byte {
	if key == "shipping_get" {
		return f.lastGet
	}
	return f.fakeClient.GetRawJSON(key)
}

// freightReconcileStore is a fakeFreightStore that also indexes recorded
// outcomes by contract ID, so reconcile-set tests can assert an outcome
// without walking the results slice.
type freightReconcileStore struct {
	fakeFreightStore
	outcomes map[string]string
}

func (s *freightReconcileStore) RecordFreightResult(ctx context.Context, r market.FreightResult) error {
	if s.outcomes == nil {
		s.outcomes = make(map[string]string)
	}
	s.outcomes[r.ContractID] = r.Outcome
	return s.fakeFreightStore.RecordFreightResult(ctx, r)
}

// freightReconcileDeps builds a MissionDeps + recording store for
// freightReconcileSet tests, factored out of the single-contract mock setup
// in TestFreightReconcileUsesHeldMemoryFirst and neighbors: ShippingGet
// answers byID[contractID] verbatim (a get error when the ID is absent).
func freightReconcileDeps(t *testing.T, byID map[string]string) (MissionDeps, *freightReconcileStore) {
	t.Helper()
	store := &freightReconcileStore{}
	f := &reconcileFakeClient{fakeClient: fakeClient{state: &game.State{}}, byID: byID}
	deps := MissionDeps{Client: f, AgentID: "fighter-4", Market: store, State: &missionRunState{}}
	return deps, store
}

func TestFreightReconcileSetDropsTerminalKeepsTransit(t *testing.T) {
	// Two remembered contracts: "gone" now reports defaulted server-side
	// (recorded as breached, removed), "live" stays in_transit (refreshed).
	// Mock ShippingGet returns per-ID JSON, as the v1 reconcile tests do.
	deps, store := freightReconcileDeps(t, map[string]string{
		"gone": `{"contract":{"id":"gone","status":"defaulted","destination_base_id":"d1"}}`,
		"live": `{"contract":{"id":"live","status":"in_transit","destination_base_id":"d2","deadline_tick":99999}}`,
	})
	deps.State.addHeldFreight(&serverapi.ShipmentContract{ID: "gone"})
	deps.State.addHeldFreight(&serverapi.ShipmentContract{ID: "live"})
	held := freightReconcileSet(context.Background(), deps, io.Discard)
	if len(held) != 1 || held[0].ID != "live" {
		t.Fatalf("held = %v, want [live]", held)
	}
	if deps.State.heldFreightCount() != 1 {
		t.Fatal("terminal contract not removed from state")
	}
	if got := store.outcomes["gone"]; got != "breached" {
		t.Fatalf("defaulted contract recorded %q, want breached", got)
	}
}

func TestFreightReconcileSetFailOpenOnGetError(t *testing.T) {
	// A transient get failure must not orphan a healthy contract: memory
	// wins and the contract stays held (v1 fail-open rule, per contract).
	deps, _ := freightReconcileDeps(t, map[string]string{}) // gets error
	deps.State.addHeldFreight(&serverapi.ShipmentContract{ID: "x", DestinationBaseID: "d"})
	held := freightReconcileSet(context.Background(), deps, io.Discard)
	if len(held) != 1 || held[0].ID != "x" {
		t.Fatalf("held = %v, want [x] (fail-open)", held)
	}
}

// freightChainDeps builds the scripted-client fixture shared by the
// chain-run tests, factored from TestFreightRunTripDeliversAndRecordsPayout:
// the ship is docked (so freightSettleDock never has to poll), the profile
// is debt-free with zero server-side actives (so the gate never trips on
// the reconcile-gap guard), and the shipping board is EMPTY (`shipping_list`
// decodes to zero shipments) so freightChainRefill is always a no-op here —
// refill-through-the-gate is already covered by the Task 3 gate tests.
func freightChainDeps(t *testing.T) (MissionDeps, *fakeFreightStore) {
	t.Helper()
	store := &fakeFreightStore{}
	f := &fakeClient{
		state: &game.State{Doc: true},
		raw: map[string][]byte{
			"shipping_profile": shippingProfileJSON(t, false, ""),
			"shipping_list":    shippingListJSON(t),
		},
	}
	deps := MissionDeps{Client: f, AgentID: "fighter-4", Market: store, State: &missionRunState{}}
	return deps, store
}

func TestFreightChainRunDeliversNearestFirstAndAll(t *testing.T) {
	// Held: far (5 hops to baseB), near (2 hops to baseA). The chain must
	// nav to baseA first, deliver near, then baseB, deliver far. Both
	// recorded delivered; held set empty at exit; step Proceed.
	deps, store := freightChainDeps(t)
	deps.State.addHeldFreight(&serverapi.ShipmentContract{ID: "near", DestinationBaseID: "baseA", DeadlineTick: 99999, Status: "in_transit"})
	deps.State.addHeldFreight(&serverapi.ShipmentContract{ID: "far", DestinationBaseID: "baseB", DeadlineTick: 99999, Status: "in_transit"})
	var navved []string
	nav := func(ctx context.Context, baseID string) error { navved = append(navved, baseID); return nil }
	hops := func(base string) (int, bool) { return map[string]int{"baseA": 2, "baseB": 5}[base], true }
	step, err := freightChainRun(context.Background(), deps, nav, hops, nil, io.Discard)
	if err != nil || step != freightStepProceed {
		t.Fatalf("step=%v err=%v", step, err)
	}
	if len(navved) != 2 || navved[0] != "baseA" || navved[1] != "baseB" {
		t.Fatalf("nav order = %v, want [baseA baseB]", navved)
	}
	if deps.State.heldFreightCount() != 0 {
		t.Fatal("held set not empty after full chain")
	}
	if store.outcomes["near"] != "delivered" || store.outcomes["far"] != "delivered" {
		t.Fatalf("outcomes = %v", store.outcomes)
	}
}

func TestFreightChainRunNavFailureLeavesInFlight(t *testing.T) {
	deps, _ := freightChainDeps(t)
	deps.State.addHeldFreight(&serverapi.ShipmentContract{ID: "x", DestinationBaseID: "baseA", DeadlineTick: 99999, Status: "in_transit"})
	nav := func(ctx context.Context, baseID string) error { return fmt.Errorf("no route") }
	hops := func(string) (int, bool) { return 2, true }
	step, err := freightChainRun(context.Background(), deps, nav, hops, nil, io.Discard)
	if err != nil || step != freightStepProceed {
		t.Fatalf("step=%v err=%v; nav failure must leave contract in flight, not error", step, err)
	}
	if deps.State.heldFreightCount() != 1 {
		t.Fatal("contract must remain held for the next pass")
	}
}

func TestFreightChainRunReturnsDegradedContractOnly(t *testing.T) {
	// "dead" can no longer make its deadline from here (bound blown);
	// "fine" can. Chain returns dead (outcome returned_inflight), keeps and
	// delivers fine.
	deps, store := freightChainDeps(t)
	deps.State.addHeldFreight(&serverapi.ShipmentContract{ID: "dead", DestinationBaseID: "baseB", DeadlineTick: 10, Status: "in_transit"})
	deps.State.addHeldFreight(&serverapi.ShipmentContract{ID: "fine", DestinationBaseID: "baseA", DeadlineTick: 99999, Status: "in_transit"})
	nav := func(ctx context.Context, baseID string) error { return nil }
	hops := func(base string) (int, bool) { return map[string]int{"baseA": 1, "baseB": 5}[base], true }
	step, _ := freightChainRun(context.Background(), deps, nav, hops, nil, io.Discard)
	if step != freightStepProceed {
		t.Fatalf("step = %v", step)
	}
	if store.outcomes["dead"] != "returned_inflight" || store.outcomes["fine"] != "delivered" {
		t.Fatalf("outcomes = %v", store.outcomes)
	}
}

// TestFreightChainRunRefillStuckPropagates proves the Critical-review fix:
// a FAILED return surfaced inside freightChainRefill (not the deadline-victim
// loop) must still park the whole pass. Held starts with one deliverable
// contract ("near") so the loop reaches the refill step after delivering it.
// At refill, the board offers one eligible contract ("refill"); its accept
// call succeeds but the reply is undecodable (no "shipping_accept" raw JSON
// staged), which drives freightAccept's internal freightReturn — and that
// return is scripted to FAIL. freightChainRun must come back Stuck, not
// Proceed, even though nothing here touches the deadline-victim loop.
func TestFreightChainRunRefillStuckPropagates(t *testing.T) {
	store := &fakeFreightStore{}
	f := &fakeClient{
		state: &game.State{Doc: true, Ship: game.Ship{CargoCapacity: 200}},
		raw: map[string][]byte{
			"shipping_profile": shippingProfileJSON(t, false, ""),
			"shipping_list":    shippingListJSON(t, listing("refill", true, 5000, 2)),
			// "shipping_accept" deliberately absent: an empty raw reply is what
			// drives freightAccept's decode-failure return path below.
		},
		shippingErr: map[string]error{"return": errors.New("network blip")},
	}
	deps := MissionDeps{Client: f, AgentID: "fighter-4", Market: store, State: &missionRunState{}}
	// Refill only runs above cap 1 (C1 fix: the default cap 1 skips refill
	// entirely for v1 equivalence, see TestFreightChainRunCap1NoRefillAfterDelivery).
	// This test is specifically about refill's Stuck-propagation, so it needs
	// a cap that lets refill actually run.
	deps.FreightMaxPackages = 3
	deps.State.addHeldFreight(&serverapi.ShipmentContract{ID: "near", DestinationBaseID: "baseA", DeadlineTick: 99999, Status: "in_transit"})
	nav := func(ctx context.Context, baseID string) error { return nil }
	hops := func(base string) (int, bool) { return map[string]int{"baseA": 2, "sol_central": 2}[base], true }

	step, err := freightChainRun(context.Background(), deps, nav, hops, noFuel, io.Discard)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if step != freightStepStuck {
		t.Fatalf("step = %v, want freightStepStuck — a failed return inside chain refill must park the pass", step)
	}
	if !slices.Contains(f.shippingCalls, "deliver") {
		t.Fatalf("must have delivered 'near' before reaching refill, calls were %v", f.shippingCalls)
	}
	if !slices.Contains(f.shippingCalls, "accept") || !slices.Contains(f.shippingCalls, "return") {
		t.Fatalf("must have attempted accept+return on the refill candidate, calls were %v", f.shippingCalls)
	}
	if store.outcomes["refill"] != "return_failed" {
		t.Fatalf("outcomes = %v, want refill -> return_failed", store.outcomes)
	}
}

// freightAcceptFatalClient wraps fakeClient and fails the test the instant
// ShippingAccept is called. Used to prove the C1 fix: at the default cap of
// 1, a clean delivery must end the pass without the chain ever touching the
// board again — v1 equivalence means the worker does not accept anything
// else at this dock, not merely that some policy discourages it.
type freightAcceptFatalClient struct {
	fakeClient
	t *testing.T
}

func (f *freightAcceptFatalClient) ShippingAccept(ctx context.Context, shipmentID, carrier string) error {
	f.t.Fatal("freight: cap 1 must not accept anything after delivering — no refill at cap 1")
	return nil
}

// TestFreightChainRunCap1NoRefillAfterDelivery is the C1 regression test.
// FreightMaxPackages is left at its zero value (freightEffectiveMax normalizes
// 0/1 to the v1 single-contract cap). One held contract ("near") is due at
// baseA; the board separately offers a highly profitable contract ("high")
// that would clear the gate easily if refill ran. Pre-fix, freightChainRun
// called freightChainRefill unconditionally after a zero-failure delivery, so
// this test failed on freightAcceptFatalClient's t.Fatal (recorded red
// evidence: "freight: cap 1 must not accept anything after delivering — no
// refill at cap 1"). Post-fix, refill is skipped outright at cap 1, so
// ShippingAccept is never called and the pass ends with an empty held set.
func TestFreightChainRunCap1NoRefillAfterDelivery(t *testing.T) {
	store := &fakeFreightStore{}
	bf := &freightAcceptFatalClient{
		fakeClient: fakeClient{
			state: &game.State{Doc: true, Ship: game.Ship{CargoCapacity: 200}},
			raw: map[string][]byte{
				"shipping_profile": shippingProfileJSON(t, false, ""),
				"shipping_list":    shippingListJSON(t, listing("high", true, 9000, 2)),
			},
		},
		t: t,
	}
	deps := MissionDeps{Client: bf, AgentID: "fighter-4", Market: store, State: &missionRunState{}}
	deps.State.addHeldFreight(&serverapi.ShipmentContract{ID: "near", DestinationBaseID: "baseA", DeadlineTick: 99999, Status: "in_transit"})
	nav := func(ctx context.Context, baseID string) error { return nil }
	hops := func(base string) (int, bool) { return map[string]int{"baseA": 2}[base], true }

	step, err := freightChainRun(context.Background(), deps, nav, hops, noFuel, io.Discard)
	if err != nil || step != freightStepProceed {
		t.Fatalf("step=%v err=%v", step, err)
	}
	if store.outcomes["near"] != "delivered" {
		t.Fatalf("outcomes = %v, want near -> delivered", store.outcomes)
	}
	if deps.State.heldFreightCount() != 0 {
		t.Fatal("held set must be empty after the v1-equivalent cap-1 delivery")
	}
}

// TestFreightChainRunCap3StillRefillsAfterDelivery is the companion in the
// other direction: above cap 1, refill must still run after a clean
// delivery. Without this, the C1 fix could over-apply and silently disable
// refill for every cap, not just 1.
func TestFreightChainRunCap3StillRefillsAfterDelivery(t *testing.T) {
	store := &fakeFreightStore{}
	f := &fakeClient{
		state: &game.State{Doc: true, Ship: game.Ship{CargoCapacity: 300}},
		raw: map[string][]byte{
			"shipping_profile": shippingProfileJSON(t, false, ""),
			"shipping_list":    shippingListJSON(t, listing("high", true, 9000, 2)),
			"shipping_accept":  shippingContractJSON(t, "accept", acceptedContract(0, 99999)),
		},
	}
	// The real /shipping board drops a listing once accepted; model that so
	// refill doesn't re-offer "high" forever (it would just hit the chain's
	// leg-count guard instead of proving anything new).
	f.onShippingAccept = func(string) { f.raw["shipping_list"] = shippingListJSON(t) }
	deps := MissionDeps{Client: f, AgentID: "fighter-4", Market: store, State: &missionRunState{}, FreightMaxPackages: 3}
	deps.State.addHeldFreight(&serverapi.ShipmentContract{ID: "near", DestinationBaseID: "baseA", DeadlineTick: 99999, Status: "in_transit"})
	nav := func(ctx context.Context, baseID string) error { return nil }
	hops := func(base string) (int, bool) { return map[string]int{"baseA": 2, "sol_central": 2}[base], true }

	if _, err := freightChainRun(context.Background(), deps, nav, hops, noFuel, io.Discard); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !slices.Contains(f.shippingCalls, "accept") {
		t.Fatalf("cap>1 must still refill after a clean delivery, calls were %v", f.shippingCalls)
	}
}

// limitedBoardClient offers a fixed board listing for a bounded number of
// ShippingList calls, then reports the board empty. Used by
// TestFreightChainRefillStopsAfterLoadRelease to bound a would-be-infinite
// refill loop deterministically (rather than hanging the test on the pre-fix
// behavior) while still proving the discriminating accept count.
type limitedBoardClient struct {
	fakeClient
	offersLeft int
	boardRaw   []byte
	emptyRaw   []byte
}

func (f *limitedBoardClient) ShippingList(ctx context.Context, sort string) error {
	f.calls = append(f.calls, "shipping_list")
	f.shippingCalls = append(f.shippingCalls, "list")
	if f.offersLeft > 0 {
		f.offersLeft--
		f.raw["shipping_list"] = f.boardRaw
	} else {
		f.raw["shipping_list"] = f.emptyRaw
	}
	return f.shippingErr["list"]
}

// TestFreightChainRefillStopsAfterLoadRelease is the I2 regression test.
// WithdrawItems always fails, so every accepted candidate is immediately
// load-released (package would not load) and returned. The board is scripted
// to keep offering the same eligible listing for 2 ShippingList calls before
// going empty — bounding what would otherwise be a true infinite loop
// pre-fix, while still exposing the discriminating signal: pre-fix,
// freightChainRefill `continue`s after a load-release and accepts the
// candidate a SECOND time (2 accepts total before the board finally empties);
// post-fix, a load-release stops refill immediately (exactly 1 accept).
func TestFreightChainRefillStopsAfterLoadRelease(t *testing.T) {
	store := &fakeFreightStore{}
	bc := &limitedBoardClient{
		fakeClient: fakeClient{
			state: &game.State{Doc: true, Ship: game.Ship{CargoCapacity: 300}},
			raw: map[string][]byte{
				"shipping_profile": shippingProfileJSON(t, false, ""),
				"shipping_accept":  shippingContractJSON(t, "accept", acceptedContract(1200, 99999)),
			},
			withdrawErr: errors.New("no such item in storage"),
		},
		offersLeft: 2,
		boardRaw:   shippingListJSON(t, listing("high", true, 9000, 2)),
		emptyRaw:   shippingListJSON(t),
	}
	bc.state.CurrentTick = 1200
	deps := MissionDeps{Client: bc, AgentID: "fighter-4", Market: store, State: &missionRunState{}, FreightMaxPackages: 5}
	hops := func(string) (int, bool) { return 2, true }

	step := freightChainRefill(context.Background(), deps, hops, noFuel, io.Discard)
	if step != freightStepProceed {
		t.Fatalf("step = %v, want freightStepProceed", step)
	}
	acceptCount := 0
	for _, c := range bc.shippingCalls {
		if c == "accept" {
			acceptCount++
		}
	}
	if acceptCount != 1 {
		t.Fatalf("accept count = %d, want exactly 1 — a load-release must stop refill at this dock, not retry (calls were %v)", acceptCount, bc.shippingCalls)
	}
	if !slices.Contains(bc.shippingCalls, "return") {
		t.Fatalf("must have returned the unloadable contract, calls were %v", bc.shippingCalls)
	}
}

func TestEffectiveFreightFloor(t *testing.T) {
	cases := []struct {
		name      string
		tier      string
		enabled   bool
		spent     float64
		budget    float64
		wantFloor float64
	}{
		{"probationary with budget remaining relaxes", "probationary", true, 0, freightProbationBudget, freightProbationFloor},
		{"probationary partway through budget still relaxes", "probationary", true, 2999, freightProbationBudget, freightProbationFloor},
		{"probationary budget exhausted reverts", "probationary", true, freightProbationBudget, freightProbationBudget, freightMinNet},
		{"probationary over budget reverts", "probationary", true, 3500, freightProbationBudget, freightMinNet},
		{"licensed keeps normal floor", "licensed", true, 0, freightProbationBudget, freightMinNet},
		{"trusted keeps normal floor", "trusted", true, 0, freightProbationBudget, freightMinNet},
		{"prime keeps normal floor", "prime", true, 0, freightProbationBudget, freightMinNet},
		{"empty tier keeps normal floor", "", true, 0, freightProbationBudget, freightMinNet},
		{"unknown legacy tier keeps normal floor", "gold", true, 0, freightProbationBudget, freightMinNet},
		{"bootstrap disabled forces normal floor even when probationary", "probationary", false, 0, freightProbationBudget, freightMinNet},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := effectiveFreightFloor(tc.tier, tc.enabled, tc.spent, tc.budget); got != tc.wantFloor {
				t.Fatalf("effectiveFreightFloor(%q, %v, %v, %v) = %v, want %v",
					tc.tier, tc.enabled, tc.spent, tc.budget, got, tc.wantFloor)
			}
		})
	}
}

func TestCarrierTierAboveProbationary(t *testing.T) {
	cases := map[string]bool{
		"":             false, // unknown/empty: never treated as advanced
		"probationary": false,
		"licensed":     true,
		"trusted":      true,
		"prime":        true,
	}
	for tier, want := range cases {
		if got := carrierTierAboveProbationary(tier); got != want {
			t.Fatalf("carrierTierAboveProbationary(%q) = %v, want %v", tier, got, want)
		}
	}
}

func TestFreightBandExcluded(t *testing.T) {
	cases := []struct {
		name         string
		carrierTier  string
		contractBand string
		bootstrap    bool
		want         bool
	}{
		{"probationary carrier is never fenced from probationary cargo", "probationary", "probationary", true, false},
		{"licensed carrier is fenced from probationary cargo", "licensed", "probationary", true, true},
		{"trusted carrier is fenced from probationary cargo", "trusted", "probationary", true, true},
		{"licensed carrier keeps its own-tier cargo", "licensed", "licensed", true, false},
		{"bootstrap off disables the fence", "licensed", "probationary", false, false},
		{"unknown carrier tier is never fenced", "", "probationary", true, false},
		{"empty contract band is never fenced", "licensed", "", true, false},
		{"unpriced contract band is never fenced", "licensed", "unpriced", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := freightBandExcluded(tc.carrierTier, tc.contractBand, tc.bootstrap); got != tc.want {
				t.Fatalf("freightBandExcluded(%q, %q, %v) = %v, want %v",
					tc.carrierTier, tc.contractBand, tc.bootstrap, got, tc.want)
			}
		})
	}
}

func TestBootstrapSpentTally(t *testing.T) {
	var nilState *missionRunState
	if got := nilState.bootstrapSpent(); got != 0 {
		t.Fatalf("nil state bootstrapSpent = %v, want 0", got)
	}
	nilState.addBootstrapSpent(500) // must not panic on nil receiver

	s := &missionRunState{}
	if got := s.bootstrapSpent(); got != 0 {
		t.Fatalf("fresh state bootstrapSpent = %v, want 0", got)
	}
	s.addBootstrapSpent(300)
	s.addBootstrapSpent(200)
	if got := s.bootstrapSpent(); got != 500 {
		t.Fatalf("bootstrapSpent = %v, want 500", got)
	}
}

// A probationary bootstrap worker accepts a contract whose net is below the
// normal 500 floor but above the -400 probation floor. The same board with
// bootstrap OFF selects nothing.
func TestFreightCandidateProbationBootstrapAdmitsSubFloor(t *testing.T) {
	// One eligible contract: reward 100, no fuel -> net +100 (below 500,
	// above -400). Profile reports the probationary tier.
	profile := `{"profile":{"active_contracts":0},"progression":{"current_tier":"probationary"},"capacity":{"active_contracts_unlimited":true,"liability_unlimited":true}}`
	board := `{"shipments":[{"eligible":true,"contract":{"id":"p1","destination_base_id":"sol_central","base_reward":100,"route_hops":2,"service_level":"standard"}}]}`

	// Bootstrap ON: the sub-floor contract clears.
	deps := freightGateDeps(t, profile, board)
	deps.State = &missionRunState{}
	deps.FreightBootstrap = true
	in := freightInputs{CargoFree: 500, FuelCostFor: noFuel, NowTick: 0}
	cand, skip := freightCandidate(context.Background(), deps, in, io.Discard)
	if cand == nil {
		t.Fatalf("probationary bootstrap must admit the sub-floor contract, got skip %q", skip)
	}
	if cand.Contract.ID != "p1" {
		t.Fatalf("wrong candidate %q, want p1", cand.Contract.ID)
	}

	// Bootstrap OFF: same board, normal 500 floor rejects it.
	depsOff := freightGateDeps(t, profile, board)
	depsOff.State = &missionRunState{}
	depsOff.FreightBootstrap = false
	if cand, _ := freightCandidate(context.Background(), depsOff, in, io.Discard); cand != nil {
		t.Fatalf("bootstrap off must reject the sub-floor contract, got %+v", cand)
	}
}

// TestFreightCandidateProbationAdmitsNegativeNetWithinFloor drives genuinely
// NEGATIVE-net contracts through the real freightCandidate gate (unlike
// TestFreightAcceptAccruesBootstrapLoss, which builds a freightCand{Net: -300}
// directly and never exercises buildFreightCand's `net < floor` comparison).
//
// Listing "neg300": base_reward 100, route_hops 4, fuel priced at 100/hop ->
// fuel 400 -> net = 100 - 400 = -300. That sits strictly inside (-400, 0), so
// a probationary carrier with bootstrap on must ADMIT it.
//
// Listing "neg450": base_reward 100, route_hops 5, fuel priced at 110/hop ->
// fuel 550 -> net = 100 - 550 = -450. That is at-or-below -400, so the same
// probationary+bootstrap-on carrier must REJECT it — proven not by routing,
// eligibility or deadline (both listings are eligible, routable via
// route_hops, and carry no deadline_tick yet, since the server only assigns
// one at accept) but by asserting the logged skip reason names the net/floor
// numbers themselves.
func TestFreightCandidateProbationAdmitsNegativeNetWithinFloor(t *testing.T) {
	profile := `{"profile":{"active_contracts":0},"progression":{"current_tier":"probationary"},"capacity":{"active_contracts_unlimited":true,"liability_unlimited":true}}`

	// -300 net: admitted.
	admitBoard := `{"shipments":[{"eligible":true,"contract":{"id":"neg300","destination_base_id":"sol_central","base_reward":100,"route_hops":4,"service_level":"standard"}}]}`
	deps := freightGateDeps(t, profile, admitBoard)
	deps.State = &missionRunState{}
	deps.FreightBootstrap = true
	admitFuel := func(jumps int) float64 { return float64(jumps) * 100 } // 4 hops * 100 = 400
	in := freightInputs{CargoFree: 500, FuelCostFor: admitFuel, NowTick: 0}

	cand, skip := freightCandidate(context.Background(), deps, in, io.Discard)
	if cand == nil {
		t.Fatalf("a -300 net must clear the relaxed -400 floor, got skip %q", skip)
	}
	if cand.Contract.ID != "neg300" {
		t.Fatalf("wrong candidate %q, want neg300", cand.Contract.ID)
	}
	if cand.Net != -300 {
		t.Fatalf("Net = %.0f, want -300 (reward 100 - fuel 400)", cand.Net)
	}

	// -450 net: rejected, and specifically by the net-vs-floor gate.
	rejectBoard := `{"shipments":[{"eligible":true,"contract":{"id":"neg450","destination_base_id":"sol_central","base_reward":100,"route_hops":5,"service_level":"standard"}}]}`
	depsReject := freightGateDeps(t, profile, rejectBoard)
	depsReject.State = &missionRunState{}
	depsReject.FreightBootstrap = true
	rejectFuel := func(jumps int) float64 { return float64(jumps) * 110 } // 5 hops * 110 = 550
	inReject := freightInputs{CargoFree: 500, FuelCostFor: rejectFuel, NowTick: 0}
	var log strings.Builder

	candReject, skipReject := freightCandidate(context.Background(), depsReject, inReject, &log)
	if candReject != nil {
		t.Fatalf("a -450 net must still be rejected under the -400 floor, got %+v", candReject)
	}
	if !strings.Contains(skipReject, "no freight cleared the gate") {
		t.Fatalf("want the board-emptied skip reason, got %q", skipReject)
	}
	if !strings.Contains(log.String(), "net -450 below floor -400 (reward 100, fuel 550)") {
		t.Fatalf("the per-listing rejection must name the net/floor numbers (not routing/eligibility/deadline), got log %q", log.String())
	}
}

// The budget caps losses: once bootstrapSpent >= freightProbationBudget the
// floor reverts to 500 even while still probationary, so a sub-500 contract is
// rejected.
func TestFreightCandidateProbationBudgetExhaustedReverts(t *testing.T) {
	profile := `{"profile":{"active_contracts":0},"progression":{"current_tier":"probationary"},"capacity":{"active_contracts_unlimited":true,"liability_unlimited":true}}`
	board := `{"shipments":[{"eligible":true,"contract":{"id":"p1","destination_base_id":"sol_central","base_reward":100,"route_hops":2,"service_level":"standard"}}]}`
	deps := freightGateDeps(t, profile, board)
	deps.State = &missionRunState{}
	deps.State.addBootstrapSpent(freightProbationBudget) // budget fully spent
	deps.FreightBootstrap = true
	in := freightInputs{CargoFree: 500, FuelCostFor: noFuel, NowTick: 0}
	if cand, _ := freightCandidate(context.Background(), deps, in, io.Discard); cand != nil {
		t.Fatalf("a spent budget must revert to the 500 floor and reject, got %+v", cand)
	}
}

// A negative-net accept accrues -net against the budget; a positive-net accept
// does not touch it.
func TestFreightAcceptAccruesBootstrapLoss(t *testing.T) {
	feasible := shippingContractJSON(t, "accept", acceptedContract(1200, 1380))

	// Negative net -> accrue.
	f := &fakeClient{state: &game.State{CurrentTick: 1200}, raw: map[string][]byte{"shipping_accept": feasible}}
	deps := MissionDeps{Client: f, AgentID: "fighter-4", Market: &fakeMissionStore{}, State: &missionRunState{}}
	cand := &freightCand{Contract: serverapi.ShipmentContract{ID: "high"}, Hops: 3, Net: -300}
	if _, step := freightAccept(context.Background(), deps, cand, nil, io.Discard); step != freightStepProceed {
		t.Fatal("a feasible contract must proceed")
	}
	if got := deps.State.bootstrapSpent(); got != 300 {
		t.Fatalf("bootstrapSpent = %v, want 300 after a -300 accept", got)
	}

	// Positive net -> no accrual.
	f2 := &fakeClient{state: &game.State{CurrentTick: 1200}, raw: map[string][]byte{"shipping_accept": shippingContractJSON(t, "accept", acceptedContract(1200, 1380))}}
	deps2 := MissionDeps{Client: f2, AgentID: "fighter-4", Market: &fakeMissionStore{}, State: &missionRunState{}}
	candPos := &freightCand{Contract: serverapi.ShipmentContract{ID: "high2"}, Hops: 3, Net: 6000}
	if _, step := freightAccept(context.Background(), deps2, candPos, nil, io.Discard); step != freightStepProceed {
		t.Fatal("a feasible positive contract must proceed")
	}
	if got := deps2.State.bootstrapSpent(); got != 0 {
		t.Fatalf("bootstrapSpent = %v, want 0 after a positive-net accept", got)
	}
}

// fenceBoard: two eligible contracts. The probationary-band one has the higher
// net (reward 800, no fuel -> net 800); the licensed-band one is lower (600).
// Without the fence selectFreightCand picks prob800; the fence flips it.
const fenceBoard = `{"shipments":[` +
	`{"eligible":true,"contract":{"id":"prob800","destination_base_id":"sol_central","base_reward":800,"route_hops":2,"service_level":"standard","risk_band":"probationary"}},` +
	`{"eligible":true,"contract":{"id":"lic600","destination_base_id":"sol_central","base_reward":600,"route_hops":2,"service_level":"standard","risk_band":"licensed"}}` +
	`],"total":2}`

// An above-probationary carrier skips the (higher-net) probationary cargo and
// takes its own-tier licensed cargo instead; the skip is logged.
func TestFreightCandidateFencesProbationaryForAdvancedCarrier(t *testing.T) {
	profile := `{"profile":{"active_contracts":0},"progression":{"current_tier":"licensed"},"capacity":{"active_contracts_unlimited":true,"liability_unlimited":true}}`
	deps := freightGateDeps(t, profile, fenceBoard)
	deps.State = &missionRunState{}
	deps.FreightBootstrap = true
	in := freightInputs{CargoFree: 500, FuelCostFor: noFuel, NowTick: 0}
	var log strings.Builder

	cand, skip := freightCandidate(context.Background(), deps, in, &log)
	if cand == nil {
		t.Fatalf("a licensed carrier must still take licensed cargo, got skip %q", skip)
	}
	if cand.Contract.ID != "lic600" {
		t.Fatalf("want the licensed contract lic600 (probationary fenced), got %q", cand.Contract.ID)
	}
	if !strings.Contains(log.String(), "prob800") || !strings.Contains(log.String(), "reserved for climbing carriers") {
		t.Fatalf("the fence must log why prob800 was skipped, got %q", log.String())
	}
}

// A probationary carrier is never fenced: it takes the highest-net probationary
// contract as before, and nothing is logged as reserved.
func TestFreightCandidateProbationaryCarrierNotFenced(t *testing.T) {
	profile := `{"profile":{"active_contracts":0},"progression":{"current_tier":"probationary"},"capacity":{"active_contracts_unlimited":true,"liability_unlimited":true}}`
	deps := freightGateDeps(t, profile, fenceBoard)
	deps.State = &missionRunState{}
	deps.FreightBootstrap = true
	in := freightInputs{CargoFree: 500, FuelCostFor: noFuel, NowTick: 0}
	var log strings.Builder

	cand, skip := freightCandidate(context.Background(), deps, in, &log)
	if cand == nil {
		t.Fatalf("a probationary carrier must take probationary cargo, got skip %q", skip)
	}
	if cand.Contract.ID != "prob800" {
		t.Fatalf("want the highest-net probationary contract prob800, got %q", cand.Contract.ID)
	}
	if strings.Contains(log.String(), "reserved for climbing carriers") {
		t.Fatalf("a probationary carrier must never fence itself, log was %q", log.String())
	}
}

// Bootstrap off disables the fence entirely: even an advanced carrier takes the
// highest-net contract regardless of band.
func TestFreightCandidateFenceOffWhenBootstrapDisabled(t *testing.T) {
	profile := `{"profile":{"active_contracts":0},"progression":{"current_tier":"licensed"},"capacity":{"active_contracts_unlimited":true,"liability_unlimited":true}}`
	deps := freightGateDeps(t, profile, fenceBoard)
	deps.State = &missionRunState{}
	deps.FreightBootstrap = false
	in := freightInputs{CargoFree: 500, FuelCostFor: noFuel, NowTick: 0}

	cand, skip := freightCandidate(context.Background(), deps, in, io.Discard)
	if cand == nil {
		t.Fatalf("bootstrap off must not fence, got skip %q", skip)
	}
	if cand.Contract.ID != "prob800" {
		t.Fatalf("bootstrap off: want the highest-net contract prob800, got %q", cand.Contract.ID)
	}
}

// An above-probationary carrier at a station whose only eligible cargo is
// probationary-band fences everything and falls through (nil candidate).
func TestFreightCandidateAdvancedCarrierProbationaryOnlyBoardFallsThrough(t *testing.T) {
	profile := `{"profile":{"active_contracts":0},"progression":{"current_tier":"licensed"},"capacity":{"active_contracts_unlimited":true,"liability_unlimited":true}}`
	board := `{"shipments":[{"eligible":true,"contract":{"id":"prob800","destination_base_id":"sol_central","base_reward":800,"route_hops":2,"service_level":"standard","risk_band":"probationary"}}],"total":1}`
	deps := freightGateDeps(t, profile, board)
	deps.State = &missionRunState{}
	deps.FreightBootstrap = true
	in := freightInputs{CargoFree: 500, FuelCostFor: noFuel, NowTick: 0}

	cand, skip := freightCandidate(context.Background(), deps, in, io.Discard)
	if cand != nil {
		t.Fatalf("all cargo fenced must yield no candidate, got %q", cand.Contract.ID)
	}
	if !strings.Contains(skip, "no freight cleared the gate") {
		t.Fatalf("want the board-emptied skip reason, got %q", skip)
	}
}
