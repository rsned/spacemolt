package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// deliverFakeClient wraps the package's fakeClient (dispatch_test.go) with
// withdraw/deposit/gift simulation. It mirrors the LIVE client's real
// behavior (verified against pkg/game/client.go's parseActionResult, which
// has no "withdraw_items" case): WithdrawItems draws from a simulated
// storage pool but does NOT update the shared *game.State's cargo — the
// withdrawn amount sits in pendingWithdraw until GetCargo is explicitly
// called, exactly like the real client only learns cargo contents via a
// get_cargo round trip. DepositItems and SendGift remove the delivered
// quantity from cargo and record the call for assertions.
type deliverFakeClient struct {
	*fakeClient
	storageStock    map[string]float64 // item_id -> qty available at the source station
	pendingWithdraw map[string]float64 // item_id -> withdrawn qty not yet visible in cargo
	pendingDrain    map[string]float64 // item_id -> gifted/deposited qty not yet removed from cargo

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
	if got > 0 {
		if f.pendingWithdraw == nil {
			f.pendingWithdraw = map[string]float64{}
		}
		f.pendingWithdraw[itemID] += got
	}
	if got < quantity {
		return fmt.Errorf("insufficient_cargo: not enough %s in storage (wanted %.0f, had %.0f)", itemID, quantity, got)
	}
	return nil
}

// GetCargo overrides the embedded fakeClient's no-op stub: it applies any
// pending withdrawals to the shared *game.State's cargo, mirroring how the
// live client's get_cargo round trip is the only thing that makes a withdraw
// visible in state.Ship.Cargo. It also applies any pending gift/deposit
// drains staged by SendGift/DepositItems — the live client's
// parseActionResult has no "send_gift" case at all, and its "deposit_items"
// case only updates CargoCapacity (not the cargo item list), so a successful
// hand-off does NOT remove the delivered/gifted units from state.Ship.Cargo
// until an explicit get_cargo round trip, exactly like withdraw.
func (f *deliverFakeClient) GetCargo(ctx context.Context) error {
	f.calls = append(f.calls, "get_cargo")
	if f.state == nil {
		return nil
	}
	for itemID, qty := range f.pendingWithdraw {
		if qty <= 0 {
			continue
		}
		found := false
		for i := range f.state.Ship.Cargo {
			if f.state.Ship.Cargo[i].ItemID == itemID {
				f.state.Ship.Cargo[i].Quantity += qty
				found = true
			}
		}
		if !found {
			f.state.Ship.Cargo = append(f.state.Ship.Cargo, game.CargoItem{ItemID: itemID, Quantity: qty})
		}
	}
	f.pendingWithdraw = map[string]float64{}
	for itemID, qty := range f.pendingDrain {
		if qty <= 0 {
			continue
		}
		f.drainCargo(itemID, qty)
	}
	f.pendingDrain = map[string]float64{}
	return nil
}

func (f *deliverFakeClient) DepositItems(ctx context.Context, itemID string, quantity float64) error {
	f.calls = append(f.calls, fmt.Sprintf("deposit:%s:%.0f", itemID, quantity))
	f.depositCalls = append(f.depositCalls, depositCall{itemID: itemID, quantity: quantity})
	f.stageDrain(itemID, quantity)
	return nil
}

func (f *deliverFakeClient) SendGift(ctx context.Context, payload map[string]any) error {
	f.calls = append(f.calls, fmt.Sprintf("send_gift:%v", payload))
	f.giftCalls = append(f.giftCalls, payload)
	itemID, _ := payload["item_id"].(string)
	qty, _ := payload["quantity"].(float64)
	f.stageDrain(itemID, qty)
	return nil
}

// stageDrain records qty of itemID as gifted/deposited but not yet removed
// from cargo — mirroring the live client, which only reflects a
// send_gift/deposit_items hand-off in state.Ship.Cargo after an explicit
// get_cargo call.
func (f *deliverFakeClient) stageDrain(itemID string, quantity float64) {
	if f.pendingDrain == nil {
		f.pendingDrain = map[string]float64{}
	}
	f.pendingDrain[itemID] += quantity
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

// TestDeliverLoopsOverCargoSizedChunks is the regression test for the stale
// cargo bug: qty (8) exceeds the ship's cargo capacity (5) while the source
// has plenty of stock, so Deliver must round-trip source->destination twice —
// two WithdrawItems calls and two SendGift calls, consuming the full 8 units
// from source storage. If a gift's delivered units aren't reflected in
// state.Ship.Cargo (no GetCargo refresh after the hand-off), the second
// iteration reads the stale, still-full cargo count, skips its withdraw, and
// gifts phantom stock that isn't actually aboard — leaving source storage
// under-consumed.
func TestDeliverLoopsOverCargoSizedChunks(t *testing.T) {
	kb := newDeliverTestKB(t)
	agentsDir := t.TempDir()
	writeDeliverCreds(t, agentsDir, "craftsman-3", "Artisan 'Ace' Anderson")

	client := &deliverFakeClient{
		fakeClient:   &fakeClient{state: &game.State{Ship: game.Ship{CargoCapacity: 5}}},
		storageStock: map[string]float64{"pressure_seal": 100},
	}
	d := NewWorkerDispatch(client, kb, nil, io.Discard)
	d.AgentID = "craftsman-2"
	d.AgentsDir = agentsDir

	if err := d.Deliver(context.Background(), "pressure_seal", 8, "from_base", "to_base", "craftsman-3"); err != nil {
		t.Fatalf("Deliver: %v", err)
	}

	withdrawCalls := 0
	for _, c := range client.calls {
		if strings.HasPrefix(c, "withdraw:") {
			withdrawCalls++
		}
	}
	if withdrawCalls != 2 {
		t.Fatalf("WithdrawItems calls = %d, want 2 (cargo-sized round trips)", withdrawCalls)
	}
	if len(client.giftCalls) != 2 {
		t.Fatalf("SendGift calls = %d, want 2 (%+v)", len(client.giftCalls), client.giftCalls)
	}
	// The full 8 units must have actually come out of source storage — not
	// just been claimed as gifted while stale cargo state let the second
	// withdraw get skipped.
	if got, want := client.storageStock["pressure_seal"], float64(100-8); got != want {
		t.Fatalf("source storage left = %v, want %v (all 8 units actually withdrawn)", got, want)
	}
	var totalGifted float64
	for _, gc := range client.giftCalls {
		q, _ := gc["quantity"].(float64)
		totalGifted += q
	}
	if totalGifted != 8 {
		t.Fatalf("total gifted = %v, want 8", totalGifted)
	}
}

// TestDeliverBadRecipientFailsBeforeWithdraw is the regression test for the
// recipient fail-fast requirement: a recipient whose credentials.json is
// missing must fail Deliver before any goods are withdrawn from the source —
// resolving UsernameFor up front, not lazily inside the chunk loop.
func TestDeliverBadRecipientFailsBeforeWithdraw(t *testing.T) {
	kb := newDeliverTestKB(t)
	agentsDir := t.TempDir() // no credentials.json for "craftsman-missing"

	client := &deliverFakeClient{
		fakeClient:   &fakeClient{state: &game.State{Ship: game.Ship{CargoCapacity: 100}}},
		storageStock: map[string]float64{"pressure_seal": 10},
	}
	d := NewWorkerDispatch(client, kb, nil, io.Discard)
	d.AgentID = "craftsman-2"
	d.AgentsDir = agentsDir

	err := d.Deliver(context.Background(), "pressure_seal", 5, "from_base", "to_base", "craftsman-missing")
	if err == nil {
		t.Fatal("expected error for a recipient with no resolvable credentials")
	}
	if !strings.Contains(err.Error(), "craftsman-missing") {
		t.Fatalf("error = %q, want it to mention the recipient %q", err.Error(), "craftsman-missing")
	}
	for _, c := range client.calls {
		if strings.HasPrefix(c, "withdraw:") {
			t.Fatalf("Deliver must not withdraw before the recipient resolves, got call %q", c)
		}
		if c == "dock" {
			t.Fatalf("Deliver must not travel before the recipient resolves, got call %q", c)
		}
	}
}

// TestDeliverCapsDeliveryWhenCargoPreloaded is the regression test for the
// over-delivery bug: when the ship walks in already carrying MORE than qty
// (e.g. leftover cargo from a prior run), Deliver's loop skips the withdraw
// block entirely (carrying >= remaining) and must gift/deposit only
// min(carrying, remaining) — not the full pre-loaded amount. Surplus stays
// in cargo untouched. Covers both the SendGift and DepositItems branches.
func TestDeliverCapsDeliveryWhenCargoPreloaded(t *testing.T) {
	tests := []struct {
		name      string
		recipient string
		wantGift  bool
	}{
		{name: "gift recipient", recipient: "craftsman-3", wantGift: true},
		{name: "self deposit", recipient: "self", wantGift: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			kb := newDeliverTestKB(t)
			agentsDir := t.TempDir()
			writeDeliverCreds(t, agentsDir, "craftsman-3", "Artisan 'Ace' Anderson")

			client := &deliverFakeClient{
				fakeClient: &fakeClient{state: &game.State{Ship: game.Ship{
					CargoCapacity: 100,
					Cargo:         []game.CargoItem{{ItemID: "pressure_seal", Quantity: 20}},
				}}},
				// Source has nothing — proves the surplus already in cargo is
				// what would get over-delivered, not a fresh withdraw.
				storageStock: map[string]float64{"pressure_seal": 0},
			}
			d := NewWorkerDispatch(client, kb, nil, io.Discard)
			d.AgentID = "craftsman-2"
			d.AgentsDir = agentsDir

			if err := d.Deliver(context.Background(), "pressure_seal", 5, "from_base", "to_base", tc.recipient); err != nil {
				t.Fatalf("Deliver: %v", err)
			}

			for _, c := range client.calls {
				if strings.HasPrefix(c, "withdraw:") {
					t.Fatalf("Deliver must not withdraw when pre-loaded cargo already covers qty, got call %q", c)
				}
			}

			if tc.wantGift {
				if len(client.giftCalls) != 1 {
					t.Fatalf("SendGift calls = %d, want 1 (%+v)", len(client.giftCalls), client.giftCalls)
				}
				if got := client.giftCalls[0]["quantity"]; got != float64(5) {
					t.Fatalf("gift quantity = %v, want 5 (over-delivery: full pre-loaded cargo was sent instead of qty)", got)
				}
				if len(client.depositCalls) != 0 {
					t.Fatalf("DepositItems must not be called for a non-self recipient, got %+v", client.depositCalls)
				}
			} else {
				if len(client.depositCalls) != 1 {
					t.Fatalf("DepositItems calls = %d, want 1 (%+v)", len(client.depositCalls), client.depositCalls)
				}
				if got := client.depositCalls[0].quantity; got != 5 {
					t.Fatalf("deposit quantity = %v, want 5 (over-delivery: full pre-loaded cargo was sent instead of qty)", got)
				}
				if len(client.giftCalls) != 0 {
					t.Fatalf("SendGift must not be called for self, got %+v", client.giftCalls)
				}
			}

			// The 15-unit surplus above qty must remain in cargo, untouched.
			if got := cargoCount(client.state, "pressure_seal"); got != 15 {
				t.Fatalf("cargo pressure_seal after Deliver = %d, want 15 (surplus left untouched)", got)
			}
		})
	}
}

// TestDeliverExhaustedLogDistinguishesCargoFullFromSourceExhausted covers the
// "exhausted" log message Deliver emits when a withdraw pass yields nothing.
// That can happen for two very different reasons — the source truly had
// nothing left, or the ship's cargo hold had zero free space for an
// unrelated reason — and the wording must say which. Loop semantics (return
// nil either way) are unchanged; only the message differs.
func TestDeliverExhaustedLogDistinguishesCargoFullFromSourceExhausted(t *testing.T) {
	cases := []struct {
		name          string
		cargoCapacity float64
		cargoUsed     float64
		storageStock  float64
		wantSubstr    string
		wantNotSubstr string
	}{
		{
			name:          "cargo full leaves no room to withdraw",
			cargoCapacity: 10,
			cargoUsed:     10, // fully occupied by unrelated cargo; free space = 0
			storageStock:  5,  // source has plenty — that's not the problem
			wantSubstr:    "cargo full",
			wantNotSubstr: "exhausted",
		},
		{
			name:          "source truly has nothing",
			cargoCapacity: 100,
			cargoUsed:     0,
			storageStock:  0,
			wantSubstr:    "exhausted at from_base",
			wantNotSubstr: "cargo full",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kb := newDeliverTestKB(t)
			agentsDir := t.TempDir()
			writeDeliverCreds(t, agentsDir, "craftsman-3", "Artisan 'Ace' Anderson")

			var out bytes.Buffer
			client := &deliverFakeClient{
				fakeClient:   &fakeClient{state: &game.State{Ship: game.Ship{CargoCapacity: tc.cargoCapacity, CargoUsed: tc.cargoUsed}}},
				storageStock: map[string]float64{"pressure_seal": tc.storageStock},
			}
			d := NewWorkerDispatch(client, kb, nil, &out)
			d.AgentID = "craftsman-2"
			d.AgentsDir = agentsDir

			if err := d.Deliver(context.Background(), "pressure_seal", 5, "from_base", "to_base", "craftsman-3"); err != nil {
				t.Fatalf("Deliver: %v", err)
			}
			if len(client.giftCalls) != 0 {
				t.Fatalf("SendGift must not be called when nothing could be withdrawn, got %+v", client.giftCalls)
			}
			logged := out.String()
			if !strings.Contains(logged, tc.wantSubstr) {
				t.Fatalf("log output = %q, want substring %q", logged, tc.wantSubstr)
			}
			if strings.Contains(logged, tc.wantNotSubstr) {
				t.Fatalf("log output = %q, must not contain %q", logged, tc.wantNotSubstr)
			}
		})
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
