package game

import (
	"testing"

	"github.com/rsned/spacemolt/internal/protocol"
)

func TestRouter_RegisterUnregister(t *testing.T) {
	r := newResponseRouter()

	ch := make(chan protocol.Response, 1)
	sub := r.registerQuery(matchType(protocol.TypeOK), ch)
	if r.subCount() != 1 {
		t.Fatalf("expected 1 subscription, got %d", r.subCount())
	}
	r.unregister(sub)
	if r.subCount() != 0 {
		t.Fatalf("expected 0 subscriptions after unregister, got %d", r.subCount())
	}
}

func TestRouter_UnregisterMissingIsNoop(t *testing.T) {
	r := newResponseRouter()
	// Build a subscription that was never registered
	sub := &subscription{}
	r.unregister(sub) // must not panic
}
