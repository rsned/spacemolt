package game

import (
	"sync"
	"time"

	"github.com/rsned/spacemolt/internal/protocol"
)

// subscription is a single registration with the response router. Exactly
// one of respCh/handler is set: respCh for query/mutation one-shot,
// handler for push fan-out.
type subscription struct {
	match      Classifier
	terminate  Terminator             //nolint:unused // used by Task 4 dispatch
	respCh     chan protocol.Response // one-shot
	handler    func(protocol.Response) //nolint:unused // used by Task 4 dispatch
	registered time.Time
}

// responseRouter dispatches incoming responses to registered subscribers.
// It has no WebSocket awareness — callers feed it responses via Dispatch.
type responseRouter struct {
	mu   sync.Mutex
	subs []*subscription
}

// newResponseRouter constructs an empty router.
func newResponseRouter() *responseRouter {
	return &responseRouter{}
}

// registerQuery adds a one-shot query subscription. Returns the handle
// the caller must pass to unregister.
func (r *responseRouter) registerQuery(match Classifier, ch chan protocol.Response) *subscription {
	return r.register(&subscription{
		match:      match,
		respCh:     ch,
		registered: time.Now(),
	})
}

// registerMutation adds a one-shot mutation subscription (respCh delivers
// the terminal response; terminator decides when that is).
func (r *responseRouter) registerMutation(match Classifier, term Terminator, ch chan protocol.Response) *subscription { //nolint:unused // called by Task 6 execMutation
	return r.register(&subscription{
		match:      match,
		terminate:  term,
		respCh:     ch,
		registered: time.Now(),
	})
}

// registerPush adds a long-lived push subscription. Caller keeps the
// returned handle so it can call unregister to cancel.
func (r *responseRouter) registerPush(match Classifier, handler func(protocol.Response)) *subscription { //nolint:unused // called by Task 7 subscribePush
	return r.register(&subscription{
		match:      match,
		handler:    handler,
		registered: time.Now(),
	})
}

func (r *responseRouter) register(sub *subscription) *subscription {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.subs = append(r.subs, sub)
	return sub
}

// unregister removes sub from the router. No-op if sub was never registered
// or already removed.
func (r *responseRouter) unregister(sub *subscription) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, s := range r.subs {
		if s == sub {
			r.subs = append(r.subs[:i], r.subs[i+1:]...)
			return
		}
	}
}

// subCount returns the number of live subscriptions (for tests).
func (r *responseRouter) subCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.subs)
}
