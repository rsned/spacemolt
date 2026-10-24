package worker

import (
	"bytes"
	"context"
	"io"
	"slices"
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
	// byproductID, when set, gains byproductYields[i] on the i-th Mine call,
	// alongside the main yield.
	byproductID     string
	byproductYields []float64
}

// addCargo adds qty of itemID to the fake's hold.
func (f *mineFakeClient) addCargo(itemID string, qty float64) {
	for i := range f.state.Ship.Cargo {
		if f.state.Ship.Cargo[i].ItemID == itemID {
			f.state.Ship.Cargo[i].Quantity += qty
			f.state.Ship.CargoUsed += qty
			return
		}
	}
	f.state.Ship.Cargo = append(f.state.Ship.Cargo, game.CargoItem{ItemID: itemID, Quantity: qty})
	f.state.Ship.CargoUsed += qty
}

func (f *mineFakeClient) Mine(ctx context.Context) error {
	f.calls = append(f.calls, "mine")
	var yield float64
	if f.mineCalls < len(f.mineYields) {
		yield = f.mineYields[f.mineCalls]
	}
	var byproduct float64
	if f.mineCalls < len(f.byproductYields) {
		byproduct = f.byproductYields[f.mineCalls]
	}
	f.mineCalls++
	if f.state == nil {
		return nil
	}
	if byproduct > 0 && f.byproductID != "" {
		f.addCargo(f.byproductID, byproduct)
	}
	if yield <= 0 {
		return nil
	}
	yieldTo := f.mineItemID
	if f.yieldItemID != "" {
		yieldTo = f.yieldItemID
	}
	f.addCargo(yieldTo, yield)
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
				Ship:   game.Ship{CargoCapacity: 100, Fuel: 130, MaxFuel: 130},
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
				Ship:   game.Ship{CargoCapacity: 100, Fuel: 130, MaxFuel: 130},
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
					Fuel:          130,
					MaxFuel:       130,
				},
			}},
			storageStock: map[string]float64{},
		},
		mineItemID:  "raw_ore",   // mining for this
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
				Ship:   game.Ship{CargoCapacity: 5, Fuel: 130, MaxFuel: 130}, // full after one 5-unit pass
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
				Ship:   game.Ship{CargoCapacity: 100, Fuel: 130, MaxFuel: 130},
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
				Ship:   game.Ship{CargoCapacity: 100, Fuel: 130, MaxFuel: 130},
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

	sys, poi, err := d.findMinePOI(context.Background(), "mine_sys", "raw_ore", nil)
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

	sys, poi, err := d.findMinePOI(ctx, "gas_sys", "unknown_gas", nil)
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

	if _, _, err := d.findMinePOI(ctx, "from_sys", "nonexistent_ore", nil); err == nil {
		t.Fatal("expected an error when no resource POI is known anywhere")
	}
}

// TestMineQtyRefusesToDepartBelowFuelReserve: a miner that cannot afford the
// trip must not start it. The live failure this pins (2026-07-12) was
// craftsman-10 taking a mine_qty node, flying out on a tank it could not
// return on, freezing undocked at ~5 fuel, absorbing three stall-restarts and
// being quarantined. craftsman-2 then repeated it on the retry.
func TestMineQtyRefusesToDepartBelowFuelReserve(t *testing.T) {
	agentsDir := t.TempDir()
	writeDeliverCreds(t, agentsDir, "craftsman-3", "Artisan 'Ace' Anderson")
	kb := newMineTestKB(t, "raw_ore")

	client := &mineFakeClient{
		deliverFakeClient: &deliverFakeClient{
			fakeClient: &fakeClient{state: &game.State{
				System: game.SystemData{ID: "mine_sys"},
				Ship: game.Ship{
					CargoCapacity: 100,
					Fuel:          MineQtyMinFuelReserve - 1,
					MaxFuel:       130,
				},
			}},
			storageStock: map[string]float64{},
		},
		mineItemID: "raw_ore",
		mineYields: []float64{3, 4},
	}
	d := noWaitMineDispatch(client, kb, agentsDir)

	err := d.MineQty(context.Background(), "raw_ore", 7, "to_base", "craftsman-3")
	if err == nil {
		t.Fatal("MineQty returned nil; departing below the fuel reserve must be refused")
	}
	for _, c := range client.calls {
		if c == "mine" {
			t.Fatalf("MineQty mined despite insufficient fuel (calls: %v)", client.calls)
		}
	}
}

// TestMineQtyDepartsWhenFuelIsAdequate: the guard must not ground a healthy
// miner. Same setup, full tank, ordinary run.
func TestMineQtyDepartsWhenFuelIsAdequate(t *testing.T) {
	agentsDir := t.TempDir()
	writeDeliverCreds(t, agentsDir, "craftsman-3", "Artisan 'Ace' Anderson")
	kb := newMineTestKB(t, "raw_ore")

	client := &mineFakeClient{
		deliverFakeClient: &deliverFakeClient{
			fakeClient: &fakeClient{state: &game.State{
				System: game.SystemData{ID: "mine_sys"},
				Ship:   game.Ship{CargoCapacity: 100, Fuel: 130, MaxFuel: 130},
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
	if mineCalls == 0 {
		t.Error("MineQty did not mine despite a full tank — the guard is too strict")
	}
}

func TestLawlessBlockedRefusesLockedBot(t *testing.T) {
	sys := &knowledge.System{ID: "glenhaven", SecurityStatus: "Lawless (no police protection)", PoliceLevel: 0}
	blocked, reason := lawlessBlocked(sys, false)
	if !blocked || !strings.Contains(reason, "glenhaven") {
		t.Fatalf("want blocked with system in reason, got %v %q", blocked, reason)
	}
}

func TestLawlessBlockedAdmitsUnlockedBot(t *testing.T) {
	sys := &knowledge.System{ID: "glenhaven", SecurityStatus: "Lawless (no police protection)"}
	if blocked, _ := lawlessBlocked(sys, true); blocked {
		t.Fatal("unlocked bot must pass")
	}
}

func TestLawlessBlockedFailsOpenOnUnknownSecurity(t *testing.T) {
	sys := &knowledge.System{ID: "alniyat", SecurityStatus: "", PoliceLevel: 0}
	blocked, reason := lawlessBlocked(sys, false)
	if blocked {
		t.Fatal("unknown security must not block")
	}
	if !strings.Contains(reason, "unknown") {
		t.Fatalf("want a warning reason, got %q", reason)
	}
}

func TestLawlessBlockedIgnoresCase(t *testing.T) {
	if blocked, _ := lawlessBlocked(&knowledge.System{ID: "x", SecurityStatus: "LAWLESS"}, false); !blocked {
		t.Fatal("case-insensitive match")
	}
}

func TestPirateUnlockedFalseWithoutAssets(t *testing.T) {
	d := &WorkerDispatch{AgentID: "miner-1", Out: io.Discard}
	if d.pirateUnlocked(context.Background()) {
		t.Fatal("nil Assets store must read as locked")
	}
}

// newMineAtTestKB seeds a Glenhaven system (with the given security and
// stronghold flag) and its ice belt on top of the mine/deliver fixtures.
func newMineAtTestKB(t *testing.T, security string, stronghold bool) *knowledge.SQLiteKB {
	t.Helper()
	kb := newMineTestKB(t, "raw_ore")
	ctx := context.Background()
	if err := kb.RememberSystem(ctx, knowledge.System{
		ID: "glenhaven", Name: "Glenhaven", SecurityStatus: security, IsStronghold: stronghold, LastUpdatedTick: 1,
	}); err != nil {
		t.Fatalf("RememberSystem(glenhaven): %v", err)
	}
	if err := kb.RememberPOI(ctx, knowledge.POI{
		ID: "glenhaven_ice_belt", SystemID: "glenhaven", Name: "Ice Belt", Type: "asteroid_belt", LastUpdatedTick: 1,
	}); err != nil {
		t.Fatalf("RememberPOI(glenhaven_ice_belt): %v", err)
	}
	return kb
}

func newMineAtClient(fuel float64, yields ...float64) *mineFakeClient {
	return &mineFakeClient{
		deliverFakeClient: &deliverFakeClient{
			fakeClient: &fakeClient{state: &game.State{
				System: game.SystemData{ID: "glenhaven"},
				Ship:   game.Ship{CargoCapacity: 100, Fuel: fuel, MaxFuel: 130},
			}},
			storageStock: map[string]float64{},
		},
		mineItemID: "ice",
		mineYields: yields,
	}
}

func hasCallPrefix(calls []string, prefixes ...string) bool {
	for _, c := range calls {
		for _, p := range prefixes {
			if strings.HasPrefix(c, p) {
				return true
			}
		}
	}
	return false
}

func TestMineQtyAtUsesThePinnedPOI(t *testing.T) {
	agentsDir := t.TempDir()
	writeDeliverCreds(t, agentsDir, "craftsman-3", "Artisan 'Ace' Anderson")
	kb := newMineAtTestKB(t, "", false) // unknown security fails open
	client := newMineAtClient(130, 5)
	d := noWaitMineDispatch(client, kb, agentsDir)
	var out bytes.Buffer
	d.Out = &out

	if err := d.MineQtyAt(context.Background(), "ice", 5, "glenhaven", "glenhaven_ice_belt", "to_base", "craftsman-3"); err != nil {
		t.Fatalf("MineQtyAt: %v", err)
	}
	if !slices.Contains(client.calls, "travel:glenhaven_ice_belt") {
		t.Fatalf("calls %v lack travel to the pinned POI", client.calls)
	}
	if !slices.Contains(client.calls, "mine") {
		t.Fatalf("calls %v lack mine", client.calls)
	}
	if len(client.giftCalls) != 1 || client.giftCalls[0]["quantity"] != float64(5) {
		t.Fatalf("gift calls = %+v, want one gift of 5", client.giftCalls)
	}
	if !strings.Contains(out.String(), "unknown") {
		t.Fatalf("expected unknown-security warning on Out, got %q", out.String())
	}
}

func TestMineQtyAtRefusesLawlessWhenLocked(t *testing.T) {
	kb := newMineAtTestKB(t, "Lawless (no police protection)", false)
	client := newMineAtClient(130, 5)
	d := noWaitMineDispatch(client, kb, t.TempDir()) // Assets nil => locked

	err := d.MineQtyAt(context.Background(), "ice", 5, "glenhaven", "glenhaven_ice_belt", "to_base", "craftsman-3")
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "lawless") || !strings.Contains(err.Error(), "glenhaven") {
		t.Fatalf("err = %v, want lawless refusal naming glenhaven", err)
	}
	if hasCallPrefix(client.calls, "travel:", "jump:", "mine", "undock") {
		t.Fatalf("locked bot moved/mined: %v", client.calls)
	}
}

func TestMineQtyAtRefusesStrongholdWhenLocked(t *testing.T) {
	kb := newMineAtTestKB(t, "Lawless (no police protection)", true)
	client := newMineAtClient(130, 5)
	d := noWaitMineDispatch(client, kb, t.TempDir())

	err := d.MineQtyAt(context.Background(), "ice", 5, "glenhaven", "glenhaven_ice_belt", "to_base", "craftsman-3")
	if err == nil || !strings.Contains(err.Error(), "stronghold") {
		t.Fatalf("err = %v, want stronghold refusal", err)
	}
	if hasCallPrefix(client.calls, "travel:", "jump:", "mine", "undock") {
		t.Fatalf("locked bot moved/mined: %v", client.calls)
	}
}

func TestMineQtyAtKeepsFuelFloor(t *testing.T) {
	kb := newMineAtTestKB(t, "", false)
	client := newMineAtClient(MineQtyMinFuelReserve-1, 5)
	d := noWaitMineDispatch(client, kb, t.TempDir())

	err := d.MineQtyAt(context.Background(), "ice", 5, "glenhaven", "glenhaven_ice_belt", "to_base", "craftsman-3")
	if err == nil || !strings.Contains(err.Error(), "reserve") {
		t.Fatalf("err = %v, want fuel reserve refusal", err)
	}
	if hasCallPrefix(client.calls, "travel:", "jump:", "mine", "undock") {
		t.Fatalf("grounded bot moved/mined: %v", client.calls)
	}
}

// giftsByItem maps item_id to gifted quantity across the recorded gift calls.
func giftsByItem(client *mineFakeClient) map[string]float64 {
	got := map[string]float64{}
	for _, g := range client.giftCalls {
		id, _ := g["item_id"].(string)
		q, _ := g["quantity"].(float64)
		got[id] += q
	}
	return got
}

func TestMineQtyAtHandsOffByproduct(t *testing.T) {
	agentsDir := t.TempDir()
	writeDeliverCreds(t, agentsDir, "craftsman-3", "Artisan 'Ace' Anderson")
	kb := newMineAtTestKB(t, "", false)
	client := newMineAtClient(130, 60, 50)
	client.mineItemID = "water_ice"
	client.byproductID = "nitrogen_ice"
	client.byproductYields = []float64{30, 30}
	client.state.Ship.CargoCapacity = 500
	d := noWaitMineDispatch(client, kb, agentsDir)
	d.Out = io.Discard

	if err := d.MineQtyAt(context.Background(), "water_ice", 110, "glenhaven", "glenhaven_ice_belt", "to_base", "craftsman-3"); err != nil {
		t.Fatalf("MineQtyAt: %v", err)
	}
	got := giftsByItem(client)
	if len(client.giftCalls) != 2 || got["water_ice"] != 110 || got["nitrogen_ice"] != 60 {
		t.Fatalf("gift calls = %+v, want water_ice 110 and nitrogen_ice 60", client.giftCalls)
	}
	if first, _ := client.giftCalls[0]["item_id"].(string); first != "water_ice" {
		t.Fatalf("first gift = %q, want largest line water_ice first", first)
	}
	for _, g := range client.giftCalls {
		if g["recipient"] == "" || g["recipient"] == nil {
			t.Fatalf("gift lacks recipient: %+v", g)
		}
	}
}

// The hand-off is the whole target line plus every other line that grew on
// this trip, in full. A line that did not grow stays aboard, and
// energy_crystal is never gifted.
func TestMineQtyAtHandsOffTargetAndGrownLines(t *testing.T) {
	agentsDir := t.TempDir()
	writeDeliverCreds(t, agentsDir, "craftsman-3", "Artisan 'Ace' Anderson")
	kb := newMineAtTestKB(t, "", false)
	client := newMineAtClient(130, 60, 50)
	client.mineItemID = "water_ice"
	client.byproductID = "nitrogen_ice"
	client.byproductYields = []float64{30, 30}
	client.state.Ship.CargoCapacity = 500
	client.state.Ship.CargoUsed = 65
	client.state.Ship.Cargo = []game.CargoItem{
		{ItemID: "energy_crystal", Quantity: 40},
		{ItemID: "nitrogen_ice", Quantity: 20},
		{ItemID: "repair_kit", Quantity: 5},
	}
	d := noWaitMineDispatch(client, kb, agentsDir)
	d.Out = io.Discard

	if err := d.MineQtyAt(context.Background(), "water_ice", 110, "glenhaven", "glenhaven_ice_belt", "to_base", "craftsman-3"); err != nil {
		t.Fatalf("MineQtyAt: %v", err)
	}
	got := giftsByItem(client)
	if len(client.giftCalls) != 2 || got["water_ice"] != 110 || got["nitrogen_ice"] != 80 {
		t.Fatalf("gift calls = %+v, want only water_ice 110 and nitrogen_ice 80", client.giftCalls)
	}
}

// A load left aboard by a failed hand-off is gifted on the next pass, even
// though no mining happens.
func TestMineQtyAtHandsOffLeftoverTargetWithoutMining(t *testing.T) {
	agentsDir := t.TempDir()
	writeDeliverCreds(t, agentsDir, "craftsman-3", "Artisan 'Ace' Anderson")
	kb := newMineAtTestKB(t, "", false)
	client := newMineAtClient(130)
	client.mineItemID = "water_ice"
	client.state.Ship.CargoCapacity = 500
	client.state.Ship.CargoUsed = 140
	client.state.Ship.Cargo = []game.CargoItem{
		{ItemID: "water_ice", Quantity: 100},
		{ItemID: "energy_crystal", Quantity: 40},
	}
	d := noWaitMineDispatch(client, kb, agentsDir)
	d.Out = io.Discard

	if err := d.MineQtyAt(context.Background(), "water_ice", 100, "glenhaven", "glenhaven_ice_belt", "to_base", "craftsman-3"); err != nil {
		t.Fatalf("MineQtyAt: %v", err)
	}
	if slices.Contains(client.calls, "mine") {
		t.Fatalf("calls %v: mined although the target was already aboard", client.calls)
	}
	if got := giftsByItem(client); len(client.giftCalls) != 1 || got["water_ice"] != 100 {
		t.Fatalf("gift calls = %+v, want one gift of water_ice 100", client.giftCalls)
	}
}

// Target units aboard before the trip go with the load: 100 aboard, qty 110,
// mines 10, gifts 110.
func TestMineQtyAtHandsOffPreloadedTargetInFull(t *testing.T) {
	agentsDir := t.TempDir()
	writeDeliverCreds(t, agentsDir, "craftsman-3", "Artisan 'Ace' Anderson")
	kb := newMineAtTestKB(t, "", false)
	client := newMineAtClient(130, 10)
	client.mineItemID = "water_ice"
	client.state.Ship.CargoCapacity = 500
	client.state.Ship.CargoUsed = 100
	client.state.Ship.Cargo = []game.CargoItem{{ItemID: "water_ice", Quantity: 100}}
	d := noWaitMineDispatch(client, kb, agentsDir)
	d.Out = io.Discard

	if err := d.MineQtyAt(context.Background(), "water_ice", 110, "glenhaven", "glenhaven_ice_belt", "to_base", "craftsman-3"); err != nil {
		t.Fatalf("MineQtyAt: %v", err)
	}
	if got := giftsByItem(client); len(client.giftCalls) != 1 || got["water_ice"] != 110 {
		t.Fatalf("gift calls = %+v, want one gift of water_ice 110", client.giftCalls)
	}
}

func TestMineQtyAtHandsOffEvenWithZeroTarget(t *testing.T) {
	agentsDir := t.TempDir()
	writeDeliverCreds(t, agentsDir, "craftsman-3", "Artisan 'Ace' Anderson")
	kb := newMineAtTestKB(t, "", false)
	client := newMineAtClient(130) // target never yields
	client.byproductID = "nitrogen_ice"
	client.byproductYields = []float64{40}
	d := noWaitMineDispatch(client, kb, agentsDir)
	d.Out = io.Discard

	if err := d.MineQtyAt(context.Background(), "ice", 5, "glenhaven", "glenhaven_ice_belt", "to_base", "craftsman-3"); err != nil {
		t.Fatalf("MineQtyAt: %v", err)
	}
	if got := giftsByItem(client); len(client.giftCalls) != 1 || got["nitrogen_ice"] != 40 {
		t.Fatalf("gift calls = %+v, want one gift of nitrogen_ice 40", client.giftCalls)
	}
}

func TestMineQtyAtEmptyHoldAfterDryBeltIsNil(t *testing.T) {
	agentsDir := t.TempDir()
	writeDeliverCreds(t, agentsDir, "craftsman-3", "Artisan 'Ace' Anderson")
	kb := newMineAtTestKB(t, "", false)
	client := newMineAtClient(130)
	d := noWaitMineDispatch(client, kb, agentsDir)
	var out bytes.Buffer
	d.Out = &out

	if err := d.MineQtyAt(context.Background(), "ice", 5, "glenhaven", "glenhaven_ice_belt", "to_base", "craftsman-3"); err != nil {
		t.Fatalf("MineQtyAt: %v", err)
	}
	if len(client.giftCalls) != 0 || !strings.Contains(out.String(), "nothing to hand off") {
		t.Fatalf("gifts = %+v, out = %q; want no gifts and a nothing-to-hand-off log", client.giftCalls, out.String())
	}
}

// A hold of kept and unchanged lines is not empty, so the log must not say so.
func TestMineQtyAtKeptHoldLogsNothingToHandOff(t *testing.T) {
	agentsDir := t.TempDir()
	writeDeliverCreds(t, agentsDir, "craftsman-3", "Artisan 'Ace' Anderson")
	kb := newMineAtTestKB(t, "", false)
	client := newMineAtClient(130) // dry belt
	client.state.Ship.CargoUsed = 45
	client.state.Ship.Cargo = []game.CargoItem{
		{ItemID: "energy_crystal", Quantity: 40},
		{ItemID: "repair_kit", Quantity: 5},
	}
	d := noWaitMineDispatch(client, kb, agentsDir)
	var out bytes.Buffer
	d.Out = &out

	if err := d.MineQtyAt(context.Background(), "ice", 5, "glenhaven", "glenhaven_ice_belt", "to_base", "craftsman-3"); err != nil {
		t.Fatalf("MineQtyAt: %v", err)
	}
	if len(client.giftCalls) != 0 {
		t.Fatalf("gift calls = %+v, want none", client.giftCalls)
	}
	if log := out.String(); !strings.Contains(log, "nothing to hand off") || strings.Contains(log, "hold empty") {
		t.Fatalf("out = %q, want a nothing-to-hand-off log that does not claim the hold is empty", log)
	}
}

func TestHandOffHoldSkipsKeepList(t *testing.T) {
	agentsDir := t.TempDir()
	writeDeliverCreds(t, agentsDir, "craftsman-3", "Artisan 'Ace' Anderson")
	kb := newMineAtTestKB(t, "", false)
	client := newMineAtClient(130)
	client.state.Ship.Cargo = []game.CargoItem{
		{ItemID: "fuel_cell", Quantity: 20},
		{ItemID: "energy_crystal", Quantity: 30},
		{ItemID: "nitrogen_ice", Quantity: 5},
		{ItemID: "water_ice", Quantity: 7},
	}
	d := noWaitMineDispatch(client, kb, agentsDir)
	d.Out = io.Discard

	// Empty snapshot: every line grew, so only the keep list holds goods back.
	n, err := d.handOffHold(context.Background(), "to_base", "craftsman-3", "", map[string]int{}, map[string]bool{"water_ice": true})
	if err != nil || n != 1 {
		t.Fatalf("handOffHold = %d, %v; want 1, nil", n, err)
	}
	if got := giftsByItem(client); len(client.giftCalls) != 1 || got["nitrogen_ice"] != 5 {
		t.Fatalf("gift calls = %+v, want only nitrogen_ice", client.giftCalls)
	}
}

func TestHandOffHoldGiftsTargetAndGrownLinesWhole(t *testing.T) {
	agentsDir := t.TempDir()
	writeDeliverCreds(t, agentsDir, "craftsman-3", "Artisan 'Ace' Anderson")
	kb := newMineAtTestKB(t, "", false)
	client := newMineAtClient(130)
	client.state.Ship.Cargo = []game.CargoItem{
		{ItemID: "nitrogen_ice", Quantity: 25},
		{ItemID: "iron_ore", Quantity: 9},
		{ItemID: "water_ice", Quantity: 4},
		{ItemID: "repair_kit", Quantity: 3},
	}
	d := noWaitMineDispatch(client, kb, agentsDir)
	d.Out = io.Discard

	// nitrogen_ice grew (whole line goes); water_ice shrank but is the target
	// (whole line goes); iron_ore and repair_kit are unchanged (stay).
	before := map[string]int{"nitrogen_ice": 20, "iron_ore": 9, "water_ice": 10, "repair_kit": 3}
	n, err := d.handOffHold(context.Background(), "to_base", "craftsman-3", "water_ice", before, nil)
	if err != nil || n != 2 {
		t.Fatalf("handOffHold = %d, %v; want 2, nil", n, err)
	}
	got := giftsByItem(client)
	if len(client.giftCalls) != 2 || got["nitrogen_ice"] != 25 || got["water_ice"] != 4 {
		t.Fatalf("gift calls = %+v, want nitrogen_ice 25 and water_ice 4", client.giftCalls)
	}
	if first, _ := client.giftCalls[0]["item_id"].(string); first != "nitrogen_ice" {
		t.Fatalf("first gift = %q, want largest line nitrogen_ice first", first)
	}
}
