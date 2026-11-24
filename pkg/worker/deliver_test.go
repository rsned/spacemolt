package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/knowledge/knowledgetest"
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
	factionStock    map[string]float64 // faction storage at the source, for faction: sources
	pendingDrain    map[string]float64 // item_id -> gifted/deposited qty not yet removed from cargo

	depositCalls []depositCall
	giftCalls    []map[string]any
	// factionDepositCalls records FactionDepositItems (recipient "faction").
	factionDepositCalls []depositCall

	// giftErrOnAttempt, when > 0, makes the Nth SendGift call (1-indexed,
	// counting every attempt including earlier failures) return giftErr
	// instead of succeeding — for simulating a transient failure partway
	// through a multi-batch pass. All other attempts succeed normally.
	giftErrOnAttempt int
	giftErr          error
	giftAttempts     int

	// hookOnAttempt, when > 0, runs hook synchronously at the start of the
	// Nth SendGift call (1-indexed, same counting as giftErrOnAttempt),
	// before that call's own gift/error handling — for injecting a side
	// effect (e.g. a concurrent writer mutating the handoff queue via a
	// second *handoff.Queue handle on the same file) at a precise point
	// between a record's pickup and its progress persist.
	hookOnAttempt int
	hook          func()

	// atomicWithdraw mirrors the live server: a withdraw larger than the
	// stock moves nothing and answers "Storage only has N x item".
	atomicWithdraw bool

	// liveCapacity, when > 0, is the hold size the server reports on
	// get_cargo; until then state.Ship.CargoCapacity may be stale (a hull
	// switch leaves the old ship's capacity cached).
	liveCapacity float64

	// itemSize, when set for an item, is its cargo footprint per unit: a
	// withdraw whose units x size exceed the free hold moves nothing and
	// answers like the live server ("cargo_full: ... Need 5700 but only 1900
	// available").
	itemSize map[string]int
}

type depositCall struct {
	itemID   string
	quantity float64
}

// FactionWithdrawItems draws from faction storage into cargo, landing on the
// next GetCargo like a personal withdraw.
func (f *deliverFakeClient) FactionWithdrawItems(_ context.Context, itemID string, quantity int) error {
	f.calls = append(f.calls, fmt.Sprintf("faction_withdraw:%s:%d", itemID, quantity))
	got := min(float64(quantity), f.factionStock[itemID])
	f.factionStock[itemID] -= got
	if f.pendingWithdraw == nil {
		f.pendingWithdraw = map[string]float64{}
	}
	f.pendingWithdraw[itemID] += got
	return nil
}

func (f *deliverFakeClient) WithdrawItems(ctx context.Context, itemID string, quantity float64) error {
	f.calls = append(f.calls, fmt.Sprintf("withdraw:%s:%.0f", itemID, quantity))
	avail := f.storageStock[itemID]
	if size := f.itemSize[itemID]; size > 0 && f.state != nil {
		need := int(quantity) * size
		free := int(f.state.Ship.CargoCapacity - f.state.Ship.CargoUsed)
		if need > free {
			return fmt.Errorf("cargo_full: Not enough cargo space. Need %d but only %d available. Use 'deposit_items' or 'jettison' to free space.", need, free)
		}
	}
	if f.atomicWithdraw && quantity > avail {
		return fmt.Errorf("insufficient_storage: Storage only has %.0f x %s. Use 'view_storage' to check.", avail, itemID)
	}
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
	if f.liveCapacity > 0 {
		f.state.Ship.CargoCapacity = f.liveCapacity
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

func (f *deliverFakeClient) FactionDepositItems(ctx context.Context, itemID string, quantity int) error {
	f.calls = append(f.calls, fmt.Sprintf("faction_deposit:%s:%d", itemID, quantity))
	f.factionDepositCalls = append(f.factionDepositCalls, depositCall{itemID: itemID, quantity: float64(quantity)})
	f.stageDrain(itemID, float64(quantity))
	return nil
}

func (f *deliverFakeClient) SendGift(ctx context.Context, payload map[string]any) error {
	f.giftAttempts++
	if f.hookOnAttempt > 0 && f.giftAttempts == f.hookOnAttempt && f.hook != nil {
		f.hook()
	}
	if f.giftErrOnAttempt > 0 && f.giftAttempts == f.giftErrOnAttempt {
		f.calls = append(f.calls, fmt.Sprintf("send_gift_err:%v", payload))
		return f.giftErr
	}
	itemID, _ := payload["item_id"].(string)
	qty, _ := payload["quantity"].(float64)
	if payload["source"] == "storage" {
		// A storage gift never touches cargo: it draws straight from the
		// station storage, and an oversized one moves nothing.
		if qty > f.storageStock[itemID] {
			f.calls = append(f.calls, fmt.Sprintf("send_gift_short:%v", payload))
			return fmt.Errorf("insufficient_storage: You only have %.0f x %s in storage.", f.storageStock[itemID], itemID)
		}
		f.storageStock[itemID] -= qty
		f.calls = append(f.calls, fmt.Sprintf("send_gift:%v", payload))
		f.giftCalls = append(f.giftCalls, payload)
		return nil
	}
	f.calls = append(f.calls, fmt.Sprintf("send_gift:%v", payload))
	f.giftCalls = append(f.giftCalls, payload)
	f.stageDrain(itemID, qty)
	return nil
}

// GetRawJSON serves "storage" as a view_storage frame built from storageStock
// at the docked base (nil when storageStock is nil, i.e. no storage modelled),
// deferring every other key to the embedded fakeClient.
func (f *deliverFakeClient) GetRawJSON(key string) []byte {
	if key != "storage" || f.storageStock == nil {
		return f.fakeClient.GetRawJSON(key)
	}
	type item struct {
		ItemID   string  `json:"item_id"`
		Quantity float64 `json:"quantity"`
	}
	frame := struct {
		BaseID string `json:"base_id"`
		Items  []item `json:"items"`
	}{BaseID: f.state.Player.DockedAtBase}
	for id, q := range f.storageStock {
		if q > 0 {
			frame.Items = append(frame.Items, item{id, q})
		}
	}
	raw, _ := json.Marshal(frame)
	return raw
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
	kb, err := knowledge.NewSQLiteKB(knowledge.Config{DBPath: knowledgetest.Path(t)})
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

// TestDeliverFactionDepositsToFactionStorage: recipient "faction" deposits
// into the faction's storage at TO (a player station with no personal
// storage yet) — no credentials lookup, no SendGift, no personal deposit.
func TestDeliverFactionDepositsToFactionStorage(t *testing.T) {
	kb := newDeliverTestKB(t)
	client := &deliverFakeClient{
		fakeClient:   &fakeClient{state: &game.State{Ship: game.Ship{CargoCapacity: 100}}},
		storageStock: map[string]float64{"steel_plate": 10},
	}
	d := NewWorkerDispatch(client, kb, nil, io.Discard)
	d.AgentID = "hauler-0"
	d.AgentsDir = t.TempDir() // no credentials.json present; must never be read

	if err := d.Deliver(context.Background(), "steel_plate", 4, "from_base", "to_base", "faction"); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if len(client.factionDepositCalls) != 1 || client.factionDepositCalls[0].itemID != "steel_plate" || client.factionDepositCalls[0].quantity != 4 {
		t.Fatalf("FactionDepositItems calls = %+v, want one call of steel_plate x4", client.factionDepositCalls)
	}
	if len(client.giftCalls) != 0 || len(client.depositCalls) != 0 {
		t.Fatalf("faction recipient must not gift or personal-deposit: gifts %+v deposits %+v", client.giftCalls, client.depositCalls)
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

// TestDeliverAtomicShortWithdrawTakesWhatIsThere: the live server refuses a
// withdraw larger than the stock outright ("Storage only has 1222 x
// pirate_moonshine") and moves nothing. Deliver must retry for the stock the
// refusal names and deliver it, not report the source empty.
func TestDeliverAtomicShortWithdrawTakesWhatIsThere(t *testing.T) {
	kb := newDeliverTestKB(t)
	agentsDir := t.TempDir()
	writeDeliverCreds(t, agentsDir, "marketbot_treasure_cache", "Cache Keeper")

	client := &deliverFakeClient{
		fakeClient:     &fakeClient{state: &game.State{Ship: game.Ship{CargoCapacity: 1900}}},
		storageStock:   map[string]float64{"pirate_moonshine": 1222},
		atomicWithdraw: true,
	}
	d := NewWorkerDispatch(client, kb, nil, io.Discard)
	d.AgentID = "trader-9"
	d.AgentsDir = agentsDir

	if err := d.Deliver(context.Background(), "pirate_moonshine", 1900, "from_base", "to_base", "marketbot_treasure_cache"); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if len(client.giftCalls) != 1 {
		t.Fatalf("SendGift calls = %d, want 1 (%+v)", len(client.giftCalls), client.giftCalls)
	}
	if got := client.giftCalls[0]["quantity"]; got != float64(1222) {
		t.Fatalf("gift quantity = %v, want 1222", got)
	}
	if got := client.storageStock["pirate_moonshine"]; got != 0 {
		t.Fatalf("source storage left = %v, want 0", got)
	}
}

// TestDeliverBulkyItemWithdrawFitsTheHold: quantum_fragments take 3 cargo
// space per unit, so a 1,900-unit withdraw into an empty 1,900 hold is
// refused outright ("Need 5700 but only 1900 available") and moves nothing.
// Live 2026-11-23: four Congregations delivered 0 of ~30k each, every hour.
// Deliver must size the withdraw from the refusal and move what fits.
func TestDeliverBulkyItemWithdrawFitsTheHold(t *testing.T) {
	kb := newDeliverTestKB(t)
	agentsDir := t.TempDir()
	writeDeliverCreds(t, agentsDir, "craftsman-3", "Artisan 'Ace' Anderson")

	client := &deliverFakeClient{
		fakeClient:   &fakeClient{state: &game.State{Ship: game.Ship{CargoCapacity: 1900}}},
		storageStock: map[string]float64{"quantum_fragments": 1000},
		itemSize:     map[string]int{"quantum_fragments": 3},
	}
	d := NewWorkerDispatch(client, kb, nil, io.Discard)
	d.AgentID = "trucker-126"
	d.AgentsDir = agentsDir

	if err := d.Deliver(context.Background(), "quantum_fragments", 1000, "from_base", "to_base", "craftsman-3"); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	var gifted float64
	for _, g := range client.giftCalls {
		q, _ := g["quantity"].(float64)
		if q*3 > 1900 {
			t.Fatalf("gift of %v units exceeds the hold (%v)", q, client.giftCalls)
		}
		gifted += q
	}
	if gifted != 1000 {
		t.Fatalf("gifted %v, want 1000 (%+v)", gifted, client.giftCalls)
	}
	if got := client.storageStock["quantum_fragments"]; got != 0 {
		t.Fatalf("source storage left = %v, want 0", got)
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

// TestDeliverZeroDeliveredErrorsForReplan (I2b): when a withdraw pass yields
// nothing and NOTHING has been delivered yet, Deliver must return a
// replan-flavored error — not nil — so the plan runner (task_failed) retries
// or parks instead of marking the node done with DoneQty=0 and letting a
// downstream craft fail blaming the wrong node. A partial delivery (>0) still
// returns nil.
func TestDeliverZeroDeliveredErrorsForReplan(t *testing.T) {
	kb := newDeliverTestKB(t)
	agentsDir := t.TempDir()
	writeDeliverCreds(t, agentsDir, "craftsman-3", "Artisan 'Ace' Anderson")

	client := &deliverFakeClient{
		fakeClient:   &fakeClient{state: &game.State{Ship: game.Ship{CargoCapacity: 100}}},
		storageStock: map[string]float64{"pressure_seal": 0}, // source empty
	}
	d := NewWorkerDispatch(client, kb, nil, io.Discard)
	d.AgentID = "craftsman-2"
	d.AgentsDir = agentsDir

	err := d.Deliver(context.Background(), "pressure_seal", 5, "from_base", "to_base", "craftsman-3")
	if err == nil {
		t.Fatal("expected an error when nothing could be delivered")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "replan") {
		t.Fatalf("error = %q, want a replan-flavored message", err.Error())
	}
	if len(client.giftCalls) != 0 {
		t.Fatalf("SendGift must not be called when nothing was delivered, got %+v", client.giftCalls)
	}
}

// TestDeliverExhaustedLogDistinguishesCargoFullFromSourceExhausted covers the
// "exhausted" log message Deliver emits when a withdraw pass yields nothing.
// That can happen for two very different reasons — the source truly had
// nothing left, or the ship's cargo hold had zero free space for an
// unrelated reason — and the wording must say which. Since I2b, a zero-delivery
// pass returns a replan error (not nil), but the distinguishing log line is
// still emitted first.
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

			// Since I2b a zero-delivery pass returns a replan error; the log
			// line under test is still written before the return.
			if err := d.Deliver(context.Background(), "pressure_seal", 5, "from_base", "to_base", "craftsman-3"); err == nil {
				t.Fatal("expected a replan error when nothing could be delivered")
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

// TestDeliverEmptyFromSkipsWithdrawAndTravelsStraightToDestination is the
// regression test for MineQty's hand-off mode: from == "" means the goods
// are already in cargo (just mined), so Deliver must skip BOTH the withdraw
// leg and the FROM travel leg entirely — no WithdrawItems call, no "dock" at
// a source — and go straight to TO.
func TestDeliverEmptyFromSkipsWithdrawAndTravelsStraightToDestination(t *testing.T) {
	kb := newDeliverTestKB(t)
	agentsDir := t.TempDir()
	writeDeliverCreds(t, agentsDir, "craftsman-3", "Artisan 'Ace' Anderson")

	client := &deliverFakeClient{
		fakeClient: &fakeClient{state: &game.State{Ship: game.Ship{
			CargoCapacity: 100,
			Cargo:         []game.CargoItem{{ItemID: "raw_ore", Quantity: 6}},
		}}},
		// Source storage empty/unused — proves the withdraw leg never runs.
		storageStock: map[string]float64{},
	}
	d := NewWorkerDispatch(client, kb, nil, io.Discard)
	d.AgentID = "craftsman-2"
	d.AgentsDir = agentsDir

	if err := d.Deliver(context.Background(), "raw_ore", 6, "", "to_base", "craftsman-3"); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	for _, c := range client.calls {
		if strings.HasPrefix(c, "withdraw:") {
			t.Fatalf("Deliver must not withdraw when from is empty, got call %q", c)
		}
	}
	if len(client.giftCalls) != 1 {
		t.Fatalf("SendGift calls = %d, want 1 (%+v)", len(client.giftCalls), client.giftCalls)
	}
	if got := client.giftCalls[0]["quantity"]; got != float64(6) {
		t.Fatalf("gift quantity = %v, want 6", got)
	}
}

// TestDeliverEmptyFromShortDeliversWhatsAboard covers the shortfall path: with
// from == "", cargo holds less than qty and there is no source to make a
// second withdraw pass against, so Deliver must deliver what's on hand and
// return nil (never loop forever waiting for cargo that will never arrive).
func TestDeliverEmptyFromShortDeliversWhatsAboard(t *testing.T) {
	kb := newDeliverTestKB(t)
	agentsDir := t.TempDir()
	writeDeliverCreds(t, agentsDir, "craftsman-3", "Artisan 'Ace' Anderson")

	client := &deliverFakeClient{
		fakeClient: &fakeClient{state: &game.State{Ship: game.Ship{
			CargoCapacity: 100,
			Cargo:         []game.CargoItem{{ItemID: "raw_ore", Quantity: 3}},
		}}},
		storageStock: map[string]float64{},
	}
	d := NewWorkerDispatch(client, kb, nil, io.Discard)
	d.AgentID = "craftsman-2"
	d.AgentsDir = agentsDir

	if err := d.Deliver(context.Background(), "raw_ore", 6, "", "to_base", "craftsman-3"); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if len(client.giftCalls) != 1 {
		t.Fatalf("SendGift calls = %d, want 1 (%+v)", len(client.giftCalls), client.giftCalls)
	}
	if got := client.giftCalls[0]["quantity"]; got != float64(3) {
		t.Fatalf("gift quantity = %v, want 3 (what was aboard)", got)
	}
}

// TestDeliverEmptyFromNothingAboardErrorsForReplan covers the degenerate case:
// from == "" and cargo has none of the item at all — Deliver must not travel to
// TO or gift/deposit anything. Since I2b, delivering zero of a positive qty is
// a replan error, not a silent no-op (a mine that yielded nothing needs a
// re-plan, not a fake-done).
func TestDeliverEmptyFromNothingAboardErrorsForReplan(t *testing.T) {
	kb := newDeliverTestKB(t)
	agentsDir := t.TempDir()
	writeDeliverCreds(t, agentsDir, "craftsman-3", "Artisan 'Ace' Anderson")

	client := &deliverFakeClient{
		fakeClient:   &fakeClient{state: &game.State{Ship: game.Ship{CargoCapacity: 100}}},
		storageStock: map[string]float64{},
	}
	d := NewWorkerDispatch(client, kb, nil, io.Discard)
	d.AgentID = "craftsman-2"
	d.AgentsDir = agentsDir

	err := d.Deliver(context.Background(), "raw_ore", 6, "", "to_base", "craftsman-3")
	if err == nil {
		t.Fatal("expected a replan error when nothing is aboard to deliver")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "replan") {
		t.Fatalf("error = %q, want a replan-flavored message", err.Error())
	}
	if len(client.giftCalls) != 0 {
		t.Fatalf("SendGift must not be called when nothing is aboard, got %+v", client.giftCalls)
	}
	for _, c := range client.calls {
		if c == "dock" {
			t.Fatalf("Deliver must not travel to destination when nothing is aboard to deliver, got call %q", c)
		}
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

// TestDeliverAlreadyDockedAtSourceStillDelivers: a resident hauler scheduled to
// deliver FROM the station it is pinned to is already docked there, and the
// server answers its dock with "Already docked". That is the desired
// postcondition, not a failure — live 2026-10-25 trader-9 failed every hourly
// moonshine backhaul from Voss for seven passes on exactly this refusal while
// 572 units waited in its storage.
func TestDeliverAlreadyDockedAtSourceStillDelivers(t *testing.T) {
	kb := newDeliverTestKB(t)
	agentsDir := t.TempDir()
	writeDeliverCreds(t, agentsDir, "craftsman-3", "Artisan 'Ace' Anderson")

	client := &deliverFakeClient{
		fakeClient:   &fakeClient{state: &game.State{Ship: game.Ship{CargoCapacity: 100}}, dockErr: errors.New("action_error: Already docked at from_base")},
		storageStock: map[string]float64{"pressure_seal": 10},
	}
	d := NewWorkerDispatch(client, kb, nil, io.Discard)
	d.AgentID = "craftsman-2"
	d.AgentsDir = agentsDir

	if err := d.Deliver(context.Background(), "pressure_seal", 5, "from_base", "to_base", "craftsman-3"); err != nil {
		t.Fatalf("Deliver with an Already-docked source refusal must succeed, got: %v", err)
	}
	if len(client.giftCalls) != 1 {
		t.Fatalf("SendGift calls = %d, want 1 (%+v)", len(client.giftCalls), client.giftCalls)
	}
}

// A "faction:" source withdraws from the faction's storage at FROM instead of
// the worker's own: CJ's liquid_hydrogen to Beid's fuel combustor (2026-11-20,
// Beid's bunker at 0/5000 with no hydrogen to burn).
func TestDeliverFromFactionStorage(t *testing.T) {
	kb := newDeliverTestKB(t)
	client := &deliverFakeClient{
		fakeClient:   &fakeClient{state: &game.State{Ship: game.Ship{CargoCapacity: 100}}},
		storageStock: map[string]float64{},
		factionStock: map[string]float64{"liquid_hydrogen": 500},
	}
	d := NewWorkerDispatch(client, kb, nil, io.Discard)
	d.AgentID = "craftsman-2"
	d.AgentsDir = t.TempDir()

	if err := d.Deliver(context.Background(), "liquid_hydrogen", 150, "faction:from_base", "to_base", "faction"); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	total := 0.0
	for _, c := range client.factionDepositCalls {
		total += c.quantity
	}
	if total != 150 {
		t.Fatalf("faction deposits = %+v, want 150 liquid_hydrogen in total (two trips of a 100 hold)", client.factionDepositCalls)
	}
	for _, c := range client.calls {
		if strings.HasPrefix(c, "withdraw:") {
			t.Fatalf("personal storage withdraw on a faction source: %v", client.calls)
		}
	}
	if client.factionStock["liquid_hydrogen"] != 350 {
		t.Fatalf("faction stock left = %v, want 350", client.factionStock["liquid_hydrogen"])
	}
}

// After a hull switch the cached capacity is the OLD ship's. Deliver must size
// its first withdraw from a fresh get_cargo, not the stale cache, or a
// 1,900-cargo Congregation leaves with the old hull's 100 (live 2026-11-21:
// craftsman-5 carried 100 of 1,000 purified_water to Voss).
func TestDeliverSizesWithdrawFromFreshCapacity(t *testing.T) {
	kb := newDeliverTestKB(t)
	client := &deliverFakeClient{
		fakeClient:   &fakeClient{state: &game.State{Ship: game.Ship{CargoCapacity: 100}}},
		storageStock: map[string]float64{},
		factionStock: map[string]float64{"purified_water": 5000},
		liveCapacity: 1900,
	}
	d := NewWorkerDispatch(client, kb, nil, io.Discard)
	d.AgentID = "craftsman-5"
	d.AgentsDir = t.TempDir()

	if err := d.Deliver(context.Background(), "purified_water", 1000, "faction:from_base", "to_base", "faction"); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	var withdraws []string
	for _, c := range client.calls {
		if strings.HasPrefix(c, "faction_withdraw:") {
			withdraws = append(withdraws, c)
		}
	}
	if len(withdraws) != 1 || withdraws[0] != "faction_withdraw:purified_water:1000" {
		t.Fatalf("faction withdraws = %v, want one withdraw of 1000 sized from the live 1900 hold", withdraws)
	}
	if len(client.factionDepositCalls) != 1 || client.factionDepositCalls[0].quantity != 1000 {
		t.Fatalf("deposits = %+v, want a single trip of 1000", client.factionDepositCalls)
	}
}

// A scheduled "deliver ITEM QTY cargo TO RECIPIENT" has no way to pass an
// empty FROM (tasks split on spaces), so the literal "cargo" selects the
// from-cargo mode: no withdraw, straight to TO. Live 2026-11-21: craftsman-5
// broke off a 28-jump Nyx run holding the water and would otherwise fly back
// to CJ to reload what it already carried.
func TestDeliverCargoKeywordSkipsWithdraw(t *testing.T) {
	kb := newDeliverTestKB(t)
	agentsDir := t.TempDir()
	writeDeliverCreds(t, agentsDir, "craftsman-3", "Artisan 'Ace' Anderson")
	client := &deliverFakeClient{
		fakeClient: &fakeClient{state: &game.State{Ship: game.Ship{
			CargoCapacity: 100,
			Cargo:         []game.CargoItem{{ItemID: "raw_ore", Quantity: 6}},
		}}},
		storageStock: map[string]float64{"raw_ore": 50},
	}
	d := NewWorkerDispatch(client, kb, nil, io.Discard)
	d.AgentID = "craftsman-2"
	d.AgentsDir = agentsDir

	if err := d.Run(context.Background(), []string{"deliver", "raw_ore", "6", "cargo", "to_base", "craftsman-3"}); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	for _, c := range client.calls {
		if strings.HasPrefix(c, "withdraw:") || strings.HasPrefix(c, "faction_withdraw:") {
			t.Fatalf("FROM cargo must not withdraw, got call %q", c)
		}
	}
	if len(client.giftCalls) != 1 || client.giftCalls[0]["quantity"] != float64(6) {
		t.Fatalf("gifts = %+v, want one gift of 6", client.giftCalls)
	}
}

// Live 2026-11-22 (trucker-126): a disconnect mid-jump killed a deliver 19 jumps
// into the leg to TO with a full hold of energy_crystal. The next hourly run saw
// 1,900 aboard against 200,000 wanted and flew back to FROM to withdraw more --
// into a hold with no room -- before ever reaching TO. A full hold of the item
// must go to TO first; the withdraw leg comes after it is emptied.
func TestDeliverFullHoldOfItemDeliversBeforeWithdrawing(t *testing.T) {
	kb := newDeliverTestKB(t)
	client := &deliverFakeClient{
		fakeClient: &fakeClient{state: &game.State{Ship: game.Ship{
			CargoCapacity: 100,
			CargoUsed:     100,
			Cargo:         []game.CargoItem{{ItemID: "raw_ore", Quantity: 100}},
		}}},
		storageStock: map[string]float64{"raw_ore": 30},
	}
	d := NewWorkerDispatch(client, kb, nil, io.Discard)
	d.AgentID = "craftsman-2"

	if err := d.Deliver(context.Background(), "raw_ore", 500, "from_base", "to_base", RecipientFaction); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	first := ""
	for _, c := range client.calls {
		if strings.HasPrefix(c, "withdraw:") || strings.HasPrefix(c, "faction_deposit:") {
			first = c
			break
		}
	}
	if first != "faction_deposit:raw_ore:100" {
		t.Fatalf("first transfer = %q, want the 100 aboard deposited at TO before any withdraw (calls %v)", first, client.calls)
	}
}
