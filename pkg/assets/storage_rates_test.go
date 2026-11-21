package assets

import (
	"context"
	"math"
	"testing"
	"time"
)

var rateT0 = time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)

// A capture appends history: two passes 12h apart are both kept, and an item
// that vanished in the second pass reads as 0 there.
func TestReplaceStorageAppendsHistory(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	first := []StorageBase{{BaseID: "gold_run", Items: []StorageItem{
		{ItemID: "carbon_ore", Quantity: 100}, {ItemID: "iron_ore", Quantity: 50}}}}
	second := []StorageBase{{BaseID: "gold_run", Items: []StorageItem{
		{ItemID: "carbon_ore", Quantity: 400}}}}
	if err := st.ReplaceStorage(ctx, "p1", first, rateT0); err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceStorage(ctx, "p1", second, rateT0.Add(12*time.Hour)); err != nil {
		t.Fatal(err)
	}
	snaps, err := st.LoadStorageHistory(ctx, rateT0.Add(-time.Hour), []string{"p1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) != 2 {
		t.Fatalf("want 2 snapshots, got %d", len(snaps))
	}
	if got := snaps[1].Items[BaseItem{"gold_run", "carbon_ore"}]; got != 400 {
		t.Errorf("second carbon: want 400, got %v", got)
	}
	if _, ok := snaps[1].Items[BaseItem{"gold_run", "iron_ore"}]; ok {
		t.Errorf("iron vanished in pass 2; it must be absent (read as 0)")
	}
}

// Mined per day = (last - first + what left storage - what entered it) / days.
// gold_run carbon: 100 -> 400 over 12h, 300 sold, 60 consumed by crafting,
// 10 bought in -> mined 650 in 12h = 1,300/day. Iron vanished (sold 50) -> 0/day.
func TestStorageRatesCountsSalesAndCrafting(t *testing.T) {
	snaps := []StorageSnapshot{
		{PlayerID: "p1", At: rateT0, Items: map[BaseItem]float64{
			{"gold_run", "carbon_ore"}: 100, {"gold_run", "iron_ore"}: 50}},
		{PlayerID: "p1", At: rateT0.Add(12 * time.Hour), Items: map[BaseItem]float64{
			{"gold_run", "carbon_ore"}: 400}},
	}
	in := rateT0.Add(6 * time.Hour)
	flows := []StorageFlow{
		{PlayerID: "p1", BaseID: "gold_run", ItemID: "carbon_ore", At: in, Out: 300},
		{PlayerID: "p1", BaseID: "gold_run", ItemID: "carbon_ore", At: in, Out: 60},
		{PlayerID: "p1", BaseID: "gold_run", ItemID: "carbon_ore", At: in, Out: -10},
		{PlayerID: "p1", BaseID: "gold_run", ItemID: "iron_ore", At: in, Out: 50},
		// Outside the window: ignored.
		{PlayerID: "p1", BaseID: "gold_run", ItemID: "carbon_ore", At: rateT0.Add(13 * time.Hour), Out: 999},
	}
	rates := StorageRates(snaps, flows, nil)
	byItem := map[string]StorageRate{}
	for _, r := range rates {
		byItem[r.ItemID] = r
	}
	if got := byItem["carbon_ore"].PerDay; math.Abs(got-1300) > 1e-9 {
		t.Errorf("carbon per day: want 1300, got %v", got)
	}
	if got := byItem["iron_ore"].PerDay; got != 0 {
		t.Errorf("iron per day: want 0, got %v", got)
	}
}

// The window ends at the last snapshot the action log covers: flows after the
// cursor are not known yet, so a later snapshot would undercount sales.
func TestStorageRatesStopsAtActionLogCoverage(t *testing.T) {
	snaps := []StorageSnapshot{
		{PlayerID: "p1", At: rateT0, Items: map[BaseItem]float64{{"b", "x"}: 0}},
		{PlayerID: "p1", At: rateT0.Add(24 * time.Hour), Items: map[BaseItem]float64{{"b", "x"}: 100}},
		{PlayerID: "p1", At: rateT0.Add(36 * time.Hour), Items: map[BaseItem]float64{{"b", "x"}: 0}},
	}
	covered := map[string]time.Time{"p1": rateT0.Add(30 * time.Hour)}
	rates := StorageRates(snaps, nil, covered)
	if len(rates) != 1 || rates[0].PerDay != 100 || !rates[0].To.Equal(rateT0.Add(24*time.Hour)) {
		t.Fatalf("want one 100/day rate ending at +24h, got %+v", rates)
	}
}

func TestFlowsFromEvent(t *testing.T) {
	recipes := map[string]RecipeIO{"process_nitrogen_ice": {
		Inputs: map[string]float64{"nitrogen_ice": 5}, Outputs: map[string]float64{"liquid_nitrogen": 2}}}
	sell := FlowsFromEvent("p1", rateT0, "trading.exchange_fill",
		`{"base_id":"crimson_war_citadel","item_id":"iron_ore","quantity":"78","role":"seller"}`, recipes)
	if len(sell) != 1 || sell[0].Out != 78 || sell[0].BaseID != "crimson_war_citadel" {
		t.Errorf("seller fill: got %+v", sell)
	}
	buy := FlowsFromEvent("p1", rateT0, "trading.exchange_fill",
		`{"base_id":"b","item_id":"argon_gas","quantity":"10","role":"buyer"}`, recipes)
	if len(buy) != 1 || buy[0].Out != -10 {
		t.Errorf("buyer fill: got %+v", buy)
	}
	craft := FlowsFromEvent("p1", rateT0, "crafting.completed",
		`{"facility_id":"workshop:e66:the_levy_customs_station","recipe_id":"process_nitrogen_ice","runs":"50","storage":"station"}`, recipes)
	want := map[string]float64{"nitrogen_ice": 250, "liquid_nitrogen": -100}
	if len(craft) != 2 {
		t.Fatalf("craft: want 2 flows, got %+v", craft)
	}
	for _, f := range craft {
		if f.BaseID != "the_levy_customs_station" || f.Out != want[f.ItemID] {
			t.Errorf("craft flow: got %+v", f)
		}
	}
	if got := FlowsFromEvent("p1", rateT0, "crafting.completed",
		`{"facility_id":"workshop:e66:x","recipe_id":"process_nitrogen_ice","runs":"5","storage":"cargo"}`, recipes); len(got) != 0 {
		t.Errorf("cargo craft must not touch storage, got %+v", got)
	}
}

// A non-drone agent keeps one history snapshot a day: captures inside the gap
// still refresh current storage but add no history row.
func TestReplaceStorageEveryThrottlesHistory(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	base := func(q float64) []StorageBase {
		return []StorageBase{{BaseID: "cj", Items: []StorageItem{{ItemID: "iron_ore", Quantity: q}}}}
	}
	for h, q := range []float64{10, 20, 30} { // +0h, +1h, +2h: only the first is kept
		if err := st.ReplaceStorageEvery(ctx, "p1", base(q), rateT0.Add(time.Duration(h)*time.Hour), 23*time.Hour); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.ReplaceStorageEvery(ctx, "p1", base(40), rateT0.Add(24*time.Hour), 23*time.Hour); err != nil {
		t.Fatal(err)
	}
	snaps, err := st.LoadStorageHistory(ctx, rateT0.Add(-time.Hour), []string{"p1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) != 2 || snaps[0].Items[BaseItem{"cj", "iron_ore"}] != 10 || snaps[1].Items[BaseItem{"cj", "iron_ore"}] != 40 {
		t.Fatalf("want history at +0h (10) and +24h (40), got %+v", snaps)
	}
}

func TestStorageHistoryEvery(t *testing.T) {
	for agent, want := range map[string]time.Duration{
		"marketbot_gold_run": 0, "marketbot_home2": 0,
		"miningbot_0001": StorageHistoryDaily, "trucker-10": StorageHistoryDaily,
	} {
		if got := StorageHistoryEvery(agent); got != want {
			t.Errorf("StorageHistoryEvery(%q) = %v, want %v", agent, got, want)
		}
	}
}

// A sell order escrows its units when it is created, a storage gift leaves at
// once, and a station craft at a bare facility id names no station: all three
// yield flows with no base, which StorageRates books at the agent's main base.
func TestFlowsFromEventWithoutABase(t *testing.T) {
	recipes := map[string]RecipeIO{"refine_steel": {
		Inputs: map[string]float64{"iron_ore": 5}, Outputs: map[string]float64{"steel_plate": 2}}}
	order := FlowsFromEvent("p1", rateT0, "trading.sell_order_created",
		`{"item_id":"iron_ore","order_id":"o1","price":"1","quantity":"25000"}`, recipes)
	if len(order) != 1 || order[0].Out != 25000 || order[0].BaseID != "" {
		t.Errorf("sell order created: got %+v", order)
	}
	cancel := FlowsFromEvent("p1", rateT0, "trading.order_cancelled",
		`{"base_id":"b","item_id":"iron_ore","order_id":"o1","quantity_remaining":"101"}`, recipes)
	if len(cancel) != 1 || cancel[0].Out != -101 || cancel[0].BaseID != "b" {
		t.Errorf("order cancelled: got %+v", cancel)
	}
	gift := FlowsFromEvent("p1", rateT0, "trading.gift_sent",
		`{"item_id":"carbon_ore","quantity":"1900","source":"storage"}`, recipes)
	if len(gift) != 1 || gift[0].Out != 1900 || gift[0].BaseID != "" {
		t.Errorf("storage gift: got %+v", gift)
	}
	if got := FlowsFromEvent("p1", rateT0, "trading.gift_sent",
		`{"item_id":"carbon_ore","quantity":"1900","source":"cargo"}`, recipes); len(got) != 0 {
		t.Errorf("cargo gift must not touch storage, got %+v", got)
	}
	craft := FlowsFromEvent("p1", rateT0, "crafting.completed",
		`{"facility_id":"5ea501c1f44023d5742086dddc0703bd","recipe_id":"refine_steel","runs":"250","storage":"station"}`, recipes)
	if len(craft) != 2 || craft[0].BaseID != "" || craft[1].BaseID != "" {
		t.Errorf("bare facility craft: got %+v", craft)
	}

	snaps := []StorageSnapshot{
		{PlayerID: "p1", At: rateT0, Items: map[BaseItem]float64{
			{"kael", "iron_ore"}: 70000, {"frontier", "iron_ore"}: 72}},
		{PlayerID: "p1", At: rateT0.Add(12 * time.Hour), Items: map[BaseItem]float64{
			{"kael", "iron_ore"}: 50000, {"frontier", "iron_ore"}: 72}},
	}
	order[0].At = rateT0.Add(time.Hour)
	for _, r := range StorageRates(snaps, order, nil) {
		want := 0.0
		if r.BaseID == "kael" {
			want = 10000 // (-20000 + 25000) in 12h
		}
		if r.PerDay != want {
			t.Errorf("%s: want %v/day, got %v", r.BaseID, want, r.PerDay)
		}
	}
}

// A fill of the agent's own sell order moved nothing (the units were escrowed
// at creation), and only a sell order's cancel returns units.
func TestMovesStorage(t *testing.T) {
	own := map[string]string{"o1": "b"}
	for _, tc := range []struct {
		typ, data string
		want      bool
	}{
		{"trading.exchange_fill", `{"order_id":"o1","role":"seller"}`, false},
		{"trading.exchange_fill", `{"order_id":"theirs","role":"seller"}`, true},
		{"trading.exchange_fill", `{"order_id":"o1","role":"buyer"}`, true},
		{"trading.order_cancelled", `{"order_id":"o1"}`, true},
		{"trading.order_cancelled", `{"order_id":"a-buy-order"}`, false},
		{"trading.sell_order_created", `{"order_id":"o1"}`, true},
		{"crafting.completed", `{}`, true},
	} {
		if got := movesStorage(tc.typ, tc.data, own); got != tc.want {
			t.Errorf("%s %s: want %v, got %v", tc.typ, tc.data, tc.want, got)
		}
	}
}
