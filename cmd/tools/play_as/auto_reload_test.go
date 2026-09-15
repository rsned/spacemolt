package main

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rsned/spacemolt/internal/protocol"
)

func TestIsOutOfAmmo(t *testing.T) {
	// The exact frame explorer-8 received once a tick with no visible effect.
	yes := protocol.Response{Type: protocol.TypeError, Payload: map[string]any{
		"code":    "out_of_ammo",
		"message": "Your weapons cannot fire — magazine empty! Use 'reload' with ammo from cargo.",
	}}
	if !isOutOfAmmo(yes) {
		t.Error("out_of_ammo error must be recognised")
	}

	// action_error carries the same codes.
	if !isOutOfAmmo(protocol.Response{Type: protocol.TypeActionError, Payload: map[string]any{"code": "out_of_ammo"}}) {
		t.Error("action_error out_of_ammo must be recognised")
	}

	for _, no := range []protocol.Response{
		{Type: protocol.TypeError, Payload: map[string]any{"code": "out_of_range"}},
		{Type: protocol.TypeError, Payload: map[string]any{"message": "magazine empty"}}, // message alone is not the contract
		{Type: protocol.TypeOK, Payload: map[string]any{"code": "out_of_ammo"}},
		{Type: protocol.TypeError},
	} {
		if isOutOfAmmo(no) {
			t.Errorf("must not match: %+v", no)
		}
	}
}

// The server repeats out_of_ammo every tick for as long as the magazine is
// dry, and a reload takes a tick of its own. Repeats must collapse into one
// in-flight reload rather than queueing a burst of them.
func TestAutoReloaderCoalescesRepeats(t *testing.T) {
	var calls int32
	release := make(chan struct{})
	ar := newAutoReloader(func(context.Context) error {
		atomic.AddInt32(&calls, 1)
		<-release
		return nil
	})
	ar.SetEnabled(true)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go ar.Run(ctx)

	frame := protocol.Response{Type: protocol.TypeError, Payload: map[string]any{"code": "out_of_ammo"}}
	for range 5 {
		ar.Notify(frame)
	}

	// One reload starts; the other four collapse into at most one follow-up.
	waitFor(t, func() bool { return atomic.LoadInt32(&calls) == 1 })
	close(release)

	if got := atomic.LoadInt32(&calls); got > 2 {
		t.Errorf("5 repeats produced %d reloads, want at most 2", got)
	}
}

func TestAutoReloaderIgnoresOtherFramesAndStaysOffWhenDisabled(t *testing.T) {
	var calls int32
	ar := newAutoReloader(func(context.Context) error {
		atomic.AddInt32(&calls, 1)
		return nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go ar.Run(ctx)

	ammo := protocol.Response{Type: protocol.TypeError, Payload: map[string]any{"code": "out_of_ammo"}}

	// Disabled: an out_of_ammo must not spend a tick.
	ar.SetEnabled(false)
	ar.Notify(ammo)

	// Enabled, but the frame is something else.
	ar.SetEnabled(true)
	ar.Notify(protocol.Response{Type: protocol.TypeError, Payload: map[string]any{"code": "out_of_range"}})

	time.Sleep(50 * time.Millisecond)
	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Errorf("want no reloads, got %d", got)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met within 2s")
}
