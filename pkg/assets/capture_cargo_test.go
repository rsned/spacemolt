package assets

import (
	"context"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// TestReplaceCargoRoundTrip pins the manifest we have never had. agent_hulls
// records cargo_used -- the SCALAR -- so a hold reads as "301/315" with no way
// to learn what the 301 units are. That blind spot hid 200 starshine and 100
// nerve_burn sitting in explorer-5's hold for ten days while the mission layer
// insisted it carried none (2026-10-02); ~1.9M credits of supplied contraband
// fleetwide was invisible to every query that could be written.
func TestReplaceCargoRoundTrip(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 3, 1, 0, 0, 0, time.UTC)

	rows := []CargoRow{
		{ItemID: "starshine", Name: "Starshine", Quantity: 200, UnitSize: 1},
		{ItemID: "nerve_burn", Name: "Nerve Burn", Quantity: 100, UnitSize: 1},
	}
	if err := st.ReplaceCargo(ctx, "p1", "ship_a", rows, now); err != nil {
		t.Fatalf("ReplaceCargo: %v", err)
	}

	got, err := st.LoadCargo(ctx, "p1")
	if err != nil {
		t.Fatalf("LoadCargo: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 cargo rows, got %d: %+v", len(got), got)
	}
	byID := map[string]CargoRow{}
	for _, r := range got {
		byID[r.ItemID] = r
	}
	if s := byID["starshine"]; s.Quantity != 200 || s.Name != "Starshine" || s.ShipID != "ship_a" {
		t.Fatalf("starshine row wrong: %+v", s)
	}
}

// A hold that empties must come back empty. Carrying a stale manifest forward
// would be worse than having none: it would report contraband the agent has
// already delivered, which is exactly the mistake that made cargo_used
// untrustworthy as a proxy.
func TestReplaceCargoClearsASoldOffHold(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 3, 1, 0, 0, 0, time.UTC)

	if err := st.ReplaceCargo(ctx, "p1", "ship_a",
		[]CargoRow{{ItemID: "starshine", Quantity: 200, UnitSize: 1}}, now); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := st.ReplaceCargo(ctx, "p1", "ship_a", nil, now); err != nil {
		t.Fatalf("clear: %v", err)
	}
	got, err := st.LoadCargo(ctx, "p1")
	if err != nil {
		t.Fatalf("LoadCargo: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("an emptied hold must read empty, got %+v", got)
	}
}

// CaptureCargo reads the live hold off state. A source failure degrades to
// "less captured this pass" and must never fail the pass, matching every other
// capture in this package.
func TestCaptureCargoRecordsTheLiveHold(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	fc := &cargoFakeClient{
		state: &game.State{
			Player: game.Player{ID: "p1"},
			Ship: game.Ship{
				ID: "ship_a",
				Cargo: []game.CargoItem{
					{ItemID: "starshine", Quantity: 200},
					{ItemID: "nerve_burn", Quantity: 100},
				},
			},
		},
	}
	if err := CaptureCargo(ctx, fc, st, "explorer-5", time.Now()); err != nil {
		t.Fatalf("CaptureCargo: %v", err)
	}
	got, err := st.LoadCargo(ctx, "p1")
	if err != nil {
		t.Fatalf("LoadCargo: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want the 2-item manifest, got %+v", got)
	}
}

// cargoFakeClient serves a fixed state; GetCargo is a no-op because the state
// already models what the server would have returned.
type cargoFakeClient struct {
	game.GameClient
	state *game.State
}

func (f *cargoFakeClient) GetCargo(context.Context) error { return nil }
func (f *cargoFakeClient) GetState() *game.State          { return f.state }
