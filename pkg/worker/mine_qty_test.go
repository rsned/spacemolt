package worker

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// mineFakeClient wraps deliverFakeClient (deliver_test.go) with mining
// simulation: each Mine(ctx) call consumes the next entry of mineYields (0
// once exhausted) and, mirroring the LIVE client's TypeMiningYield handler
// (pkg/game/client.go: mining_yield pushes update state.Ship.Cargo directly,
// no get_cargo round trip required — verified 2026-07-10), adds it straight
// to state.Ship.Cargo/CargoUsed with no separate refresh call needed.
// If yieldItemID is set, yields go to that item instead of mineItemID
// (used to test cross-item isolation: the belt yields something OTHER than
// what we're mining for).
type mineFakeClient struct {
	*deliverFakeClient
	mineItemID  string
	yieldItemID string // if non-empty, yield to this item instead of mineItemID
	mineYields  []float64
	mineCalls   int
}

func (f *mineFakeClient) Mine(ctx context.Context) error {
	f.calls = append(f.calls, "mine")
	var yield float64
	if f.mineCalls < len(f.mineYields) {
		yield = f.mineYields[f.mineCalls]
	}
	f.mineCalls++
	if yield <= 0 || f.state == nil {
		return nil
	}
	yieldTo := f.mineItemID
	if f.yieldItemID != "" {
		yieldTo = f.yieldItemID
	}
	found := false
	for i := range f.state.Ship.Cargo {
		if f.state.Ship.Cargo[i].ItemID == yieldTo {
			f.state.Ship.Cargo[i].Quantity += yield
			found = true
		}
	}
	if !found {
		f.state.Ship.Cargo = append(f.state.Ship.Cargo, game.CargoItem{ItemID: yieldTo, Quantity: yield})
	}
	f.state.Ship.CargoUsed += yield
	return nil
}

// newMineTestKB builds an in-memory SQLiteKB seeded with:
//   - system "mine_sys" containing POI "ore_belt" (type asteroid_belt) that
//     carries a poi_resources row for resourceID
//   - the "to_base"/"to_poi"/"to_sys" delivery destination (via
//     newDeliverTestKB, reused across the package's deliver-adjacent tests)
func newMineTestKB(t *testing.T, resourceID string) *knowledge.SQLiteKB {
	t.Helper()
	kb := newDeliverTestKB(t)
	ctx := context.Background()
	if err := kb.RememberSystem(ctx, knowledge.System{ID: "mine_sys", Name: "Mine Sys", LastUpdatedTick: 1}); err != nil {
		t.Fatalf("RememberSystem(mine_sys): %v", err)
	}
	if err := kb.RememberPOI(ctx, knowledge.POI{
		ID: "ore_belt", SystemID: "mine_sys", Name: "Ore Belt", Type: "asteroid_belt",
		Resources:       []game.POIResource{{ResourceID: resourceID, Richness: 50, Remaining: 5000}},
		LastUpdatedTick: 1,
	}); err != nil {
		t.Fatalf("RememberPOI(ore_belt): %v", err)
	}
	return kb
}

// noWaitMineDispatch builds a WorkerDispatch over client/kb with the between-
// pass mine sleep stubbed to a zero-delay no-op, mirroring craft_node_test's
// craftPollSleep override so the suite doesn't burn real wall-clock time on
// game.SleepTick waits between simulated mine passes.
func noWaitMineDispatch(client game.GameClient, kb knowledge.Base, agentsDir string) *WorkerDispatch {
	d := NewWorkerDispatch(client, kb, nil, io.Discard)
	d.AgentID = "craftsman-2"
	d.AgentsDir = agentsDir
	d.minePollSleep = func(ctx context.Context, dur time.Duration) error { return nil }
	return d
}

// TestMineQtyMinesUntilQtyThenDelivers: the belt yields across two passes
// (3, then 4 — 7 total), qty is 7, so MineQty must mine exactly two passes,
// then gift the full 7 at the destination via Deliver's empty-FROM mode.
func TestMineQtyMinesUntilQtyThenDelivers(t *testing.T) {
	agentsDir := t.TempDir()
	writeDeliverCreds(t, agentsDir, "craftsman-3", "Artisan 'Ace' Anderson")
	kb := newMineTestKB(t, "raw_ore")

	client := &mineFakeClient{
		deliverFakeClient: &deliverFakeClient{
			fakeClient: &fakeClient{state: &game.State{
				System: game.SystemData{ID: "mine_sys"},
				Ship:   game.Ship{CargoCapacity: 100},
			}},
			storageStock: map[string]float64{},
		},
		mineItemID: "raw_ore",
		mineYields: []float64{3, 4},
	}
	d := noWaitMineDispatch(client, kb, agentsDir)

	if err := d.MineQty(context.Background(), "raw_ore", 7, "to_base", "craftsman-3"); err != nil {
		t.Fatalf("MineQty: %v", err)
	}

	mineCalls := 0
	for _, c := range client.calls {
		if c == "mine" {
			mineCalls++
		}
	}
	if mineCalls != 2 {
		t.Fatalf("mine calls = %d, want 2", mineCalls)
	}
	if len(client.giftCalls) != 1 {
		t.Fatalf("SendGift calls = %d, want 1 (%+v)", len(client.giftCalls), client.giftCalls)
	}
	if got := client.giftCalls[0]["quantity"]; got != float64(7) {
		t.Fatalf("gift quantity = %v, want 7", got)
	}
	if got := client.giftCalls[0]["item_id"]; got != "raw_ore" {
		t.Fatalf("gift item_id = %v, want raw_ore", got)
	}
}

// TestMineQtyDryBeltStopsAndDeliversPartial: the belt yields once (3 units)
// then goes dry. After MineQtyMaxDryPasses consecutive no-growth passes,
// MineQty must stop mining, deliver the 3 units it has (partial success is
// success), and return nil — never mine forever chasing a dead belt.
func TestMineQtyDryBeltStopsAndDeliversPartial(t *testing.T) {
	agentsDir := t.TempDir()
	writeDeliverCreds(t, agentsDir, "craftsman-3", "Artisan 'Ace' Anderson")
	kb := newMineTestKB(t, "raw_ore")

	yields := make([]float64, 0, 1+MineQtyMaxDryPasses+2)
	yields = append(yields, 3)
	for range MineQtyMaxDryPasses + 2 { // pad past the dry threshold to catch an off-by-one
		yields = append(yields, 0)
	}
	client := &mineFakeClient{
		deliverFakeClient: &deliverFakeClient{
			fakeClient: &fakeClient{state: &game.State{
				System: game.SystemData{ID: "mine_sys"},
				Ship:   game.Ship{CargoCapacity: 100},
			}},
			storageStock: map[string]float64{},
		},
		mineItemID: "raw_ore",
		mineYields: yields,
	}
	d := noWaitMineDispatch(client, kb, agentsDir)

	if err := d.MineQty(context.Background(), "raw_ore", 100, "to_base", "craftsman-3"); err != nil {
		t.Fatalf("MineQty: %v", err)
	}

	mineCalls := 0
	for _, c := range client.calls {
		if c == "mine" {
			mineCalls++
		}
	}
	if want := 1 + MineQtyMaxDryPasses; mineCalls != want {
		t.Fatalf("mine calls = %d, want %d (1 successful pass + %d dry passes before stopping)", mineCalls, want, MineQtyMaxDryPasses)
	}
	if len(client.giftCalls) != 1 {
		t.Fatalf("SendGift calls = %d, want 1 (%+v)", len(client.giftCalls), client.giftCalls)
	}
	if got := client.giftCalls[0]["quantity"]; got != float64(3) {
		t.Fatalf("gift quantity = %v, want 3 (delivered what was aboard, short of the requested 100)", got)
	}
}

// TestMineQtyDryCounterIgnoresOtherItemYields: the dry-pass counter in mineLoop
// tracks the TARGET item's cargo count, not total cargo. This test verifies that
// when the belt yields a DIFFERENT item on every pass (while the target item
// never grows), the verb still stops after MineQtyMaxDryPasses passes and
// delivers whatever target quantity it had (e.g., 0 for a clean no-op). This
// guards against a mutation that tracks total cargo instead of per-item count.
func TestMineQtyDryCounterIgnoresOtherItemYields(t *testing.T) {
	agentsDir := t.TempDir()
	writeDeliverCreds(t, agentsDir, "craftsman-3", "Artisan 'Ace' Anderson")
	kb := newMineTestKB(t, "raw_ore")

	// Start with 1 unit of raw_ore in cargo. Belt yields DIFFERENT item (other_ore)
	// on every pass — raw_ore never grows. Counter must stop at MineQtyMaxDryPasses.
	yields := make([]float64, 0, MineQtyMaxDryPasses+2)
	for range MineQtyMaxDryPasses + 2 {
		yields = append(yields, 5) // yields 5 units each pass, but to "other_ore", not "raw_ore"
	}
	client := &mineFakeClient{
		deliverFakeClient: &deliverFakeClient{
			fakeClient: &fakeClient{state: &game.State{
				System: game.SystemData{ID: "mine_sys"},
				Ship: game.Ship{
					CargoCapacity: 100,
					Cargo:         []game.CargoItem{{ItemID: "raw_ore", Quantity: 1}},
					CargoUsed:     1,
				},
			}},
			storageStock: map[string]float64{},
		},
		mineItemID:  "raw_ore", // mining for this
		yieldItemID: "other_ore", // but belt yields this instead
		mineYields:  yields,
	}
	d := noWaitMineDispatch(client, kb, agentsDir)

	if err := d.MineQty(context.Background(), "raw_ore", 100, "to_base", "craftsman-3"); err != nil {
		t.Fatalf("MineQty: %v", err)
	}

	mineCalls := 0
	for _, c := range client.calls {
		if c == "mine" {
			mineCalls++
		}
	}
	if want := MineQtyMaxDryPasses; mineCalls != want {
		t.Fatalf("mine calls = %d, want %d (must stop after %d dry passes with target item not growing)",
			mineCalls, want, MineQtyMaxDryPasses)
	}
	if len(client.giftCalls) != 1 {
		t.Fatalf("SendGift calls = %d, want 1", len(client.giftCalls))
	}
	// Should deliver 1 unit of raw_ore (what it started with; didn't grow due to belt yielding other_ore)
	if got := client.giftCalls[0]["quantity"]; got != float64(1) {
		t.Fatalf("gift quantity = %v, want 1 (target item didn't grow despite belt yielding other_ore)",
			got)
	}
	if got := client.giftCalls[0]["item_id"]; got != "raw_ore" {
		t.Fatalf("gift item_id = %v, want raw_ore", got)
	}
}

// TestMineQtyStopsWhenCargoFull: the belt keeps yielding, but the hold has no
// room after the first pass — MineQty must stop mining (not spin forever
// trying to mine into a full hold) and deliver what's aboard.
func TestMineQtyStopsWhenCargoFull(t *testing.T) {
	agentsDir := t.TempDir()
	writeDeliverCreds(t, agentsDir, "craftsman-3", "Artisan 'Ace' Anderson")
	kb := newMineTestKB(t, "raw_ore")

	client := &mineFakeClient{
		deliverFakeClient: &deliverFakeClient{
			fakeClient: &fakeClient{state: &game.State{
				System: game.SystemData{ID: "mine_sys"},
				Ship:   game.Ship{CargoCapacity: 5}, // full after one 5-unit pass
			}},
			storageStock: map[string]float64{},
		},
		mineItemID: "raw_ore",
		mineYields: []float64{5, 5, 5}, // would keep yielding if allowed to keep mining
	}
	d := noWaitMineDispatch(client, kb, agentsDir)

	if err := d.MineQty(context.Background(), "raw_ore", 50, "to_base", "craftsman-3"); err != nil {
		t.Fatalf("MineQty: %v", err)
	}

	mineCalls := 0
	for _, c := range client.calls {
		if c == "mine" {
			mineCalls++
		}
	}
	if mineCalls != 1 {
		t.Fatalf("mine calls = %d, want 1 (must stop once cargo is full)", mineCalls)
	}
	if len(client.giftCalls) != 1 || client.giftCalls[0]["quantity"] != float64(5) {
		t.Fatalf("SendGift calls = %+v, want one gift of 5 (what fit before the hold filled)", client.giftCalls)
	}
}

// TestMineQtyUndocksBeforeMining: mining requires being undocked. If the
// worker is still docked after autopiloting to the resource POI (e.g. a
// prior task left it docked in this system), MineQty must undock before its
// first Mine() call.
func TestMineQtyUndocksBeforeMining(t *testing.T) {
	agentsDir := t.TempDir()
	writeDeliverCreds(t, agentsDir, "craftsman-3", "Artisan 'Ace' Anderson")
	kb := newMineTestKB(t, "raw_ore")

	client := &mineFakeClient{
		deliverFakeClient: &deliverFakeClient{
			fakeClient: &fakeClient{state: &game.State{
				System: game.SystemData{ID: "mine_sys"},
				Ship:   game.Ship{CargoCapacity: 100},
				Doc:    true, // already docked somewhere in mine_sys
			}},
			storageStock: map[string]float64{},
		},
		mineItemID: "raw_ore",
		mineYields: []float64{5},
	}
	d := noWaitMineDispatch(client, kb, agentsDir)

	if err := d.MineQty(context.Background(), "raw_ore", 5, "to_base", "craftsman-3"); err != nil {
		t.Fatalf("MineQty: %v", err)
	}

	undockIdx, mineIdx := -1, -1
	for i, c := range client.calls {
		if c == "undock" && undockIdx == -1 {
			undockIdx = i
		}
		if c == "mine" && mineIdx == -1 {
			mineIdx = i
		}
	}
	if undockIdx == -1 {
		t.Fatalf("expected an undock call, got %+v", client.calls)
	}
	if mineIdx == -1 || undockIdx > mineIdx {
		t.Fatalf("undock must happen before the first mine call, got calls %+v", client.calls)
	}
}

// TestMineQtyBadRecipientSurfacesViaFinalDeliver verifies that a recipient
// whose credentials.json is missing causes MineQty to fail. Mining and travel
// to the resource POI happen first (which is expected — the ore is real
// regardless of a bad recipient), but the error surfaces via the trailing
// Deliver call at the end of MineQty when it tries to resolve the recipient.
func TestMineQtyBadRecipientSurfacesViaFinalDeliver(t *testing.T) {
	agentsDir := t.TempDir() // no credentials.json for "craftsman-missing"
	kb := newMineTestKB(t, "raw_ore")

	client := &mineFakeClient{
		deliverFakeClient: &deliverFakeClient{
			fakeClient: &fakeClient{state: &game.State{
				System: game.SystemData{ID: "mine_sys"},
				Ship:   game.Ship{CargoCapacity: 100},
			}},
			storageStock: map[string]float64{},
		},
		mineItemID: "raw_ore",
		mineYields: []float64{5},
	}
	d := noWaitMineDispatch(client, kb, agentsDir)

	err := d.MineQty(context.Background(), "raw_ore", 5, "to_base", "craftsman-missing")
	if err == nil {
		t.Fatal("expected error for a recipient with no resolvable credentials")
	}
	if !strings.Contains(err.Error(), "craftsman-missing") {
		t.Fatalf("error = %q, want it to mention the recipient %q", err.Error(), "craftsman-missing")
	}
}

// TestFindMinePOIPrimaryPathUsesPOIResourcesMatch verifies findMinePOI's
// primary lookup: a POI carrying a poi_resources row for itemID is found via
// galaxy.FindNearestByResource, even in the worker's current system.
func TestFindMinePOIPrimaryPathUsesPOIResourcesMatch(t *testing.T) {
	kb := newMineTestKB(t, "raw_ore")
	d := NewWorkerDispatch(&fakeClient{}, kb, nil, io.Discard)

	sys, poi, err := d.findMinePOI(context.Background(), "mine_sys", "raw_ore")
	if err != nil {
		t.Fatalf("findMinePOI: %v", err)
	}
	if sys != "mine_sys" || poi != "ore_belt" {
		t.Fatalf("findMinePOI = (%q, %q), want (mine_sys, ore_belt)", sys, poi)
	}
}

// TestFindMinePOIFallsBackToResourcePOIType covers the fallback path: no
// poi_resources row anywhere names itemID (never surveyed), but a POI of a
// known resource-bearing type exists — findMinePOI must fall back to
// galaxy.FindNearestByPOIType and resolve that POI instead of erroring.
func TestFindMinePOIFallsBackToResourcePOIType(t *testing.T) {
	kb := newDeliverTestKB(t)
	ctx := context.Background()
	if err := kb.RememberSystem(ctx, knowledge.System{ID: "gas_sys", Name: "Gas Sys", LastUpdatedTick: 1}); err != nil {
		t.Fatalf("RememberSystem: %v", err)
	}
	// A gas_cloud POI with no poi_resources rows at all — the KB has never
	// surveyed what it actually contains.
	if err := kb.RememberPOI(ctx, knowledge.POI{
		ID: "unsurveyed_cloud", SystemID: "gas_sys", Name: "Unsurveyed Cloud", Type: "gas_cloud",
		LastUpdatedTick: 1,
	}); err != nil {
		t.Fatalf("RememberPOI: %v", err)
	}
	d := NewWorkerDispatch(&fakeClient{}, kb, nil, io.Discard)

	sys, poi, err := d.findMinePOI(ctx, "gas_sys", "unknown_gas")
	if err != nil {
		t.Fatalf("findMinePOI: %v", err)
	}
	if sys != "gas_sys" || poi != "unsurveyed_cloud" {
		t.Fatalf("findMinePOI = (%q, %q), want (gas_sys, unsurveyed_cloud)", sys, poi)
	}
}

// TestFindMinePOINoKnownResourceErrors: neither the exact resource nor any
// resource-bearing POI type is known anywhere in the KB — findMinePOI must
// error rather than return a zero-value location.
func TestFindMinePOINoKnownResourceErrors(t *testing.T) {
	kb := newDeliverTestKB(t) // only station POIs — no resource-bearing types
	ctx := context.Background()
	if err := kb.RememberSystem(ctx, knowledge.System{ID: "from_sys", Name: "From Sys", LastUpdatedTick: 1}); err != nil {
		t.Fatalf("RememberSystem: %v", err)
	}
	d := NewWorkerDispatch(&fakeClient{}, kb, nil, io.Discard)

	if _, _, err := d.findMinePOI(ctx, "from_sys", "nonexistent_ore"); err == nil {
		t.Fatal("expected an error when no resource POI is known anywhere")
	}
}
