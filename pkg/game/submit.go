package game

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rsned/spacemolt/internal/protocol"
)

// ID returns the current request_id. May change across reconnect-replay.
func (h *RequestHandle) ID() string {
	h.idMu.Lock()
	defer h.idMu.Unlock()
	return h.id
}

// setID is used by the replay path to update the handle's id after
// the router has been rekeyed under a new UUID.
func (h *RequestHandle) setID(id string) {
	h.idMu.Lock()
	defer h.idMu.Unlock()
	h.id = id
}

// Result wraps the terminal response or terminal error from a Submit.
type Result struct {
	Response protocol.Response
	Err      error
}

// RequestHandle is returned by Submit. Callers read Ack() for the
// pending:true ack (multi-tick mutations only) and Result() for the
// terminal action_result / error. Result is idempotent.
type RequestHandle struct {
	idMu    sync.Mutex // protects id (rekey can mutate)
	id      string
	msgType string // message type from the original Submit call; used in timeout diagnostics
	ack     chan protocol.Response
	result  chan Result
	// done is closed once the cleanup goroutine has finalized
	// (released slot + lock). Used by tests.
	done chan struct{}

	// submitCtx is the context passed to Submit. If it is cancelled
	// before the terminal arrives, Result surfaces ctx.Err() immediately.
	submitCtx context.Context

	mu     sync.Mutex
	cond   *sync.Cond // broadcast when cached is populated
	cached *Result    // populated by first successful Result; subsequent calls return this
}

// Ack waits for the server's pending:true ack. Multi-tick actions
// (travel, jump, dock, mine, attack, self_destruct) emit one; queries
// and instant mutations may not. Returns ctx.Err() on cancellation.
func (h *RequestHandle) Ack(ctx context.Context) (protocol.Response, error) {
	select {
	case resp, ok := <-h.ack:
		if !ok {
			return protocol.Response{}, errors.New("no ack: request resolved without pending")
		}
		return resp, nil
	case <-ctx.Done():
		return protocol.Response{}, ctx.Err()
	}
}

// Result waits for the terminal response. Idempotent: subsequent
// calls return the same value. Returns ctx.Err() on cancellation;
// the underlying subscription stays live so a late terminal does not
// become an orphan. If the submit-time context is cancelled, that
// cancellation is also surfaced regardless of the caller's ctx.
func (h *RequestHandle) Result(ctx context.Context) (protocol.Response, error) {
	h.mu.Lock()
	if h.cached != nil {
		c := h.cached
		h.mu.Unlock()
		return c.Response, c.Err
	}
	h.mu.Unlock()

	select {
	case r := <-h.result:
		h.mu.Lock()
		if h.cached == nil {
			h.cached = &r
			// Refill h.result so any concurrent Result() caller can drain
			// it too instead of blocking forever on a one-shot channel.
			select {
			case h.result <- r:
			default:
			}
			h.cond.Broadcast()
		}
		c := h.cached
		h.mu.Unlock()
		return c.Response, c.Err
	default:
	}

	// No result yet — wait on the cond. A context-watcher goroutine
	// broadcasts on the cond when ctx or submitCtx is cancelled so
	// that cond.Wait does not block indefinitely.
	stop := context.AfterFunc(ctx, func() { h.cond.Broadcast() })
	defer stop()
	stopSubmit := context.AfterFunc(h.submitCtx, func() { h.cond.Broadcast() })
	defer stopSubmit()

	h.mu.Lock()
	for h.cached == nil {
		// Re-check the channel while holding the lock to avoid the window
		// between the default-branch drain above and entering Wait.
		select {
		case r := <-h.result:
			if h.cached == nil {
				h.cached = &r
				select {
				case h.result <- r:
				default:
				}
				h.cond.Broadcast()
			}
		default:
		}
		if h.cached != nil {
			break
		}
		if ctx.Err() != nil || h.submitCtx.Err() != nil {
			break
		}
		h.cond.Wait()
	}
	c := h.cached
	h.mu.Unlock()

	if c != nil {
		return c.Response, c.Err
	}
	if err := h.submitCtx.Err(); err != nil {
		return protocol.Response{}, err
	}
	return protocol.Response{}, ctx.Err()
}

// SubmitOption customizes Submit. See WithTerminator, WithTimeout,
// WithAckOnly.
type SubmitOption func(*submitConfig)

type submitConfig struct {
	terminator Terminator
	timeout    time.Duration
	ackOnly    bool
}

// WithTerminator overrides the default terminateOnAction.
func WithTerminator(t Terminator) SubmitOption {
	return func(c *submitConfig) { c.terminator = t }
}

// WithTimeout overrides the default SleepTick*3.
func WithTimeout(d time.Duration) SubmitOption {
	return func(c *submitConfig) { c.timeout = d }
}

// WithAckOnly marks this Submit as a query: the first response (any
// type) is treated as terminal. Skips the per-action lock entirely.
// Overrides any WithTerminator passed earlier or later.
func WithAckOnly() SubmitOption {
	return func(c *submitConfig) { c.ackOnly = true; c.terminator = nil }
}

// resultSinkKey is the context key under which a caller stores a
// *protocol.Response sink. Unexported zero-size struct type keeps the key
// collision-free.
type resultSinkKey struct{}

// WithResultSink returns a context that captures the terminal protocol.Response
// of any Submit awaited via (*Client).await into *sink. Interactive callers
// (the play_as REPL) use this to obtain the exact request_id-correlated frame
// for the command they issued, rather than reading the racy command-keyed
// latestRawJSON slot that a concurrent background command can clobber. The sink
// belongs to the caller's own context, so unrelated goroutines (background
// pollers) using their own contexts never write to it.
func WithResultSink(ctx context.Context, sink *protocol.Response) context.Context {
	return context.WithValue(ctx, resultSinkKey{}, sink)
}

// resultSinkFrom returns the sink attached by WithResultSink, or nil.
func resultSinkFrom(ctx context.Context) *protocol.Response {
	s, _ := ctx.Value(resultSinkKey{}).(*protocol.Response)
	return s
}

// await waits for h's terminal response and, when ctx carries a sink (see
// WithResultSink), records the response into it before returning. It is the
// single chokepoint that replaces inline `h.Result(ctx)` across the command
// methods, so any caller can capture the correlated frame without changing
// method signatures. The response is captured even on a terminal *ServerError
// (the error frame is carried in resp), which is what raw/json error display
// needs; it is left untouched on ctx-cancel/timeout (resp is the zero value).
func (c *Client) await(ctx context.Context, h *RequestHandle) (protocol.Response, error) {
	resp, err := h.Result(ctx)
	if sink := resultSinkFrom(ctx); sink != nil {
		*sink = resp
	}
	return resp, err
}

// Submit sends msg with a fresh request_id and returns a handle for
// retrieving the ack and terminal response. Blocks while acquiring
// the in-flight slot and (for mutations) the per-action lock.
//
// Concurrency: queries (WithAckOnly) acquire only the in-flight slot.
// Mutations acquire in-flight then per-action lock; release in
// reverse order on resolution.
//
// On send-failure, Submit returns (h, nil) — never (nil, err). The
// underlying send error is delivered via h.Result() as a *ServerError
// with Code="send_failed". Callers MUST call h.Result() to observe
// success or failure; checking the Submit return error alone is not
// sufficient.
func (c *Client) Submit(ctx context.Context, msg protocol.Message, opts ...SubmitOption) (*RequestHandle, error) {
	cfg := submitConfig{
		terminator: terminateOnAction,
		timeout:    SleepTick * 3,
	}
	for _, o := range opts {
		o(&cfg)
	}
	if cfg.ackOnly {
		cfg.terminator = nil
	}

	// 1. Acquire global cap.
	if err := c.inflight.Acquire(ctx); err != nil {
		return nil, fmt.Errorf("inflight: %w", err)
	}

	// 2. Acquire per-action lock (mutations only).
	var releaseAction func()
	if !cfg.ackOnly {
		rel, err := c.actionLocks.lock(ctx, msg.Type)
		if err != nil {
			c.inflight.Release()
			return nil, fmt.Errorf("action lock %q: %w", msg.Type, err)
		}
		releaseAction = rel
	}

	// 3. Stamp request_id and register subscription BEFORE Send so the
	//    response can't arrive in the gap.
	id := uuid.Must(uuid.NewV7()).String()
	msg.RequestID = id

	h := &RequestHandle{
		id:        id,
		msgType:   msg.Type,
		ack:       make(chan protocol.Response, 1),
		result:    make(chan Result, 1),
		done:      make(chan struct{}),
		submitCtx: ctx,
	}
	h.cond = sync.NewCond(&h.mu)
	sub := c.router.registerByID(id, make(chan protocol.Response, 1), cfg.terminator)
	c.router.setAckChannel(sub, h.ack)
	c.router.setPendingChannel(sub, make(chan struct{}, 1))
	c.router.setReplayMsg(sub, msg)
	c.router.setHandle(sub, h)

	// 4. Spawn cleanup goroutine. Owns: resolving result, releasing
	//    lock + slot, unregistering subscription.
	go c.runSubmit(sub, h, cfg, releaseAction)

	// 5. Send. If Send fails, signal cleanup via a synthetic error frame.
	// We return h, nil intentionally: the handle is valid and the error is
	// surfaced via h.Result() through the synthetic dispatch below.
	if sendErr := c.send(ctx, msg); sendErr != nil {
		c.router.dispatch(protocol.Response{
			Type:      protocol.TypeError,
			RequestID: id,
			Payload:   map[string]any{"code": "send_failed", "message": sendErr.Error()},
		})
	}

	return h, nil
}

// runSubmit drains sub.respCh (terminal or error) and the per-Submit
// timeout/ctx, then releases resources. Runs exactly once per Submit.
//
// ctx is intentionally NOT plumbed in here; the Submit-caller's ctx
// is observed via h.Result() reads. Cancelling the caller's ctx does
// NOT abort the underlying subscription (so a late server reply does
// not become an orphan) — it only affects the caller's Result wait.
func (c *Client) runSubmit(sub *subscription, h *RequestHandle, cfg submitConfig, releaseAction func()) {
	defer close(h.done)
	defer c.inflight.Release()
	if releaseAction != nil {
		defer releaseAction()
	}

	timer := time.NewTimer(cfg.timeout)
	defer timer.Stop()

	for {
		select {
		case resp := <-sub.respCh:
			var err error
			if cfg.terminator != nil {
				// Surface error from terminator (router discards it).
				if _, e := cfg.terminator(resp); e != nil {
					err = e
				}
			} else if resp.Type == protocol.TypeError || resp.Type == protocol.TypeActionError {
				err = serverErrorFromPayload(resp.Payload)
			}
			h.result <- Result{Response: resp, Err: err}
			// Wake any concurrent Result() callers blocked on cond.Wait.
			h.cond.Broadcast()
			return
		case <-sub.pendingCh:
			// A pending ack was routed for this request: mutations execute
			// serially server-side, so a mutation sent while another is in
			// flight is queued and its terminal frame won't land until the
			// in-flight action (possibly a multi-tick travel/jump) completes.
			// The original short timeout assumed an immediate terminal; extend
			// the deadline to the worst-case multi-tick wait so the terminal
			// is not orphaned. Coalesced — extra pings only re-extend.
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(SleepJumpMaxWait)
		case <-timer.C:
			c.router.unregister(sub)
			h.result <- Result{Err: fmt.Errorf("timeout waiting for %s (request_id=%s)", h.msgType, h.ID())}
			h.cond.Broadcast()
			return
		}
	}
}
