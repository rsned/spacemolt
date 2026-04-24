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
