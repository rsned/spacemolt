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
	terminate  Terminator
	respCh     chan protocol.Response // one-shot
	handler    func(protocol.Response)
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
func (r *responseRouter) registerMutation(match Classifier, term Terminator, ch chan protocol.Response) *subscription {
	return r.register(&subscription{
		match:      match,
		terminate:  term,
		respCh:     ch,
		registered: time.Now(),
	})
}

// registerPush adds a long-lived push subscription. Caller keeps the
// returned handle so it can call unregister to cancel.
func (r *responseRouter) registerPush(match Classifier, handler func(protocol.Response)) *subscription {
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
			// Clear the freed slot so the backing array doesn't retain
			// the *subscription (and its respCh/handler closure) after
			// length shrinks — matters once the router is on the hot
			// read-loop path and unregister churn is high.
			copy(r.subs[i:], r.subs[i+1:])
			r.subs[len(r.subs)-1] = nil
			r.subs = r.subs[:len(r.subs)-1]
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

// dispatch routes a single response through the subscriber list. Order:
//  1. Fan out to every matching push subscriber.
//  2. Deliver to the active mutation subscriber if classifier matches AND
//     terminator fires; remove the subscription on delivery.
//  3. Deliver to the earliest-registered matching query subscriber; remove
//     the subscription on delivery.
//
// Responses that match nothing fall through silently — the caller's legacy
// _last-slot capture still runs in Client.handleResponse.
func (r *responseRouter) dispatch(resp protocol.Response) {
	// Snapshot live subs under the lock, then run handlers without holding
	// it so slow push handlers can't block register/unregister.
	r.mu.Lock()
	snapshot := make([]*subscription, len(r.subs))
	copy(snapshot, r.subs)
	r.mu.Unlock()

	// 1. Push fan-out — handlers run synchronously; document "don't block".
	for _, s := range snapshot {
		if s.handler != nil && s.match(resp) {
			s.handler(resp)
		}
	}

	// 2. Active mutation: there is at most one; if its classifier matches
	//    and terminator fires, deliver.
	for _, s := range snapshot {
		if s.terminate == nil || s.respCh == nil {
			continue
		}
		if !s.match(resp) {
			continue
		}
		done, _ := s.terminate(resp)
		if !done {
			// Intermediate message for this mutation; do not deliver.
			return
		}
		// Deliver terminal and unregister.
		select {
		case s.respCh <- resp:
		default:
		}
		r.unregister(s)
		return
	}

	// 3. Query FIFO: deliver to earliest-registered matching query.
	var winner *subscription
	for _, s := range snapshot {
		if s.handler != nil || s.terminate != nil || s.respCh == nil {
			continue // skip pushes and mutations
		}
		if !s.match(resp) {
			continue
		}
		if winner == nil || s.registered.Before(winner.registered) {
			winner = s
		}
	}
	if winner != nil {
		select {
		case winner.respCh <- resp:
		default:
		}
		r.unregister(winner)
	}
}
