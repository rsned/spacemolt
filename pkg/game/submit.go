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

// Result wraps the terminal response or terminal error from a Submit.
type Result struct {
	Response protocol.Response
	Err      error
}

// RequestHandle is returned by Submit. Callers read Ack() for the
// pending:true ack (multi-tick mutations only) and Result() for the
// terminal action_result / error. Result is idempotent.
type RequestHandle struct {
	ID     string
	ack    chan protocol.Response
	result chan Result
	// done is closed once the cleanup goroutine has finalized
	// (released slot + lock). Used by tests.
	done chan struct{}

	// submitCtx is the context passed to Submit. If it is cancelled
	// before the terminal arrives, Result surfaces ctx.Err() immediately.
	submitCtx context.Context

	mu     sync.Mutex
	cached *Result // populated by first successful Result; subsequent calls return this
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

	// Surface submit-time cancellation in addition to caller ctx.
	submitDone := h.submitCtx.Done()

	select {
	case r := <-h.result:
		h.mu.Lock()
		if h.cached == nil {
			h.cached = &r
		}
		c := h.cached
		h.mu.Unlock()
		return c.Response, c.Err
	case <-submitDone:
		return protocol.Response{}, h.submitCtx.Err()
	case <-ctx.Done():
		return protocol.Response{}, ctx.Err()
	}
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

// Submit sends msg with a fresh request_id and returns a handle for
// retrieving the ack and terminal response. Blocks while acquiring
// the in-flight slot and (for mutations) the per-action lock.
//
// Concurrency: queries (WithAckOnly) acquire only the in-flight slot.
// Mutations acquire in-flight then per-action lock; release in
// reverse order on resolution.
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
		ID:        id,
		ack:       make(chan protocol.Response, 1),
		result:    make(chan Result, 1),
		done:      make(chan struct{}),
		submitCtx: ctx,
	}
	sub := c.router.registerByID(id, make(chan protocol.Response, 1), cfg.terminator)
	c.router.setAckChannel(sub, h.ack)

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
	case <-timer.C:
		c.router.unregister(sub)
		h.result <- Result{Err: fmt.Errorf("timeout waiting for %s (request_id=%s)", "submit", h.ID)}
	}
}
