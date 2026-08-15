package worker

import (
	"context"
	"io"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/market"
	"github.com/rsned/spacemolt/pkg/navigation"
)

// retankFixtures builds a KB (home—s1, plus a stronghold "hold" adjacent to
// home) and a market DB with a wet desk in s1 and a cheap desk in the
// stronghold that must be ignored.
func retankFixtures(t *testing.T) (knowledge.Base, *market.Collector) {
	t.Helper()
	kb := knowledge.NewMemoryKB()
	ctx := context.Background()
	systems := []knowledge.System{
		{ID: "home", Connections: []knowledge.SystemConnection{{SystemID: "s1"}, {SystemID: "hold"}}},
		{ID: "s1", Connections: []knowledge.SystemConnection{{SystemID: "home"}}},
		{ID: "hold", IsStronghold: true, Connections: []knowledge.SystemConnection{{SystemID: "home"}}},
	}
	for _, s := range systems {
		if err := kb.RememberSystem(ctx, s); err != nil {
			t.Fatalf("remember %s: %v", s.ID, err)
		}
	}

	mc, err := market.Open(market.Config{DBPath: filepath.Join(t.TempDir(), "m.db")})
	if err != nil {
		t.Fatalf("market open: %v", err)
	}
	t.Cleanup(func() { _ = mc.Close() })
	fresh := time.Now().UTC().Format(time.RFC3339)
	for _, s := range []struct{ station, system string }{
		{"wet_station", "s1"}, {"pirate_station", "hold"},
	} {
		if err := mc.UpsertStation(ctx, market.Station{StationID: s.station,
			StationName: s.station, SystemID: s.system, SystemName: s.system}); err != nil {
			t.Fatalf("seed station: %v", err)
		}
	}
	for _, r := range []market.StationFuel{
		{StationID: "wet_station", FuelPriceAllIn: 9, CapturedAt: fresh,
			FuelReserve: 8000, FuelCapacity: 9000, ReserveObservedAt: fresh},
		{StationID: "pirate_station", FuelPriceAllIn: 1, CapturedAt: fresh,
			FuelReserve: 8000, FuelCapacity: 9000, ReserveObservedAt: fresh},
	} {
		if err := mc.UpsertStationFuel(ctx, r); err != nil {
			t.Fatalf("seed fuel: %v", err)
		}
	}

	return kb, mc
}

func TestAssistRetankElsewhereFliesToWetDesk(t *testing.T) {
	kb, mc := retankFixtures(t)
	client := &fakeClient{state: &game.State{Fuel: 100, MaxFuel: 1500}}
	client.state.System.ID = "home"
	client.state.CurrentPOI = "home_station"

	var visited []string
	deps := AssistDeps{
		Client: client, KB: kb, Market: mc, Out: io.Discard,
		AgentID: "assist-t", HomeStation: "home_station",
		Navigate: func(ctx context.Context, system, poi string) error {
			visited = append(visited, system+"/"+poi)
			return nil
		},
	}
	assistRetankElsewhere(context.Background(), deps, "home")

	// Must fly to the wet desk in s1, never to the cheaper stronghold desk.
	if len(visited) != 1 || visited[0] != "s1/wet_station" {
		t.Fatalf("visited = %v, want [s1/wet_station]", visited)
	}
	if !slices.Contains(client.calls, "refuel") {
		t.Fatalf("must refuel at the destination desk, calls=%v", client.calls)
	}

	// The dry home desk observation must be recorded as a measured zero.
	stops, err := mc.NearestFuel(context.Background(), "home", 1,
		navigation.JumpGraph{"home": {"s1"}, "s1": {"home"}},
		nil, nil, time.Hour, time.Now())
	if err != nil {
		t.Fatalf("NearestFuel: %v", err)
	}
	for _, s := range stops {
		if s.StationID == "home_station" && s.KnownWet {
			t.Fatalf("home desk must be recorded dry, got %+v", s)
		}
	}
}

func TestAssistRetankSkipsWhenNearlyFull(t *testing.T) {
	kb, mc := retankFixtures(t)
	client := &fakeClient{state: &game.State{Fuel: 1400, MaxFuel: 1500}}
	client.state.System.ID = "home"

	var visited []string
	deps := AssistDeps{
		Client: client, KB: kb, Market: mc, Out: io.Discard,
		AgentID: "assist-t", HomeStation: "home_station",
		Navigate: func(ctx context.Context, system, poi string) error {
			visited = append(visited, system+"/"+poi)
			return nil
		},
	}
	assistRetankElsewhere(context.Background(), deps, "home")
	if len(visited) != 0 {
		t.Fatalf("a nearly-full tank must not travel, visited %v", visited)
	}
}
