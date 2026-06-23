package worker

import (
	"context"
	"io"
	"path/filepath"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/market"
)

// fakeClient records which command methods were invoked.
type fakeClient struct {
	game.GameClient // embedded; unimplemented methods panic if called
	calls           []string
	state           *game.State
	route           []game.RouteStep // returned by FindRoute
	jumpCanceled    bool             // Jump returns Canceled=true when set
}

func (f *fakeClient) Undock(ctx context.Context) error { f.calls = append(f.calls, "undock"); return nil }
func (f *fakeClient) Dock(ctx context.Context) error   { f.calls = append(f.calls, "dock"); return nil }
func (f *fakeClient) Mine(ctx context.Context) error   { f.calls = append(f.calls, "mine"); return nil }
func (f *fakeClient) Refuel(ctx context.Context) error { f.calls = append(f.calls, "refuel"); return nil }
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
func (f *fakeClient) GetRawJSON(key string) []byte { return nil }

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
