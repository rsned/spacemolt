package game

import (
	"context"
	"errors"
	"io"
	"log"
	"sync"
	"testing"
	"time"

	"github.com/rsned/spacemolt/internal/protocol"
)

// newSubmitTestClient returns a Client with stubbed connection so Submit can
// run without a real WebSocket. sendCh receives every Send call.
func newSubmitTestClient(t *testing.T) (*Client, chan protocol.Message) {
	t.Helper()
	c := newSubmitClientSkeleton()
	sendCh := make(chan protocol.Message, 16)
	c.sendOverride = func(ctx context.Context, msg protocol.Message) error {
		sendCh <- msg
		return nil
	}
	return c, sendCh
}

// newSubmitClientSkeleton returns a minimal Client suitable for Submit unit
// tests — no real WebSocket, no listener goroutine. Mirrors the
// constructor's field init for the bits Submit/router touch.
func newSubmitClientSkeleton() *Client {
	c := &Client{
		debugLogger: log.New(io.Discard, "", 0),
	}
	c.router = newResponseRouter()
	c.inflight = newInflight(16)
	c.actionLocks = newActionLockMap()
	c.connected = true
	return c
}

func TestSubmit_AckOnly_QueryReturnsTerminal(t *testing.T) {
	c, sendCh := newSubmitTestClient(t)
	ctx := context.Background()

	h, err := c.Submit(ctx, protocol.Message{Type: "get_status"}, WithAckOnly())
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}

	sent := <-sendCh
	if sent.RequestID == "" {
		t.Fatal("sent message missing RequestID")
	}
	if h.ID != sent.RequestID {
		t.Errorf("handle.ID = %q, want %q", h.ID, sent.RequestID)
	}

	go c.router.dispatch(protocol.Response{
		Type: protocol.TypeOK, RequestID: sent.RequestID,
		Payload: map[string]any{"status": "ok"},
	})

	resp, err := h.Result(ctx)
	if err != nil {
		t.Fatalf("Result: %v", err)
	}
	if resp.RequestID != sent.RequestID {
		t.Errorf("resp.RequestID = %q, want %q", resp.RequestID, sent.RequestID)
	}
}

func TestSubmit_Mutation_PendingThenTerminal(t *testing.T) {
	c, sendCh := newSubmitTestClient(t)
	ctx := context.Background()

	h, err := c.Submit(ctx,
		protocol.Message{Type: "mine"},
		WithTerminator(terminateOnAction),
	)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	sent := <-sendCh

	go func() {
		c.router.dispatch(protocol.Response{
			Type: protocol.TypeOK, RequestID: sent.RequestID,
			Payload: map[string]any{"pending": true, "command": "mine"},
		})
		time.Sleep(5 * time.Millisecond)
		c.router.dispatch(protocol.Response{
			Type: protocol.TypeActionResult, RequestID: sent.RequestID,
			Payload: map[string]any{"command": "mine", "yield": 3},
		})
	}()

	ack, err := h.Ack(ctx)
	if err != nil {
		t.Fatalf("Ack: %v", err)
	}
	if p, _ := ack.Payload["pending"].(bool); !p {
		t.Errorf("expected pending=true on ack")
	}

	resp, err := h.Result(ctx)
	if err != nil {
		t.Fatalf("Result: %v", err)
	}
	if resp.Type != protocol.TypeActionResult {
		t.Errorf("Result type = %q, want action_result", resp.Type)
	}
}

func TestSubmit_ServerError(t *testing.T) {
	c, sendCh := newSubmitTestClient(t)
	ctx := context.Background()

	h, err := c.Submit(ctx, protocol.Message{Type: "buy"},
		WithTerminator(terminateOnAction))
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	sent := <-sendCh

	go c.router.dispatch(protocol.Response{
		Type: protocol.TypeError, RequestID: sent.RequestID,
		Payload: map[string]any{"code": "insufficient_credits", "message": "not enough credits"},
	})

	_, err = h.Result(ctx)
	if err == nil {
		t.Fatal("expected ServerError, got nil")
	}
	var se *ServerError
	if !errors.As(err, &se) {
		t.Fatalf("err type = %T, want *ServerError", err)
	}
	if se.Code != "insufficient_credits" {
		t.Errorf("se.Code = %q, want insufficient_credits", se.Code)
	}
}

func TestSubmit_ResultCalledTwiceReturnsSameValue(t *testing.T) {
	c, sendCh := newSubmitTestClient(t)
	ctx := context.Background()

	h, err := c.Submit(ctx, protocol.Message{Type: "get_status"}, WithAckOnly())
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	sent := <-sendCh

	go c.router.dispatch(protocol.Response{
		Type: protocol.TypeOK, RequestID: sent.RequestID,
		Payload: map[string]any{"status": "ok"},
	})

	r1, err1 := h.Result(ctx)
	r2, err2 := h.Result(ctx)
	if err1 != nil || err2 != nil {
		t.Fatalf("err1=%v err2=%v", err1, err2)
	}
	if r1.RequestID != r2.RequestID {
		t.Errorf("Result/Result mismatch: %q vs %q", r1.RequestID, r2.RequestID)
	}
}

func TestSubmit_CtxCancelReleasesResources(t *testing.T) {
	c, sendCh := newSubmitTestClient(t)
	ctx, cancel := context.WithCancel(context.Background())

	h, err := c.Submit(ctx, protocol.Message{Type: "mine"},
		WithTerminator(terminateOnAction),
		WithTimeout(100*time.Millisecond))
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	<-sendCh

	cancel()
	_, err = h.Result(context.Background())
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}

	// Resources released: cap should drain to 0.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if c.inflight.InFlight() == 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Errorf("inflight slot not released, count=%d", c.inflight.InFlight())
}

func TestSubmit_ResultConcurrentCallersAllResolve(t *testing.T) {
	c, sendCh := newSubmitTestClient(t)
	ctx := context.Background()

	h, err := c.Submit(ctx, protocol.Message{Type: "get_status"}, WithAckOnly())
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	sent := <-sendCh

	go c.router.dispatch(protocol.Response{
		Type: protocol.TypeOK, RequestID: sent.RequestID,
		Payload: map[string]any{"status": "ok"},
	})

	var wg sync.WaitGroup
	var results [4]protocol.Response
	var errs [4]error
	for i := range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			callCtx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			results[i], errs[i] = h.Result(callCtx)
		}()
	}
	wg.Wait()

	for i := range 4 {
		if errs[i] != nil {
			t.Errorf("Result[%d]: %v", i, errs[i])
		}
		if results[i].RequestID != sent.RequestID {
			t.Errorf("Result[%d].RequestID = %q, want %q", i, results[i].RequestID, sent.RequestID)
		}
	}
}

func TestSubmit_UsesUUIDv7(t *testing.T) {
	c, sendCh := newSubmitTestClient(t)
	ctx := context.Background()

	_, err := c.Submit(ctx, protocol.Message{Type: "get_status"}, WithAckOnly())
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	sent := <-sendCh
	// UUIDv7 string is 36 chars with version "7" at position 14.
	if len(sent.RequestID) != 36 || sent.RequestID[14] != '7' {
		t.Errorf("RequestID = %q, want UUIDv7 (36 chars, version 7)", sent.RequestID)
	}
}
