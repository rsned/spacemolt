package worker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
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
	jumpCanceled    bool              // Jump returns Canceled=true when set
	dockErr         error             // when set, Dock returns it (ship not at a station)
	fuelLow         bool              // when set, Travel fails with insufficient fuel until Refuel clears it
	raw             map[string][]byte // GetRawJSON responses keyed by store key (e.g. "sell", "buy")
}

func (f *fakeClient) Undock(ctx context.Context) error { f.calls = append(f.calls, "undock"); return nil }
func (f *fakeClient) Dock(ctx context.Context) error {
	f.calls = append(f.calls, "dock")
	return f.dockErr
}
func (f *fakeClient) Mine(ctx context.Context) error   { f.calls = append(f.calls, "mine"); return nil }
func (f *fakeClient) Buy(ctx context.Context, itemID string, qty float64) error {
	f.calls = append(f.calls, "buy:"+itemID)
	return nil
}
func (f *fakeClient) Sell(ctx context.Context, itemID string, qty float64) error {
	f.calls = append(f.calls, "sell:"+itemID)
	return nil
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
func (f *fakeClient) Repair(ctx context.Context) error { f.calls = append(f.calls, "repair"); return nil }
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
func (f *fakeClient) GetState() *game.State { return f.state }
func (f *fakeClient) ViewMarket(ctx context.Context, params map[string]any) error {
	f.calls = append(f.calls, "view_market")
	return nil
}
func (f *fakeClient) FindRoute(ctx context.Context, target string) ([]game.RouteStep, error) {
	f.calls = append(f.calls, "find_route:"+target)
	return f.route, nil
}
func (f *fakeClient) Jump(ctx context.Context, sys string) (*game.JumpResult, error) {
	f.calls = append(f.calls, "jump:"+sys)
	return &game.JumpResult{Canceled: f.jumpCanceled}, nil
}
func (f *fakeClient) GetRawJSON(key string) []byte {
	if f.raw != nil {
		return f.raw[key]
	}
	return nil
}
func (f *fakeClient) RawCommand(ctx context.Context, command string, args map[string]any) error {
	f.calls = append(f.calls, "raw:"+command)
	return nil
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
