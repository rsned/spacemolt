package worker

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// buyCall records one Buy(ctx, itemID, quantity) invocation.
type buyCall struct {
	itemID   string
	quantity float64
}

// buyDirectedFakeClient extends deliverFakeClient (which already stages
// withdraw effects until an explicit GetCargo, mirroring the live client) with
// a Buy path that stages the SAME way: the live client's parseActionResult has
// no case that updates state.Ship.Cargo for "buy" (only "buy":true storage +
// XP bookkeeping), so a successful Buy call does NOT make the purchased goods
// visible in cargo until GetCargo is explicitly called — exactly like
// WithdrawItems. It also carries a configurable market-listings snapshot for
// the live-ask check BuyDirected performs after ViewMarket.
type buyDirectedFakeClient struct {
	*deliverFakeClient
	listings []game.MarketListing
	buyCalls []buyCall
	buyErr   error
}

func (f *buyDirectedFakeClient) GetMarketListings() []game.MarketListing {
	return f.listings
}

func (f *buyDirectedFakeClient) ViewMarket(ctx context.Context, params map[string]any) error {
	f.calls = append(f.calls, "view_market")
	return nil
}

func (f *buyDirectedFakeClient) Buy(ctx context.Context, itemID string, quantity float64) error {
	f.calls = append(f.calls, fmt.Sprintf("buy:%s:%.0f", itemID, quantity))
	f.buyCalls = append(f.buyCalls, buyCall{itemID: itemID, quantity: quantity})
	if f.buyErr != nil {
		return f.buyErr
	}
	if f.pendingWithdraw == nil {
		f.pendingWithdraw = map[string]float64{}
	}
	f.pendingWithdraw[itemID] += quantity
	return nil
}

// newBuyDirectedTestKB builds an in-memory SQLiteKB with a single "station"
// base, so resolveBase has a real row to resolve for BuyDirected's STATION arg.
func newBuyDirectedTestKB(t *testing.T) *knowledge.SQLiteKB {
	t.Helper()
	kb, err := knowledge.NewSQLiteKB(knowledge.Config{DBPath: ":memory:"})
	if err != nil {
		t.Fatalf("NewSQLiteKB: %v", err)
	}
	ctx := context.Background()
	db := kb.DB()
	rows := []string{
		`INSERT INTO pois (id, system_id, name, type, position_x, position_y)
		 VALUES ('station_poi','station_sys','Station Poi','station',0,0)`,
		`INSERT INTO bases (id, poi_id, name) VALUES ('station_base','station_poi','Station Base')`,
	}
	for _, q := range rows {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatalf("seed KB: %v", err)
		}
	}
	return kb
}

// TestBuyDirectedBuysAndGifts: the live ask clears the ceiling, cargo has
// plenty of room for the whole qty in one purchase, so BuyDirected buys once
// and gifts the full quantity to the resolved recipient.
func TestBuyDirectedBuysAndGifts(t *testing.T) {
	kb := newBuyDirectedTestKB(t)
	agentsDir := t.TempDir()
	writeDeliverCreds(t, agentsDir, "craftsman-3", "Artisan 'Ace' Anderson")

	client := &buyDirectedFakeClient{
		deliverFakeClient: &deliverFakeClient{
			fakeClient: &fakeClient{state: &game.State{Ship: game.Ship{CargoCapacity: 100}}},
		},
		listings: []game.MarketListing{
			{ItemID: "pressure_seal", Type: "sell", PricePerUnit: 10},
		},
	}
	d := NewWorkerDispatch(client, kb, nil, io.Discard)
	d.AgentID = "craftsman-2"
	d.AgentsDir = agentsDir

	if err := d.BuyDirected(context.Background(), "pressure_seal", 5, "station_base", 20, "craftsman-3"); err != nil {
		t.Fatalf("BuyDirected: %v", err)
	}
	if len(client.buyCalls) != 1 || client.buyCalls[0].quantity != 5 {
		t.Fatalf("buyCalls = %+v, want one call of pressure_seal x5", client.buyCalls)
	}
	if len(client.giftCalls) != 1 {
		t.Fatalf("SendGift calls = %d, want 1 (%+v)", len(client.giftCalls), client.giftCalls)
	}
	got := client.giftCalls[0]
	if got["item_id"] != "pressure_seal" || got["quantity"] != float64(5) || got["recipient"] != "Artisan 'Ace' Anderson" {
		t.Fatalf("SendGift payload = %+v, want item_id=pressure_seal quantity=5 recipient=Artisan 'Ace' Anderson", got)
	}
	if len(client.depositCalls) != 0 {
		t.Fatalf("DepositItems must not be called for a non-self recipient, got %+v", client.depositCalls)
	}
}

// TestBuyDirectedCeilingExceededFailsWithoutBuying: the live ask is above
// MAX_UNIT_PRICE, so BuyDirected must fail before ever calling Buy, and the
// error must carry "replan" so the runner's park detail can route it back
// through planning.
func TestBuyDirectedCeilingExceededFailsWithoutBuying(t *testing.T) {
	kb := newBuyDirectedTestKB(t)
	agentsDir := t.TempDir()
	writeDeliverCreds(t, agentsDir, "craftsman-3", "Artisan 'Ace' Anderson")

	client := &buyDirectedFakeClient{
		deliverFakeClient: &deliverFakeClient{
			fakeClient: &fakeClient{state: &game.State{Ship: game.Ship{CargoCapacity: 100}}},
		},
		listings: []game.MarketListing{
			{ItemID: "pressure_seal", Type: "sell", PricePerUnit: 10},
		},
	}
	d := NewWorkerDispatch(client, kb, nil, io.Discard)
	d.AgentID = "craftsman-2"
	d.AgentsDir = agentsDir

	err := d.BuyDirected(context.Background(), "pressure_seal", 5, "station_base", 5, "craftsman-3")
	if err == nil {
		t.Fatal("expected error when live ask exceeds MAX_UNIT_PRICE")
	}
	if !strings.Contains(err.Error(), "replan") {
		t.Fatalf("error = %q, want it to contain %q", err.Error(), "replan")
	}
	if len(client.buyCalls) != 0 {
		t.Fatalf("Buy must not be called when the ceiling is exceeded, got %+v", client.buyCalls)
	}
	if len(client.giftCalls) != 0 {
		t.Fatalf("SendGift must not be called when the ceiling is exceeded, got %+v", client.giftCalls)
	}
}

// TestBuyDirectedZeroCeilingMeansNoCap: MAX_UNIT_PRICE "0" disables the
// ceiling entirely, so an arbitrarily high live ask must not block the buy.
func TestBuyDirectedZeroCeilingMeansNoCap(t *testing.T) {
	kb := newBuyDirectedTestKB(t)
	agentsDir := t.TempDir()
	writeDeliverCreds(t, agentsDir, "craftsman-3", "Artisan 'Ace' Anderson")

	client := &buyDirectedFakeClient{
		deliverFakeClient: &deliverFakeClient{
			fakeClient: &fakeClient{state: &game.State{Ship: game.Ship{CargoCapacity: 100}}},
		},
		listings: []game.MarketListing{
			{ItemID: "pressure_seal", Type: "sell", PricePerUnit: 999999},
		},
	}
	d := NewWorkerDispatch(client, kb, nil, io.Discard)
	d.AgentID = "craftsman-2"
	d.AgentsDir = agentsDir

	if err := d.BuyDirected(context.Background(), "pressure_seal", 5, "station_base", 0, "craftsman-3"); err != nil {
		t.Fatalf("BuyDirected: %v", err)
	}
	if len(client.buyCalls) != 1 {
		t.Fatalf("buyCalls = %+v, want one call", client.buyCalls)
	}
}

// TestBuyDirectedLoopsOverCargoSizedChunks: qty (8) exceeds the ship's cargo
// capacity (5), so BuyDirected must buy, hand off to the recipient (freeing
// cargo), then buy again — two Buy calls and two SendGift calls, no second
// travel leg (the handoff happens at STATION where the buy already occurred).
func TestBuyDirectedLoopsOverCargoSizedChunks(t *testing.T) {
	kb := newBuyDirectedTestKB(t)
	agentsDir := t.TempDir()
	writeDeliverCreds(t, agentsDir, "craftsman-3", "Artisan 'Ace' Anderson")

	client := &buyDirectedFakeClient{
		deliverFakeClient: &deliverFakeClient{
			fakeClient: &fakeClient{state: &game.State{Ship: game.Ship{CargoCapacity: 5}}},
		},
		listings: []game.MarketListing{
			{ItemID: "pressure_seal", Type: "sell", PricePerUnit: 10},
		},
	}
	d := NewWorkerDispatch(client, kb, nil, io.Discard)
	d.AgentID = "craftsman-2"
	d.AgentsDir = agentsDir

	if err := d.BuyDirected(context.Background(), "pressure_seal", 8, "station_base", 0, "craftsman-3"); err != nil {
		t.Fatalf("BuyDirected: %v", err)
	}
	if len(client.buyCalls) != 2 {
		t.Fatalf("buyCalls = %+v, want 2 calls (cargo-sized chunks)", client.buyCalls)
	}
	if got, want := client.buyCalls[0].quantity+client.buyCalls[1].quantity, float64(8); got != want {
		t.Fatalf("total bought = %v, want %v", got, want)
	}
	if len(client.giftCalls) != 2 {
		t.Fatalf("SendGift calls = %d, want 2 (%+v)", len(client.giftCalls), client.giftCalls)
	}
	// No second travel leg: dock only once, at the station.
	dockCalls := 0
	for _, c := range client.calls {
		if c == "dock" {
			dockCalls++
		}
	}
	if dockCalls != 1 {
		t.Fatalf("dock calls = %d, want 1 (gift/deposit must happen at STATION, no second travel leg)", dockCalls)
	}
}

// TestBuyDirectedSelfDepositsInstead: recipient "self" deposits into the
// worker's own storage at STATION instead of gifting.
func TestBuyDirectedSelfDepositsInstead(t *testing.T) {
	kb := newBuyDirectedTestKB(t)
	client := &buyDirectedFakeClient{
		deliverFakeClient: &deliverFakeClient{
			fakeClient: &fakeClient{state: &game.State{Ship: game.Ship{CargoCapacity: 100}}},
		},
		listings: []game.MarketListing{
			{ItemID: "copper_piping", Type: "sell", PricePerUnit: 10},
		},
	}
	d := NewWorkerDispatch(client, kb, nil, io.Discard)
	d.AgentID = "craftsman-2"
	d.AgentsDir = t.TempDir() // no credentials.json present; must never be read

	if err := d.BuyDirected(context.Background(), "copper_piping", 4, "station_base", 0, "self"); err != nil {
		t.Fatalf("BuyDirected: %v", err)
	}
	if len(client.depositCalls) != 1 || client.depositCalls[0].itemID != "copper_piping" || client.depositCalls[0].quantity != 4 {
		t.Fatalf("DepositItems calls = %+v, want one call of copper_piping x4", client.depositCalls)
	}
	if len(client.giftCalls) != 0 {
		t.Fatalf("SendGift must not be called for self, got %+v", client.giftCalls)
	}
}

// TestBuyDirectedNoLiveAskFails: the market has no sell listing for the item
// at all — BuyDirected must fail cleanly instead of buying at an unknown price.
func TestBuyDirectedNoLiveAskFails(t *testing.T) {
	kb := newBuyDirectedTestKB(t)
	agentsDir := t.TempDir()
	writeDeliverCreds(t, agentsDir, "craftsman-3", "Artisan 'Ace' Anderson")

	client := &buyDirectedFakeClient{
		deliverFakeClient: &deliverFakeClient{
			fakeClient: &fakeClient{state: &game.State{Ship: game.Ship{CargoCapacity: 100}}},
		},
		listings: nil,
	}
	d := NewWorkerDispatch(client, kb, nil, io.Discard)
	d.AgentID = "craftsman-2"
	d.AgentsDir = agentsDir

	err := d.BuyDirected(context.Background(), "pressure_seal", 5, "station_base", 0, "craftsman-3")
	if err == nil {
		t.Fatal("expected error when there is no live ask for the item")
	}
	if len(client.buyCalls) != 0 {
		t.Fatalf("Buy must not be called when there is no live ask, got %+v", client.buyCalls)
	}
}
