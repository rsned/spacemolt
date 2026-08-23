package worker

import (
	"context"
	"io"
	"slices"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

// Every idle script opens with `refuel` and most close with `travel`/`dock`,
// and Run sent all three unconditionally. Measured 2026-08-23 across a 7-minute
// window, all fleets: 3,932 "Already docked", 1,071 "Already in target system",
// 976 "tank is already full" -- ~6,000 round-trips that changed nothing, at up
// to 156 in a single second. That is the load that trips the shared IP limiter.
//
// The skip is deliberately asymmetric with the routing gate: there, an unknown
// state must fail CLOSED because guessing wrong costs a ship. Here an unknown
// state must fail OPEN and send, because a wrongly-skipped dock strands a
// worker mid-loop while a wrongly-sent one costs a single wasted call.

func precondDispatch(st *game.State) (*WorkerDispatch, *fakeClient) {
	c := &fakeClient{state: st}
	return NewWorkerDispatch(c, nil, nil, io.Discard), c
}

func TestRunSkipsDockWhenAlreadyDocked(t *testing.T) {
	d, c := precondDispatch(&game.State{Doc: true})
	if err := d.Run(context.Background(), []string{"dock"}); err != nil {
		t.Fatalf("dock: %v", err)
	}
	if slices.Contains(c.calls, "dock") {
		t.Errorf("sent dock while already docked; calls=%v", c.calls)
	}
}

func TestRunSendsDockWhenUndocked(t *testing.T) {
	d, c := precondDispatch(&game.State{Doc: false})
	if err := d.Run(context.Background(), []string{"dock"}); err != nil {
		t.Fatalf("dock: %v", err)
	}
	if !slices.Contains(c.calls, "dock") {
		t.Errorf("did not send dock while undocked; calls=%v", c.calls)
	}
}

func TestRunSkipsUndockWhenAlreadyUndocked(t *testing.T) {
	d, c := precondDispatch(&game.State{Doc: false})
	if err := d.Run(context.Background(), []string{"undock"}); err != nil {
		t.Fatalf("undock: %v", err)
	}
	if slices.Contains(c.calls, "undock") {
		t.Errorf("sent undock while not docked; calls=%v", c.calls)
	}
}

func TestRunSkipsRefuelAtFullTank(t *testing.T) {
	st := &game.State{}
	st.Ship.Fuel, st.Ship.MaxFuel = 130, 130
	d, c := precondDispatch(st)
	if err := d.Run(context.Background(), []string{"refuel"}); err != nil {
		t.Fatalf("refuel: %v", err)
	}
	if slices.Contains(c.calls, "refuel") {
		t.Errorf("sent refuel at a full tank; calls=%v", c.calls)
	}
}

func TestRunSendsRefuelWhenTankHasRoom(t *testing.T) {
	st := &game.State{}
	st.Ship.Fuel, st.Ship.MaxFuel = 129, 130
	d, c := precondDispatch(st)
	if err := d.Run(context.Background(), []string{"refuel"}); err != nil {
		t.Fatalf("refuel: %v", err)
	}
	if !slices.Contains(c.calls, "refuel") {
		t.Errorf("skipped refuel with a unit of room; calls=%v", c.calls)
	}
}

func TestRunSkipsTravelWhenAlreadyAtPOI(t *testing.T) {
	d, c := precondDispatch(&game.State{CurrentPOI: "sol_central"})
	if err := d.Run(context.Background(), []string{"travel", "sol_central"}); err != nil {
		t.Fatalf("travel: %v", err)
	}
	if slices.Contains(c.calls, "travel:sol_central") {
		t.Errorf("sent travel to the POI we are standing on; calls=%v", c.calls)
	}
}

func TestRunSendsTravelToADifferentPOI(t *testing.T) {
	d, c := precondDispatch(&game.State{CurrentPOI: "sol_central"})
	if err := d.Run(context.Background(), []string{"travel", "neptune"}); err != nil {
		t.Fatalf("travel: %v", err)
	}
	if !slices.Contains(c.calls, "travel:neptune") {
		t.Errorf("did not send travel to a different POI; calls=%v", c.calls)
	}
}

// Fail-open cases: an unreadable state must never suppress the command.

func TestRunSendsEverythingWhenStateIsUnknown(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		tokens     []string
	}{
		{"dock", "dock", []string{"dock"}},
		{"undock", "undock", []string{"undock"}},
		{"refuel", "refuel", []string{"refuel"}},
		{"travel", "travel:neptune", []string{"travel", "neptune"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, c := precondDispatch(nil) // GetState() returns nil
			if err := d.Run(context.Background(), tc.tokens); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if !slices.Contains(c.calls, tc.want) {
				t.Errorf("nil state suppressed %s; calls=%v", tc.name, c.calls)
			}
		})
	}
}

func TestRunSendsRefuelWhenMaxFuelIsUnknown(t *testing.T) {
	// MaxFuel 0 means "not loaded yet", not "tank is size zero".
	st := &game.State{}
	st.Ship.Fuel, st.Ship.MaxFuel = 0, 0
	d, c := precondDispatch(st)
	if err := d.Run(context.Background(), []string{"refuel"}); err != nil {
		t.Fatalf("refuel: %v", err)
	}
	if !slices.Contains(c.calls, "refuel") {
		t.Errorf("unknown MaxFuel suppressed refuel; calls=%v", c.calls)
	}
}

func TestRunSendsTravelWhenCurrentPOIIsUnknown(t *testing.T) {
	d, c := precondDispatch(&game.State{CurrentPOI: ""})
	if err := d.Run(context.Background(), []string{"travel", "neptune"}); err != nil {
		t.Fatalf("travel: %v", err)
	}
	if !slices.Contains(c.calls, "travel:neptune") {
		t.Errorf("empty CurrentPOI suppressed travel; calls=%v", c.calls)
	}
}

func TestRunSendsTravelWhileInTransit(t *testing.T) {
	// Mid-travel the cached CurrentPOI can still read as the destination;
	// suppressing then would abandon the leg.
	d, c := precondDispatch(&game.State{CurrentPOI: "neptune", Traveling: true})
	if err := d.Run(context.Background(), []string{"travel", "neptune"}); err != nil {
		t.Fatalf("travel: %v", err)
	}
	if !slices.Contains(c.calls, "travel:neptune") {
		t.Errorf("in-transit state suppressed travel; calls=%v", c.calls)
	}
}
