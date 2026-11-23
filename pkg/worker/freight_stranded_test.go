package worker

// freight_stranded_test.go — regression cover for the live 2026-11-22
// engineer-1 strand. The worker accepted a contract at Ironlight Crossroads,
// its withdraw collided with an operator action ("Another action is already
// pending"), the fallback return failed the same way, and the pass parked.
// The next pass resumed the held contract and flew 17 jumps to Gold Run with
// the package still in origin storage, then retried the deliver 860 times
// over 7.5 hours: package_not_present every time.

import (
	"context"
	"errors"
	"io"
	"slices"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// errPackageNotPresent is the server's deliver refusal, verbatim from the
// engineer-1 log.
var errPackageNotPresent = errors.New("package_not_present: That exact sealed package is not in your active ship.") //nolint:staticcheck // ST1005: server text

// strandedPkgClient models a package left in origin storage: a deliver is
// refused until a withdraw has put the package in the hold, and a withdraw
// only works while docked at the origin.
type strandedPkgClient struct {
	*fakeClient
	origin   string
	item     string
	delivers int
}

func (c *strandedPkgClient) WithdrawItems(ctx context.Context, itemID string, qty float64) error {
	if err := c.fakeClient.WithdrawItems(ctx, itemID, qty); err != nil {
		return err
	}
	if c.state.Player.DockedAtBase != c.origin {
		return errors.New("item not in storage here")
	}
	c.state.Ship.Cargo = append(c.state.Ship.Cargo, game.CargoItem{ItemID: itemID, Quantity: qty})
	return nil
}

func (c *strandedPkgClient) ShippingDeliver(ctx context.Context, id string) error {
	c.delivers++
	if err := c.fakeClient.ShippingDeliver(ctx, id); err != nil {
		return err
	}
	if cargoCount(c.state, c.item) < 1 {
		return errPackageNotPresent
	}
	return nil
}

func strandedSetup(t *testing.T, dockedAt string) (MissionDeps, *strandedPkgClient, *fakeFreightStore, *[]string) {
	t.Helper()
	deps, store := freightChainDeps(t)
	base := deps.Client.(*fakeClient)
	c := serverapi.ShipmentContract{
		ID: "strand", PackageID: "pk", OriginBaseID: "home", DestinationBaseID: "baseB",
		DeadlineTick: 99999, Status: "in_transit",
	}
	delivered := c
	delivered.Status = "delivered"
	base.raw["shipping_deliver"] = shippingSettlementJSON(t, "deliver", delivered, 1358)
	base.state.Player.DockedAtBase = dockedAt
	sc := &strandedPkgClient{fakeClient: base, origin: "home", item: freightPackageItemID("pk")}
	deps.Client = sc
	deps.sleep = func(context.Context, time.Duration) error { return nil }
	deps.State.addHeldFreight(&c)
	var navved []string
	return deps, sc, store, &navved
}

func strandedNav(sc *strandedPkgClient, navved *[]string) func(context.Context, string) error {
	return func(_ context.Context, baseID string) error {
		*navved = append(*navved, baseID)
		sc.state.Player.DockedAtBase = baseID
		return nil
	}
}

func strandedHops(base string) (int, bool) { return map[string]int{"baseB": 17, "home": 17}[base], true }

// A resumed pass still docked at the origin must load the package before it
// flies, not leave it in storage.
func TestFreightChainRunLoadsStrandedPackageBeforeLeavingOrigin(t *testing.T) {
	deps, sc, store, navved := strandedSetup(t, "home")
	step, err := freightChainRun(context.Background(), deps, strandedNav(sc, navved), strandedHops, nil, io.Discard)
	if err != nil || step != freightStepProceed {
		t.Fatalf("step=%v err=%v", step, err)
	}
	wi := slices.Index(sc.calls, "withdraw:package:pk:1")
	di := slices.Index(sc.calls, "shipping_deliver:strand")
	if wi < 0 || di < 0 || wi > di {
		t.Fatalf("calls %v: want the package withdrawn at the origin before the deliver", sc.calls)
	}
	if !slices.Equal(*navved, []string{"baseB"}) {
		t.Fatalf("nav = %v, want one trip straight to the destination", *navved)
	}
	if store.outcomes["strand"] != "delivered" {
		t.Fatalf("outcomes = %v, want strand delivered", store.outcomes)
	}
}

// A ship that reaches the destination without the package must go back to
// the origin, load it, and deliver, instead of retrying the deliver.
func TestFreightChainRunRecoversPackageNotPresentFromOrigin(t *testing.T) {
	deps, sc, store, navved := strandedSetup(t, "")
	step, err := freightChainRun(context.Background(), deps, strandedNav(sc, navved), strandedHops, nil, io.Discard)
	if err != nil || step != freightStepProceed {
		t.Fatalf("step=%v err=%v", step, err)
	}
	if !slices.Equal(*navved, []string{"baseB", "home", "baseB"}) {
		t.Fatalf("nav = %v, want destination, origin to load, destination again", *navved)
	}
	if sc.delivers != 2 {
		t.Fatalf("%d delivers, want 2 (the refused one and the real one)", sc.delivers)
	}
	if store.outcomes["strand"] != "delivered" {
		t.Fatalf("outcomes = %v, want strand delivered", store.outcomes)
	}
	if deps.State.heldFreightCount() != 0 {
		t.Fatal("a delivered contract must leave the held set")
	}
}

// When the package cannot be found at the origin either, the recovery must
// settle the contract there (return it) rather than loop.
func TestFreightChainRunPackageNotPresentIsBoundedWhenOriginHasNothing(t *testing.T) {
	deps, sc, _, navved := strandedSetup(t, "")
	sc.withdrawErr = errors.New("item not in storage")
	_, err := freightChainRun(context.Background(), deps, strandedNav(sc, navved), strandedHops, nil, io.Discard)
	if err != nil {
		t.Fatalf("freightChainRun: %v", err)
	}
	if sc.delivers != 1 {
		t.Fatalf("%d delivers, want exactly 1: a package_not_present must not be retried blind", sc.delivers)
	}
	if !slices.Contains(sc.shippingCalls, "return") {
		t.Fatalf("shipping calls %v: want the contract returned at its origin", sc.shippingCalls)
	}
}
