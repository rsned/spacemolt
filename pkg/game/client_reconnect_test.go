package game

import (
	"context"
	"errors"
	"io"
	"log"
	"sync/atomic"
	"testing"
	"time"
)

// newDisplacedTestHandler builds a handler over a displaced client whose
// reconnectFn counts calls instead of dialing.
func newDisplacedTestHandler(t *testing.T) (*ReconnectingHandler, *atomic.Int32) {
	t.Helper()
	client := newTestClientForContentionTests()
	client.connectTime = time.Now().Add(-1 * time.Second) // short-lived uptime
	client.displaced.Store(true)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	r := NewReconnectingHandler(client, nil, ctx, log.New(io.Discard, "", 0))
	r.displacedRetryDelay = 10 * time.Millisecond

	var calls atomic.Int32
	r.reconnectFn = func(context.Context) error {
		calls.Add(1)
		return errors.New("slot still taken")
	}
	return r, &calls
}

func TestReconnectingHandler_DisplacedDoesNotReconnectImmediately(t *testing.T) {
	r, calls := newDisplacedTestHandler(t)
	r.OnDisconnected(errors.New("4001"))
	time.Sleep(5 * time.Millisecond)
	if got := calls.Load(); got != 0 {
		t.Errorf("reconnectFn called %d times immediately after displacement, want 0", got)
	}
	r.WaitForShutdown(time.Second)
}

func TestReconnectingHandler_DisplacedRetriesOnceAfterDelay(t *testing.T) {
	r, calls := newDisplacedTestHandler(t)
	r.OnDisconnected(errors.New("4001"))
	if !r.WaitForShutdown(time.Second) {
		t.Fatal("displaced timer did not finish")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("reconnectFn called %d times, want exactly 1", got)
	}
	if r.client.Displaced() {
		t.Error("Displaced() still true after the retry attempt")
	}
	if r.reconnecting.Load() {
		t.Error("reconnecting guard left set after a failed attempt")
	}
}

func TestReconnectingHandler_TriggerReconnectNoopWhileDisplaced(t *testing.T) {
	r, calls := newDisplacedTestHandler(t)
	if r.TriggerReconnect() {
		t.Error("TriggerReconnect returned true while displaced")
	}
	time.Sleep(30 * time.Millisecond)
	if got := calls.Load(); got != 0 {
		t.Errorf("reconnectFn called %d times, want 0", got)
	}
}

func TestReconnectingHandler_DisplacedDoesNotCountAsContention(t *testing.T) {
	r, _ := newDisplacedTestHandler(t)
	var trips atomic.Int32
	r.SetOnSessionContention(func(int, time.Duration) { trips.Add(1) })

	for range SessionContentionMaxShortLived + 1 {
		r.client.displaced.Store(true)
		r.OnDisconnected(errors.New("4001"))
		r.WaitForShutdown(time.Second)
	}
	if got := trips.Load(); got != 0 {
		t.Errorf("onSessionContention called %d times for displaced closes, want 0", got)
	}
	if r.shortLivedCount != 0 {
		t.Errorf("shortLivedCount = %d, want 0", r.shortLivedCount)
	}
}
