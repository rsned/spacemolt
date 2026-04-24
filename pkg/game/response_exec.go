package game

import (
	"context"
	"fmt"
	"time"

	"github.com/rsned/spacemolt/internal/protocol"
)

// execQuery sends msg and blocks until a response satisfying match arrives,
// timeout elapses, or ctx is cancelled. Safe to call concurrently: multiple
// queries with the same classifier resolve FIFO by registration time.
func (c *Client) execQuery(
	ctx context.Context,
	msg protocol.Message,
	match Classifier,
	timeout time.Duration,
) (protocol.Response, error) {
	ch := make(chan protocol.Response, 1)
	// Register BEFORE Send so the server's response can't land between
	// the wire transmit and the subscribe call.
	sub := c.router.registerQuery(match, ch)
	defer c.router.unregister(sub)

	if err := c.Send(ctx, msg); err != nil {
		return protocol.Response{}, fmt.Errorf("send: %w", err)
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case resp := <-ch:
		return resp, nil
	case <-timer.C:
		return protocol.Response{}, fmt.Errorf("timeout waiting for response to %s", msg.Type)
	case <-ctx.Done():
		return protocol.Response{}, ctx.Err()
	}
}

// execMutation sends msg, holds c.mutationMu for the entire duration, and
// blocks until a response satisfies both match AND terminate — or timeout /
// ctx cancellation. Concurrent calls serialize on the mutex.
func (c *Client) execMutation(
	ctx context.Context,
	msg protocol.Message,
	match Classifier,
	terminate Terminator,
	timeout time.Duration,
) (protocol.Response, error) {
	c.mutationMu.Lock()
	defer c.mutationMu.Unlock()

	ch := make(chan protocol.Response, 1)
	// Register BEFORE Send so the server's response can't land between
	// the wire transmit and the subscribe call.
	sub := c.router.registerMutation(match, terminate, ch)
	defer c.router.unregister(sub)

	if err := c.Send(ctx, msg); err != nil {
		return protocol.Response{}, fmt.Errorf("send: %w", err)
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case resp := <-ch:
		// The router only delivers when terminate returned done=true (see
		// dispatch step 2 in response_router.go). It discards the err return,
		// so we re-run the terminator here to surface any error it produced.
		if _, err := terminate(resp); err != nil {
			return resp, err
		}
		return resp, nil
	case <-timer.C:
		return protocol.Response{}, fmt.Errorf("timeout waiting for %s to complete", msg.Type)
	case <-ctx.Done():
		return protocol.Response{}, ctx.Err()
	}
}
