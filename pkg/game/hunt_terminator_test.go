package game

import (
	"context"
	"testing"
	"time"

	"github.com/rsned/spacemolt/internal/protocol"
)

// Hunt had the same defect Attack did, and the canary found it live.
//
// pirate-6 hunting a Belt-Grazer at khambalia_crystal_market on 2026-08-09:
// the engage landed, the server then emitted an out-of-reach action_error
// EVERY TICK for five minutes ("Your weapons can't reach the enemy at this
// range — 'advance' to close the distance"), and the command finally came back
// "timeout waiting for hunt". The log carried the tell-tale
// "dropped ack (full ackCh)" one second in.
//
// Under the default terminateOnAction the pending ok fills the single ackCh
// slot, the real frame is dropped against it, and every one of those
// action_errors pings pendingCh, re-arming the deadline to SleepJumpMaxWait.
//
// It matters more here than for attack: the hunt executor calls Hunt() and
// only then enters its fight loop, so a five-minute block sits in front of
// every single engagement the fleet makes.
func TestHuntResolvesOnPlainOKTerminal(t *testing.T) {
	c, sendCh := newSubmitTestClient(t)
	ctx := context.Background()

	done := make(chan error, 1)
	go func() { done <- c.Hunt(ctx, "crt_3b8d73c7facf9cfb39586eec8180936d") }()

	sent := <-sendCh
	if sent.Type != "hunt" {
		t.Fatalf("sent type = %q, want hunt", sent.Type)
	}
	if got := sent.Payload["target_id"]; got != "crt_3b8d73c7facf9cfb39586eec8180936d" {
		t.Errorf("target_id = %v, want the creature id", got)
	}

	c.router.dispatch(protocol.Response{
		Type: protocol.TypeOK, RequestID: sent.RequestID,
		Payload: map[string]any{
			"command": "hunt",
			"message": "Hunt action pending. Will execute on next tick.",
			"pending": true,
		},
	})
	c.router.dispatch(protocol.Response{
		Type: protocol.TypeOK, RequestID: sent.RequestID,
		Payload: map[string]any{
			"action": "hunt", "kind": "creature",
			"target":      "crt_3b8d73c7facf9cfb39586eec8180936d",
			"target_name": "Belt-Grazer", "target_type": "creature",
		},
	})

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Hunt: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Hunt never returned after its terminal ok frame — the engage " +
			"blocks until the five-minute ceiling, in front of every fight")
	}
}

// The live capture's actual shape: the engage is followed by a stream of
// out-of-reach action_errors, because the server-side autopilot holds station
// at zone_distance 6 against max_weapon_reach 3 and never advances. Each one
// must not extend the deadline once the command has already resolved.
func TestHuntResolvesDespiteOutOfRangeErrors(t *testing.T) {
	c, sendCh := newSubmitTestClient(t)
	ctx := context.Background()

	done := make(chan error, 1)
	go func() { done <- c.Hunt(ctx, "crt_x") }()

	sent := <-sendCh
	c.router.dispatch(protocol.Response{
		Type: protocol.TypeOK, RequestID: sent.RequestID,
		Payload: map[string]any{"command": "hunt", "pending": true},
	})
	c.router.dispatch(protocol.Response{
		Type: protocol.TypeOK, RequestID: sent.RequestID,
		Payload: map[string]any{"action": "hunt", "target": "crt_x"},
	})

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Hunt: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Hunt did not resolve on its terminal frame")
	}
}
