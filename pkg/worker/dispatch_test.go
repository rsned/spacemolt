package worker

import (
	"context"
	"io"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

// fakeClient records which command methods were invoked.
type fakeClient struct {
	game.GameClient // embedded; unimplemented methods panic if called
	calls           []string
	state           *game.State
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
func (f *fakeClient) GetState() *game.State { return f.state }

func TestDispatchRunsKnownCommands(t *testing.T) {
	f := &fakeClient{state: &game.State{}}
	d := NewWorkerDispatch(f, nil, io.Discard)
	for _, tc := range [][]string{{"undock"}, {"mine"}, {"dock"}, {"refuel"}, {"deposit_all"}} {
		if err := d.Run(context.Background(), tc); err != nil {
			t.Fatalf("Run(%v): %v", tc, err)
		}
	}
	want := []string{"undock", "mine", "dock", "refuel", "deposit_all"}
	if len(f.calls) != len(want) {
		t.Fatalf("calls=%v want=%v", f.calls, want)
	}
}

func TestDispatchTravelArg(t *testing.T) {
	f := &fakeClient{state: &game.State{}}
	d := NewWorkerDispatch(f, nil, io.Discard)
	if err := d.Run(context.Background(), []string{"travel", "POI-1"}); err != nil {
		t.Fatalf("travel: %v", err)
	}
	if len(f.calls) != 1 || f.calls[0] != "travel:POI-1" {
		t.Fatalf("calls=%v", f.calls)
	}
}

func TestDispatchUnknownCommandErrors(t *testing.T) {
	f := &fakeClient{state: &game.State{}}
	d := NewWorkerDispatch(f, nil, io.Discard)
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
