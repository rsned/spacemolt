package game

import (
	"context"
	"testing"
	"time"

	"github.com/rsned/spacemolt/internal/protocol"
)

// pay_bounty (v0.564.0) settles an empire's outstanding bounty from anywhere —
// docked, in open space, or mid-jump — and releases an already-detained pilot
// immediately. That breaks the 0-credit spiral: an agent with a bounty could
// not buy fuel, and gifted credits were seized when it next entered territory.
// Seven mining agents were stranded on exactly that loop on 2026-08-27.

// The empire key must be OMITTED when the caller does not name one: the server
// only infers the target when you owe exactly one empire, and sending an empty
// string names an empire that does not exist.
func TestPayBounty_OmitsEmpireWhenUnset(t *testing.T) {
	c, sendCh := newSubmitTestClient(t)
	ctx := context.Background()

	done := make(chan error, 1)
	go func() { done <- c.PayBounty(ctx, "", "") }()

	sent := <-sendCh
	if sent.Type != "pay_bounty" {
		t.Fatalf("sent type = %q, want pay_bounty", sent.Type)
	}
	if _, ok := sent.Payload["empire"]; ok {
		t.Error("empire sent when unset; the server infers it only when the key is absent")
	}
	if _, ok := sent.Payload["source"]; ok {
		t.Error("source sent when unset; the server defaults it to self")
	}

	c.router.dispatch(protocol.Response{
		Type: protocol.TypeActionResult, RequestID: sent.RequestID,
		Payload: map[string]any{"action": "pay_bounty", "amount_paid": 5482},
	})
	if err := <-done; err != nil {
		t.Fatalf("PayBounty: %v", err)
	}
}

func TestPayBounty_SendsEmpireAndSource(t *testing.T) {
	c, sendCh := newSubmitTestClient(t)
	ctx := context.Background()

	done := make(chan error, 1)
	go func() { done <- c.PayBounty(ctx, "solarian", "faction") }()

	sent := <-sendCh
	if got := sent.Payload["empire"]; got != "solarian" {
		t.Errorf("empire = %v, want solarian", got)
	}
	if got := sent.Payload["source"]; got != "faction" {
		t.Errorf("source = %v, want faction", got)
	}

	c.router.dispatch(protocol.Response{
		Type: protocol.TypeActionResult, RequestID: sent.RequestID,
		Payload: map[string]any{"action": "pay_bounty"},
	})
	if err := <-done; err != nil {
		t.Fatalf("PayBounty: %v", err)
	}
}

// It is a mutation (1 per tick), so returning at the ack would report the debt
// cleared before the empire had taken the credits — and payment is
// all-or-nothing, so a premature success is a lie about the agent's state.
func TestPayBounty_WaitsForActionResult(t *testing.T) {
	c, sendCh := newSubmitTestClient(t)
	ctx := context.Background()

	done := make(chan error, 1)
	go func() { done <- c.PayBounty(ctx, "crimson", "self") }()

	sent := <-sendCh
	c.router.dispatch(protocol.Response{
		Type: protocol.TypeOK, RequestID: sent.RequestID,
		Payload: map[string]any{"pending": true, "command": "pay_bounty"},
	})

	select {
	case err := <-done:
		t.Fatalf("returned at the ack (err=%v); must wait for the action result", err)
	case <-time.After(50 * time.Millisecond):
	}

	c.router.dispatch(protocol.Response{
		Type: protocol.TypeActionResult, RequestID: sent.RequestID,
		Payload: map[string]any{"action": "pay_bounty", "released_from_detention": true},
	})
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("PayBounty: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("never terminated on its action_result")
	}
}
