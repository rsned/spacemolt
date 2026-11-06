package worker

import (
	"context"
	"io"
	"slices"
	"testing"

	"github.com/rsned/spacemolt/pkg/galaxy"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// strandedMap is the live shape: keep is a stronghold whose station is stored
// non-public, one jump from the station-less void; town is two jumps out.
func strandedMap(t *testing.T) (knowledge.Base, *galaxy.GalaxyGraph, []knowledge.System) {
	t.Helper()
	ctx := context.Background()
	kb := knowledge.NewMemoryKB()
	t.Cleanup(func() { _ = kb.Close() })

	systems := []knowledge.System{
		{ID: "void", Connections: []knowledge.SystemConnection{{SystemID: "keep", Distance: 1}, {SystemID: "mid", Distance: 1}}},
		{ID: "keep", IsStronghold: true, Connections: []knowledge.SystemConnection{{SystemID: "void", Distance: 1}}},
		{ID: "mid", Connections: []knowledge.SystemConnection{{SystemID: "void", Distance: 1}, {SystemID: "town", Distance: 1}}},
		{ID: "town", Connections: []knowledge.SystemConnection{{SystemID: "mid", Distance: 1}}},
	}
	for _, s := range systems {
		if err := kb.RememberSystem(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []struct {
		poi, system string
		public      bool
	}{{"keep-stn", "keep", false}, {"town-stn", "town", true}} {
		if err := kb.RememberPOI(ctx, knowledge.POI{ID: p.poi, SystemID: p.system, Type: "station"}); err != nil {
			t.Fatal(err)
		}
		if err := kb.RememberBase(ctx, knowledge.SpaceBase{ID: p.poi + "-base", POIID: p.poi, PublicAccess: p.public}); err != nil {
			t.Fatal(err)
		}
	}
	g := &galaxy.GalaxyGraph{}
	if err := g.BuildFromDB(ctx, kb); err != nil {
		t.Fatal(err)
	}

	return kb, g, systems
}

func strandedClient(system string, unlocked, docked bool) *fakeClient {
	st := &game.State{}
	if unlocked {
		st = unlockedState(10)
	}
	st.System.ID = system
	st.Doc = docked

	return &fakeClient{state: st, route: []game.RouteStep{{SystemID: system}}}
}

func TestStrandedUnlockedOffStationInStrongholdDocksAndRefuels(t *testing.T) {
	kb, g, systems := strandedMap(t)
	f := strandedClient("keep", true, false)

	rescued, err := haulRecoverIfStranded(context.Background(), HaulDeps{Client: f, KB: kb}, io.Discard, g, "keep", systems)
	if err != nil {
		t.Fatal(err)
	}
	if !rescued {
		t.Fatal("an off-station agent should end the pass after docking")
	}
	for _, want := range []string{"travel:keep-stn", "dock", "refuel"} {
		if !slices.Contains(f.calls, want) {
			t.Fatalf("missing %q in %v", want, f.calls)
		}
	}
}

func TestStrandedUnlockedDockedInStrongholdIsNotStranded(t *testing.T) {
	kb, g, systems := strandedMap(t)
	f := strandedClient("keep", true, true)

	rescued, err := haulRecoverIfStranded(context.Background(), HaulDeps{Client: f, KB: kb}, io.Discard, g, "keep", systems)
	if err != nil {
		t.Fatal(err)
	}
	if rescued || len(f.calls) != 0 {
		t.Fatalf("a docked agent must be left alone: rescued=%v calls=%v", rescued, f.calls)
	}
}

// The nearest usable station picks the relocation target, so the stronghold
// must win only for the agent that can dock there.
func TestStrandedRelocationTarget(t *testing.T) {
	for _, tc := range []struct {
		name     string
		unlocked bool
		want     string
	}{
		{"unlocked goes to the near stronghold", true, "keep"},
		{"locked goes around it", false, "town"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			kb, g, systems := strandedMap(t)
			f := strandedClient("void", tc.unlocked, false)

			rescued, err := haulRecoverIfStranded(context.Background(), HaulDeps{Client: f, KB: kb}, io.Discard, g, "void", systems)
			if err != nil {
				t.Fatal(err)
			}
			if !rescued {
				t.Fatal("a station-less agent must relocate")
			}
			if !slices.Contains(f.calls, "find_route:"+tc.want) {
				t.Fatalf("want a route to %s, got %v", tc.want, f.calls)
			}
		})
	}
}
