package market

import (
	"context"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

// captureClient serves a fixed state and market reply; every other GameClient
// method panics through the nil embedded interface.
type captureClient struct {
	game.GameClient
	state *game.State
	raw   []byte
}

func (c *captureClient) GetState() *game.State { return c.state }

func (c *captureClient) GetRawJSON(key string) []byte {
	if key == "market" {
		return c.raw
	}
	return nil
}

func ordersAtCJ(t *testing.T, replyBase string) int {
	t.Helper()
	const cjBase = "c976fa7df53c87f9616165d7d411b41a"
	st := &game.State{CurrentPOI: "862e07fa76571d0a76cab3cbaf4046e7", CurrentSystem: "cloverfield"}
	st.Player.DockedAtBase = cjBase
	raw := []byte(`{"action":"view_market","base_id":"` + replyBase + `","items":[{"item_id":"engine_core",` +
		`"sell_orders":[{"price_each":1369,"quantity":33}]}]}`)
	c := newTestCollector(t)
	if err := CaptureFromClient(context.Background(), &captureClient{state: st, raw: raw}, c); err != nil {
		t.Fatalf("CaptureFromClient: %v", err)
	}
	var n int
	if err := c.db.QueryRow(`SELECT COUNT(*) FROM market_orders`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

// v0.728.0: a remote book read with view_market base_id is not this station's.
func TestCaptureFromClientSkipsARemoteBook(t *testing.T) {
	if n := ordersAtCJ(t, "grand_exchange_station"); n != 0 {
		t.Fatalf("remote book captured as CJ's: %d orders", n)
	}
}

func TestCaptureFromClientKeepsTheLocalBook(t *testing.T) {
	if n := ordersAtCJ(t, "c976fa7df53c87f9616165d7d411b41a"); n == 0 {
		t.Fatal("local book was not captured")
	}
}
