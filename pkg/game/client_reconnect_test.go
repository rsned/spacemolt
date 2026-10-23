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

// displacedTestHandler pairs a handler with the cancel of its context.
type displacedTestHandler struct {
	*ReconnectingHandler
	cancel context.CancelFunc
}

// newDisplacedTestHandler builds a handler over a displaced client whose
// reconnectFn counts calls instead of dialing.
func newDisplacedTestHandler(t *testing.T) (*displacedTestHandler, *atomic.Int32) {
	t.Helper()
	client := newTestClientForContentionTests()
	client.connectTime = time.Now().Add(-1 * time.Second) // short-lived uptime
	client.displaced.Store(true)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	r := &displacedTestHandler{
		ReconnectingHandler: NewReconnectingHandler(client, nil, ctx, log.New(io.Discard, "", 0)),
		cancel:              cancel,
	}
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
	r.displacedRetryDelay = time.Hour // never fires inside the test
	r.OnDisconnected(errors.New("4001"))
	if got := calls.Load(); got != 0 {
		t.Errorf("reconnectFn called %d times immediately after displacement, want 0", got)
	}
	r.cancel()
	if !r.WaitForShutdown(time.Second) {
		t.Fatal("parked timer did not exit on cancel")
	}
	if got := calls.Load(); got != 0 {
		t.Errorf("reconnectFn called %d times after cancel, want 0", got)
	}
}

func TestReconnectingHandler_DisplacedRetriesOnceAfterDelay(t *testing.T) {
	r, calls := newDisplacedTestHandler(t)
	start := time.Now()
	r.reconnectFn = func(context.Context) error {
		if time.Since(start) < r.displacedRetryDelay {
			t.Error("reconnectFn ran before the displaced delay elapsed")
		}
		calls.Add(1)
		return errors.New("slot still taken")
	}
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

// A 4001 that lands while the reconnecting guard is held loses the CAS in
// OnDisconnected; the exit step must re-park so the worker is not stranded.
func TestReconnectingHandler_DisplacedDuringParkedAttemptReparks(t *testing.T) {
	r, _ := newDisplacedTestHandler(t)
	var times []time.Time
	r.reconnectFn = func(context.Context) error {
		times = append(times, time.Now())
		if len(times) == 1 {
			r.client.displaced.Store(true)
			r.OnDisconnected(errors.New("4001")) // loses the CAS
		}
		return nil
	}
	r.OnDisconnected(errors.New("4001"))
	if !r.WaitForShutdown(2 * time.Second) {
		t.Fatal("handler did not settle")
	}
	if len(times) != 2 {
		t.Fatalf("reconnectFn called %d times, want 2", len(times))
	}
	if gap := times[1].Sub(times[0]); gap < r.displacedRetryDelay {
		t.Errorf("second attempt after %v, want >= %v", gap, r.displacedRetryDelay)
	}
}

// Same hole from a normal reconnect burst: a 4001 during the burst's login.
func TestReconnectingHandler_DisplacedDuringNormalBurstParks(t *testing.T) {
	r, _ := newDisplacedTestHandler(t)
	r.client.clearDisplaced()
	var times []time.Time
	r.reconnectFn = func(context.Context) error {
		times = append(times, time.Now())
		if len(times) == 1 {
			r.client.displaced.Store(true)
			r.OnDisconnected(errors.New("4001")) // loses the CAS
		}
		return nil
	}
	if !r.TriggerReconnect() {
		t.Fatal("TriggerReconnect did not start a burst")
	}
	if !r.WaitForShutdown(2 * time.Second) {
		t.Fatal("handler did not settle")
	}
	if len(times) != 2 {
		t.Fatalf("reconnectFn called %d times, want 2 (burst then parked retry)", len(times))
	}
	if gap := times[1].Sub(times[0]); gap < r.displacedRetryDelay {
		t.Errorf("parked retry after %v, want >= %v", gap, r.displacedRetryDelay)
	}
}

// A 4001 that lands during a failed burst attempt must end the burst: the
// remaining attempts would be extra gated dials inside one displaced interval.
func TestReconnectingHandler_BurstStopsWhenDisplaced(t *testing.T) {
	r, _ := newDisplacedTestHandler(t)
	r.client.clearDisplaced()
	var times []time.Time
	r.reconnectFn = func(context.Context) error {
		times = append(times, time.Now())
		if len(times) == 1 {
			r.client.displaced.Store(true) // 4001 arrived; its OnDisconnected lost the CAS
		}
		return errors.New("session replaced")
	}
	if !r.TriggerReconnect() {
		t.Fatal("TriggerReconnect did not start a burst")
	}
	if !r.WaitForShutdown(2 * time.Second) {
		t.Fatal("handler did not settle")
	}
	if len(times) != 2 {
		t.Fatalf("reconnectFn called %d times, want 2 (one burst try, then one parked retry)", len(times))
	}
	if gap := times[1].Sub(times[0]); gap < r.displacedRetryDelay {
		t.Errorf("second dial after %v, want >= %v (burst kept dialing)", gap, r.displacedRetryDelay)
	}
}

// An operator lease (play_as) holds the parked client: it must not dial while
// the hold reports true, and must dial exactly once after it clears.
func TestReconnectingHandler_DisplacedHoldSkipsDial(t *testing.T) {
	r, calls := newDisplacedTestHandler(t)
	var holds atomic.Int32
	r.SetDisplacedHold(func() bool {
		if calls.Load() != 0 {
			t.Error("reconnectFn ran while the hold was still being consulted")
		}
		return holds.Add(1) <= 2
	})
	start := time.Now()
	r.OnDisconnected(errors.New("4001"))
	if !r.WaitForShutdown(2 * time.Second) {
		t.Fatal("handler did not settle")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("reconnectFn called %d times, want exactly 1 after the hold cleared", got)
	}
	if got := holds.Load(); got != 3 {
		t.Errorf("hold consulted %d times, want 3 (two held intervals, then clear)", got)
	}
	if elapsed := time.Since(start); elapsed < 3*r.displacedRetryDelay {
		t.Errorf("dialed after %v, want >= 3 intervals (%v)", elapsed, 3*r.displacedRetryDelay)
	}
}

// play_as must not park: onDisplaced replaces parking entirely.
func TestReconnectingHandler_OnDisplacedOverridesParking(t *testing.T) {
	r, calls := newDisplacedTestHandler(t)
	var fired atomic.Int32
	r.SetOnDisplaced(func() { fired.Add(1) })
	r.OnDisconnected(errors.New("4001"))
	// A parked retry would hold a wg slot, so WaitForShutdown would see it dial.
	if !r.WaitForShutdown(time.Second) {
		t.Fatal("handler did not settle")
	}
	if got := fired.Load(); got != 1 {
		t.Errorf("onDisplaced fired %d times, want 1", got)
	}
	if got := calls.Load(); got != 0 {
		t.Errorf("reconnectFn called %d times, want 0 (client must not park)", got)
	}
	if r.reconnecting.Load() {
		t.Error("reconnecting guard left set after onDisplaced")
	}
}
