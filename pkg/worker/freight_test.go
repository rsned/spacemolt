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
	if _, reason := buildFreightCand(listing("a", false, 5000, 2), 2, nil, 0, noFuel); reason == "" {
		t.Fatal("an ineligible listing must be rejected")
	}
	// Below the net floor.
	if _, reason := buildFreightCand(listing("b", true, 100, 2), 2, nil, 0, noFuel); reason == "" {
		t.Fatal("a reward below freightMinNet must be rejected")
	}
	// Fuel can push an otherwise-acceptable reward under the floor.
	if _, reason := buildFreightCand(listing("c", true, 520, 5), 5, nil, 0, flatFuel); reason == "" {
		t.Fatal("net after fuel below freightMinNet must be rejected")
	}
	// No destination is unroutable.
	bad := listing("d", true, 5000, 2)
	bad.Contract.DestinationBaseID = ""
	if _, reason := buildFreightCand(bad, 2, nil, 0, noFuel); reason == "" {
		t.Fatal("a listing with no destination must be rejected")
	}
}

func TestBuildFreightCandAccepts(t *testing.T) {
	c, reason := buildFreightCand(listing("e", true, 5000, 3), 3, nil, 0, flatFuel)
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
	if _, reason := buildFreightCand(low, 1, nil, 0, noFuel); reason == "" {
		t.Fatal("max_speed_bonus must not count toward the net floor")
	}
}

func TestSelectFreightCandPicksHighestNet(t *testing.T) {
	if got := selectFreightCand(nil); got != nil {
		t.Fatal("no candidates must select nothing")
	}
	a, _ := buildFreightCand(listing("a", true, 1000, 1), 1, nil, 0, noFuel)
	b, _ := buildFreightCand(listing("b", true, 9000, 1), 1, nil, 0, noFuel)
	c, _ := buildFreightCand(listing("c", true, 3000, 1), 1, nil, 0, noFuel)
	got := selectFreightCand([]freightCand{a, b, c})
	if got == nil || got.Contract.ID != "b" {
		t.Fatalf("want highest-net candidate b, got %+v", got)
	}
}

// The deadline is only knowable after accept, so this gate runs post-accept.
// The smoke's own contract (3 hops, 180 ticks granted) must clear it.
func TestFreightDeadlineOK(t *testing.T) {
	// 3 hops * 19 ticks * 1.5 slack = 85.5 needed.
	if ok, reason := freightDeadlineOK(3, 1380, 1200); !ok {
		t.Fatalf("the live smoke contract must clear the gate, got %q", reason)
	}
	if ok, _ := freightDeadlineOK(3, 1240, 1200); ok {
		t.Fatal("40 ticks for a 3-hop trip must fail the gate")
	}
	// A missing deadline must fail closed: without a deadline we cannot prove
	// feasibility, and guessing is how you breach.
	if ok, _ := freightDeadlineOK(3, 0, 1200); ok {
		t.Fatal("a zero deadline_tick must fail the gate, not pass it")
	}
	// Zero hops (same-base contract) still needs a positive deadline window.
	if ok, _ := freightDeadlineOK(0, 1201, 1200); !ok {
		t.Fatal("a zero-hop contract with a live deadline must pass")
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

func TestFreightRunTripDeliversAndRecordsPayout(t *testing.T) {
	store := &fakeFreightStore{}
	delivered := acceptedContract(1200, 1380)
	delivered.Status = "delivered"
	f := &fakeClient{
		// Doc: the ship is docked at the destination — freightSettleDock
		// requires it before deliver is issued (dock is tick-deferred live).
		state: &game.State{Doc: true},
		raw:   map[string][]byte{"shipping_deliver": shippingSettlementJSON(t, "deliver", delivered, 6000)},
	}
	deps := MissionDeps{Client: f, AgentID: "fighter-4", Market: store}
	c := acceptedContract(1200, 1380)
	navigated := ""
	nav := func(ctx context.Context, baseID string) error { navigated = baseID; return nil }

	if err := freightRunTrip(context.Background(), deps, &c, &freightCand{Hops: 3}, nav, io.Discard); err != nil {
		t.Fatalf("freightRunTrip: %v", err)
	}
	if navigated != "sol_central" {
		t.Fatalf("must navigate to destination_base_id, went to %q", navigated)
	}
	// The package must be pulled from origin storage into the hold before transit.
	if !slices.Contains(f.calls, "withdraw:package:pkg_hash:1") {
		t.Fatalf("must withdraw the package: prefix item, calls were %v", f.calls)
	}
	if len(store.results) != 1 || store.results[0].Outcome != "delivered" {
		t.Fatalf("want a delivered result, got %+v", store.results)
	}
	if store.results[0].CarrierPayout != 6000 {
		t.Fatalf("payout must come from the settlement reply, got %v", store.results[0].CarrierPayout)
	}
}

func TestFreightInFlightCheckReturnsWhenBufferCollapses(t *testing.T) {
	store := &fakeFreightStore{}
	f := &fakeClient{state: &game.State{CurrentTick: 1200}}
	deps := MissionDeps{Client: f, AgentID: "fighter-4", Market: store}
	c := acceptedContract(1100, 1210) // only 10 ticks left at tick 1200

	if step := freightInFlightCheck(context.Background(), deps, &c, &freightCand{}, 3, io.Discard); step == freightStepProceed {
		t.Fatal("a collapsed buffer must not keep the contract")
	}
	if !slices.Contains(f.shippingCalls, "return") {
		t.Fatalf("must return, calls were %v", f.shippingCalls)
	}
	if len(store.results) != 1 || store.results[0].Outcome != "returned_inflight" {
		t.Fatalf("want returned_inflight, got %+v", store.results)
	}
}

func TestFreightInFlightCheckKeepsHealthyContract(t *testing.T) {
	f := &fakeClient{state: &game.State{CurrentTick: 1200}}
	deps := MissionDeps{Client: f, AgentID: "fighter-4", Market: &fakeFreightStore{}}
	c := acceptedContract(1200, 1380)

	if step := freightInFlightCheck(context.Background(), deps, &c, &freightCand{}, 1, io.Discard); step != freightStepProceed {
		t.Fatal("a healthy buffer must keep the contract")
	}
	if slices.Contains(f.shippingCalls, "return") {
		t.Fatal("must not return a healthy contract")
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

	got, ok := freightReconcile(context.Background(), deps, &log)
	if ok || got != nil {
		t.Fatalf("empty memory must never resume anything from the board, got %+v", got)
	}
	if slices.Contains(f.shippingCalls, "list") {
		t.Fatal("board-scan recovery is dropped; must not read the board")
	}
	if !strings.Contains(log.String(), "UNRECOVERABLE") {
		t.Fatalf("a profile/memory mismatch must log loudly for operator rescue, got %q", log.String())
	}
}

func TestFreightReconcileNoActiveContracts(t *testing.T) {
	f := &fakeClient{
		state: &game.State{},
		raw:   map[string][]byte{"shipping_profile": shippingProfileJSON(t, false, "")},
	}
	deps := MissionDeps{Client: f, AgentID: "fighter-4", Market: &fakeFreightStore{}}

	if got, ok := freightReconcile(context.Background(), deps, io.Discard); ok || got != nil {
		t.Fatalf("no active contracts must reconcile to nothing, got %+v", got)
	}
}

// If the package cannot be pulled into the hold we must not transit — a contract
// we cannot physically carry is a guaranteed breach.
func TestFreightRunTripReturnsWhenWithdrawFails(t *testing.T) {
	store := &fakeFreightStore{}
	f := &fakeClient{
		state:       &game.State{},
		withdrawErr: errors.New("no such item in storage"),
	}
	deps := MissionDeps{Client: f, AgentID: "fighter-4", Market: store}
	c := acceptedContract(1200, 1380)
	navigated := false
	nav := func(ctx context.Context, baseID string) error { navigated = true; return nil }

	_ = freightRunTrip(context.Background(), deps, &c, &freightCand{Hops: 3}, nav, io.Discard)
	if navigated {
		t.Fatal("must not transit a package we could not load")
	}
	if !slices.Contains(f.shippingCalls, "return") {
		t.Fatalf("must return the contract, calls were %v", f.shippingCalls)
	}
	if len(store.results) != 1 || store.results[0].Outcome != "returned_infeasible" {
		t.Fatalf("want returned_infeasible, got %+v", store.results)
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

// The reconcile-resume path re-enters freightRunTrip for a contract whose
// package is ALREADY in the hold (the nav-failure branch deliberately leaves it
// in flight). Withdrawing again fails with "not in storage", and treating that
// as "cannot carry" would destroy the very contract the nav-failure branch went
// out of its way to preserve. Package aboard => no withdraw, no return, carry on.
func TestFreightRunTripSkipsWithdrawWhenPackageAboard(t *testing.T) {
	store := &fakeFreightStore{}
	delivered := acceptedContract(1200, 1380)
	delivered.Status = "delivered"
	f := &fakeClient{
		state: &game.State{Doc: true, Ship: game.Ship{
			Cargo: []game.CargoItem{{ItemID: "package:pkg_hash", Quantity: 1}},
		}},
		// A withdraw would fail here, exactly as the live server behaves once the
		// package has left storage — the test fails loudly if one is attempted.
		withdrawErr: errors.New("no such item in storage"),
		raw:         map[string][]byte{"shipping_deliver": shippingSettlementJSON(t, "deliver", delivered, 6000)},
	}
	deps := MissionDeps{Client: f, AgentID: "fighter-4", Market: store}
	c := acceptedContract(1200, 1380)
	navigated := ""
	nav := func(ctx context.Context, baseID string) error { navigated = baseID; return nil }

	if err := freightRunTrip(context.Background(), deps, &c, &freightCand{Hops: 3}, nav, io.Discard); err != nil {
		t.Fatalf("freightRunTrip: %v", err)
	}
	for _, call := range f.calls {
		if strings.HasPrefix(call, "withdraw:") {
			t.Fatalf("must not re-withdraw a package already aboard, calls were %v", f.calls)
		}
	}
	if slices.Contains(f.shippingCalls, "return") {
		t.Fatalf("must not return a healthy in-flight contract, calls were %v", f.shippingCalls)
	}
	if navigated != "sol_central" {
		t.Fatalf("must resume transit to the destination, went to %q", navigated)
	}
	if len(store.results) != 1 || store.results[0].Outcome != "delivered" {
		t.Fatalf("want a delivered result, got %+v", store.results)
	}
}

// A settlement that never decoded records payout 0. Without a reason a canary
// reading freight_results cannot tell that from a genuinely zero-payout
// contract, so the outcome stays "delivered" but the reason must say why.
func TestFreightRunTripFlagsUndecodableSettlement(t *testing.T) {
	store := &fakeFreightStore{}
	f := &fakeClient{state: &game.State{Doc: true}} // no shipping_deliver raw reply at all
	deps := MissionDeps{Client: f, AgentID: "fighter-4", Market: store}
	c := acceptedContract(1200, 1380)
	nav := func(ctx context.Context, baseID string) error { return nil }

	if err := freightRunTrip(context.Background(), deps, &c, &freightCand{Hops: 3}, nav, io.Discard); err != nil {
		t.Fatalf("freightRunTrip: %v", err)
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
	return &fakeClient{
		state: missionState(true, 5000, 0),
		route: []game.RouteStep{{SystemID: "sol", Jumps: 2}},
		raw:   raw,
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
func TestFreightRunTripSettlesDockBeforeDeliver(t *testing.T) {
	store := &fakeFreightStore{}
	delivered := acceptedContract(1200, 1380)
	delivered.Status = "delivered"
	f := &fakeClient{
		state: &game.State{}, // undocked: nav's dock is still pending
		raw:   map[string][]byte{"shipping_deliver": shippingSettlementJSON(t, "deliver", delivered, 6000)},
	}
	deps := MissionDeps{Client: f, AgentID: "fighter-4", Market: store}
	// The injected sleep stands in for the pending dock landing on the next
	// tick: the first poll wait flips the state to docked.
	deps.sleep = func(ctx context.Context, d time.Duration) error {
		f.state.Doc = true
		return nil
	}
	c := acceptedContract(1200, 1380)
	nav := func(ctx context.Context, baseID string) error { return nil }

	if err := freightRunTrip(context.Background(), deps, &c, &freightCand{Hops: 3}, nav, io.Discard); err != nil {
		t.Fatalf("freightRunTrip: %v", err)
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
// pending dock has had a full tick to land.
func TestFreightRunTripLeavesInFlightWhenDockNeverSettles(t *testing.T) {
	store := &fakeFreightStore{}
	f := &fakeClient{state: &game.State{}} // undocked forever
	deps := MissionDeps{Client: f, AgentID: "fighter-4", Market: store}
	deps.sleep = func(ctx context.Context, d time.Duration) error { return nil }
	c := acceptedContract(1200, 1380)
	nav := func(ctx context.Context, baseID string) error { return nil }

	if err := freightRunTrip(context.Background(), deps, &c, &freightCand{Hops: 3}, nav, io.Discard); err != nil {
		t.Fatalf("a dock-settle failure must not fail the pass: %v", err)
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

	got, ok := freightReconcile(context.Background(), deps, io.Discard)
	if !ok || got == nil || got.ID != "high" {
		t.Fatalf("must resume the held contract from memory, got %+v ok=%v", got, ok)
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

	got, ok := freightReconcile(context.Background(), deps, io.Discard)
	if ok || got != nil {
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

	got, ok := freightReconcile(context.Background(), deps, io.Discard)
	if !ok || got == nil || got.ID != "high" {
		t.Fatalf("memory must win over a transient get failure, got %+v ok=%v", got, ok)
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

	accepted, step := freightAccept(context.Background(), deps, cand, nil, io.Discard)
	if step != freightStepProceed {
		t.Fatalf("accept must proceed, got step %v", step)
	}
	if all := st.heldFreightAll(); len(all) == 0 || all[0].ID != "high" {
		t.Fatalf("accept must set the held-freight memory, got %+v", all)
	}

	nav := func(ctx context.Context, baseID string) error { return nil }
	if err := freightRunTrip(context.Background(), deps, accepted, cand, nav, io.Discard); err != nil {
		t.Fatalf("freightRunTrip: %v", err)
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
	cand, reason := buildFreightCand(l, 2, held, 0, func(j int) float64 { return float64(j) * 100 })
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
	_, reason := buildFreightCand(l, 2, held, 0, nil)
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
