package worker

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/handoff"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/market"
)

func consignedOpp(qty float64) market.ArbitrageOpportunity {
	return market.ArbitrageOpportunity{
		ID: 42, ItemID: "adamantite_ore", Quantity: qty,
		FromStationID: "voss_redoubt", ToStationID: "factory_belt",
		ActionType: market.ActionWithdrawThenSell,
		Notes:      "consign holder=marketbot_alhena base=voss_redoubt_station",
	}
}

func consignGiftDeps(t *testing.T) (HaulDeps, *handoff.Queue) {
	t.Helper()
	q := handoff.NewQueue(filepath.Join(t.TempDir(), "handoff.json"))
	kb := knowledge.NewMemoryKB()
	if err := kb.StoreItems(context.Background(), []knowledge.CatalogItem{{ID: "adamantite_ore", Size: 2}}); err != nil {
		t.Fatalf("StoreItems: %v", err)
	}
	return HaulDeps{KB: kb, AgentID: "hauler-3", Handoff: q}, q
}

// The claim asks the collector for one hold's worth, sized by the item's cargo
// footprint (a size-2 ore fills a 1,900 hold at 950), never more than the
// route offers.
func TestRequestConsignGiftSizesByItemSize(t *testing.T) {
	deps, q := consignGiftDeps(t)
	if err := requestConsignGift(context.Background(), deps, consignedOpp(2838), 1900); err != nil {
		t.Fatalf("requestConsignGift: %v", err)
	}
	recs, _ := q.List()
	if len(recs) != 1 {
		t.Fatalf("records = %+v, want 1", recs)
	}
	r := recs[0]
	if r.ID != "consign/42/hauler-3" || r.Holder != "marketbot_alhena" || r.Station != "voss_redoubt_station" ||
		r.ItemID != "adamantite_ore" || r.Qty != 950 || r.Recipient != "hauler-3" || r.Status != handoff.StatusPending {
		t.Fatalf("record = %+v", r)
	}

	// Smaller offer than the hold: ask for the offer. Re-requesting the same
	// claim (a resumed run) must not add a second record.
	deps2, q2 := consignGiftDeps(t)
	for range 2 {
		if err := requestConsignGift(context.Background(), deps2, consignedOpp(300), 1900); err != nil {
			t.Fatalf("requestConsignGift: %v", err)
		}
	}
	recs2, _ := q2.List()
	if len(recs2) != 1 || recs2[0].Qty != 300 {
		t.Fatalf("records = %+v, want one of 300", recs2)
	}
}

func TestRequestConsignGiftRejectsUnusableRoutes(t *testing.T) {
	deps, _ := consignGiftDeps(t)
	ordinary := consignedOpp(100)
	ordinary.ActionType = market.ActionBuyThenSell
	if err := requestConsignGift(context.Background(), deps, ordinary, 1900); err == nil {
		t.Error("an ordinary route was accepted as consigned")
	}
	if err := requestConsignGift(context.Background(), deps, consignedOpp(100), 1); err == nil {
		t.Error("a hold too small for one unit was accepted")
	}
	deps.Handoff = nil
	if err := requestConsignGift(context.Background(), deps, consignedOpp(100), 1900); err == nil {
		t.Error("no handoff queue was accepted: the gift could never be requested")
	}
}

func TestAwaitConsignGift(t *testing.T) {
	ctx := context.Background()
	opp := consignedOpp(300)

	t.Run("done returns moved", func(t *testing.T) {
		deps, q := consignGiftDeps(t)
		if err := requestConsignGift(ctx, deps, opp, 1900); err != nil {
			t.Fatalf("request: %v", err)
		}
		if _, err := q.Transition("consign/42/hauler-3", handoff.StatusPending, handoff.StatusDone,
			func(r *handoff.Record) { r.MovedQty = 280 }); err != nil {
			t.Fatalf("Transition: %v", err)
		}
		moved, err := awaitConsignGift(ctx, deps, opp)
		if err != nil || moved != 280 {
			t.Fatalf("awaitConsignGift = %d, %v; want 280, nil", moved, err)
		}
	})

	t.Run("lands while waiting", func(t *testing.T) {
		deps, q := consignGiftDeps(t)
		if err := requestConsignGift(ctx, deps, opp, 1900); err != nil {
			t.Fatalf("request: %v", err)
		}
		polls := 0
		restore := sleepFunc
		sleepFunc = func(context.Context, time.Duration) error {
			polls++
			if polls == 3 {
				_, _ = q.Transition("consign/42/hauler-3", handoff.StatusPending, handoff.StatusDone,
					func(r *handoff.Record) { r.MovedQty = 300 })
			}
			return nil
		}
		t.Cleanup(func() { sleepFunc = restore })
		moved, err := awaitConsignGift(ctx, deps, opp)
		if err != nil || moved != 300 || polls != 3 {
			t.Fatalf("awaitConsignGift = %d, %v after %d polls; want 300, nil after 3", moved, err, polls)
		}
	})

	t.Run("failed means the source is gone", func(t *testing.T) {
		deps, q := consignGiftDeps(t)
		if err := requestConsignGift(ctx, deps, opp, 1900); err != nil {
			t.Fatalf("request: %v", err)
		}
		if _, err := q.Transition("consign/42/hauler-3", handoff.StatusPending, handoff.StatusFailed,
			func(r *handoff.Record) { r.Error = "holder storage empty" }); err != nil {
			t.Fatalf("Transition: %v", err)
		}
		if _, err := awaitConsignGift(ctx, deps, opp); !errors.Is(err, errConsignSourceGone) {
			t.Fatalf("err = %v, want errConsignSourceGone", err)
		}
	})

	t.Run("still pending after the cap", func(t *testing.T) {
		deps, _ := consignGiftDeps(t)
		if err := requestConsignGift(ctx, deps, opp, 1900); err != nil {
			t.Fatalf("request: %v", err)
		}
		if _, err := awaitConsignGift(ctx, deps, opp); !errors.Is(err, errConsignGiftPending) {
			t.Fatalf("err = %v, want errConsignGiftPending", err)
		}
	})
}

// consignRunDeps stands a size-aware hauler (hold 500) at the source of a
// consigned adamantite route with a live 1,600 bid at the destination.
func consignRunDeps(t *testing.T) (HaulDeps, *fakeClient, *fakeStore, *handoff.Queue, market.ArbitrageOpportunity) {
	t.Helper()
	deps, q := consignGiftDeps(t)
	deps.AgentID = "t"
	fc := &fakeClient{
		state: &game.State{
			System: game.SystemData{ID: "a", Name: "A"},
			Fuel:   100, MaxFuel: 100,
			Ship: game.Ship{CargoCapacity: 500},
		},
		route: []game.RouteStep{{SystemID: "a", Name: "A"}},
	}
	f := &fakeStore{
		admitOK: true,
		prices:  []market.ItemStationPrice{{StationID: "sell-stn", HasBuy: true, BestBid: 1600, BidQty: 1000}},
	}
	deps.Client, deps.Market = fc, f
	opp := consignedOpp(1000)
	opp.ID, opp.FromSystemName, opp.ToSystemName = 7, "a", "a"
	opp.FromStationID, opp.ToStationID = "buy-stn", "sell-stn"
	return deps, fc, f, q, opp
}

func withdrawCalls(fc *fakeClient) []string {
	var out []string
	for _, c := range fc.calls {
		if strings.HasPrefix(c, "withdraw:") {
			out = append(out, c)
		}
	}
	return out
}

// The claim requests the gift, and the load takes no more than the gift
// actually moved and no more than the hold fits at the item's size.
func TestRunClaimedHaulConsignedWithdrawsWhatWasGifted(t *testing.T) {
	deps, fc, _, q, opp := consignRunDeps(t)
	// The collector already fulfilled this claim's request, short: 300 moved.
	if _, err := q.Enqueue(handoff.Record{ID: consignHandoffID(7, "t"), Holder: "marketbot_alhena",
		Station: "voss_redoubt_station", ItemID: "adamantite_ore", Qty: 250, Recipient: "t"}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if _, err := q.Transition(consignHandoffID(7, "t"), handoff.StatusPending, handoff.StatusDone,
		func(r *handoff.Record) { r.MovedQty = 300 }); err != nil {
		t.Fatalf("Transition: %v", err)
	}
	_, n2id := graphFor([]string{"a"}, [2]string{"a", "a"})

	_ = runClaimedHaul(context.Background(), deps, io.Discard, opp, n2id, nil, haulFuel{}, 3)

	// hold 500 / size 2 = 250 fit, 300 were gifted, the gate offered 500.
	if got := withdrawCalls(fc); len(got) != 1 || got[0] != "withdraw:adamantite_ore:250" {
		t.Fatalf("withdraws = %v, want [withdraw:adamantite_ore:250]", got)
	}
}

// A fresh claim enqueues the gift request for the holder named on the route.
func TestRunClaimedHaulConsignedRequestsTheGift(t *testing.T) {
	deps, _, _, q, opp := consignRunDeps(t)
	_, n2id := graphFor([]string{"a"}, [2]string{"a", "a"})

	_ = runClaimedHaul(context.Background(), deps, io.Discard, opp, n2id, nil, haulFuel{}, 3)

	recs, _ := q.List()
	if len(recs) != 1 || recs[0].ID != consignHandoffID(7, "t") || recs[0].Holder != "marketbot_alhena" || recs[0].Qty != 250 {
		t.Fatalf("records = %+v, want one request to marketbot_alhena for 250", recs)
	}
}

// A failed gift means the collector holds none: the book is dead for every
// destination, so it is invalidated, and nothing is withdrawn.
func TestRunClaimedHaulConsignedSourceGoneInvalidatesBook(t *testing.T) {
	deps, fc, f, q, opp := consignRunDeps(t)
	if _, err := q.Enqueue(handoff.Record{ID: consignHandoffID(7, "t"), Holder: "marketbot_alhena",
		Station: "voss_redoubt_station", ItemID: "adamantite_ore", Qty: 250, Recipient: "t"}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if _, err := q.Transition(consignHandoffID(7, "t"), handoff.StatusPending, handoff.StatusFailed,
		func(r *handoff.Record) { r.Error = "holder storage empty" }); err != nil {
		t.Fatalf("Transition: %v", err)
	}
	_, n2id := graphFor([]string{"a"}, [2]string{"a", "a"})

	if err := runClaimedHaul(context.Background(), deps, io.Discard, opp, n2id, nil, haulFuel{}, 3); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := withdrawCalls(fc); len(got) != 0 {
		t.Fatalf("withdrew %v from a source that gifted nothing", got)
	}
	if len(f.invalidated) != 1 || f.invalidated[0] != "adamantite_ore/buy-stn" {
		t.Fatalf("invalidated = %v, want the consigned book", f.invalidated)
	}
}

// Without a handoff queue the gift can never be requested; the route goes
// back to the pool before the hauler flies anywhere.
func TestRunClaimedHaulConsignedWithoutQueueReleases(t *testing.T) {
	deps, fc, f, _, opp := consignRunDeps(t)
	deps.Handoff = nil
	_, n2id := graphFor([]string{"a"}, [2]string{"a", "a"})

	if err := runClaimedHaul(context.Background(), deps, io.Discard, opp, n2id, nil, haulFuel{}, 3); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(f.released) != 1 {
		t.Fatalf("released = %v, want the row back in the pool", f.released)
	}
	if got := withdrawCalls(fc); len(got) != 0 {
		t.Fatalf("withdrew %v without a gift", got)
	}
	for _, c := range fc.calls {
		if strings.HasPrefix(c, "travel") || strings.HasPrefix(c, "jump") || strings.HasPrefix(c, "autopilot") {
			t.Fatalf("flew (%s) before learning the gift could not be requested", c)
		}
	}
}
