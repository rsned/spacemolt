package worker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/market"
)

// fakeClient records which command methods were invoked.
type fakeClient struct {
	game.GameClient // embedded; unimplemented methods panic if called
	calls           []string
	state           *game.State
	route           []game.RouteStep  // returned by FindRoute
	routeErr        error             // when set, FindRoute returns it instead of route
	jumpCanceled    bool              // Jump returns Canceled=true when set
	disconnected    bool              // when set, IsConnected() returns false (game connection down)
	dockErr         error             // when set, Dock returns it (ship not at a station)
	fuelLow         bool              // when set, Travel fails with insufficient fuel until Refuel clears it
	raw             map[string][]byte // GetRawJSON responses keyed by store key (e.g. "sell", "buy")

	refuelShipCalls []refuelShipCall // records of RefuelShip(target, quantity) calls
	refuelShipErr   error            // when set, RefuelShip returns it instead of recording success

	acceptErr      error   // when set, AcceptMission returns it instead of recording success
	buyErr         error   // when set, Buy returns it instead of recording success
	sellErr        error   // when set, Sell returns it (models "no local buyer")
	completeReward float64 // credited to state.Credits on CompleteMission (mission tests)

	viewStorageErr error // when set, ViewStorage returns it instead of recording success
	withdrawErr    error // when set, WithdrawItems returns it instead of recording success

	shippingErr   map[string]error // per-action error, keyed by shipping action
	shippingCalls []string         // shipping actions issued, in order
	// onShippingAccept fires after a successful ShippingAccept, letting a test
	// model the real server dropping an accepted listing off the /shipping
	// board (e.g. so a static canned shipping_list reply doesn't offer the
	// same contract forever, which the chain refill loop would otherwise
	// re-accept every leg).
	onShippingAccept func(shipmentID string)

	// cloneState makes GetState return a copy, as the real client does, so a
	// stale snapshot is distinguishable from a fresh read.
	cloneState bool
	// onGetActiveMissions fires when GetActiveMissions is called, letting a
	// test model server-side state changes that happen mid-pass (e.g. a resume
	// unloading cargo and freeing the hold).
	onGetActiveMissions func()

	// activeMissionsSeq, when non-nil, supplies successive
	// GetRawJSON("active_missions") results in call order — one entry per
	// GetActiveMissions call in the pass (e.g. empty before accept, then
	// populated with the newly created active instance after). Once
	// exhausted, later calls return the last entry (steady state). Falls
	// back to raw["active_missions"] when nil, for tests that only need a
	// single static snapshot.
	activeMissionsSeq  [][]byte
	activeMissionsCall int
}

// refuelShipCall records one RefuelShip(ctx, target, quantity) invocation.
type refuelShipCall struct {
	target   string
	quantity int
}

func (f *fakeClient) Undock(ctx context.Context) error {
	f.calls = append(f.calls, "undock")
	return nil
}
func (f *fakeClient) Dock(ctx context.Context) error {
	f.calls = append(f.calls, "dock")
	return f.dockErr
}
func (f *fakeClient) Mine(ctx context.Context) error { f.calls = append(f.calls, "mine"); return nil }
func (f *fakeClient) Buy(ctx context.Context, itemID string, qty float64) error {
	f.calls = append(f.calls, "buy:"+itemID)
	return f.buyErr
}
func (f *fakeClient) Sell(ctx context.Context, itemID string, qty float64) error {
	f.calls = append(f.calls, "sell:"+itemID)
	return f.sellErr
}
func (f *fakeClient) CreateSellOrder(ctx context.Context, payload map[string]any) error {
	f.calls = append(f.calls, fmt.Sprintf("sell_order:%v@%v", payload["item_id"], payload["price_each"]))
	return nil
}
func (f *fakeClient) Refuel(ctx context.Context) error {
	f.calls = append(f.calls, "refuel")
	f.fuelLow = false // a successful refuel clears the fuel shortage
	return nil
}
func (f *fakeClient) Repair(ctx context.Context) error {
	f.calls = append(f.calls, "repair")
	return nil
}
func (f *fakeClient) DepositAllItems(ctx context.Context) error {
	f.calls = append(f.calls, "deposit_all")
	return nil
}
func (f *fakeClient) SellAllBulk(ctx context.Context, reserved []string) error {
	f.calls = append(f.calls, "sell_all")
	return nil
}
func (f *fakeClient) Travel(ctx context.Context, poi string) (*game.TravelResult, error) {
	f.calls = append(f.calls, "travel:"+poi)
	if f.fuelLow {
		return nil, errors.New("Insufficient fuel for travel")
	}
	return &game.TravelResult{}, nil
}
func (f *fakeClient) GetStatus(ctx context.Context) error {
	f.calls = append(f.calls, "get_status")
	return nil
}
func (f *fakeClient) GetSystem(ctx context.Context) error {
	f.calls = append(f.calls, "get_system")
	return nil
}
func (f *fakeClient) GetCargo(ctx context.Context) error {
	f.calls = append(f.calls, "get_cargo")
	return nil
}
func (f *fakeClient) GetPOI(ctx context.Context) error {
	f.calls = append(f.calls, "get_poi")
	return nil
}
func (f *fakeClient) GetMissions(ctx context.Context) error {
	f.calls = append(f.calls, "get_missions")
	return nil
}

// GetState returns the live object by default. The REAL client returns
// state.Clone() (client.go:2095), and that difference matters: with a shared
// pointer, a snapshot taken early in a pass and a fresh read later are the same
// object, so code that wrongly reuses a stale snapshot still looks correct.
// Tests that need to tell those apart set cloneState. It is opt-in because
// cloning unconditionally deadlocks TestKBUpdateMissionsUpsertsHandAuthoredOnly,
// which relies on observing its own mutations through this pointer.
func (f *fakeClient) GetState() *game.State {
	if f.state == nil {
		return nil
	}
	if f.cloneState {
		return f.state.Clone()
	}
	return f.state
}

// IsConnected reports the game-connection state; connected unless a test sets
// disconnected. The haul/mission passes skip work when disconnected.
func (f *fakeClient) IsConnected() bool { return !f.disconnected }
func (f *fakeClient) ViewMarket(ctx context.Context, params map[string]any) error {
	f.calls = append(f.calls, "view_market")
	return nil
}
func (f *fakeClient) FindRoute(ctx context.Context, target string) ([]game.RouteStep, error) {
	f.calls = append(f.calls, "find_route:"+target)
	if f.routeErr != nil {
		return nil, f.routeErr
	}
	return f.route, nil
}
func (f *fakeClient) Jump(ctx context.Context, sys string) (*game.JumpResult, error) {
	f.calls = append(f.calls, "jump:"+sys)
	return &game.JumpResult{Canceled: f.jumpCanceled}, nil
}
func (f *fakeClient) GetRawJSON(key string) []byte {
	if key == "active_missions" && f.activeMissionsSeq != nil {
		idx := f.activeMissionsCall
		f.activeMissionsCall++
		if idx < len(f.activeMissionsSeq) {
			return f.activeMissionsSeq[idx]
		}
		if n := len(f.activeMissionsSeq); n > 0 {
			return f.activeMissionsSeq[n-1] // exhausted: steady-state on the last entry
		}
	}
	if f.raw != nil {
		return f.raw[key]
	}
	return nil
}
func (f *fakeClient) ViewStorage(ctx context.Context) error {
	f.calls = append(f.calls, "view_storage")
	return f.viewStorageErr
}
func (f *fakeClient) WithdrawItems(ctx context.Context, itemID string, quantity float64) error {
	f.calls = append(f.calls, fmt.Sprintf("withdraw:%s:%.0f", itemID, quantity))
	return f.withdrawErr
}
func (f *fakeClient) RawCommand(ctx context.Context, command string, args map[string]any) error {
	f.calls = append(f.calls, "raw:"+command)
	return nil
}
func (f *fakeClient) RefuelShip(ctx context.Context, target string, quantity int) error {
	f.refuelShipCalls = append(f.refuelShipCalls, refuelShipCall{target: target, quantity: quantity})
	return f.refuelShipErr
}
func (f *fakeClient) GetBase(ctx context.Context) error {
	f.calls = append(f.calls, "get_base")
	return nil
}
func (f *fakeClient) GetListings(ctx context.Context) error {
	f.calls = append(f.calls, "get_listings")
	return nil
}
func (f *fakeClient) GetMarketListings() []game.MarketListing { return nil }
func (f *fakeClient) BrowseShips(ctx context.Context, payload map[string]any) error {
	f.calls = append(f.calls, "browse_ships")
	return nil
}
func (f *fakeClient) ShippingList(ctx context.Context, sort string) error {
	f.calls = append(f.calls, "shipping_list")
	f.shippingCalls = append(f.shippingCalls, "list")
	return f.shippingErr["list"]
}
func (f *fakeClient) ShippingProfile(ctx context.Context) error {
	f.calls = append(f.calls, "shipping_profile")
	f.shippingCalls = append(f.shippingCalls, "profile")
	return f.shippingErr["profile"]
}
func (f *fakeClient) ShippingAccept(ctx context.Context, shipmentID, carrier string) error {
	f.calls = append(f.calls, "shipping_accept:"+shipmentID)
	f.shippingCalls = append(f.shippingCalls, "accept")
	err := f.shippingErr["accept"]
	if err == nil && f.onShippingAccept != nil {
		f.onShippingAccept(shipmentID)
	}
	return err
}
func (f *fakeClient) ShippingDeliver(ctx context.Context, shipmentID string) error {
	f.calls = append(f.calls, "shipping_deliver:"+shipmentID)
	f.shippingCalls = append(f.shippingCalls, "deliver")
	return f.shippingErr["deliver"]
}
func (f *fakeClient) ShippingReturn(ctx context.Context, shipmentID string) error {
	f.calls = append(f.calls, "shipping_return:"+shipmentID)
	f.shippingCalls = append(f.shippingCalls, "return")
	return f.shippingErr["return"]
}
func (f *fakeClient) ShippingGet(ctx context.Context, shipmentID string) error {
	f.calls = append(f.calls, "shipping_get:"+shipmentID)
	f.shippingCalls = append(f.shippingCalls, "get")
	return f.shippingErr["get"]
}

func TestDispatchRunsKnownCommands(t *testing.T) {
	f := &fakeClient{state: &game.State{}}
	d := NewWorkerDispatch(f, nil, nil, io.Discard)
	for _, tc := range [][]string{
		{"undock"}, {"mine"}, {"dock"}, {"refuel"}, {"deposit_all"},
		{"sell_all"}, {"repair"}, {"get_status"}, {"get_system"}, {"get_cargo"},
	} {
		if err := d.Run(context.Background(), tc); err != nil {
			t.Fatalf("Run(%v): %v", tc, err)
		}
	}
	want := []string{
		"undock", "mine", "dock", "refuel", "deposit_all",
		"sell_all", "repair", "get_status", "get_system", "get_cargo",
	}
	if len(f.calls) != len(want) {
		t.Fatalf("calls=%v want=%v", f.calls, want)
	}
	for i, got := range f.calls {
		if got != want[i] {
			t.Errorf("call %d = %q, want %q", i, got, want[i])
		}
	}
}

func TestDispatchTravelArg(t *testing.T) {
	f := &fakeClient{state: &game.State{}}
	d := NewWorkerDispatch(f, nil, nil, io.Discard)
	if err := d.Run(context.Background(), []string{"travel", "POI-1"}); err != nil {
		t.Fatalf("travel: %v", err)
	}
	if len(f.calls) != 1 || f.calls[0] != "travel:POI-1" {
		t.Fatalf("calls=%v", f.calls)
	}
}

func TestDispatchUpdateMarketRequiresCollector(t *testing.T) {
	f := &fakeClient{state: &game.State{}}
	d := NewWorkerDispatch(f, nil, nil, io.Discard)
	if err := d.Run(context.Background(), []string{"update_market"}); err == nil {
		t.Fatal("expected error when market collector is nil")
	}
	if !d.Supports("update_market") {
		t.Fatal("update_market should be in the curated vocabulary")
	}
}

func TestDispatchUpdateMarketPrimesAndCaptures(t *testing.T) {
	// CurrentPOI is empty, so CaptureFromClient gracefully no-ops after the
	// ViewMarket prime — we assert the prime happened and no error surfaced.
	f := &fakeClient{state: &game.State{}}
	mc, err := market.Open(market.Config{DBPath: filepath.Join(t.TempDir(), "m.db")})
	if err != nil {
		t.Fatalf("market.Open: %v", err)
	}
	t.Cleanup(func() { _ = mc.Close() })

	d := NewWorkerDispatch(f, nil, mc, io.Discard)
	if err := d.Run(context.Background(), []string{"update_market"}); err != nil {
		t.Fatalf("update_market: %v", err)
	}
	found := false
	for _, c := range f.calls {
		if c == "view_market" {
			found = true
		}
	}
	if !found {
		t.Fatalf("update_market must call ViewMarket to prime the cache; calls=%v", f.calls)
	}
}

func TestDispatchUnknownCommandErrors(t *testing.T) {
	f := &fakeClient{state: &game.State{}}
	d := NewWorkerDispatch(f, nil, nil, io.Discard)
	if err := d.Run(context.Background(), []string{"frobnicate"}); err == nil {
		t.Fatal("expected error for unknown command")
	}
	if d.Supports("frobnicate") {
		t.Fatal("Supports should be false for unknown command")
	}
	if !d.Supports("mine") {
		t.Fatal("Supports should be true for mine")
	}
}

func TestDispatchJump(t *testing.T) {
	f := &fakeClient{state: &game.State{}}
	d := NewWorkerDispatch(f, nil, nil, io.Discard)
	if !d.Supports("jump") {
		t.Fatal("jump should be supported")
	}
	if err := d.Run(context.Background(), []string{"jump", "sys_z"}); err != nil {
		t.Fatalf("Run jump: %v", err)
	}
	if !slices.Contains(f.calls, "jump:sys_z") {
		t.Errorf("expected jump:sys_z, got %v", f.calls)
	}
}

func TestDispatchAutopilot(t *testing.T) {
	f := autopilotFake()
	d := NewWorkerDispatch(f, nil, nil, io.Discard)
	if !d.Supports("autopilot") {
		t.Fatal("autopilot should be supported")
	}
	if err := d.Run(context.Background(), []string{"autopilot", "sys_c"}); err != nil {
		t.Fatalf("Run autopilot: %v", err)
	}
	if !slices.Contains(f.calls, "find_route:sys_c") || !slices.Contains(f.calls, "jump:sys_c") {
		t.Errorf("expected find_route + jumps, got %v", f.calls)
	}
}

func (f *fakeClient) Scan(ctx context.Context) error {
	f.calls = append(f.calls, "scan")
	return nil
}

func TestDispatchScan(t *testing.T) {
	f := &fakeClient{state: &game.State{}}
	d := NewWorkerDispatch(f, nil, nil, io.Discard)
	if !d.Supports("scan") {
		t.Fatal("scan should be supported")
	}
	if err := d.Run(context.Background(), []string{"scan"}); err != nil {
		t.Fatalf("Run scan: %v", err)
	}
	if !slices.Contains(f.calls, "scan") {
		t.Errorf("expected scan call, got %v", f.calls)
	}
}

func TestDispatchExploreAutopilotsToFrontier(t *testing.T) {
	// Current system id "a" with a frontier neighbour "b". explore should pick b
	// and autopilot there (find_route + jump). Selection must key on the system
	// ID (state.System.ID), NOT the display name: CurrentSystem is deliberately
	// "A" (≠ id "a") here — the KB graph uses ids, so a name would reach no node.
	f := &fakeClient{
		state: &game.State{
			CurrentSystem: "A",
			System:        game.SystemData{ID: "a", Name: "A"},
			Fuel:          100, MaxFuel: 100,
		},
		route: []game.RouteStep{{SystemID: "a", Name: "A"}, {SystemID: "b", Name: "B"}},
	}
	kb := &fakeKB{
		systems: []knowledge.System{{ID: "a", LastVisitedTick: 5, LastUpdatedTick: 1}},
		conns:   undirected([2]string{"a", "b"}),
	}
	d := NewWorkerDispatch(f, kb, nil, io.Discard)
	if !d.Supports("explore") {
		t.Fatal("explore should be supported")
	}
	if err := d.Run(context.Background(), []string{"explore"}); err != nil {
		t.Fatalf("Run explore: %v", err)
	}
	if !slices.Contains(f.calls, "find_route:b") || !slices.Contains(f.calls, "jump:b") {
		t.Errorf("expected autopilot to frontier b, got %v", f.calls)
	}
}

func TestDispatchExploreNoTargetNoOp(t *testing.T) {
	// No connections -> nothing reachable -> explore no-ops without navigating.
	f := &fakeClient{state: &game.State{CurrentSystem: "A", System: game.SystemData{ID: "a", Name: "A"}}}
	kb := &fakeKB{systems: []knowledge.System{{ID: "a", LastVisitedTick: 5}}}
	d := NewWorkerDispatch(f, kb, nil, io.Discard)
	if err := d.Run(context.Background(), []string{"explore"}); err != nil {
		t.Fatalf("Run explore (no target): %v", err)
	}
	for _, c := range f.calls {
		if len(c) >= 11 && c[:11] == "find_route:" {
			t.Errorf("explore with no target must not navigate, got %v", f.calls)
		}
	}
}

func TestHaulIsSupported(t *testing.T) {
	d := NewWorkerDispatch(nil, nil, nil, nil)
	if !d.Supports("haul") {
		t.Fatal("haul should be in the supported command set")
	}
}

func TestHaulNilMarketIsSafeNoop(t *testing.T) {
	// No market collector configured -> haul logs and returns nil, never panics.
	d := NewWorkerDispatch(nil, nil, nil, nil)
	if err := d.Run(context.Background(), []string{"haul"}); err != nil {
		t.Fatalf("haul with nil market should no-op, got %v", err)
	}
}

func TestEnsureHomeNoStation(t *testing.T) {
	c := &fakeClient{state: &game.State{}}
	d := NewWorkerDispatch(c, nil, nil, io.Discard)
	d.Station = "" // no home configured
	navigated := false
	d.ensureHomeNav = func(ctx context.Context, system, poi string) error { navigated = true; return nil }
	if err := d.Run(context.Background(), []string{"ensure_home"}); err != nil {
		t.Fatalf("ensure_home: %v", err)
	}
	if navigated {
		t.Error("navigated despite no home configured")
	}
	for _, call := range c.calls {
		if strings.HasPrefix(call, "find_route") {
			t.Errorf("called %q with no home", call)
		}
	}
}

func TestEnsureHomeAlreadyDocked(t *testing.T) {
	c := &fakeClient{state: &game.State{CurrentPOI: "grand_exchange", Doc: true}}
	d := NewWorkerDispatch(c, nil, nil, io.Discard)
	d.Station = "grand_exchange"
	navigated := false
	d.ensureHomeNav = func(ctx context.Context, system, poi string) error { navigated = true; return nil }
	if err := d.Run(context.Background(), []string{"ensure_home"}); err != nil {
		t.Fatalf("ensure_home: %v", err)
	}
	if navigated {
		t.Error("navigated while already docked at home")
	}
	for _, call := range c.calls {
		if strings.HasPrefix(call, "find_route") || call == "dock" {
			t.Errorf("unexpected call %q when already home", call)
		}
	}
}

func TestEnsureHomeDisplacedTravelsAndDocks(t *testing.T) {
	c := &fakeClient{
		state: &game.State{CurrentPOI: "unknown_edge_waystation", Doc: true},
		route: []game.RouteStep{{SystemID: "market_prime"}},
	}
	d := NewWorkerDispatch(c, nil, nil, io.Discard)
	d.Station = "market_prime_exchange"
	var gotSystem, gotPOI string
	d.ensureHomeNav = func(ctx context.Context, system, poi string) error {
		gotSystem, gotPOI = system, poi
		return nil
	}
	if err := d.Run(context.Background(), []string{"ensure_home"}); err != nil {
		t.Fatalf("ensure_home: %v", err)
	}
	if gotSystem != "market_prime" || gotPOI != "market_prime_exchange" {
		t.Errorf("navigated to %s/%s, want market_prime/market_prime_exchange", gotSystem, gotPOI)
	}
	if !slices.Contains(c.calls, "find_route:market_prime_exchange") {
		t.Errorf("find_route not called; calls=%v", c.calls)
	}
	if !slices.Contains(c.calls, "dock") {
		t.Errorf("dock not called; calls=%v", c.calls)
	}
}

func TestEnsureHomeEmptyRouteUsesCurrentSystem(t *testing.T) {
	// In the home system already (route empty) but parked at a belt, not the station.
	c := &fakeClient{
		state: &game.State{CurrentPOI: "market_prime_belt", Doc: false, System: game.SystemData{ID: "market_prime"}},
		route: nil,
	}
	d := NewWorkerDispatch(c, nil, nil, io.Discard)
	d.Station = "market_prime_exchange"
	var gotSystem string
	d.ensureHomeNav = func(ctx context.Context, system, poi string) error { gotSystem = system; return nil }
	if err := d.Run(context.Background(), []string{"ensure_home"}); err != nil {
		t.Fatalf("ensure_home: %v", err)
	}
	if gotSystem != "market_prime" {
		t.Errorf("system=%q, want market_prime from current state", gotSystem)
	}
}

func TestEnsureHomeFindRouteErrorIsBestEffort(t *testing.T) {
	c := &fakeClient{
		state:    &game.State{CurrentPOI: "somewhere", Doc: false},
		routeErr: errors.New("You are not in a system"),
	}
	d := NewWorkerDispatch(c, nil, nil, io.Discard)
	d.Station = "market_prime_exchange"
	navigated := false
	d.ensureHomeNav = func(ctx context.Context, system, poi string) error { navigated = true; return nil }
	if err := d.Run(context.Background(), []string{"ensure_home"}); err != nil {
		t.Fatalf("ensure_home must be best-effort nil, got %v", err)
	}
	if navigated {
		t.Error("navigated despite find_route error")
	}
}

func TestEnsureHomeToleratesAlreadyDockedError(t *testing.T) {
	c := &fakeClient{
		state:   &game.State{CurrentPOI: "market_prime_belt", Doc: false, System: game.SystemData{ID: "market_prime"}},
		dockErr: errors.New("Already docked at this station"),
	}
	d := NewWorkerDispatch(c, nil, nil, io.Discard)
	d.Station = "market_prime_exchange"
	d.ensureHomeNav = func(ctx context.Context, system, poi string) error { return nil }
	if err := d.Run(context.Background(), []string{"ensure_home"}); err != nil {
		t.Fatalf("ensure_home must swallow 'Already docked', got %v", err)
	}
}

func TestCaptureFuelWritesRow(t *testing.T) {
	c, err := market.Open(market.Config{DBPath: filepath.Join(t.TempDir(), "test.db")})
	if err != nil {
		t.Fatalf("open collector: %v", err)
	}
	defer c.Close() //nolint:errcheck

	fc := &fakeClient{
		state: &game.State{CurrentPOI: "sol_central"},
		raw:   map[string][]byte{"base": []byte(`{"action":"get_base","fuel_price":2,"fuel_tax_per_unit":5,"fuel_price_all_in":7}`)},
	}
	d := NewWorkerDispatch(fc, nil, c, io.Discard)
	d.AgentID = "marketbot_sol"

	if err := d.Run(context.Background(), []string{"capture_fuel"}); err != nil {
		t.Fatalf("capture_fuel: %v", err)
	}
	allIn, _, ok, err := c.GetStationFuelPrice(context.Background(), "sol_central")
	if err != nil || !ok || allIn != 7 {
		t.Fatalf("row not written: allIn=%d ok=%v err=%v", allIn, ok, err)
	}
	if !slices.Contains(fc.calls, "get_base") {
		t.Errorf("get_base not issued; calls=%v", fc.calls)
	}
}

func TestDispatchMissionsCommand(t *testing.T) {
	fc := &fakeClient{state: missionState(true, 5000, 0), raw: map[string][]byte{}}
	d := NewWorkerDispatch(fc, nil, nil, io.Discard)
	if !d.Supports("missions") {
		t.Fatal("missions must be a supported worker command")
	}
	// No market collector configured -> logs and returns nil (degraded no-op),
	// matching the haul command's contract.
	if err := d.Run(context.Background(), []string{"missions"}); err != nil {
		t.Fatalf("missions without market collector must no-op, got %v", err)
	}
}
