package main

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/overmind/control"
)

// recordSender captures the Status frames the boot heartbeat emits.
type recordSender struct {
	mu   sync.Mutex
	sent []control.Status
	err  error
}

func (r *recordSender) send(s control.Status) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = append(r.sent, s)
	return r.err
}

func (r *recordSender) snapshot() []control.Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]control.Status, len(r.sent))
	copy(out, r.sent)
	return out
}

// A worker blocked in InitializeAgent used to emit nothing at all: hello landed
// at Step 4 and the heartbeat loop did not start until Step 8, after login. The
// supervisor saw an established worker go silent, and its 90s SilenceTimeout
// killed the process mid-login -- "initialize agent: context canceled" -- then
// respawned it, forcing another fresh login into the very rate block that was
// stalling the first one. This heartbeat is what stops that loop.
func TestBootHeartbeatEmitsWhileConnecting(t *testing.T) {
	r := &recordSender{}
	stop := startBootHeartbeat(context.Background(), r.send, time.Millisecond, nil)
	defer stop()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(r.snapshot()) >= 3 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	got := r.snapshot()
	if len(got) < 3 {
		t.Fatalf("boot heartbeat emitted %d frames, want >= 3 -- a worker slow to log in must keep reporting in", len(got))
	}
}

// The frame must report the game connection as DOWN. That is what routes the
// supervisor into its DisconnectGrace branch (30min, "leave it to the reconnect
// gate") instead of the 90s silence kill. A frame that claimed to be connected
// would be worse than none.
func TestBootHeartbeatReportsDisconnected(t *testing.T) {
	r := &recordSender{}
	stop := startBootHeartbeat(context.Background(), r.send, time.Millisecond, nil)
	defer stop()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(r.snapshot()) == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	got := r.snapshot()
	if len(got) == 0 {
		t.Fatal("no boot heartbeat frame emitted")
	}
	for i, s := range got {
		if !s.Disconnected {
			t.Errorf("frame %d: Disconnected = false, want true", i)
		}
		if s.Activity == "" {
			t.Errorf("frame %d: Activity is empty; the fleet table needs to show why this worker has no state", i)
		}
		if s.Timestamp == "" {
			t.Errorf("frame %d: Timestamp is empty", i)
		}
	}
}

// stop must be synchronous: once it returns, the goroutine is done and cannot
// race a later frame onto the encoder that the real heartbeat loop now owns.
func TestBootHeartbeatStopIsSynchronous(t *testing.T) {
	r := &recordSender{}
	stop := startBootHeartbeat(context.Background(), r.send, time.Millisecond, nil)
	time.Sleep(20 * time.Millisecond)
	stop()
	after := len(r.snapshot())
	time.Sleep(30 * time.Millisecond)
	if now := len(r.snapshot()); now != after {
		t.Fatalf("frames kept arriving after stop() returned: %d -> %d", after, now)
	}
}

// A send failure must not kill the worker mid-login; it is reported and the
// loop continues.
func TestBootHeartbeatSurvivesSendError(t *testing.T) {
	r := &recordSender{err: errors.New("broken pipe")}
	var errCount int
	var mu sync.Mutex
	stop := startBootHeartbeat(context.Background(), r.send, time.Millisecond, func(error) {
		mu.Lock()
		errCount++
		mu.Unlock()
	})
	defer stop()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := errCount
		mu.Unlock()
		if n >= 2 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("send errors did not keep being reported; the loop stopped on the first failure")
}
