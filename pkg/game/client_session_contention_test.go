package game

import (
	"context"
	"fmt"
	"log"
	"io"
	"sync/atomic"
	"testing"
	"time"
)

// newTestClientForContentionTests returns a Client with just enough state
// to not panic if attemptReconnection happens to race past our cancelled
// context: discard logger and initialized goroutineCancel.
func newTestClientForContentionTests() *Client {
	ctx, cancel := context.WithCancel(context.Background())
	return &Client{
		debugLogger:     log.New(io.Discard, "", 0),
		goroutineCtx:    ctx,
		goroutineCancel: cancel,
	}
}

// TestReconnectingHandler_SessionContentionTripsAfterShortLivedConnects
// verifies the heuristic: when SessionContentionMaxShortLived consecutive
// OnDisconnected events fire with the client's Uptime() below
// SessionContentionMinUptime, onSessionContention is invoked instead of
// scheduling another reconnect. Uses SetOnSessionContention to avoid the
// default os.Exit path, and a pre-cancelled ctx so the pre-threshold
// reconnect goroutine exits without attempting any real network I/O.
func TestReconnectingHandler_SessionContentionTripsAfterShortLivedConnects(t *testing.T) {
	client := newTestClientForContentionTests()
	client.connectTime = time.Now().Add(-1 * time.Second) // pretend uptime of 1s (< threshold)

	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	r := NewReconnectingHandler(client, nil, cancelledCtx,
		log.New(io.Discard, "", 0))

	var trips atomic.Int32
	var lastUptime atomic.Int64
	r.SetOnSessionContention(func(n int, u time.Duration) {
		trips.Add(1)
		lastUptime.Store(int64(u))
	})

	// SessionContentionMaxShortLived consecutive short-lived drops should trip.
	for range SessionContentionMaxShortLived {
		// After the first OnDisconnected returns, reconnecting is true and
		// no goroutine runs because the default path wasn't taken; we must
		// reset it so the next call is allowed to re-enter.
		r.reconnecting.Store(false)
		r.OnDisconnected(fmt.Errorf("simulated close"))
	}

	// Any background reconnect goroutine spawned pre-threshold should have
	// returned immediately on the cancelled ctx; wait for it so the test
	// binary doesn't carry a dangling goroutine into later tests.
	r.wg.Wait()

	if got := trips.Load(); got != 1 {
		t.Errorf("onSessionContention called %d times, want 1", got)
	}
	if lastUptime.Load() == 0 {
		t.Error("lastUptime was zero; expected non-zero snapshot of Uptime()")
	}
}

// TestReconnectingHandler_LongLivedConnectResetsShortCount verifies that a
// reconnect which survives beyond SessionContentionMinUptime clears the
// running short-lived counter.
func TestReconnectingHandler_LongLivedConnectResetsShortCount(t *testing.T) {
	client := newTestClientForContentionTests()

	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	r := NewReconnectingHandler(client, nil, cancelledCtx,
		log.New(io.Discard, "", 0))
	tripped := false
	r.SetOnSessionContention(func(int, time.Duration) { tripped = true })

	// First disconnect: short-lived.
	client.connectTime = time.Now().Add(-1 * time.Second)
	r.reconnecting.Store(false)
	r.OnDisconnected(fmt.Errorf("kick 1"))
	if r.shortLivedCount != 1 {
		t.Fatalf("shortLivedCount after first short kick = %d, want 1", r.shortLivedCount)
	}

	// Second disconnect: long-lived — should reset the counter.
	client.connectTime = time.Now().Add(-10 * time.Minute)
	r.reconnecting.Store(false)
	r.OnDisconnected(fmt.Errorf("normal close"))
	r.wg.Wait()
	if r.shortLivedCount != 0 {
		t.Errorf("shortLivedCount after long-lived connect = %d, want 0", r.shortLivedCount)
	}
	if tripped {
		t.Error("onSessionContention fired after a long-lived reconnect; should not")
	}
}

// TestReconnectingHandler_BurstGivesUpThenWakesOnTrigger verifies the
// dormant-then-wake contract: a reconnection burst stops after
// reconnectMaxAttempts failures (it does NOT spin forever), and a later
// TriggerReconnect starts a fresh burst that can succeed.
func TestReconnectingHandler_BurstGivesUpThenWakesOnTrigger(t *testing.T) {
	client := newTestClientForContentionTests()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := NewReconnectingHandler(client, nil, ctx, log.New(io.Discard, "", 0))
	r.reconnectBackoffUnit = time.Millisecond // keep the test fast

	var attempts atomic.Int32
	var fail atomic.Bool
	fail.Store(true)
	r.reconnectFn = func(context.Context) error {
		attempts.Add(1)
		if fail.Load() {
			return fmt.Errorf("simulated dial failure")
		}
		return nil
	}

	// First burst: every attempt fails, so it gives up after reconnectMaxAttempts
	// and goes dormant (does not loop forever).
	r.wg.Add(1)
	r.attemptReconnection()
	if got := attempts.Load(); got != reconnectMaxAttempts {
		t.Fatalf("first burst made %d attempts, want %d (bounded give-up)", got, reconnectMaxAttempts)
	}
	if r.reconnecting.Load() {
		t.Fatal("reconnecting flag still set after a burst gave up")
	}

	// Connectivity returns; a user command triggers a fresh burst that succeeds.
	fail.Store(false)
	if !r.TriggerReconnect() {
		t.Fatal("TriggerReconnect returned false on a dormant, disconnected handler")
	}
	r.wg.Wait()
	if got := attempts.Load(); got != reconnectMaxAttempts+1 {
		t.Errorf("after wake, total attempts = %d, want %d (one more, succeeding)", got, reconnectMaxAttempts+1)
	}
}

// TestReconnectingHandler_TriggerReconnectNoOpWhenConnected verifies the guard:
// TriggerReconnect does nothing when the client reports connected.
func TestReconnectingHandler_TriggerReconnectNoOpWhenConnected(t *testing.T) {
	client := newTestClientForContentionTests()
	client.mu.Lock()
	client.connected = true
	client.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := NewReconnectingHandler(client, nil, ctx, log.New(io.Discard, "", 0))

	called := false
	r.reconnectFn = func(context.Context) error { called = true; return nil }
	if r.TriggerReconnect() {
		t.Error("TriggerReconnect started a burst while connected; want no-op")
	}
	r.wg.Wait()
	if called {
		t.Error("reconnectFn was called while connected")
	}
}

// TestClient_UptimeZeroWhenNeverConnected pins the Client.Uptime() contract:
// returns zero when connectTime is zero value, otherwise time since connect.
func TestClient_UptimeZeroWhenNeverConnected(t *testing.T) {
	c := &Client{}
	if got := c.Uptime(); got != 0 {
		t.Errorf("Uptime() with zero connectTime = %v, want 0", got)
	}

	c.connectTime = time.Now().Add(-500 * time.Millisecond)
	got := c.Uptime()
	if got < 400*time.Millisecond || got > 2*time.Second {
		t.Errorf("Uptime() ~500ms ago = %v, out of expected range", got)
	}
}
