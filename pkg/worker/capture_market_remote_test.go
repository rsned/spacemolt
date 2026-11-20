package worker

import (
	"context"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

const (
	cjBase = "c976fa7df53c87f9616165d7d411b41a"
	cjPOI  = "862e07fa76571d0a76cab3cbaf4046e7"
)

func marketReply(baseID string) []byte {
	return []byte(`{"action":"view_market","base_id":"` + baseID + `","items":[{"item_id":"engine_core",` +
		`"buy_orders":[{"price_each":900,"quantity":5}],"sell_orders":[{"price_each":1369,"quantity":33}]}]}`)
}

func dockedAtCJ(raw []byte) *fakeClient {
	st := &game.State{CurrentPOI: cjPOI, CurrentSystem: "cloverfield"}
	st.Player.DockedAtBase = cjBase
	return &fakeClient{state: st, raw: map[string][]byte{"market": raw}}
}

// v0.728.0: a docked agent can read another station's book with base_id. That
// book must not be filed as the docked station's buy and sell orders.
func TestCaptureMarketSkipsARemoteBook(t *testing.T) {
	kb := newCraftTestKB(t)
	CaptureMarket(context.Background(), dockedAtCJ(marketReply("grand_exchange_station")), kb)
	if _, ok, err := kb.LatestDemandCapture(context.Background(), cjPOI); err != nil || ok {
		t.Fatalf("remote book was captured as CJ's (ok=%v err=%v)", ok, err)
	}
}

func TestCaptureMarketKeepsTheLocalBook(t *testing.T) {
	kb := newCraftTestKB(t)
	CaptureMarket(context.Background(), dockedAtCJ(marketReply(cjBase)), kb)
	if _, ok, err := kb.LatestDemandCapture(context.Background(), cjPOI); err != nil || !ok {
		t.Fatalf("local book was not captured (ok=%v err=%v)", ok, err)
	}
}
