package game

import (
	"context"
	"fmt"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/rsned/spacemolt/internal/protocol"
)

// TestStress_ManyDistinctActionsResolveCorrectly submits a large number of
// requests across many distinct action types. Because each action has its
// own lock, all 1000 should be able to progress in parallel up to the
// inflight cap. With an auto-replier feeding TypeActionResult for each
// outbound message, every handle should resolve and the inflight counter
// should return to 0.
func TestStress_ManyDistinctActionsResolveCorrectly(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test under -short")
	}

	c, sendCh := newSubmitTestClient(t)
	ctx := context.Background()

	const (
		totalSubmits   = 1000
		distinctActs   = 50
		replierDoneTry = 2 * time.Second
	)

	// Auto-replier: for every sent message, dispatch a terminal
	// action_result under its request_id.
	replierDone := make(chan struct{})
	go func() {
		defer close(replierDone)
		for {
			select {
			case msg := <-sendCh:
				c.router.dispatch(protocol.Response{
					Type:      protocol.TypeActionResult,
					RequestID: msg.RequestID,
					Payload:   map[string]any{"command": msg.Type},
				})
			case <-time.After(replierDoneTry):
				return
			}
		}
	}()

	var wg sync.WaitGroup
	resolved := atomic.Int64{}
	failures := atomic.Int64{}

	for i := range totalSubmits {
		wg.Go(func() {
			action := fmt.Sprintf("act_%d", i%distinctActs)
			h, err := c.Submit(ctx, protocol.Message{Type: action})
			if err != nil {
				failures.Add(1)
				return
			}
			waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			if _, err := h.Result(waitCtx); err != nil {
				failures.Add(1)
				return
			}
			resolved.Add(1)
		})
	}
	wg.Wait()
	<-replierDone

	if got := failures.Load(); got != 0 {
		t.Errorf("failures = %d, want 0", got)
	}
	if got := resolved.Load(); got != totalSubmits {
		t.Errorf("resolved = %d, want %d", got, totalSubmits)
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if c.inflight.InFlight() == 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Errorf("inflight slot leak, count=%d", c.inflight.InFlight())
}

// TestStress_SameActionSerializes confirms that submits sharing a single
// action type cannot execute in parallel — the per-action ctxMutex must
// fully serialize them. Serialization is observed from the replier side:
// because the action lock gates Submit's Send call, the test sendCh
// should never witness two pending sends for the same action (Submit B
// cannot Send until A's reply has dispatched and A's cleanup released
// the lock).
func TestStress_SameActionSerializes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test under -short")
	}

	c, sendCh := newSubmitTestClient(t)
	ctx := context.Background()

	const submits = 100

	var (
		processing atomic.Int64
		peak       atomic.Int64
	)

	// Replier: for every send, spawn a goroutine that increments a
	// counter, sleeps to widen the window, dispatches the reply, then
	// decrements. Dispatching in a goroutine (not synchronously) is
	// critical: if the action lock is broken, multiple same-action
	// Submits can pass through sendCh and overlap in this window, so
	// peak will exceed 1. With the lock in place, Submit B cannot Send
	// until A's reply has dispatched and A's cleanup released the lock,
	// so peak must stay at 1.
	replierDone := make(chan struct{})
	go func() {
		defer close(replierDone)
		var workers sync.WaitGroup
		defer workers.Wait()
		for {
			select {
			case msg := <-sendCh:
				workers.Add(1)
				go func(m protocol.Message) {
					defer workers.Done()
					cur := processing.Add(1)
					for {
						p := peak.Load()
						if cur <= p || peak.CompareAndSwap(p, cur) {
							break
						}
					}
					time.Sleep(1 * time.Millisecond)
					// Decrement BEFORE dispatch: dispatching writes to
					// respCh, which unblocks runSubmit and triggers the
					// action-lock release. If we decremented AFTER
					// dispatch, the next Submit's increment could race
					// our decrement and we'd see false peak=2 even with
					// the lock working correctly.
					processing.Add(-1)
					c.router.dispatch(protocol.Response{
						Type:      protocol.TypeActionResult,
						RequestID: m.RequestID,
						Payload:   map[string]any{"command": m.Type},
					})
				}(msg)
			case <-time.After(2 * time.Second):
				return
			}
		}
	}()

	var (
		wg       sync.WaitGroup
		resolved atomic.Int64
	)

	for range submits {
		wg.Go(func() {
			h, err := c.Submit(ctx, protocol.Message{Type: "mine"})
			if err != nil {
				return
			}
			waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			if _, err := h.Result(waitCtx); err == nil {
				resolved.Add(1)
			}
		})
	}
	wg.Wait()
	<-replierDone

	if got := peak.Load(); got != 1 {
		t.Errorf("peak concurrent same-action processing = %d, want 1", got)
	}
	if got := resolved.Load(); got != submits {
		t.Errorf("resolved = %d, want %d", got, submits)
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if c.inflight.InFlight() == 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Errorf("inflight slot leak, count=%d", c.inflight.InFlight())
}

// TestStress_NoSlotLeakUnderRandomOutcomes exercises every terminal path:
// success, server error, action error, and timeout-by-drop. After all
// submits settle, the inflight semaphore MUST drain to 0 — any leak here
// would silently throttle the production client until restart.
func TestStress_NoSlotLeakUnderRandomOutcomes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test under -short")
	}

	c, sendCh := newSubmitTestClient(t)

	const (
		submits      = 500
		distinctActs = 20
	)

	// Outcome dispatcher: read each send and pick one of four fates.
	// On "drop" we deliberately do not dispatch; the WithTimeout on the
	// submit drives the cleanup.
	dispatcherDone := make(chan struct{})
	rng := rand.New(rand.NewPCG(0xc0ffee, 0xdeadbeef))
	go func() {
		defer close(dispatcherDone)
		for {
			select {
			case msg := <-sendCh:
				switch rng.IntN(4) {
				case 0:
					c.router.dispatch(protocol.Response{
						Type:      protocol.TypeActionResult,
						RequestID: msg.RequestID,
						Payload:   map[string]any{"command": msg.Type},
					})
				case 1:
					c.router.dispatch(protocol.Response{
						Type:      protocol.TypeError,
						RequestID: msg.RequestID,
						Payload:   map[string]any{"code": "boom", "message": "synthetic"},
					})
				case 2:
					c.router.dispatch(protocol.Response{
						Type:      protocol.TypeActionError,
						RequestID: msg.RequestID,
						Payload:   map[string]any{"code": "act_err", "message": "synthetic"},
					})
				default:
					// drop: let the timeout fire.
				}
			case <-time.After(2 * time.Second):
				return
			}
		}
	}()

	var wg sync.WaitGroup
	for i := range submits {
		wg.Go(func() {
			action := fmt.Sprintf("rand_act_%d", i%distinctActs)
			h, err := c.Submit(context.Background(),
				protocol.Message{Type: action},
				WithTimeout(50*time.Millisecond))
			if err != nil {
				return
			}
			callCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			// Ignore error: every outcome is acceptable, we only care
			// about resource release.
			_, _ = h.Result(callCtx)
		})
	}
	wg.Wait()
	<-dispatcherDone

	// Cooldown: allow any final cleanup goroutines to release slots.
	time.Sleep(200 * time.Millisecond)

	if got := c.inflight.InFlight(); got != 0 {
		t.Errorf("inflight slot leak after random outcomes, count=%d", got)
	}
}

// TestStress_ReplayMidLoad simulates rolling-restart pressure: while
// hundreds of submits are inflight, repeatedly trigger replayPending +
// drainPendingReplay (the real Reconnect flow). All handles should still
// resolve and inflight should drain to 0.
func TestStress_ReplayMidLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test under -short")
	}

	c, sendCh := newSubmitTestClient(t)
	ctx := context.Background()

	const (
		submits      = 200
		distinctActs = 10
	)

	// Auto-replier — replies to whatever request_id is currently on
	// the wire, including post-replay rekeyed ids.
	replierStop := make(chan struct{})
	replierDone := make(chan struct{})
	go func() {
		defer close(replierDone)
		for {
			select {
			case msg := <-sendCh:
				c.router.dispatch(protocol.Response{
					Type:      protocol.TypeActionResult,
					RequestID: msg.RequestID,
					Payload:   map[string]any{"command": msg.Type},
				})
			case <-replierStop:
				return
			}
		}
	}()

	// Replay storm: fires every 50ms until told to stop. Mirrors the
	// Reconnect cycle (stage + drain) in one shot.
	stormStop := make(chan struct{})
	stormDone := make(chan struct{})
	go func() {
		defer close(stormDone)
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				c.replayPending(&websocket.CloseError{
					Code:   websocket.StatusNormalClosure,
					Reason: "rolling restart",
				})
				c.drainPendingReplay(ctx)
			case <-stormStop:
				return
			}
		}
	}()

	var (
		wg       sync.WaitGroup
		resolved atomic.Int64
	)
	for i := range submits {
		wg.Go(func() {
			action := fmt.Sprintf("replay_act_%d", i%distinctActs)
			h, err := c.Submit(ctx, protocol.Message{Type: action})
			if err != nil {
				return
			}
			waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			if _, err := h.Result(waitCtx); err == nil {
				resolved.Add(1)
			}
		})
	}
	wg.Wait()

	close(stormStop)
	<-stormDone
	close(replierStop)
	<-replierDone

	if got := resolved.Load(); got != submits {
		t.Errorf("resolved = %d, want %d", got, submits)
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if c.inflight.InFlight() == 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Errorf("inflight slot leak after replay storm, count=%d", c.inflight.InFlight())
}
