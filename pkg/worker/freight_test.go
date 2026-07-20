package worker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
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
	if _, reason := buildFreightCand(listing("a", false, 5000, 2), 2, noFuel); reason == "" {
		t.Fatal("an ineligible listing must be rejected")
	}
	// Below the net floor.
	if _, reason := buildFreightCand(listing("b", true, 100, 2), 2, noFuel); reason == "" {
		t.Fatal("a reward below freightMinNet must be rejected")
	}
	// Fuel can push an otherwise-acceptable reward under the floor.
	if _, reason := buildFreightCand(listing("c", true, 520, 5), 5, flatFuel); reason == "" {
		t.Fatal("net after fuel below freightMinNet must be rejected")
	}
	// No destination is unroutable.
	bad := listing("d", true, 5000, 2)
	bad.Contract.DestinationBaseID = ""
	if _, reason := buildFreightCand(bad, 2, noFuel); reason == "" {
		t.Fatal("a listing with no destination must be rejected")
	}
}

func TestBuildFreightCandAccepts(t *testing.T) {
	c, reason := buildFreightCand(listing("e", true, 5000, 3), 3, flatFuel)
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
	if _, reason := buildFreightCand(low, 1, noFuel); reason == "" {
		t.Fatal("max_speed_bonus must not count toward the net floor")
	}
}

func TestSelectFreightCandPicksHighestNet(t *testing.T) {
	if got := selectFreightCand(nil); got != nil {
		t.Fatal("no candidates must select nothing")
	}
	a, _ := buildFreightCand(listing("a", true, 1000, 1), 1, noFuel)
	b, _ := buildFreightCand(listing("b", true, 9000, 1), 1, noFuel)
	c, _ := buildFreightCand(listing("c", true, 3000, 1), 1, noFuel)
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

	got, ok := freightAccept(context.Background(), deps, cand, io.Discard)
	if !ok || got == nil {
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

	got, ok := freightAccept(context.Background(), deps, cand, io.Discard)
	if ok || got != nil {
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

	if _, ok := freightAccept(context.Background(), deps, cand, io.Discard); ok {
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

	got, ok := freightAccept(context.Background(), deps, cand, io.Discard)
	if ok || got != nil {
		t.Fatal("an infeasible contract must be released")
	}
	if len(store.results) != 1 || store.results[0].Outcome != "return_failed" {
		t.Fatalf("must record return_failed, got %+v", store.results)
	}
	if !strings.Contains(store.results[0].Reason, "connection reset") {
		t.Fatalf("reason must carry the underlying error, got %q", store.results[0].Reason)
	}
}
