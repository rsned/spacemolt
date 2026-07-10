package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// deliverFakeClient wraps the package's fakeClient (dispatch_test.go) with
// withdraw/deposit/gift simulation: WithdrawItems draws from a simulated
// storage pool and lands items in the shared *game.State's cargo (mirroring
// what a live GetCargo refresh would show); DepositItems and SendGift remove
// the delivered quantity from cargo and record the call for assertions.
type deliverFakeClient struct {
	*fakeClient
	storageStock map[string]float64 // item_id -> qty available at the source station

	depositCalls []depositCall
	giftCalls    []map[string]any
}

type depositCall struct {
	itemID   string
	quantity float64
}

func (f *deliverFakeClient) WithdrawItems(ctx context.Context, itemID string, quantity float64) error {
	f.calls = append(f.calls, fmt.Sprintf("withdraw:%s:%.0f", itemID, quantity))
	avail := f.storageStock[itemID]
	got := quantity
	if got > avail {
		got = avail
	}
	f.storageStock[itemID] = avail - got
	if f.state != nil {
		found := false
		for i := range f.state.Ship.Cargo {
			if f.state.Ship.Cargo[i].ItemID == itemID {
				f.state.Ship.Cargo[i].Quantity += got
				found = true
			}
		}
		if !found && got > 0 {
			f.state.Ship.Cargo = append(f.state.Ship.Cargo, game.CargoItem{ItemID: itemID, Quantity: got})
		}
	}
	if got < quantity {
		return fmt.Errorf("insufficient_cargo: not enough %s in storage (wanted %.0f, had %.0f)", itemID, quantity, got)
	}
	return nil
}

func (f *deliverFakeClient) DepositItems(ctx context.Context, itemID string, quantity float64) error {
	f.calls = append(f.calls, fmt.Sprintf("deposit:%s:%.0f", itemID, quantity))
	f.depositCalls = append(f.depositCalls, depositCall{itemID: itemID, quantity: quantity})
	f.drainCargo(itemID, quantity)
	return nil
}

func (f *deliverFakeClient) SendGift(ctx context.Context, payload map[string]any) error {
	f.calls = append(f.calls, fmt.Sprintf("send_gift:%v", payload))
	f.giftCalls = append(f.giftCalls, payload)
	itemID, _ := payload["item_id"].(string)
	qty, _ := payload["quantity"].(float64)
	f.drainCargo(itemID, qty)
	return nil
}

func (f *deliverFakeClient) drainCargo(itemID string, quantity float64) {
	if f.state == nil {
		return
	}
	for i := range f.state.Ship.Cargo {
		if f.state.Ship.Cargo[i].ItemID == itemID {
			f.state.Ship.Cargo[i].Quantity -= quantity
		}
	}
}

// newDeliverTestKB builds an in-memory SQLiteKB with a "from" and "to" base,
// each in their own system, so resolveBase has real rows to resolve.
func newDeliverTestKB(t *testing.T) *knowledge.SQLiteKB {
	t.Helper()
	kb, err := knowledge.NewSQLiteKB(knowledge.Config{DBPath: ":memory:"})
	if err != nil {
		t.Fatalf("NewSQLiteKB: %v", err)
	}
	ctx := context.Background()
	db := kb.DB()
	rows := []string{
		`INSERT INTO pois (id, system_id, name, type, position_x, position_y)
		 VALUES ('from_poi','from_sys','From Poi','station',0,0)`,
		`INSERT INTO bases (id, poi_id, name) VALUES ('from_base','from_poi','From Base')`,
		`INSERT INTO pois (id, system_id, name, type, position_x, position_y)
		 VALUES ('to_poi','to_sys','To Poi','station',0,0)`,
		`INSERT INTO bases (id, poi_id, name) VALUES ('to_base','to_poi','To Base')`,
	}
	for _, q := range rows {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatalf("seed KB: %v", err)
		}
	}
	return kb
}

// writeDeliverCreds writes <dir>/<agentID>/credentials.json with the given
// username, mirroring the live data/agents/<id>/credentials.json shape.
func writeDeliverCreds(t *testing.T, dir, agentID, username string) {
	t.Helper()
	agentDir := filepath.Join(dir, agentID)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	raw, err := json.Marshal(map[string]string{
		"username": username,
		"empire":   "solarian",
		"password": "deadbeef",
	})
	if err != nil {
		t.Fatalf("marshal credentials: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "credentials.json"), raw, 0o600); err != nil {
		t.Fatalf("WriteFile credentials.json: %v", err)
	}
}

// TestDeliverFullyGiftsAtDestination: enough item is on hand at FROM to cover
// qty in a single withdraw; the verb autopilots to TO, docks, resolves the
// recipient agent id to its username, and gifts the full quantity.
func TestDeliverFullyGiftsAtDestination(t *testing.T) {
	kb := newDeliverTestKB(t)
	agentsDir := t.TempDir()
	writeDeliverCreds(t, agentsDir, "craftsman-3", "Artisan 'Ace' Anderson")

	client := &deliverFakeClient{
		fakeClient:   &fakeClient{state: &game.State{Ship: game.Ship{CargoCapacity: 100}}},
		storageStock: map[string]float64{"pressure_seal": 10},
	}
	d := NewWorkerDispatch(client, kb, nil, io.Discard)
	d.AgentID = "craftsman-2"
	d.AgentsDir = agentsDir

	if err := d.Deliver(context.Background(), "pressure_seal", 5, "from_base", "to_base", "craftsman-3"); err != nil {
		t.Fatalf("Deliver: %v", err)
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

// TestDeliverSelfDepositsInstead: recipient "self" deposits into the worker's
// own storage at TO instead of gifting — no credentials lookup, no SendGift.
func TestDeliverSelfDepositsInstead(t *testing.T) {
	kb := newDeliverTestKB(t)
	client := &deliverFakeClient{
		fakeClient:   &fakeClient{state: &game.State{Ship: game.Ship{CargoCapacity: 100}}},
		storageStock: map[string]float64{"copper_piping": 10},
	}
	d := NewWorkerDispatch(client, kb, nil, io.Discard)
	d.AgentID = "craftsman-2"
	d.AgentsDir = t.TempDir() // no credentials.json present; must never be read

	if err := d.Deliver(context.Background(), "copper_piping", 4, "from_base", "to_base", "self"); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if len(client.depositCalls) != 1 || client.depositCalls[0].itemID != "copper_piping" || client.depositCalls[0].quantity != 4 {
		t.Fatalf("DepositItems calls = %+v, want one call of copper_piping x4", client.depositCalls)
	}
	if len(client.giftCalls) != 0 {
		t.Fatalf("SendGift must not be called for self, got %+v", client.giftCalls)
	}
}

// TestDeliverSourceShortGiftsWhatExists: FROM storage only holds 3 of the
// requested 5. The verb withdraws what exists, gifts 3, then finds the next
// withdraw pass makes no progress and returns nil (short-delivery note logged,
// not an error) instead of looping forever.
func TestDeliverSourceShortGiftsWhatExists(t *testing.T) {
	kb := newDeliverTestKB(t)
	agentsDir := t.TempDir()
	writeDeliverCreds(t, agentsDir, "craftsman-3", "Artisan 'Ace' Anderson")

	client := &deliverFakeClient{
		fakeClient:   &fakeClient{state: &game.State{Ship: game.Ship{CargoCapacity: 100}}},
		storageStock: map[string]float64{"pressure_seal": 3},
	}
	d := NewWorkerDispatch(client, kb, nil, io.Discard)
	d.AgentID = "craftsman-2"
	d.AgentsDir = agentsDir

	if err := d.Deliver(context.Background(), "pressure_seal", 5, "from_base", "to_base", "craftsman-3"); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if len(client.giftCalls) != 1 {
		t.Fatalf("SendGift calls = %d, want 1 (%+v)", len(client.giftCalls), client.giftCalls)
	}
	if got := client.giftCalls[0]["quantity"]; got != float64(3) {
		t.Fatalf("gift quantity = %v, want 3", got)
	}
}

func TestUsernameForReadsCredentials(t *testing.T) {
	dir := t.TempDir()
	writeDeliverCreds(t, dir, "craftsman-3", "Artisan 'Ace' Anderson")

	got, err := UsernameFor(dir, "craftsman-3")
	if err != nil {
		t.Fatalf("UsernameFor: %v", err)
	}
	if got != "Artisan 'Ace' Anderson" {
		t.Fatalf("UsernameFor = %q, want %q", got, "Artisan 'Ace' Anderson")
	}

	if _, err := UsernameFor(dir, "no-such-agent"); err == nil {
		t.Fatal("expected error for missing credentials.json")
	}
}

func TestResolveBaseTwoStepLookup(t *testing.T) {
	kb := newDeliverTestKB(t)
	ctx := context.Background()

	sys, poi, err := resolveBase(ctx, kb, "from_base")
	if err != nil {
		t.Fatalf("resolveBase(from_base): %v", err)
	}
	if sys != "from_sys" || poi != "from_poi" {
		t.Fatalf("resolveBase(from_base) = (%q, %q), want (from_sys, from_poi)", sys, poi)
	}

	// Fallback: a poi id passed directly (not a base id) still resolves.
	sys, poi, err = resolveBase(ctx, kb, "to_poi")
	if err != nil {
		t.Fatalf("resolveBase(to_poi): %v", err)
	}
	if sys != "to_sys" || poi != "to_poi" {
		t.Fatalf("resolveBase(to_poi) = (%q, %q), want (to_sys, to_poi)", sys, poi)
	}

	if _, _, err := resolveBase(ctx, knowledge.NewMemoryKB(), "from_base"); err == nil {
		t.Fatal("resolveBase against a non-SQLiteKB must error")
	}
}
