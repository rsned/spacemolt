package game

import (
	"context"
	"testing"
	"time"

	"github.com/rsned/spacemolt/internal/protocol"
)

// TestRecycleWithOptionsPayload verifies the async-queued recycle payload:
// type=="recycle", recipe_id + quantity are present, deliver_to is OMITTED
// when empty. Uses newSubmitTestClient (submit_test.go) — same pattern as
// TestCraftWithOptionsPayload.
func TestRecycleWithOptionsPayload(t *testing.T) {
	c, sendCh := newSubmitTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- c.RecycleWithOptions(ctx, "basic_iron_smelting", 20, "")
	}()

	var sent protocol.Message
	select {
	case sent = <-sendCh:
	case <-ctx.Done():
		t.Fatal("timeout waiting for send — RecycleWithOptions likely rejected before sending")
	}

	if sent.Type != "recycle" {
		t.Fatalf("type = %q, want %q", sent.Type, "recycle")
	}
	if got := sent.Payload["recipe_id"]; got != "basic_iron_smelting" {
		t.Fatalf("recipe_id = %v, want %q", got, "basic_iron_smelting")
	}
	if got := sent.Payload["quantity"]; got != 20 {
		t.Fatalf("quantity = %v, want 20", got)
	}
	if _, ok := sent.Payload["deliver_to"]; ok {
		t.Fatalf("deliver_to should be omitted when empty: %v", sent.Payload)
	}

	// Simulate server's single ok (async-queued model — same as craft).
	c.router.dispatch(protocol.Response{
		Type:      protocol.TypeOK,
		RequestID: sent.RequestID,
		Payload:   map[string]any{"action": "recycle", "job_id": "r1"},
	})

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("RecycleWithOptions returned unexpected error: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("RecycleWithOptions did not return after ok")
	}
}

// TestRecycleWithOptionsDeliverTo verifies deliver_to IS included when non-empty.
func TestRecycleWithOptionsDeliverTo(t *testing.T) {
	c, sendCh := newSubmitTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- c.RecycleWithOptions(ctx, "basic_iron_smelting", 5, "faction")
	}()

	var sent protocol.Message
	select {
	case sent = <-sendCh:
	case <-ctx.Done():
		t.Fatal("timeout waiting for send")
	}

	if got := sent.Payload["deliver_to"]; got != "faction" {
		t.Fatalf("deliver_to = %v, want %q", got, "faction")
	}

	c.router.dispatch(protocol.Response{
		Type:      protocol.TypeOK,
		RequestID: sent.RequestID,
		Payload:   map[string]any{"action": "recycle", "job_id": "r2"},
	})

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("RecycleWithOptions(deliverTo=faction) unexpected error: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("RecycleWithOptions did not return after ok")
	}
}

// TestRecycleRejectsBadQuantity verifies validation rejects quantity < 1.
func TestRecycleRejectsBadQuantity(t *testing.T) {
	c := newSubmitClientSkeleton()
	if err := c.RecycleWithOptions(context.Background(), "r", 0, ""); err == nil {
		t.Fatal("expected error for quantity 0")
	}
	if err := c.RecycleWithOptions(context.Background(), "r", -1, ""); err == nil {
		t.Fatal("expected error for quantity -1")
	}
}

// TestRecycle_DelegatesToRecycleWithOptions verifies the Recycle convenience
// wrapper produces the same wire message as RecycleWithOptions with empty deliverTo.
func TestRecycle_DelegatesToRecycleWithOptions(t *testing.T) {
	c, sendCh := newSubmitTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- c.Recycle(ctx, "copper_smelting", 3)
	}()

	var sent protocol.Message
	select {
	case sent = <-sendCh:
	case <-ctx.Done():
		t.Fatal("timeout waiting for send")
	}

	if sent.Type != "recycle" {
		t.Fatalf("type = %q, want %q", sent.Type, "recycle")
	}
	if got := sent.Payload["recipe_id"]; got != "copper_smelting" {
		t.Fatalf("recipe_id = %v, want %q", got, "copper_smelting")
	}
	if _, ok := sent.Payload["deliver_to"]; ok {
		t.Fatalf("deliver_to should be absent when using Recycle: %v", sent.Payload)
	}

	c.router.dispatch(protocol.Response{
		Type:      protocol.TypeOK,
		RequestID: sent.RequestID,
		Payload:   map[string]any{"action": "recycle", "job_id": "r3"},
	})

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Recycle returned unexpected error: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("Recycle did not return after ok")
	}
}
