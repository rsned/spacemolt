package worker

import (
	"context"
	"errors"
	"io"
	"slices"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

func autopilotFake() *fakeClient {
	return &fakeClient{
		// Fuel full so autopilotRefuelIfNeeded no-ops; empty cargo.
		state: &game.State{Fuel: 100, MaxFuel: 100},
		route: []game.RouteStep{
			{SystemID: "sys_a", Name: "Alpha"}, // current system, skipped
			{SystemID: "sys_b", Name: "Bravo"},
			{SystemID: "sys_c", Name: "Charlie"},
		},
	}
}

func TestAutopilotJumpsEachHopAndCaptures(t *testing.T) {
	f := autopilotFake()
	var captures int
	err := Autopilot(context.Background(), AutopilotDeps{
		Client:     f,
		Out:        io.Discard,
		OnWaypoint: func(ctx context.Context) error { captures++; return nil },
	}, "sys_c", "")
	if err != nil {
		t.Fatalf("Autopilot: %v", err)
	}
	// First route entry (current system) is skipped; two jumps follow.
	if !slices.Contains(f.calls, "jump:sys_b") || !slices.Contains(f.calls, "jump:sys_c") {
		t.Fatalf("expected jumps to sys_b and sys_c, got %v", f.calls)
	}
	if captures != 2 {
		t.Errorf("OnWaypoint called %d times, want 2 (one per arrival)", captures)
	}
	// No POI -> no final travel.
	for _, c := range f.calls {
		if len(c) >= 7 && c[:7] == "travel:" {
			t.Errorf("unexpected travel call %q with empty POI", c)
		}
	}
}

func TestAutopilotTravelsToPOIAfterJumps(t *testing.T) {
	f := autopilotFake()
	err := Autopilot(context.Background(), AutopilotDeps{Client: f, Out: io.Discard}, "sys_c", "trade_hub")
	if err != nil {
		t.Fatalf("Autopilot: %v", err)
	}
	if !slices.Contains(f.calls, "travel:trade_hub") {
		t.Errorf("expected final travel:trade_hub, got %v", f.calls)
	}
}

func TestAutopilotCaptureErrorIsNonFatal(t *testing.T) {
	f := autopilotFake()
	err := Autopilot(context.Background(), AutopilotDeps{
		Client:     f,
		Out:        io.Discard,
		OnWaypoint: func(ctx context.Context) error { return errors.New("kb down") },
	}, "sys_c", "")
	if err != nil {
		t.Fatalf("capture errors must be non-fatal, got %v", err)
	}
	if !slices.Contains(f.calls, "jump:sys_c") {
		t.Errorf("route should still complete despite capture errors, got %v", f.calls)
	}
}

func TestFormatDuration(t *testing.T) {
	cases := map[int]string{5: "5s", 60: "1m", 90: "1m 30s", 125: "2m 5s"}
	for secs, want := range cases {
		if got := FormatDuration(secs); got != want {
			t.Errorf("FormatDuration(%d) = %q, want %q", secs, got, want)
		}
	}
}
