package worker

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// avoidKB: sol - tidewater - izar - rigel is the short way; tidewater - vega -
// rigel is the detour around izar.
func avoidKB(vegaStronghold bool, withDetour bool) *fakeKB {
	kb := &fakeKB{
		systems: []knowledge.System{
			{ID: "sol", Name: "Sol"}, {ID: "tidewater", Name: "Tidewater"},
			{ID: "izar", Name: "Izar"}, {ID: "rigel", Name: "Rigel"},
			{ID: "vega", Name: "Vega", IsStronghold: vegaStronghold},
		},
		conns: []knowledge.Connection{
			{FromSystem: "sol", ToSystem: "tidewater"},
			{FromSystem: "tidewater", ToSystem: "izar"},
			{FromSystem: "izar", ToSystem: "rigel"},
		},
	}
	if withDetour {
		kb.conns = append(kb.conns,
			knowledge.Connection{FromSystem: "tidewater", ToSystem: "vega"},
			knowledge.Connection{FromSystem: "vega", ToSystem: "rigel"})
	}
	return kb
}

func izarRouteFake() *fakeClient {
	return &fakeClient{
		state: &game.State{},
		route: []game.RouteStep{
			{SystemID: "sol", Name: "Sol"},
			{SystemID: "tidewater", Name: "Tidewater"},
			{SystemID: "izar", Name: "Izar"},
			{SystemID: "rigel", Name: "Rigel"},
		},
	}
}

func jumps(calls []string) []string {
	var out []string
	for _, c := range calls {
		if j, ok := strings.CutPrefix(c, "jump:"); ok {
			out = append(out, j)
		}
	}
	return out
}

// explorer-10 died 2026-11-13 flying THROUGH Izar on a 13-jump mission
// route: the Leviathan kills transit traffic, not just miners.
func TestAutopilotDetoursAroundAnAvoidedSystem(t *testing.T) {
	f := izarRouteFake()
	err := Autopilot(context.Background(), AutopilotDeps{
		Client: f, Out: io.Discard, KB: avoidKB(false, true),
		AvoidSystemsPath: writeAvoidSystems(t, `{"systems":{"izar":"Molt Leviathan"}}`),
	}, "rigel", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := jumps(f.calls); !slices.Equal(got, []string{"tidewater", "vega", "rigel"}) {
		t.Errorf("jumps = %v, want the detour tidewater, vega, rigel", got)
	}
}

func TestAutopilotRefusesWhenNoDetourExists(t *testing.T) {
	f := izarRouteFake()
	err := Autopilot(context.Background(), AutopilotDeps{
		Client: f, Out: io.Discard, KB: avoidKB(false, false),
		AvoidSystemsPath: writeAvoidSystems(t, `{"systems":{"izar":"Molt Leviathan"}}`),
	}, "rigel", "")
	if !errors.Is(err, ErrRouteThroughAvoided) {
		t.Fatalf("err = %v, want ErrRouteThroughAvoided", err)
	}
	if got := jumps(f.calls); len(got) != 0 {
		t.Errorf("refusal must leave the ship where it is, jumped %v", got)
	}
}

func TestAutopilotRefusesAnAvoidedDestination(t *testing.T) {
	f := izarRouteFake()
	f.route = f.route[:3]
	err := Autopilot(context.Background(), AutopilotDeps{
		Client: f, Out: io.Discard, KB: avoidKB(false, true),
		AvoidSystemsPath: writeAvoidSystems(t, `{"systems":{"izar":"Molt Leviathan"}}`),
	}, "izar", "")
	if !errors.Is(err, ErrRouteThroughAvoided) || len(jumps(f.calls)) != 0 {
		t.Fatalf("err = %v jumps %v, want refusal with no jumps", err, jumps(f.calls))
	}
}

// The detour is planned on our own map, so it must not trade the Leviathan
// for a pirate stronghold.
func TestAutopilotDetourNeverEntersAStronghold(t *testing.T) {
	f := izarRouteFake()
	err := Autopilot(context.Background(), AutopilotDeps{
		Client: f, Out: io.Discard, KB: avoidKB(true, true),
		AvoidSystemsPath: writeAvoidSystems(t, `{"systems":{"izar":"Molt Leviathan"}}`),
	}, "rigel", "")
	if !errors.Is(err, ErrRouteThroughAvoided) || len(jumps(f.calls)) != 0 {
		t.Fatalf("err = %v jumps %v, want refusal: the only detour is a stronghold", err, jumps(f.calls))
	}
}

func TestAutopilotFliesServerRouteWhenNothingAvoided(t *testing.T) {
	f := izarRouteFake()
	err := Autopilot(context.Background(), AutopilotDeps{
		Client: f, Out: io.Discard, KB: avoidKB(false, true),
		AvoidSystemsPath: writeAvoidSystems(t, `{"systems":{}}`),
	}, "rigel", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := jumps(f.calls); !slices.Equal(got, []string{"tidewater", "izar", "rigel"}) {
		t.Errorf("jumps = %v, want the server route", got)
	}
}
