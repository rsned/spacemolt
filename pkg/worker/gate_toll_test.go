package worker

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

// v0.720.0: find_route may cross a player-built jump gate, and the toll is
// charged on an ordinary jump. A ship that cannot pay must not leave.
func gateRouteFake(credits float64) *fakeClient {
	f := autopilotFake()
	f.state.Credits = credits
	f.route = []game.RouteStep{
		{SystemID: "sys_a", Name: "Alpha"},
		{SystemID: "sys_b", Name: "Bravo", ViaGate: true, GateToll: 400},
		{SystemID: "sys_c", Name: "Charlie", ViaGate: true, GateToll: 250},
	}
	return f
}

func TestRouteGateTolls(t *testing.T) {
	total, hops := routeGateTolls(gateRouteFake(0).route[1:])
	if total != 650 {
		t.Errorf("total toll = %d, want 650", total)
	}
	if want := []string{"Bravo (400 cr)", "Charlie (250 cr)"}; !slices.Equal(hops, want) {
		t.Errorf("hops = %v, want %v", hops, want)
	}
	// A free gate hop is still a gate hop; a lane hop is not.
	total, hops = routeGateTolls([]game.RouteStep{{Name: "Free", ViaGate: true}, {Name: "Lane"}})
	if total != 0 || !slices.Equal(hops, []string{"Free (free)"}) {
		t.Errorf("free gate: total %d hops %v", total, hops)
	}
}

func TestAutopilotRefusesUnaffordableGateToll(t *testing.T) {
	f := gateRouteFake(600) // 50 short of 650
	var out strings.Builder
	err := Autopilot(context.Background(), AutopilotDeps{Client: f, Out: &out}, "sys_c", "")
	if !errors.Is(err, ErrGateTollUnaffordable) {
		t.Fatalf("err = %v, want ErrGateTollUnaffordable", err)
	}
	for _, c := range f.calls {
		if strings.HasPrefix(c, "jump:") {
			t.Fatalf("jumped (%v) on a route it cannot pay for", f.calls)
		}
	}
	if !strings.Contains(out.String(), "NOT DEPARTING: gate tolls 650 cr, 600 credits") {
		t.Errorf("log = %q", out.String())
	}
}

func TestAutopilotPaysAffordableGateTollAndLogsEachHop(t *testing.T) {
	f := gateRouteFake(5000)
	var out strings.Builder
	if err := Autopilot(context.Background(), AutopilotDeps{Client: f, Out: &out}, "sys_c", ""); err != nil {
		t.Fatalf("Autopilot: %v", err)
	}
	if !slices.Contains(f.calls, "jump:sys_c") {
		t.Fatalf("calls = %v, want the gate jumps flown", f.calls)
	}
	for _, want := range []string{"gate toll: jumping to Bravo through a jump gate (400 cr)", "gate toll: jumping to Charlie through a jump gate (250 cr)"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("log missing %q", want)
		}
	}
}
