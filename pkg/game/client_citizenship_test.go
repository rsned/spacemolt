package game

import (
	"context"
	"testing"
	"time"

	"github.com/rsned/spacemolt/internal/protocol"
)

// TestCitizenship_ListTerminatesOnTheAck: the citizenship command is flagged
// x-is-mutation because three of its four actions are, but "list" is a plain
// query the server ack-terminates. Waiting for an action frame that never comes
// hangs the caller for the full timeout, which is what `citizenship` with no
// arguments did.
func TestCitizenship_ListTerminatesOnTheAck(t *testing.T) {
	c, sendCh := newSubmitTestClient(t)
	ctx := context.Background()

	done := make(chan error, 1)
	go func() { done <- c.Citizenship(ctx, "list", "") }()

	sent := <-sendCh
	if sent.Type != "citizenship" {
		t.Fatalf("sent type = %q", sent.Type)
	}
	if got := sent.Payload["action"]; got != "list" {
		t.Errorf("action = %v, want list", got)
	}
	if _, ok := sent.Payload["empire_id"]; ok {
		t.Error("empire_id sent for list; it is ignored by the server and must be omitted")
	}

	// Only the ack is ever dispatched — no action_result follows a query.
	c.router.dispatch(protocol.Response{
		Type: protocol.TypeOK, RequestID: sent.RequestID,
		Payload: map[string]any{"origin": "solarian"},
	})

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Citizenship(list): %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Citizenship(list) hung waiting for a terminal frame the server never sends")
	}
}

// TestCitizenship_ApplyWaitsForTheActionResult: apply is a real mutation that
// executes on the next tick, so returning at the ack would report success
// before the empire had decided anything.
func TestCitizenship_ApplyWaitsForTheActionResult(t *testing.T) {
	c, sendCh := newSubmitTestClient(t)
	ctx := context.Background()

	done := make(chan error, 1)
	go func() { done <- c.Citizenship(ctx, "apply", "outerrim") }()

	sent := <-sendCh
	if got := sent.Payload["empire_id"]; got != "outerrim" {
		t.Errorf("empire_id = %v, want outerrim", got)
	}

	c.router.dispatch(protocol.Response{
		Type: protocol.TypeOK, RequestID: sent.RequestID,
		Payload: map[string]any{"pending": true, "command": "citizenship"},
	})

	select {
	case err := <-done:
		t.Fatalf("apply returned at the ack (err=%v); it must wait for the result", err)
	case <-time.After(50 * time.Millisecond):
	}

	c.router.dispatch(protocol.Response{
		Type: protocol.TypeActionResult, RequestID: sent.RequestID,
		Payload: map[string]any{"command": "citizenship", "status": "pending"},
	})

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Citizenship(apply): %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("apply never terminated on its action_result")
	}
}
