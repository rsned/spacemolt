package game

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rsned/spacemolt/internal/protocol"
)

// The server answers `attack` with two request_id-tagged frames: an immediate
// `ok` carrying pending=true, then — on the next tick — the real result, also a
// plain `ok` (never an action_result). Captured from craftsman-1 attacking a
// creature on 2026-08-08:
//
//	ok {"command":"attack","message":"Attack action pending...","pending":true}
//	ok {"action":"attack","kind":"npc","target":"crt_...","target_name":"Drift-Ray","target_type":"creature"}
//
// With the default terminateOnAction, neither frame is terminal: the first
// fills the single ackCh slot, the second is dropped ("dropped ack, full
// ackCh") AND pings pendingCh, which resets the deadline to SleepJumpMaxWait.
// Attack then blocks for five minutes — in play_as that is five minutes of no
// prompt, under execMu, while the battle runs on autopilot.
func TestAttackResolvesOnPlainOKTerminal(t *testing.T) {
	c, sendCh := newSubmitTestClient(t)
	ctx := context.Background()

	done := make(chan error, 1)
	go func() { done <- c.Attack(ctx, "crt_d439a40cf658db0487e1be6bbe26a215") }()

	sent := <-sendCh
	if sent.Type != "attack" {
		t.Fatalf("sent type = %q, want attack", sent.Type)
	}

	c.router.dispatch(protocol.Response{
		Type: protocol.TypeOK, RequestID: sent.RequestID,
		Payload: map[string]any{
			"command": "attack",
			"message": "Attack action pending. Will execute on next tick.",
			"pending": true,
		},
	})
	c.router.dispatch(protocol.Response{
		Type: protocol.TypeOK, RequestID: sent.RequestID,
		Payload: map[string]any{
			"action": "attack", "kind": "npc",
			"target":      "crt_d439a40cf658db0487e1be6bbe26a215",
			"target_name": "Drift-Ray", "target_type": "creature",
		},
	})

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Attack: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Attack never returned after its terminal ok frame — the caller " +
			"is stuck until the extended deadline expires")
	}
}

// A pending ack on its own is not a result. Attack must keep waiting rather
// than reporting success the moment the server says "queued", or the REPL
// returns to the prompt before the shot resolves.
func TestAttackDoesNotTerminateOnPendingAck(t *testing.T) {
	c, sendCh := newSubmitTestClient(t)
	ctx := t.Context()

	done := make(chan error, 1)
	go func() { done <- c.Attack(ctx, "crt_x") }()

	sent := <-sendCh
	c.router.dispatch(protocol.Response{
		Type: protocol.TypeOK, RequestID: sent.RequestID,
		Payload: map[string]any{"command": "attack", "pending": true},
	})

	select {
	case err := <-done:
		t.Fatalf("Attack returned on the pending ack alone (err=%v)", err)
	case <-time.After(100 * time.Millisecond):
	}
}

// An out-of-range refusal is an error frame and must surface as one, with the
// server's code intact so callers can branch on it (out_of_range means
// "advance to close the distance", not "the attack failed").
func TestAttackSurfacesServerError(t *testing.T) {
	c, sendCh := newSubmitTestClient(t)
	ctx := context.Background()

	done := make(chan error, 1)
	go func() { done <- c.Attack(ctx, "crt_x") }()

	sent := <-sendCh
	c.router.dispatch(protocol.Response{
		Type: protocol.TypeError, RequestID: sent.RequestID,
		Payload: map[string]any{
			"code":    "out_of_range",
			"message": "Your weapons can't reach the enemy at this range — 'advance' to close the distance.",
		},
	})

	select {
	case err := <-done:
		var se *ServerError
		if !errors.As(err, &se) {
			t.Fatalf("Attack error = %v, want a *ServerError", err)
		}
		if se.Code != "out_of_range" {
			t.Errorf("error code = %q, want out_of_range", se.Code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Attack never returned after an error frame")
	}
}
