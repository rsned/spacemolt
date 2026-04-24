package game

import (
	"context"
	"testing"
	"time"

	"github.com/rsned/spacemolt/internal/protocol"
)

// newRouterTestClient returns a *Client with only the response-router pieces
// wired up — enough to exercise exec primitives without a WebSocket.
// The client's Send is stubbed via sendOverride.
//
// As new exec primitives (execMutation, subscribePush) add Client-field
// dependencies (e.g. c.mutationMu), extend this helper to initialize
// them. Otherwise these tests will silently nil-panic on the new field.
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
