package game

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// TestGeneratorIsTypedAndImplemented pins the generator command (v0.669.0,
// field generator feeding a station grid): both clients implement it and its
// response decodes into GeneratorResponse.
func TestGeneratorIsTypedAndImplemented(t *testing.T) {
	var (
		_ GameClient = (*Client)(nil)
		_ GameClient = (*MCPGameClient)(nil)
	)
	if got, want := actionResponseTypes["generator"], reflect.TypeOf(serverapi.GeneratorResponse{}); got != want {
		t.Fatalf("actionResponseTypes[generator] = %v, want %v", got, want)
	}

	raw := `{"connected":true,"base_id":"c976fa7df53c87f9616165d7d411b41a","output":12,"end_tick":2352500,
		"fuel_per_tick":1,"fuel_reserve":11,"fuel_burned":0,"power_delivered":0,"message":"Generator linked."}`
	var resp serverapi.GeneratorResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Connected || resp.Output != 12 || resp.EndTick != 2352500 || resp.FuelPerTick != 1 || resp.FuelReserve != 11 {
		t.Fatalf("decoded %+v", resp)
	}
}
