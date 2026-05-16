package game

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/rsned/spacemolt/internal/protocol"
)

// newRouterTestClient returns a *Client with only the response-router pieces
// wired up — enough to exercise subscribe primitives without a WebSocket.
// The client's Send is stubbed via sendOverride.
func newRouterTestClient(send func(ctx context.Context, msg protocol.Message) error) *Client {
	return &Client{
		router:       newResponseRouter(),
		sendOverride: send,
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

	// Idempotent: a second cancel must not panic, must not change anything.
	cancel()
	c.router.dispatch(protocol.Response{Type: protocol.TypeChatMessage})
	if got := atomic.LoadInt32(&count); got != 1 {
		t.Errorf("after second cancel: expected 1 call, got %d", got)
	}
}
