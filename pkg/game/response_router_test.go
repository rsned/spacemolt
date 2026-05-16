package game

import (
	"sync"
	"testing"
	"time"

	"github.com/rsned/spacemolt/internal/protocol"
)

func TestRouter_RegisterUnregister(t *testing.T) {
	r := newResponseRouter()

	ch := make(chan protocol.Response, 1)
	sub := r.registerQuery(matchType(protocol.TypeOK), ch)
	if r.subCount() != 1 {
		t.Fatalf("expected 1 subscription, got %d", r.subCount())
	}
	r.unregister(sub)
	if r.subCount() != 0 {
		t.Fatalf("expected 0 subscriptions after unregister, got %d", r.subCount())
	}
}

func TestRouter_UnregisterMissingIsNoop(t *testing.T) {
	r := newResponseRouter()
	// Build a subscription that was never registered
	sub := &subscription{}
	r.unregister(sub) // must not panic
}

func TestRouter_Dispatch_PushFanout(t *testing.T) {
	r := newResponseRouter()
	got := []string{}
	var mu sync.Mutex
	record := func(label string) func(protocol.Response) {
		return func(resp protocol.Response) {
			mu.Lock()
			got = append(got, label)
			mu.Unlock()
		}
	}
	r.registerPush(matchType(protocol.TypeChatMessage), record("a"))
	r.registerPush(matchType(protocol.TypeChatMessage), record("b"))
	r.registerPush(matchType(protocol.TypeTick), record("tick-only"))

	r.dispatch(protocol.Response{Type: protocol.TypeChatMessage})

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 {
		t.Fatalf("expected 2 handlers to fire, got %d: %v", len(got), got)
	}
}

func TestRouter_Dispatch_QueryOneShot(t *testing.T) {
	r := newResponseRouter()
	ch := make(chan protocol.Response, 1)
	r.registerQuery(matchType(protocol.TypeOK), ch)

	r.dispatch(protocol.Response{Type: protocol.TypeOK})
	select {
	case resp := <-ch:
		if resp.Type != protocol.TypeOK {
			t.Errorf("got type %q, want %q", resp.Type, protocol.TypeOK)
		}
	default:
		t.Fatal("expected response on channel")
	}

	// Second dispatch should not reach an already-consumed one-shot
	// (subscription was removed after first delivery).
	r.dispatch(protocol.Response{Type: protocol.TypeOK})
	if r.subCount() != 0 {
		t.Errorf("expected 0 subs after delivery, got %d", r.subCount())
	}
}

func TestRouter_Dispatch_QueryFIFO(t *testing.T) {
	r := newResponseRouter()
	ch1 := make(chan protocol.Response, 1)
	ch2 := make(chan protocol.Response, 1)
	// Register in order: ch1 first.
	r.registerQuery(matchType(protocol.TypeOK), ch1)
	time.Sleep(time.Millisecond) // ensure distinct timestamps
	r.registerQuery(matchType(protocol.TypeOK), ch2)

	r.dispatch(protocol.Response{Type: protocol.TypeOK, Payload: map[string]any{"n": 1}})
	r.dispatch(protocol.Response{Type: protocol.TypeOK, Payload: map[string]any{"n": 2}})

	r1 := <-ch1
	r2 := <-ch2
	if r1.Payload["n"].(int) != 1 || r2.Payload["n"].(int) != 2 {
		t.Errorf("FIFO violated: ch1=%v ch2=%v", r1.Payload["n"], r2.Payload["n"])
	}
}

func TestRouter_Dispatch_MutationTerminates(t *testing.T) {
	r := newResponseRouter()
	ch := make(chan protocol.Response, 1)
	r.registerMutation(matchCommand("deposit_items"), terminateOnAction, ch)

	// Pending ok with matching command → classifier matches, terminator fails → no delivery
	r.dispatch(protocol.Response{
		Type:    protocol.TypeOK,
		Payload: map[string]any{"command": "deposit_items", "pending": true},
	})
	if r.subCount() != 1 {
		t.Fatalf("mutation subscription should still be live, have %d", r.subCount())
	}

	// Terminal action_result with matching command → delivers and unregisters
	r.dispatch(protocol.Response{
		Type:    protocol.TypeActionResult,
		Payload: map[string]any{"command": "deposit_items"},
	})
	select {
	case resp := <-ch:
		if resp.Type != protocol.TypeActionResult {
			t.Errorf("got type %q", resp.Type)
		}
	default:
		t.Fatal("expected terminal on channel")
	}
	if r.subCount() != 0 {
		t.Errorf("expected mutation sub removed after terminal, have %d", r.subCount())
	}
}

func TestRouter_Dispatch_PanickyPushHandlerRecovers(t *testing.T) {
	r := newResponseRouter()
	var goodFired bool
	// Bad handler panics — must not abort dispatch of subsequent handlers.
	r.registerPush(matchType(protocol.TypeChatMessage), func(_ protocol.Response) {
		panic("boom")
	})
	r.registerPush(matchType(protocol.TypeChatMessage), func(_ protocol.Response) {
		goodFired = true
	})

	// Should not panic.
	r.dispatch(protocol.Response{Type: protocol.TypeChatMessage})
	if !goodFired {
		t.Error("expected second handler to fire after first panicked")
	}
}

func TestRouter_Dispatch_PanickyTerminatorRecovers(t *testing.T) {
	r := newResponseRouter()
	ch := make(chan protocol.Response, 1)
	// Terminator that panics on every call. The router must recover and
	// continue running (treating the panic as "not done").
	panicTerm := func(_ protocol.Response) (bool, error) {
		panic("terminator boom")
	}
	r.registerMutation(matchCommand("deposit_items"), panicTerm, ch)

	// Should not panic; should not deliver to ch (terminator never returns done).
	r.dispatch(protocol.Response{
		Type:    protocol.TypeActionResult,
		Payload: map[string]any{"command": "deposit_items"},
	})

	select {
	case <-ch:
		t.Fatal("unexpected delivery to mutation channel after terminator panic")
	default:
	}
	if r.subCount() != 1 {
		t.Errorf("expected mutation sub still live after terminator panic, have %d", r.subCount())
	}
}

func TestRouter_Dispatch_ByRequestID_Hit(t *testing.T) {
	r := newResponseRouter()
	ch := make(chan protocol.Response, 1)
	r.registerByID("req-7", ch, nil)

	r.dispatch(protocol.Response{Type: "ok", RequestID: "req-7"})

	select {
	case resp := <-ch:
		if resp.RequestID != "req-7" {
			t.Errorf("got RequestID=%q, want req-7", resp.RequestID)
		}
	case <-time.After(50 * time.Millisecond):
		t.Fatal("response not delivered")
	}
}

func TestRouter_Dispatch_ByRequestID_OrphanCounted(t *testing.T) {
	r := newResponseRouter()
	orphans := r.orphans()

	r.dispatch(protocol.Response{Type: "ok", RequestID: "no-such-id"})

	if got := orphans.Count(); got != 1 {
		t.Errorf("orphan count = %d, want 1", got)
	}
}

func TestRouter_Dispatch_UntaggedFallsThroughToPush(t *testing.T) {
	r := newResponseRouter()
	got := make(chan string, 1)
	r.registerPush(matchType(protocol.TypeChatMessage), func(resp protocol.Response) {
		got <- "fired"
	})
	r.dispatch(protocol.Response{Type: protocol.TypeChatMessage}) // no request_id

	select {
	case <-got:
	case <-time.After(50 * time.Millisecond):
		t.Fatal("push handler did not fire")
	}
}

func TestRouter_Dispatch_TaggedDoesNotFallThrough(t *testing.T) {
	r := newResponseRouter()
	got := make(chan string, 1)
	r.registerPush(matchType(protocol.TypeChatMessage), func(resp protocol.Response) {
		got <- "fired"
	})
	// Tagged + unknown ID -> orphan, must NOT fall through to push.
	r.dispatch(protocol.Response{Type: protocol.TypeChatMessage, RequestID: "tagged-but-unknown"})

	select {
	case <-got:
		t.Fatal("push handler must not fire for tagged-but-unknown frame")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestRouter_Dispatch_ByRequestID_TerminatorIntermediate(t *testing.T) {
	r := newResponseRouter()
	ch := make(chan protocol.Response, 1)
	term := func(resp protocol.Response) (bool, error) {
		return resp.Type == protocol.TypeActionResult, nil
	}
	r.registerByID("req-9", ch, term)

	// Intermediate pending ack: must NOT close out, must NOT deliver to ch.
	r.dispatch(protocol.Response{Type: protocol.TypeOK, RequestID: "req-9",
		Payload: map[string]any{"pending": true}})
	select {
	case <-ch:
		t.Fatal("intermediate must not deliver to result chan")
	case <-time.After(20 * time.Millisecond):
	}

	// Terminal frame: must deliver and unregister.
	r.dispatch(protocol.Response{Type: protocol.TypeActionResult, RequestID: "req-9"})
	select {
	case <-ch:
	case <-time.After(50 * time.Millisecond):
		t.Fatal("terminal not delivered")
	}
	if r.subCount() != 0 {
		t.Errorf("subscription not cleaned up: subCount=%d", r.subCount())
	}
}
