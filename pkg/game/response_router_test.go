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
