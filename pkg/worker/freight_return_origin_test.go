package worker

// freight_return_origin_test.go — regression cover for the live 2026-07-25
// fighter-1 breach. A shipping package may only be returned at its ORIGIN
// station; a return attempted anywhere else is refused with wrong_origin.
// Pre-fix, freightChainRun treated that refusal like any other return failure:
// record return_failed, keep the contract, park the pass. The next pass hit
// the identical state ten seconds later and retried — 28 times in 4.5 minutes,
// while the log counted the contract's own deadline down 11, 10, ... 0. Then
// it breached. Retrying a call that cannot succeed from here is the defect.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// errWrongOrigin is the server's refusal, verbatim from fighter-1's log —
// reproduced exactly (trailing period and all) so the classifier is tested
// against the real wire text rather than a tidied-up paraphrase.
var errWrongOrigin = errors.New("wrong_origin: Return the intact package at its origin station.") //nolint:staticcheck // ST1005: server text, not ours to restyle

// originGatedReturnClient models the real rule the plain fakeClient cannot:
// ShippingReturn succeeds only while the ship is at the package's origin
// station, and is refused with wrong_origin everywhere else. `at` is moved by
// the test's nav closure, so a chain that flies home really does discharge.
type originGatedReturnClient struct {
	*fakeClient
	origin string
	at     string
}

func (c *originGatedReturnClient) ShippingReturn(ctx context.Context, shipmentID string) error {
	if err := c.fakeClient.ShippingReturn(ctx, shipmentID); err != nil {
		return err
	}
	if c.at != c.origin {
		return errWrongOrigin
	}
	return nil
}

// Victim selection must skip contracts already proven undischargeable, or the
// return loop re-nominates the same one forever and never reaches the healthy
// remainder. Slack at 19 ticks/hop x1.5: doomed = 10 - 7*28.5, tight = 40 - 28.5.
func TestFreightWorstReturnableStopSkipsUndischargeable(t *testing.T) {
	stops := []chainStop{
		{ContractID: "doomed", DestBaseID: "b1", Hops: 5, DeadlineTick: 10},
		{ContractID: "tight", DestBaseID: "b2", Hops: 1, DeadlineTick: 40},
	}
	if got, ok := freightWorstReturnableStop(stops, 0, nil); !ok || got.ContractID != "doomed" {
		t.Fatalf("worst = %+v ok=%v, want doomed", got, ok)
	}
	if got, ok := freightWorstReturnableStop(stops, 0, map[string]bool{"doomed": true}); !ok || got.ContractID != "tight" {
		t.Fatalf("worst excluding doomed = %+v ok=%v, want tight", got, ok)
	}
	if _, ok := freightWorstReturnableStop(stops, 0, map[string]bool{"doomed": true, "tight": true}); ok {
		t.Fatal("with every stop excluded there is no returnable victim; ok must be false so the caller stops looping")
	}
}

// A contract the chain can no longer clear must be flown back to its origin
// and returned there, rather than retried from where it stands.
func TestFreightChainRunFliesToOriginWhenReturnRefused(t *testing.T) {
	base, store := freightChainDeps(t)
	gated := &originGatedReturnClient{fakeClient: base.Client.(*fakeClient), origin: "home", at: "elsewhere"}
	deps := base
	deps.Client = gated
	deps.State.addHeldFreight(&serverapi.ShipmentContract{
		ID: "dead", OriginBaseID: "home", DestinationBaseID: "baseB",
		DeadlineTick: 10, Status: "in_transit",
	})

	var navved []string
	nav := func(ctx context.Context, baseID string) error {
		navved = append(navved, baseID)
		gated.at = baseID
		return nil
	}
	hops := func(base string) (int, bool) { return map[string]int{"baseB": 5, "home": 1}[base], true }

	step, err := freightChainRun(context.Background(), deps, nav, hops, nil, io.Discard)
	if err != nil {
		t.Fatalf("freightChainRun: %v", err)
	}
	if step == freightStepStuck {
		t.Fatal("a recoverable wrong_origin must not park the pass")
	}
	if len(navved) == 0 || navved[0] != "home" {
		t.Fatalf("nav = %v, want the origin (home) visited first so the return is legal", navved)
	}
	if store.outcomes["dead"] != "returned_inflight" {
		t.Fatalf("outcomes = %v, want dead -> returned_inflight (a clean discharge at origin)", store.outcomes)
	}
	if deps.State.heldFreightCount() != 0 {
		t.Fatal("a contract returned at its origin must leave the held set")
	}
}

// When the origin cannot be reached, the contract is undischargeable: the pass
// must stop nominating it for return and fly it to its destination instead. A
// late delivery may still settle; sitting still guarantees the breach. Above
// all it must not re-attempt a return that can never succeed.
func TestFreightChainRunWrongOriginDoesNotParkOrHammer(t *testing.T) {
	deps, store := freightChainDeps(t)
	f := deps.Client.(*fakeClient)
	f.shippingErr = map[string]error{"return": errWrongOrigin}
	deps.State.addHeldFreight(&serverapi.ShipmentContract{
		ID: "doomed", OriginBaseID: "home", DestinationBaseID: "baseB",
		DeadlineTick: 10, Status: "in_transit",
	})

	nav := func(ctx context.Context, baseID string) error {
		if baseID == "home" {
			return fmt.Errorf("no route home")
		}
		return nil
	}
	hops := func(base string) (int, bool) { return map[string]int{"baseB": 5, "home": 9}[base], true }

	step, err := freightChainRun(context.Background(), deps, nav, hops, nil, io.Discard)
	if err != nil {
		t.Fatalf("freightChainRun: %v", err)
	}
	if step == freightStepStuck {
		t.Fatal("an unrecoverable wrong_origin must not park the pass — parking is what produced the 28-retry breach")
	}
	var returns int
	for _, c := range f.shippingCalls {
		if c == "return" {
			returns++
		}
	}
	if returns > 1 {
		t.Fatalf("return attempted %d times; a wrong_origin refusal is terminal from here and must not be retried", returns)
	}
	if store.outcomes["doomed"] != "delivered" {
		t.Fatalf("outcomes = %v, want doomed -> delivered: an undischargeable package must still be flown", store.outcomes)
	}
}
