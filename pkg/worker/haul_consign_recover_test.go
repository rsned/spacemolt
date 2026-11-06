package worker

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/handoff"
	"github.com/rsned/spacemolt/pkg/market"
)

func recoverResolve(context.Context, string) (string, string, error) { return "a", "buy-stn", nil }

// doneGift enqueues a finished gift of 300 to the fleet agent "t" and points
// agent credentials at a temp dir holding the collector's username.
func doneGift(t *testing.T, q *handoff.Queue, id string) {
	t.Helper()
	if _, err := q.Enqueue(handoff.Record{ID: id, Holder: "collector", Station: "buy-base",
		ItemID: "adamantite_ore", Qty: 300, Recipient: "t"}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if _, err := q.Transition(id, handoff.StatusPending, handoff.StatusDone,
		func(r *handoff.Record) { r.MovedQty = 300 }); err != nil {
		t.Fatalf("Transition: %v", err)
	}
	dir := filepath.Join("data", "agents", "collector")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "credentials.json"), []byte(`{"username":"Collector"}`), 0o600); err != nil {
		t.Fatal(err)
	}
}

func giftStatus(t *testing.T, q *handoff.Queue, id string) handoff.Record {
	t.Helper()
	recs, err := q.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range recs {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("no record %s", id)
	return handoff.Record{}
}

func TestRecoverConsignGiftReturnsStrandedGoods(t *testing.T) {
	t.Chdir(t.TempDir())
	deps, fc, _, q, _ := consignRunDeps(t)
	id := consignHandoffID(7, "t")
	doneGift(t, q, id)
	fc.raw = map[string][]byte{"storage": []byte(`{"base_id":"buy-base","items":[{"item_id":"adamantite_ore","quantity":250}]}`)}

	if !recoverConsignGifts(context.Background(), deps, &bytes.Buffer{}, recoverResolve) {
		t.Fatal("a stranded gift must take the pass")
	}
	if !slices.Contains(fc.calls, "gift:Collector:adamantite_ore:250:storage") {
		t.Fatalf("calls %v, want the 250 still held gifted back from storage", fc.calls)
	}
	if got := giftStatus(t, q, id); got.Status != handoff.StatusCollected {
		t.Fatalf("status %s, want collected", got.Status)
	}
	if recoverConsignGifts(context.Background(), deps, &bytes.Buffer{}, recoverResolve) {
		t.Fatal("a returned gift must not be retried")
	}
}

// Nothing left to return, such as a gift loaded before collection was tracked,
// closes the record without a gift.
func TestRecoverConsignGiftEmptyStorageClosesRecord(t *testing.T) {
	t.Chdir(t.TempDir())
	deps, fc, _, q, _ := consignRunDeps(t)
	id := consignHandoffID(7, "t")
	doneGift(t, q, id)
	fc.raw = map[string][]byte{"storage": []byte(`{"base_id":"buy-base","items":[]}`)}

	recoverConsignGifts(context.Background(), deps, &bytes.Buffer{}, recoverResolve)
	for _, c := range fc.calls {
		if len(c) > 5 && c[:5] == "gift:" {
			t.Fatalf("unexpected gift: %v", fc.calls)
		}
	}
	if got := giftStatus(t, q, id); got.Status != handoff.StatusCollected {
		t.Fatalf("status %s, want collected", got.Status)
	}
}

func TestRecoverConsignGiftGivesUpAfterThreeFailures(t *testing.T) {
	t.Chdir(t.TempDir())
	deps, _, _, q, _ := consignRunDeps(t)
	id := consignHandoffID(7, "t")
	doneGift(t, q, id)
	bad := func(context.Context, string) (string, string, error) { return "", "", errors.New("no such base") }

	for i := range consignRecoverMaxAttempts {
		if !recoverConsignGifts(context.Background(), deps, &bytes.Buffer{}, bad) {
			t.Fatalf("attempt %d skipped", i)
		}
	}
	if got := giftStatus(t, q, id); got.Status != handoff.StatusFailed || got.RecoverAttempts != consignRecoverMaxAttempts {
		t.Fatalf("record %+v, want failed after %d attempts", got, consignRecoverMaxAttempts)
	}
	if recoverConsignGifts(context.Background(), deps, &bytes.Buffer{}, bad) {
		t.Fatal("a failed gift must not hold the hauler")
	}
}

func TestUncollectedConsignGiftSkipsOthers(t *testing.T) {
	t.Chdir(t.TempDir())
	_, _, _, q, _ := consignRunDeps(t)
	doneGift(t, q, "plan/1/node") // a craft-plan handoff, not a consigned route
	if _, err := q.Enqueue(handoff.Record{ID: consignHandoffID(2, "t"), Holder: "c", Station: "s", ItemID: "x", Qty: 5, Recipient: "t"}); err != nil {
		t.Fatal(err) // still pending: the collector has not moved it
	}
	if got, ok, err := uncollectedConsignGift(q, "t", time.Now()); err != nil || ok {
		t.Fatalf("got %+v %v %v, want neither a plan record nor a pending gift", got, ok, err)
	}

	doneGift(t, q, consignHandoffID(3, "t"))
	if got, ok, err := uncollectedConsignGift(q, "t", time.Now()); err != nil || !ok || got.ID != consignHandoffID(3, "t") {
		t.Fatalf("got %+v %v %v, want the done consigned gift", got, ok, err)
	}
	if _, ok, err := uncollectedConsignGift(q, "someone-else", time.Now()); err != nil || ok {
		t.Fatalf("another hauler's gift matched: %v %v", ok, err)
	}
	if got, ok, err := uncollectedConsignGift(q, "t", time.Now().Add(consignRecoverMaxAge+time.Hour)); err != nil || ok {
		t.Fatalf("got %+v %v %v, want an old gift ignored", got, ok, err)
	}
}

// Loading the gift is what closes the record, so a sold route is never mistaken
// for a stranded one.
func TestRunClaimedHaulMarksGiftCollected(t *testing.T) {
	t.Chdir(t.TempDir())
	deps, _, _, q, opp := consignRunDeps(t)
	id := consignHandoffID(7, "t")
	doneGift(t, q, id)
	_, n2id := graphFor([]string{"a"}, [2]string{"a", "a"})

	_ = runClaimedHaul(context.Background(), deps, io.Discard, opp, n2id, nil, haulFuel{}, 3)

	if got := giftStatus(t, q, id); got.Status != handoff.StatusCollected {
		t.Fatalf("status %s, want collected once loaded", got.Status)
	}
}

// item_not_available means the ask is gone, so the book is invalidated instead
// of the route going back to the pool for the same hauler to reclaim.
func TestRunClaimedHaulAskGoneInvalidatesBook(t *testing.T) {
	o := buyLegOpp()
	fc := &fakeClient{
		state: &game.State{
			System: game.SystemData{ID: "a", Name: "A"},
			Fuel:   100, MaxFuel: 100, Credits: 1_000_000,
			Ship: game.Ship{CargoCapacity: 500},
		},
		route:  []game.RouteStep{{SystemID: "a", Name: "A"}},
		buyErr: errors.New("item_not_available: No one is selling Reinforced Bulkhead at this market."),
	}
	f := &fakeStore{
		admitOK: true,
		prices: []market.ItemStationPrice{
			{StationID: "buy-stn", HasSell: true, BestAsk: 10, AskQty: 400},
			{StationID: "sell-stn", HasBuy: true, BestBid: 200, BidQty: 400},
		},
	}
	_, n2id := graphFor([]string{"a"}, [2]string{"a", "a"})
	if err := runClaimedHaul(context.Background(), HaulDeps{Client: fc, Market: f, AgentID: "t"}, io.Discard, o, n2id, nil, haulFuel{}, 3); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(f.invalidated) != 1 {
		t.Fatalf("invalidated %v, want the source book", f.invalidated)
	}
}
