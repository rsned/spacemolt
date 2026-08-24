package worker

import (
	"context"
	"errors"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

type dockFake struct {
	game.GameClient // embedded; unimplemented methods panic if called
	err             error
	calls           int
}

func (d *dockFake) Dock(context.Context) error { d.calls++; return d.err }

// TestDockIdempotentTreatsAlreadyDockedAsSuccess pins the postcondition: the
// caller wants to BE docked, and "Already docked" means it is.
//
// Live 2026-08-23: mission_explore's return-dock treated that refusal as a
// failure and returned before missionComplete, so a finished exploration
// mission ("0 leg(s) remaining") could never retire. 22 agents across
// mission-learn and unlock looped on it at tick cadence -- 1,481 and 1,468
// "held for next pass" lines in a single log tail -- the same livelock that
// burned 47 hours on johnny_cab.
func TestDockIdempotentTreatsAlreadyDockedAsSuccess(t *testing.T) {
	ctx := t.Context()

	f := &dockFake{err: errors.New("Already docked")}
	if err := dockIdempotent(ctx, f); err != nil {
		t.Errorf("Already docked is the desired postcondition, want nil, got %v", err)
	}
	if f.calls != 1 {
		t.Errorf("want exactly one Dock call, got %d", f.calls)
	}

	// A server phrasing that embeds the reason must still be tolerated.
	f = &dockFake{err: errors.New("action_error: Already docked at sol_central")}
	if err := dockIdempotent(ctx, f); err != nil {
		t.Errorf("wrapped Already docked must be tolerated, got %v", err)
	}

	// Real failures must still propagate -- this must not become a blanket
	// "ignore dock errors", which would hide a genuinely refused dock.
	for _, msg := range []string{"docking refused: no permission", "not_docked", "ship destroyed"} {
		f = &dockFake{err: errors.New(msg)}
		if err := dockIdempotent(ctx, f); err == nil {
			t.Errorf("real dock failure %q must propagate, got nil", msg)
		}
	}

	// Success stays success.
	f = &dockFake{}
	if err := dockIdempotent(ctx, f); err != nil {
		t.Errorf("successful dock must return nil, got %v", err)
	}
}
