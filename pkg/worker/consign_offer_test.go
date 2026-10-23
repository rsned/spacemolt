package worker

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/rsned/spacemolt/pkg/handoff"
	"github.com/rsned/spacemolt/pkg/market"
)

type fakeConsignStockStore struct{ upserts []market.ConsignStock }

func (f *fakeConsignStockStore) UpsertConsignStock(_ context.Context, s market.ConsignStock) error {
	f.upserts = append(f.upserts, s)
	return nil
}

func TestParseConsignOffers(t *testing.T) {
	got, err := ParseConsignOffers("adamantite_ore, titanium_ingot:500")
	if err != nil {
		t.Fatalf("ParseConsignOffers: %v", err)
	}
	want := []ConsignOffer{{Item: "adamantite_ore"}, {Item: "titanium_ingot", Keep: 500}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if got, err := ParseConsignOffers(""); err != nil || got != nil {
		t.Fatalf("empty spec = %+v, %v; want nil, nil", got, err)
	}
	// A malformed offer is rejected, never skipped: a typo that parsed to no
	// offers would leave a collector's stock silently unadvertised.
	for _, bad := range []string{":5", "ore:x", "ore:-1", "ore:1:2", "ore,,ingot"} {
		if _, err := ParseConsignOffers(bad); err == nil {
			t.Errorf("ParseConsignOffers(%q) accepted a malformed offer", bad)
		}
	}
}

func newOfferClient(storage map[string]float64) *deliverFakeClient {
	c := newHandoffTestClient(100, true, "voss_redoubt_station", storage)
	c.state.CurrentPOI = "voss_redoubt"
	return c
}

// The report is held stock less the keep reserve and less what pending
// handoffs still owe out of this storage, keyed by both station ids.
func TestReportConsignStockNetsKeepAndOwed(t *testing.T) {
	client := newOfferClient(map[string]float64{"adamantite_ore": 5675})
	store := &fakeConsignStockStore{}
	owed := map[string]int{"adamantite_ore": 100}

	if err := ReportConsignStock(context.Background(), client, store, "marketbot_alhena",
		[]ConsignOffer{{Item: "adamantite_ore", Keep: 500}}, owed); err != nil {
		t.Fatalf("ReportConsignStock: %v", err)
	}
	if len(store.upserts) != 1 {
		t.Fatalf("upserts = %+v, want 1", store.upserts)
	}
	s := store.upserts[0]
	if s.Holder != "marketbot_alhena" || s.StationID != "voss_redoubt" || s.BaseID != "voss_redoubt_station" ||
		s.ItemID != "adamantite_ore" || s.Units != 5075 || s.UpdatedAt.IsZero() {
		t.Fatalf("upsert = %+v, want alhena voss_redoubt/voss_redoubt_station adamantite 5075", s)
	}
}

// An offered item absent from storage (or wholly kept/owed) reports a fresh
// zero, which withdraws the earlier offer instead of letting it linger.
func TestReportConsignStockReportsZeroWhenNothingSpare(t *testing.T) {
	client := newOfferClient(map[string]float64{"adamantite_ore": 300})
	store := &fakeConsignStockStore{}

	if err := ReportConsignStock(context.Background(), client, store, "marketbot_alhena",
		[]ConsignOffer{{Item: "adamantite_ore", Keep: 500}, {Item: "titanium_ingot"}}, nil); err != nil {
		t.Fatalf("ReportConsignStock: %v", err)
	}
	if len(store.upserts) != 2 || store.upserts[0].Units != 0 || store.upserts[1].Units != 0 {
		t.Fatalf("upserts = %+v, want two zero reports", store.upserts)
	}
}

// No reading is not a zero reading: an unreadable storage frame or an undocked
// worker must report nothing, leaving the last report to age out on its own.
func TestReportConsignStockSkipsWithoutAReading(t *testing.T) {
	offers := []ConsignOffer{{Item: "adamantite_ore"}}
	cases := map[string]func(*deliverFakeClient){
		"view_storage error": func(c *deliverFakeClient) { c.viewStorageErr = errors.New("timeout") },
		"no storage frame":   func(c *deliverFakeClient) { c.storageStock = nil },
		"undocked":           func(c *deliverFakeClient) { c.state.Doc = false },
		"no poi":             func(c *deliverFakeClient) { c.state.CurrentPOI = "" },
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			client := newOfferClient(map[string]float64{"adamantite_ore": 5675})
			setup(client)
			store := &fakeConsignStockStore{}
			_ = ReportConsignStock(context.Background(), client, store, "marketbot_alhena", offers, nil)
			if len(store.upserts) != 0 {
				t.Fatalf("reported %+v without a storage reading", store.upserts)
			}
		})
	}
}

// HandoffOwed sums what this holder's pending records at this base still have
// to move; done/failed records, other holders and other bases owe nothing here.
func TestHandoffOwed(t *testing.T) {
	q := handoff.NewQueue(filepath.Join(t.TempDir(), "handoff.json"))
	for _, r := range []handoff.Record{
		{ID: "a", Holder: "alhena", Station: "voss_station", ItemID: "ore", Qty: 100, Recipient: "h1"},
		{ID: "b", Holder: "alhena", Station: "voss_station", ItemID: "ore", Qty: 50, Recipient: "h2"},
		{ID: "c", Holder: "other", Station: "voss_station", ItemID: "ore", Qty: 999, Recipient: "h1"},
		{ID: "d", Holder: "alhena", Station: "elsewhere", ItemID: "ore", Qty: 999, Recipient: "h1"},
		{ID: "e", Holder: "alhena", Station: "voss_station", ItemID: "ore", Qty: 999, Recipient: "h1"},
	} {
		if _, err := q.Enqueue(r); err != nil {
			t.Fatalf("Enqueue: %v", err)
		}
	}
	if _, err := q.Transition("a", handoff.StatusPending, handoff.StatusPending, func(r *handoff.Record) { r.MovedQty = 30 }); err != nil {
		t.Fatalf("Transition a: %v", err)
	}
	if _, err := q.Transition("e", handoff.StatusPending, handoff.StatusDone, nil); err != nil {
		t.Fatalf("Transition e: %v", err)
	}

	owed, err := HandoffOwed(q, "alhena", "voss_station")
	if err != nil {
		t.Fatalf("HandoffOwed: %v", err)
	}
	if owed["ore"] != 120 || len(owed) != 1 {
		t.Fatalf("owed = %v, want ore=120 (70 left on a + 50 on b)", owed)
	}
}
