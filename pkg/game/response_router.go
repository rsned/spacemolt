package game

import (
	"log"
	"sync"
	"time"

	"github.com/rsned/spacemolt/internal/protocol"
)

// subscription is a single registration with the response router.
// idSub is set for request_id-correlated entries; legacy classifier
// entries use match/terminate/respCh/handler.
type subscription struct {
	// Request-ID correlation (preferred). When id != "", match,
	// terminate, and registered are unused. The router routes via
	// byReqID before scanning subs.
	id string

	match      Classifier
	terminate  Terminator
	respCh     chan protocol.Response // one-shot result delivery
	ackCh      chan protocol.Response // optional: pending ack delivery (id subs only)
	pendingCh  chan struct{}          // optional: pings runSubmit when a pending ack is routed (id subs only)
	handler    func(protocol.Response)
	registered time.Time

	// replayMsg holds the original outgoing Message (without
	// RequestID) so the replay path can re-send under a fresh UUID.
	// Set only for id-keyed subscriptions created by Submit.
	replayMsg *protocol.Message

	// handle back-references the RequestHandle this subscription
	// belongs to, so replay can update handle.id after rekey. Set
	// by Submit; nil for non-Submit subscriptions.
	handle *RequestHandle
}

// responseRouter dispatches incoming responses to registered subscribers.
// It has no WebSocket awareness — callers feed it responses via dispatch.
type responseRouter struct {
	mu      sync.Mutex
	byReqID map[string]*subscription
	subs    []*subscription
	orphan  *orphanStats
}

func newResponseRouter() *responseRouter {
	return &responseRouter{
		byReqID: make(map[string]*subscription),
		orphan:  newOrphanStats(),
	}
}

// orphans returns the orphan-stats counter (for diagnostics + tests).
func (r *responseRouter) orphans() *orphanStats { return r.orphan }

// registerByID registers a subscription keyed by request_id. If
// terminate is nil the next matching frame is treated as terminal. If
// terminate is non-nil, the router delivers only when terminate
// reports done=true; intermediate frames pass through to ackCh (if
// caller set it via setAckChannel).
func (r *responseRouter) registerByID(id string, ch chan protocol.Response, terminate Terminator) *subscription {
	r.mu.Lock()
	defer r.mu.Unlock()
	sub := &subscription{
		id:         id,
		respCh:     ch,
		terminate:  terminate,
		registered: time.Now(),
	}
	r.byReqID[id] = sub
	return sub
}

// setAckChannel attaches an ack channel to an existing id-subscription.
// Must be called before any frames for this id can arrive.
func (r *responseRouter) setAckChannel(sub *subscription, ackCh chan protocol.Response) {
	r.mu.Lock()
	defer r.mu.Unlock()
	sub.ackCh = ackCh
}

// setPendingChannel attaches the runSubmit deadline-reset signal channel to an
// existing id-subscription. The router pings it (non-blocking) each time it
// routes an intermediate (pending) frame, so runSubmit can extend its timeout
// for a mutation that is queued behind an in-flight one. Must be called before
// any frames for this id can arrive.
func (r *responseRouter) setPendingChannel(sub *subscription, pendingCh chan struct{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	sub.pendingCh = pendingCh
}

// setReplayMsg attaches the original outgoing message for replay.
// The message is stored without its RequestID stamp so the replay
// path can stamp a fresh one. Used only by Submit.
func (r *responseRouter) setReplayMsg(sub *subscription, msg protocol.Message) {
	r.mu.Lock()
	defer r.mu.Unlock()
	msg.RequestID = ""
	sub.replayMsg = &msg
}

// setHandle attaches the owning RequestHandle to an id-subscription.
// Used by Submit so replayPending can update handle.id after rekey.
func (r *responseRouter) setHandle(sub *subscription, h *RequestHandle) {
	r.mu.Lock()
	defer r.mu.Unlock()
	sub.handle = h
}

// registerQuery adds a one-shot classifier-based query subscription.
// Used only for untagged frames (fallback path).
func (r *responseRouter) registerQuery(match Classifier, ch chan protocol.Response) *subscription {
	return r.register(&subscription{
		match:      match,
		respCh:     ch,
		registered: time.Now(),
	})
}

// registerMutation adds a one-shot classifier-based mutation subscription.
// Used only for untagged frames (fallback path).
func (r *responseRouter) registerMutation(match Classifier, term Terminator, ch chan protocol.Response) *subscription {
	return r.register(&subscription{
		match:      match,
		terminate:  term,
		respCh:     ch,
		registered: time.Now(),
	})
}

// registerPush adds a long-lived push subscription.
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

// unregister removes sub from the router. No-op if sub was never
// registered or already removed.
func (r *responseRouter) unregister(sub *subscription) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if sub.id != "" {
		if r.byReqID[sub.id] == sub {
			delete(r.byReqID, sub.id)
		}
		return
	}
	for i, s := range r.subs {
		if s == sub {
			copy(r.subs[i:], r.subs[i+1:])
			r.subs[len(r.subs)-1] = nil
			r.subs = r.subs[:len(r.subs)-1]
			return
		}
	}
}

// snapshotByID returns all live id-keyed subscriptions. Used by the
// replay path on close=1000.
func (r *responseRouter) snapshotByID() []*subscription {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*subscription, 0, len(r.byReqID))
	for _, s := range r.byReqID {
		out = append(out, s)
	}
	return out
}

// rekey changes an id-subscription's request_id. Used by the replay
// path when re-sending under a fresh UUID.
func (r *responseRouter) rekey(sub *subscription, newID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.byReqID[sub.id] == sub {
		delete(r.byReqID, sub.id)
	}
	sub.id = newID
	r.byReqID[newID] = sub
}

// subCount returns the number of live subscriptions (for tests). Counts
// both byReqID and legacy subs.
func (r *responseRouter) subCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.subs) + len(r.byReqID)
}

// dispatch routes a single response. Order:
//  1. resp.RequestID != "": look up byReqID. Hit = deliver (with
//     terminator gate for intermediates). Miss = orphan and DROP.
//     Tagged frames never fall through.
//  2. resp.RequestID == "": fan out to matching push handlers, then
//     deliver to the earliest matching classifier subscription.
func (r *responseRouter) dispatch(resp protocol.Response) {
	if resp.RequestID != "" {
		r.dispatchByID(resp)
		return
	}
	r.dispatchUntagged(resp)
}

func (r *responseRouter) dispatchByID(resp protocol.Response) {
	r.mu.Lock()
	sub, ok := r.byReqID[resp.RequestID]
	var ackCh, respCh chan protocol.Response
	var pendingCh chan struct{}
	var terminate Terminator
	if ok {
		ackCh = sub.ackCh
		respCh = sub.respCh
		pendingCh = sub.pendingCh
		terminate = sub.terminate
	}
	r.mu.Unlock()

	if !ok {
		r.orphan.record(resp.RequestID, resp.Type)
		return
	}

	// Terminator gate: intermediate frames go to ackCh, terminals to respCh.
	// Note: a concurrent unregister between lookup and delivery can turn
	// what looks like a successful send into a dropped frame; the
	// subscription pointer remains valid (sub.respCh capacity=1) but the
	// caller has stopped reading, so the default branch fires. This is
	// acceptable — caller initiated the cancellation.
	if terminate != nil {
		done, _ := r.safeRunTerminatorFn(terminate, resp)
		if !done {
			// Auto-dock/auto-undock OK frames carry the issuing command's
			// request_id for traceability but are side-channel state
			// notifications, not the command's response. handleResponse has
			// already applied the dock state mutation by the time we get here.
			// Skip ack delivery silently — competing with the real pending ack
			// for the single ackCh slot would otherwise trigger a noisy
			// "dropped ack" log on every craft/buy/etc. that auto-docks.
			autoDock := isAutoDockTransition(resp.Payload)
			if ackCh != nil && !autoDock {
				select {
				case ackCh <- resp:
				default:
					log.Printf("response router: dropped ack (id=%s, full ackCh)", resp.RequestID)
				}
			}
			// Ping runSubmit so it can extend its timeout: an intermediate
			// (pending) frame means the mutation is queued behind an in-flight
			// one and its terminal will land many ticks later. Non-blocking;
			// the channel is buffered (cap 1) and a coalesced signal suffices.
			// Auto-dock costs one extra tick too, so extend on those as well.
			if pendingCh != nil {
				select {
				case pendingCh <- struct{}{}:
				default:
				}
			}
			return
		}
	}

	select {
	case respCh <- resp:
	default:
		log.Printf("response router: dropped terminal (id=%s, full respCh)", resp.RequestID)
	}
	r.unregister(sub)
}

func (r *responseRouter) dispatchUntagged(resp protocol.Response) {
	r.mu.Lock()
	snapshot := make([]*subscription, len(r.subs))
	copy(snapshot, r.subs)
	r.mu.Unlock()

	// 1. Push fan-out
	for _, s := range snapshot {
		if s.handler != nil && s.match(resp) {
			r.safeFireHandler(s, resp)
		}
	}

	// 2. Mutation classifier
	for _, s := range snapshot {
		if s.terminate == nil || s.respCh == nil {
			continue
		}
		if !s.match(resp) {
			continue
		}
		done, _ := r.safeRunTerminator(s, resp)
		if !done {
			return
		}
		select {
		case s.respCh <- resp:
		default:
			log.Printf("response router: dropped mutation terminal (type=%s)", resp.Type)
		}
		r.unregister(s)
		return
	}

	// 3. Query FIFO
	var winner *subscription
	for _, s := range snapshot {
		if s.handler != nil || s.terminate != nil || s.respCh == nil {
			continue
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
			log.Printf("response router: dropped query reply (type=%s)", resp.Type)
		}
		r.unregister(winner)
	}
}

func (r *responseRouter) safeFireHandler(s *subscription, resp protocol.Response) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("response router: push handler panic (type=%s): %v", resp.Type, rec)
		}
	}()
	s.handler(resp)
}

// safeRunTerminatorFn invokes a terminator with panic recovery. Identical
// to safeRunTerminator but takes the Terminator directly so the caller can
// release the router mutex before invoking it (snapshot pattern).
func (r *responseRouter) safeRunTerminatorFn(t Terminator, resp protocol.Response) (done bool, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("response router: terminator panic (type=%s): %v", resp.Type, rec)
			done = false
		}
	}()
	return t(resp)
}

func (r *responseRouter) safeRunTerminator(s *subscription, resp protocol.Response) (done bool, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("response router: terminator panic (type=%s): %v", resp.Type, rec)
			done = false
		}
	}()
	return s.terminate(resp)
}
