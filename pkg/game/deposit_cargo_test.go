package game

import "testing"

// deposit_items updated Ship.CargoCapacity from `cargo_space` and never touched
// Ship.Cargo. Two consequences, both live on 2026-08-22:
//
//   - `cargo_space` is the FREE space left in the hold, not the hold's size, so
//     the capacity shrank to whatever happened to be free at deposit time.
//   - Ship.CargoUsed is recomputed elsewhere (client.go ~:4071, ~:4154) by
//     summing Ship.Cargo, and deposits never removed the deposited items from
//     that slice, so the figure only ever grew.
//
// Together the dashboard showed miner-10 at 182/100 and prophet-2 at 168/125
// while the server's own ship payload reported cargo_used = 0 for both. It is
// not cosmetic: `cargoFree := CargoCapacity - CargoUsed` goes negative in
// pkg/worker/haul.go and pkg/worker/mission.go, and a worker that believes it
// has negative free space declines to load.
//
// Wire note: cargo_space/cargo_remaining/quantity are INTEGERS on the wire, but
// they arrive here through encoding/json into map[string]any, which decodes
// every JSON number as float64. The float64 assertions below are correct — do
// not "fix" them to .(int), which would silently never match.

func depositClient(t *testing.T) *Client {
	t.Helper()
	c := NewClient("wss://test.example.com", "testuser", "testtoken", nil)
	c.SetDebugLogging(false)
	c.state.Ship.Cargo = []CargoItem{{ItemID: "iron_ore", Quantity: 50}}
	c.state.Ship.CargoUsed = 50
	c.state.Ship.CargoCapacity = 150
	return c
}

func TestParseActionResult_DepositAllEmptiesTheHold(t *testing.T) {
	c := depositClient(t)
	c.parseActionResult(map[string]any{
		"command": "deposit_items",
		"result": map[string]any{
			"action":          "deposit_items",
			"item_id":         "iron_ore",
			"quantity":        float64(50),
			"cargo_remaining": float64(0),
			"cargo_space":     float64(150),
			"storage_total":   float64(50),
		},
	})
	st := c.GetState()
	if st.Ship.CargoUsed != 0 {
		t.Errorf("CargoUsed = %.0f, want 0 (the hold was fully deposited)", st.Ship.CargoUsed)
	}
	if st.Ship.CargoCapacity != 150 {
		t.Errorf("CargoCapacity = %.0f, want 150 (remaining 0 + free 150)", st.Ship.CargoCapacity)
	}
	if len(st.Ship.Cargo) != 0 {
		t.Errorf("Ship.Cargo still holds %d item(s); deposits must remove them or CargoUsed regrows on the next recompute", len(st.Ship.Cargo))
	}
}

func TestParseActionResult_DepositPartialKeepsTheRemainder(t *testing.T) {
	c := depositClient(t)
	c.parseActionResult(map[string]any{
		"command": "deposit_items",
		"result": map[string]any{
			"action":          "deposit_items",
			"item_id":         "iron_ore",
			"quantity":        float64(20),
			"cargo_remaining": float64(30),
			"cargo_space":     float64(120),
			"storage_total":   float64(20),
		},
	})
	st := c.GetState()
	if st.Ship.CargoUsed != 30 {
		t.Errorf("CargoUsed = %.0f, want 30", st.Ship.CargoUsed)
	}
	// The bug: 120 (free) was stored as the capacity. Capacity is 30+120.
	if st.Ship.CargoCapacity != 150 {
		t.Errorf("CargoCapacity = %.0f, want 150 (remaining 30 + free 120), not the free-space figure", st.Ship.CargoCapacity)
	}
	if len(st.Ship.Cargo) != 1 || st.Ship.Cargo[0].Quantity != 30 {
		t.Errorf("Ship.Cargo = %+v, want one iron_ore of 30", st.Ship.Cargo)
	}
}

// A cross-storage transfer (source != cargo) never touches the hold, so the
// deposited quantity must NOT be subtracted from Ship.Cargo.
func TestParseActionResult_DepositFromStorageLeavesTheHoldAlone(t *testing.T) {
	c := depositClient(t)
	c.parseActionResult(map[string]any{
		"command": "deposit_items",
		"result": map[string]any{
			"action":        "deposit_items",
			"source":        "storage",
			"destination":   "faction",
			"item_id":       "iron_ore",
			"quantity":      float64(50),
			"storage_total": float64(50),
		},
	})
	st := c.GetState()
	if len(st.Ship.Cargo) != 1 || st.Ship.Cargo[0].Quantity != 50 {
		t.Errorf("Ship.Cargo = %+v, want the hold untouched by a storage->faction transfer", st.Ship.Cargo)
	}
	if st.Ship.CargoUsed != 50 {
		t.Errorf("CargoUsed = %.0f, want 50 (unchanged)", st.Ship.CargoUsed)
	}
}
