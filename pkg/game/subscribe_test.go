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

func TestSetOnPushEvent_DeliversUntaggedPushes(t *testing.T) {
	c := newRouterTestClient(nil)
	var got []string
	cancel := c.SetOnPushEvent(func(resp protocol.Response) {
		got = append(got, resp.Type)
	})
	defer cancel()

	c.router.dispatch(protocol.Response{Type: protocol.TypeBattleDamage})
	c.router.dispatch(protocol.Response{Type: protocol.TypeBattleUpdate})
	c.router.dispatch(protocol.Response{Type: protocol.TypePlayerDied})

	want := []string{protocol.TypeBattleDamage, protocol.TypeBattleUpdate, protocol.TypePlayerDied}
	if len(got) != len(want) {
		t.Fatalf("want %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("event %d: want %q, got %q", i, want[i], got[i])
		}
	}
}

// A push subscription must observe, never consume: the existing typed
// listeners and the command paths have to keep seeing the same frames.
func TestSetOnPushEvent_DoesNotStealFromTypedListeners(t *testing.T) {
	c := newRouterTestClient(nil)
	var chats, all int32
	cancelChat := c.subscribePush(matchType(protocol.TypeChatMessage), func(_ protocol.Response) {
		atomic.AddInt32(&chats, 1)
	})
	defer cancelChat()
	cancelAll := c.SetOnPushEvent(func(_ protocol.Response) { atomic.AddInt32(&all, 1) })
	defer cancelAll()

	c.router.dispatch(protocol.Response{Type: protocol.TypeChatMessage})

	if got := atomic.LoadInt32(&chats); got != 1 {
		t.Errorf("typed chat listener: want 1 call, got %d", got)
	}
	if got := atomic.LoadInt32(&all); got != 1 {
		t.Errorf("push listener: want 1 call, got %d", got)
	}
}
