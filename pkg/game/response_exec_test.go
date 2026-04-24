package game

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rsned/spacemolt/internal/protocol"
)

// newRouterTestClient returns a *Client with only the response-router pieces
// wired up — enough to exercise exec primitives without a WebSocket.
// The client's Send is stubbed via sendOverride.
//
// mutationMu is a sync.Mutex and is zero-value usable — no explicit init
// is needed. If subscribePush or future primitives add pointer or channel
// fields to Client, extend this helper to initialize those fields.
func newRouterTestClient(send func(ctx context.Context, msg protocol.Message) error) *Client {
	c := &Client{
		router:       newResponseRouter(),
		sendOverride: send,
	}
	return c
}

func TestExecQuery_DeliversMatchingResponse(t *testing.T) {
	var sent protocol.Message
	var c *Client
	c = newRouterTestClient(func(_ context.Context, msg protocol.Message) error {
		sent = msg
		// Simulate the server replying asynchronously.
		go func() {
			time.Sleep(5 * time.Millisecond)
			c.router.dispatch(protocol.Response{
				Type:    protocol.TypeOK,
				Payload: map[string]any{"cargo": []any{}},
			})
		}()
		return nil
	})

	resp, err := c.execQuery(
		context.Background(),
		protocol.Message{Type: "get_cargo"},
		matchAll(matchType(protocol.TypeOK), matchPayloadKey("cargo")),
		1*time.Second,
	)
	if err != nil {
		t.Fatalf("execQuery: %v", err)
	}
	if sent.Type != "get_cargo" {
		t.Errorf("sent wrong message: %+v", sent)
	}
	if _, ok := resp.Payload["cargo"]; !ok {
		t.Errorf("response missing cargo key: %+v", resp.Payload)
	}
	if c.router.subCount() != 0 {
		t.Errorf("subscription leaked: %d live", c.router.subCount())
	}
}

func TestExecQuery_Timeout(t *testing.T) {
	c := newRouterTestClient(func(_ context.Context, _ protocol.Message) error {
		return nil // never reply
	})
	_, err := c.execQuery(
		context.Background(),
		protocol.Message{Type: "get_cargo"},
		matchType(protocol.TypeOK),
		20*time.Millisecond,
	)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if c.router.subCount() != 0 {
		t.Errorf("subscription leaked on timeout: %d live", c.router.subCount())
	}
}

func TestExecQuery_ContextCancel(t *testing.T) {
	c := newRouterTestClient(func(_ context.Context, _ protocol.Message) error { return nil })
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()
	_, err := c.execQuery(ctx, protocol.Message{Type: "x"}, matchType(protocol.TypeOK), 1*time.Second)
	if err == nil {
		t.Fatal("expected ctx error")
	}
	if c.router.subCount() != 0 {
		t.Errorf("subscription leaked on ctx cancel: %d live", c.router.subCount())
	}
}

func TestExecMutation_WaitsForTerminal(t *testing.T) {
	var c *Client
	c = newRouterTestClient(func(_ context.Context, _ protocol.Message) error {
		// Simulate: first ok pending, then the action_result terminal.
		go func() {
			time.Sleep(5 * time.Millisecond)
			c.router.dispatch(protocol.Response{
				Type:    protocol.TypeOK,
				Payload: map[string]any{"command": "deposit_items", "pending": true},
			})
			time.Sleep(5 * time.Millisecond)
			c.router.dispatch(protocol.Response{
				Type:    protocol.TypeActionResult,
				Payload: map[string]any{"command": "deposit_items", "quantity": 5.0},
			})
		}()
		return nil
	})

	resp, err := c.execMutation(
		context.Background(),
		protocol.Message{Type: "deposit_items"},
		matchCommand("deposit_items"),
		terminateOnAction,
		1*time.Second,
	)
	if err != nil {
		t.Fatalf("execMutation: %v", err)
	}
	if resp.Type != protocol.TypeActionResult {
		t.Errorf("expected action_result, got %q", resp.Type)
	}
}

func TestExecMutation_SerializesConcurrent(t *testing.T) {
	var active int32
	var peak int32
	var c *Client
	c = newRouterTestClient(func(_ context.Context, msg protocol.Message) error {
		n := atomic.AddInt32(&active, 1)
		if n > atomic.LoadInt32(&peak) {
			atomic.StoreInt32(&peak, n)
		}
		go func() {
			time.Sleep(10 * time.Millisecond)
			c.router.dispatch(protocol.Response{
				Type:    protocol.TypeActionResult,
				Payload: map[string]any{"command": msg.Type},
			})
			atomic.AddInt32(&active, -1)
		}()
		return nil
	})

	var wg sync.WaitGroup
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = c.execMutation(
				context.Background(),
				protocol.Message{Type: "deposit_items"},
				matchCommand("deposit_items"),
				terminateOnAction,
				1*time.Second,
			)
		}()
	}
	wg.Wait()
	if atomic.LoadInt32(&peak) > 1 {
		t.Errorf("mutations ran in parallel (peak=%d)", peak)
	}
}

func TestSubscribePush_FiresForever(t *testing.T) {
	c := newRouterTestClient(nil)
	var count int32
	cancel := c.subscribePush(matchType(protocol.TypeChatMessage), func(_ protocol.Response) {
		atomic.AddInt32(&count, 1)
	})
	defer cancel()

	c.router.dispatch(protocol.Response{Type: protocol.TypeChatMessage})
	c.router.dispatch(protocol.Response{Type: protocol.TypeChatMessage})
	c.router.dispatch(protocol.Response{Type: protocol.TypeTick}) // ignored
	if got := atomic.LoadInt32(&count); got != 2 {
		t.Errorf("expected 2 handler calls, got %d", got)
	}
}

func TestSubscribePush_CancelStopsDelivery(t *testing.T) {
	c := newRouterTestClient(nil)
	var count int32
	cancel := c.subscribePush(matchType(protocol.TypeChatMessage), func(_ protocol.Response) {
		atomic.AddInt32(&count, 1)
	})

	c.router.dispatch(protocol.Response{Type: protocol.TypeChatMessage})
	cancel()
	c.router.dispatch(protocol.Response{Type: protocol.TypeChatMessage}) // must not fire

	if got := atomic.LoadInt32(&count); got != 1 {
		t.Errorf("expected 1 call, got %d", got)
	}
	if c.router.subCount() != 0 {
		t.Errorf("push sub leaked: %d", c.router.subCount())
	}
}
