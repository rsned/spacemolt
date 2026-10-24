package worker

import (
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

const shopUser = "marketbot_alhena"

var shopNow = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

func shopOpts() ShopOptions {
	return ShopOptions{Now: func() time.Time { return shopNow }}
}

// newShopClient is a docked resident with the given personal storage and the
// given open orders in its view_orders reply. A nil storage map leaves the
// storage reading absent.
func newShopClient(t *testing.T, storage map[string]int, orders []serverapi.ExchangeOrder) *fakeClient {
	t.Helper()
	st := &game.State{Doc: true, CurrentPOI: "alhena_station"}
	st.Player.Username = shopUser
	st.Player.DockedAtBase = "alhena_base"
	f := &fakeClient{state: st, raw: map[string][]byte{}}
	if storage != nil {
		type line struct {
			ItemID   string `json:"item_id"`
			Quantity int    `json:"quantity"`
		}
		var items []line
		for k, v := range storage {
			items = append(items, line{k, v})
		}
		b, err := json.Marshal(map[string]any{"base_id": "alhena_base", "items": items})
		if err != nil {
			t.Fatal(err)
		}
		f.raw["storage"] = b
	}
	if orders == nil {
		orders = []serverapi.ExchangeOrder{}
	}
	b, err := json.Marshal(serverapi.ViewOrdersResponse{Action: "view_orders", Orders: orders})
	if err != nil {
		t.Fatal(err)
	}
	f.raw["orders"] = b
	return f
}

// sellReply is the action_result the real client stores under
// "create_sell_order": the command wrapper around the order body.
func sellReply(orderID string) []byte {
	return []byte(`{"command":"create_sell_order","tick":1,"result":{"action":"create_sell_order","order_id":"` + orderID + `"}}`)
}

func shopCalls(f *fakeClient, prefix string) []string {
	var out []string
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			out = append(out, c)
		}
	}
	return out
}

func TestParseShopRules(t *testing.T) {
	cases := []struct {
		spec string
		want []ShopRule
	}{
		{"", nil},
		{"pirate_moonshine:5000", []ShopRule{{Item: "pirate_moonshine", Start: 5000, Floor: 2500}}},
		{"pirate_moonshine:5000:4800, stolen_art:9001", []ShopRule{
			{Item: "pirate_moonshine", Start: 5000, Floor: 4800},
			{Item: "stolen_art", Start: 9001, Floor: 4500},
		}},
		{"x:1", []ShopRule{{Item: "x", Start: 1, Floor: 1}}},
		{"moonshine:1", []ShopRule{{Item: "moonshine", Start: 1, Floor: 1}}},
		{"x:3", []ShopRule{{Item: "x", Start: 3, Floor: 1}}},
		{"x:10:10", []ShopRule{{Item: "x", Start: 10, Floor: 10}}},
	}
	for _, c := range cases {
		got, err := ParseShopRules(c.spec)
		if err != nil {
			t.Errorf("ParseShopRules(%q): %v", c.spec, err)
			continue
		}
		if !slices.Equal(got, c.want) {
			t.Errorf("ParseShopRules(%q) = %+v, want %+v", c.spec, got, c.want)
		}
	}
	// Selling is irreversible: a malformed rule fails, never parses to less.
	for _, bad := range []string{
		"pirate_moonshine", ":5000", "x:0", "x:-5", "x:abc", "x:10:11",
		"x:10:-1", "x:10:y", "x:1:2:3", "x:10,,y:10", "x:10,x:20",
		// A floor of 0 lets a stale ask walk down to giving the goods away.
		"moonshine:10:0",
	} {
		if _, err := ParseShopRules(bad); err == nil {
			t.Errorf("ParseShopRules(%q) accepted a malformed rule", bad)
		}
	}
}

func TestParseShopRulesRefusesEnergyCrystal(t *testing.T) {
	_, err := ParseShopRules("pirate_moonshine:5000,energy_crystal:100")
	if err == nil || !strings.Contains(err.Error(), "energy_crystal") {
		t.Fatalf("err = %v, want energy_crystal refused", err)
	}
}

// No computed price may reach 0: an ask at 0 gives the goods away.
func TestShopkeepNeverPricesBelowOne(t *testing.T) {
	stale := func() (*fakeClient, *ShopState) {
		f := newShopClient(t, map[string]int{"moonshine": 5}, []serverapi.ExchangeOrder{
			{OrderID: "o9", Side: "sell", ItemID: "moonshine", Quantity: 5, Remaining: 5, PriceEach: 1, CreatedBy: shopUser},
		})
		st := &ShopState{Items: map[string]ShopItemState{
			"moonshine": {Price: 1, OrderID: "o9", PostedAt: shopNow.Add(-7 * time.Hour)},
		}}
		return f, st
	}

	// A parsed rule: floor 1.
	f, st := stale()
	if _, repriced, err := Shopkeep(context.Background(), f,
		[]ShopRule{{Item: "moonshine", Start: 1, Floor: 1}}, st, shopOpts(), io.Discard); err != nil || repriced != 0 {
		t.Fatalf("floor 1: repriced %d, err %v; want no reprice", repriced, err)
	}
	if got := shopCalls(f, "modify_order"); len(got) != 0 {
		t.Errorf("floor 1: modify calls %v, want none", got)
	}

	// A rule built without the parser, floor 0: 1 less 5% rounds to 0.
	f, st = stale()
	if _, repriced, err := Shopkeep(context.Background(), f,
		[]ShopRule{{Item: "moonshine", Start: 1, Floor: 0}}, st, shopOpts(), io.Discard); err != nil || repriced != 0 {
		t.Fatalf("floor 0: repriced %d, err %v; want no reprice", repriced, err)
	}
	if got := shopCalls(f, "modify_order"); len(got) != 0 {
		t.Errorf("floor 0: modify calls %v, want none", got)
	}
	if got := st.Items["moonshine"].Price; got != 1 {
		t.Errorf("floor 0: price %d, want 1", got)
	}

	// First post from a zero rule is clamped to 1.
	f = newShopClient(t, map[string]int{"moonshine": 5}, nil)
	f.sellOrderReply = sellReply("o1")
	st = &ShopState{}
	if posted, _, err := Shopkeep(context.Background(), f,
		[]ShopRule{{Item: "moonshine", Start: 0, Floor: 0}}, st, shopOpts(), io.Discard); err != nil || posted != 1 {
		t.Fatalf("zero rule: posted %d, err %v; want 1", posted, err)
	}
	if p := f.sellOrderPayloads[0]; p["price_each"] != 1 {
		t.Errorf("zero rule: payload %+v, want price_each 1", p)
	}
}

func TestShopkeepConsignOverlap(t *testing.T) {
	shop := []ShopRule{{Item: "moonshine", Start: 10, Floor: 5}, {Item: "stolen_art", Start: 90, Floor: 45}}
	cases := []struct {
		name   string
		shop   []ShopRule
		rules  []ConsignRule
		offers []ConsignOffer
		want   []string
	}{
		{"none configured", nil, nil, nil, nil},
		{"shop only", shop, nil, nil, nil},
		{"disjoint", shop, []ConsignRule{{Item: "iron_ore", To: "salvager-2", Min: 1}}, []ConsignOffer{{Item: "water_ice"}}, nil},
		{"consign overlap", shop, []ConsignRule{{Item: "moonshine", To: "salvager-2", Min: 1}}, nil, []string{"moonshine"}},
		{"offer overlap", shop, nil, []ConsignOffer{{Item: "stolen_art"}}, []string{"stolen_art"}},
		{"both lists, sorted and deduped", shop,
			[]ConsignRule{{Item: "stolen_art", To: "a", Min: 1}, {Item: "moonshine", To: "b", Min: 1}},
			[]ConsignOffer{{Item: "moonshine"}},
			[]string{"moonshine", "stolen_art"}},
		{"consign without shop", nil, []ConsignRule{{Item: "moonshine", To: "a", Min: 1}}, []ConsignOffer{{Item: "moonshine"}}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ShopkeepConsignOverlap(c.shop, c.rules, c.offers); !slices.Equal(got, c.want) {
				t.Errorf("overlap = %v, want %v", got, c.want)
			}
		})
	}
}

func TestShopkeepPostsFirstAskAtStart(t *testing.T) {
	f := newShopClient(t, map[string]int{"pirate_moonshine": 72}, nil)
	f.sellOrderReply = sellReply("o9")
	st := &ShopState{}
	var out strings.Builder

	posted, repriced, err := Shopkeep(context.Background(), f,
		[]ShopRule{{Item: "pirate_moonshine", Start: 5000, Floor: 4000}}, st, shopOpts(), &out)
	if err != nil {
		t.Fatalf("Shopkeep: %v", err)
	}
	if posted != 1 || repriced != 0 {
		t.Fatalf("posted, repriced = %d, %d; want 1, 0", posted, repriced)
	}
	if len(f.sellOrderPayloads) != 1 {
		t.Fatalf("sell orders = %+v, want 1", f.sellOrderPayloads)
	}
	p := f.sellOrderPayloads[0]
	if p["item_id"] != "pirate_moonshine" || p["quantity"] != 50 || p["price_each"] != 5000 {
		t.Errorf("payload = %+v, want pirate_moonshine x50 @5000", p)
	}
	got := st.Items["pirate_moonshine"]
	want := ShopItemState{Price: 5000, OrderID: "o9", PostedAt: shopNow}
	if got != want {
		t.Errorf("state = %+v, want %+v", got, want)
	}
	if !strings.Contains(out.String(), "shopkeep: posted 50 pirate_moonshine @5000 order o9") {
		t.Errorf("out = %q", out.String())
	}
}

func TestShopkeepNeverDoublePosts(t *testing.T) {
	f := newShopClient(t, map[string]int{"pirate_moonshine": 72}, []serverapi.ExchangeOrder{
		{OrderID: "o9", Side: "sell", ItemID: "pirate_moonshine", Quantity: 50, Remaining: 50, PriceEach: 5000, CreatedBy: shopUser},
	})
	f.sellOrderReply = sellReply("o10")
	st := &ShopState{Items: map[string]ShopItemState{
		"pirate_moonshine": {Price: 5000, OrderID: "o9", PostedAt: shopNow.Add(-time.Hour)},
	}}

	posted, repriced, err := Shopkeep(context.Background(), f,
		[]ShopRule{{Item: "pirate_moonshine", Start: 5000, Floor: 4000}}, st, shopOpts(), io.Discard)
	if err != nil {
		t.Fatalf("Shopkeep: %v", err)
	}
	if posted != 0 || repriced != 0 {
		t.Errorf("posted, repriced = %d, %d; want 0, 0", posted, repriced)
	}
	if c := append(shopCalls(f, "sell_order"), shopCalls(f, "modify_order")...); len(c) != 0 {
		t.Errorf("unexpected order calls %v", c)
	}
	if got := st.Items["pirate_moonshine"].OrderID; got != "o9" {
		t.Errorf("OrderID = %q, want o9", got)
	}
}

func TestShopkeepRepricesStaleAskDownToFloor(t *testing.T) {
	f := newShopClient(t, map[string]int{"pirate_moonshine": 22}, []serverapi.ExchangeOrder{
		{OrderID: "o9", Side: "sell", ItemID: "pirate_moonshine", Quantity: 50, Remaining: 50, PriceEach: 5000, CreatedBy: shopUser},
	})
	st := &ShopState{Items: map[string]ShopItemState{
		"pirate_moonshine": {Price: 5000, OrderID: "o9", PostedAt: shopNow.Add(-7 * time.Hour)},
	}}
	var out strings.Builder

	posted, repriced, err := Shopkeep(context.Background(), f,
		[]ShopRule{{Item: "pirate_moonshine", Start: 5000, Floor: 4800}}, st, shopOpts(), &out)
	if err != nil {
		t.Fatalf("Shopkeep: %v", err)
	}
	if posted != 0 || repriced != 1 {
		t.Fatalf("posted, repriced = %d, %d; want 0, 1", posted, repriced)
	}
	// 5000 less 5% is 4750, below the 4800 floor.
	if got := shopCalls(f, "modify_order"); !slices.Equal(got, []string{"modify_order:o9@4800"}) {
		t.Errorf("modify calls = %v, want [modify_order:o9@4800]", got)
	}
	if got := shopCalls(f, "sell_order"); len(got) != 0 {
		t.Errorf("unexpected sell orders %v", got)
	}
	got := st.Items["pirate_moonshine"]
	if got.Price != 4800 || !got.PostedAt.Equal(shopNow) || got.OrderID != "o9" {
		t.Errorf("state = %+v, want price 4800, posted now, order o9", got)
	}
	if !strings.Contains(out.String(), "shopkeep: repriced pirate_moonshine @4800 order o9") {
		t.Errorf("out = %q", out.String())
	}

	// At the floor a stale ask stays put: there is nothing lower to go to.
	st.Items["pirate_moonshine"] = ShopItemState{Price: 4800, OrderID: "o9", PostedAt: shopNow.Add(-7 * time.Hour)}
	f.calls = nil
	_, repriced, err = Shopkeep(context.Background(), f,
		[]ShopRule{{Item: "pirate_moonshine", Start: 5000, Floor: 4800}}, st, shopOpts(), io.Discard)
	if err != nil || repriced != 0 || len(shopCalls(f, "modify_order")) != 0 {
		t.Errorf("at floor: repriced %d, err %v, calls %v; want no reprice", repriced, err, f.calls)
	}
}

func TestShopkeepStepsUpAfterFill(t *testing.T) {
	f := newShopClient(t, map[string]int{"pirate_moonshine": 22}, nil)
	f.sellOrderReply = sellReply("o2")
	st := &ShopState{Items: map[string]ShopItemState{
		"pirate_moonshine": {Price: 5000, OrderID: "o1", PostedAt: shopNow.Add(-2 * time.Hour)},
	}}

	posted, _, err := Shopkeep(context.Background(), f,
		[]ShopRule{{Item: "pirate_moonshine", Start: 5000, Floor: 4000}}, st, shopOpts(), io.Discard)
	if err != nil {
		t.Fatalf("Shopkeep: %v", err)
	}
	if posted != 1 || len(f.sellOrderPayloads) != 1 {
		t.Fatalf("posted = %d, payloads %+v; want 1", posted, f.sellOrderPayloads)
	}
	p := f.sellOrderPayloads[0]
	if p["quantity"] != 22 || p["price_each"] != 5250 {
		t.Errorf("payload = %+v, want x22 @5250", p)
	}
	got := st.Items["pirate_moonshine"]
	want := ShopItemState{Price: 5250, OrderID: "o2", PostedAt: shopNow, LastFillAt: shopNow}
	if got != want {
		t.Errorf("state = %+v, want %+v", got, want)
	}

	// Sold out: the fill still steps the price, but nothing is posted.
	f = newShopClient(t, map[string]int{}, nil)
	st = &ShopState{Items: map[string]ShopItemState{
		"pirate_moonshine": {Price: 5000, OrderID: "o1", PostedAt: shopNow.Add(-2 * time.Hour)},
	}}
	posted, _, err = Shopkeep(context.Background(), f,
		[]ShopRule{{Item: "pirate_moonshine", Start: 5000, Floor: 4000}}, st, shopOpts(), io.Discard)
	if err != nil || posted != 0 || len(shopCalls(f, "sell_order")) != 0 {
		t.Fatalf("sold out: posted %d err %v calls %v; want nothing posted", posted, err, f.calls)
	}
	got = st.Items["pirate_moonshine"]
	if got.Price != 5250 || got.OrderID != "" || !got.LastFillAt.Equal(shopNow) {
		t.Errorf("sold-out state = %+v, want price 5250, no order, filled now", got)
	}
}

func TestShopkeepSkipsWithoutStorageReading(t *testing.T) {
	f := newShopClient(t, nil, nil)
	st := &ShopState{}
	var out strings.Builder

	posted, repriced, err := Shopkeep(context.Background(), f,
		[]ShopRule{{Item: "pirate_moonshine", Start: 5000, Floor: 4000}}, st, shopOpts(), &out)
	if err != nil {
		t.Fatalf("Shopkeep: %v", err)
	}
	if posted != 0 || repriced != 0 {
		t.Errorf("posted, repriced = %d, %d; want 0, 0", posted, repriced)
	}
	if c := append(shopCalls(f, "sell_order"), shopCalls(f, "modify_order")...); len(c) != 0 {
		t.Errorf("unexpected order calls %v", c)
	}
	if len(st.Items) != 0 {
		t.Errorf("state changed: %+v", st.Items)
	}
	if !strings.Contains(out.String(), "shopkeep: no storage reading; skipping") {
		t.Errorf("out = %q", out.String())
	}
}

func TestShopkeepIgnoresOtherSellersOrders(t *testing.T) {
	f := newShopClient(t, map[string]int{"pirate_moonshine": 72}, []serverapi.ExchangeOrder{
		{OrderID: "x1", Side: "sell", ItemID: "pirate_moonshine", Quantity: 10, Remaining: 10, PriceEach: 4000, CreatedBy: "someone else"},
	})
	f.sellOrderReply = sellReply("o9")
	st := &ShopState{}

	posted, _, err := Shopkeep(context.Background(), f,
		[]ShopRule{{Item: "pirate_moonshine", Start: 5000, Floor: 4000}}, st, shopOpts(), io.Discard)
	if err != nil {
		t.Fatalf("Shopkeep: %v", err)
	}
	if posted != 1 {
		t.Errorf("posted = %d, want 1 (another seller's ask is not ours)", posted)
	}
	if got := st.Items["pirate_moonshine"].OrderID; got != "o9" {
		t.Errorf("OrderID = %q, want o9", got)
	}
}

// An unread order book could hide our own open ask; posting blind would
// double-list, so the pass posts nothing.
func TestShopkeepSkipsWithoutOrdersReading(t *testing.T) {
	f := newShopClient(t, map[string]int{"pirate_moonshine": 72}, nil)
	f.viewOrdersErr = context.DeadlineExceeded
	f.sellOrderReply = sellReply("o9")
	st := &ShopState{}

	posted, _, err := Shopkeep(context.Background(), f,
		[]ShopRule{{Item: "pirate_moonshine", Start: 5000, Floor: 4000}}, st, shopOpts(), io.Discard)
	if err != nil || posted != 0 || len(shopCalls(f, "sell_order")) != 0 {
		t.Errorf("posted %d err %v calls %v; want nothing posted", posted, err, f.calls)
	}

	// A truncated page might have left our ask off it: same rule.
	f = newShopClient(t, map[string]int{"pirate_moonshine": 72}, nil)
	f.raw["orders"] = []byte(`{"action":"view_orders","orders":[],"has_more":true}`)
	posted, _, err = Shopkeep(context.Background(), f,
		[]ShopRule{{Item: "pirate_moonshine", Start: 5000, Floor: 4000}}, st, shopOpts(), io.Discard)
	if err != nil || posted != 0 || len(shopCalls(f, "sell_order")) != 0 {
		t.Errorf("truncated: posted %d err %v calls %v; want nothing posted", posted, err, f.calls)
	}
}

// A create whose reply carries no order id still records the post, so the
// next pass sees an order outstanding rather than posting again.
func TestShopkeepRecordsPendingWithoutOrderID(t *testing.T) {
	f := newShopClient(t, map[string]int{"pirate_moonshine": 72}, nil)
	// A stale reply from an earlier post must not be mistaken for this one.
	f.raw["create_sell_order"] = sellReply("old")
	st := &ShopState{}
	var out strings.Builder

	posted, _, err := Shopkeep(context.Background(), f,
		[]ShopRule{{Item: "pirate_moonshine", Start: 5000, Floor: 4000}}, st, shopOpts(), &out)
	if err != nil || posted != 1 {
		t.Fatalf("posted %d err %v; want 1, nil", posted, err)
	}
	got := st.Items["pirate_moonshine"]
	if got.OrderID != "pending" || !got.PostedAt.Equal(shopNow) {
		t.Errorf("state = %+v, want pending, posted now", got)
	}
	if !strings.Contains(out.String(), "order pending") {
		t.Errorf("out = %q", out.String())
	}
}

// Rules for goods we neither hold nor have ever listed are silent no-ops.
func TestShopkeepSkipsUnheldUnknownItems(t *testing.T) {
	f := newShopClient(t, map[string]int{"water_ice": 5}, nil)
	st := &ShopState{}
	var out strings.Builder
	posted, repriced, err := Shopkeep(context.Background(), f,
		[]ShopRule{{Item: "pirate_moonshine", Start: 5000, Floor: 4000}}, st, shopOpts(), &out)
	if err != nil || posted != 0 || repriced != 0 || out.Len() != 0 || len(st.Items) != 0 {
		t.Errorf("posted %d repriced %d err %v out %q state %+v; want silence", posted, repriced, err, out.String(), st.Items)
	}
}

func TestShopStateRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent", "shopkeep.json")
	st, err := LoadShopState(path)
	if err != nil || st == nil || len(st.Items) != 0 {
		t.Fatalf("missing file: %+v, %v; want empty state, nil", st, err)
	}
	st.Items["pirate_moonshine"] = ShopItemState{Price: 5250, OrderID: "o2", PostedAt: shopNow, LastFillAt: shopNow}
	if err = st.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	back, err := LoadShopState(path)
	if err != nil {
		t.Fatalf("LoadShopState: %v", err)
	}
	if got := back.Items["pirate_moonshine"]; !got.PostedAt.Equal(shopNow) || got.Price != 5250 || got.OrderID != "o2" {
		t.Errorf("round trip = %+v", got)
	}
}
