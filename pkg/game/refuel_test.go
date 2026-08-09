package game

import "testing"

// A station refuel reply carries `fuel` = the number of units ADDED plus the
// `cost`, and no `fuel_now`. parseActionResult assigned that delta straight
// into Ship.Fuel as though it were the new tank total, so a full tank read as
// nearly empty.
//
// Captured live 2026-08-09, fighter-4 at grand_exchange_station: tank went
// 184 -> 240 (max 240), the reply said "56 units for 392 credits", and the
// play_as status bar then rendered 56/240 = 23% in warning yellow while
// get_ship reported fuel 240, max_fuel 240.
//
// A cosmetic bar is the least of it: anything gating on a low-fuel reading
// sees a false empty tank right after a successful refuel, which is the input
// to rescue enqueueing.
func TestParseActionResult_StationRefuelAddsDelta(t *testing.T) {
	client := NewClient("wss://test.example.com", "testuser", "testtoken", nil)
	client.SetDebugLogging(false)

	client.state.Ship.Fuel = 184
	client.state.Ship.MaxFuel = 240
	client.state.Fuel = 184

	client.parseActionResult(map[string]any{
		"command": "refuel",
		"tick":    float64(1569153),
		"result": map[string]any{
			"action": "refuel",
			"source": "station",
			"fuel":   float64(56),
			"cost":   float64(392),
		},
	})

	if got := client.GetState().Ship.Fuel; got != 240 {
		t.Errorf("Ship.Fuel = %.0f, want 240 (184 + 56 added)", got)
	}
	if got := client.GetState().Fuel; got != 240 {
		t.Errorf("state.Fuel = %.0f, want 240", got)
	}
}

// fuel_now is the authoritative tank total when the server sends it, and must
// win over any delta arithmetic.
func TestParseActionResult_RefuelFuelNowIsAuthoritative(t *testing.T) {
	client := NewClient("wss://test.example.com", "testuser", "testtoken", nil)
	client.SetDebugLogging(false)

	client.state.Ship.Fuel = 10
	client.state.Ship.MaxFuel = 240

	client.parseActionResult(map[string]any{
		"command": "refuel",
		"result": map[string]any{
			"action":   "refuel",
			"fuel":     float64(56),
			"fuel_now": float64(200),
			"fuel_max": float64(260),
		},
	})

	st := client.GetState()
	if st.Ship.Fuel != 200 {
		t.Errorf("Ship.Fuel = %.0f, want 200 from fuel_now (not 10+56)", st.Ship.Fuel)
	}
	// fuel_max is the server telling us the tank size; a stale cached max is
	// what makes a correct fuel value render as the wrong percentage.
	if st.Ship.MaxFuel != 260 {
		t.Errorf("Ship.MaxFuel = %.0f, want 260 from fuel_max", st.Ship.MaxFuel)
	}
}

// Refuelling tops up; it never overfills. Clamping keeps a delta-plus-rounding
// disagreement from rendering as an over-full tank.
func TestParseActionResult_RefuelClampsToMax(t *testing.T) {
	client := NewClient("wss://test.example.com", "testuser", "testtoken", nil)
	client.SetDebugLogging(false)

	client.state.Ship.Fuel = 220
	client.state.Ship.MaxFuel = 240

	client.parseActionResult(map[string]any{
		"command": "refuel",
		"result": map[string]any{"action": "refuel", "fuel": float64(56)},
	})

	if got := client.GetState().Ship.Fuel; got != 240 {
		t.Errorf("Ship.Fuel = %.0f, want it clamped to MaxFuel 240", got)
	}
}

// With no known tank size there is nothing to clamp against, and inventing one
// would be worse than leaving the sum alone.
func TestParseActionResult_RefuelUnknownMaxIsNotClamped(t *testing.T) {
	client := NewClient("wss://test.example.com", "testuser", "testtoken", nil)
	client.SetDebugLogging(false)

	client.state.Ship.Fuel = 20
	client.state.Ship.MaxFuel = 0

	client.parseActionResult(map[string]any{
		"command": "refuel",
		"result":  map[string]any{"action": "refuel", "fuel": float64(56)},
	})

	if got := client.GetState().Ship.Fuel; got != 76 {
		t.Errorf("Ship.Fuel = %.0f, want 76 (20+56, no clamp without a known max)", got)
	}
}
