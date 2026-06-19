package game

import (
	"context"
	"fmt"
	"time"

	"github.com/rsned/spacemolt/internal/protocol"
)

// Recycle queues a recycling job: it consumes the recipe's outputs and returns
// a lossy fraction of its inputs over subsequent ticks. quantity is the number
// of output items to feed in (rounded up to whole recycling runs).
func (c *Client) Recycle(ctx context.Context, recipeID string, quantity int) error {
	return c.RecycleWithOptions(ctx, recipeID, quantity, "")
}

// RecycleWithOptions queues a recycling job with an optional delivery target.
// deliverTo may be "" (server default: station storage) or "faction" (requires
// manage-treasury permission). Like craft, recycle is async-queued: the server
// replies with a single ok job frame and delivers reclaimed inputs later via
// crafting_update.
func (c *Client) RecycleWithOptions(ctx context.Context, recipeID string, quantity int, deliverTo string) error {
	if quantity < 1 {
		return fmt.Errorf("invalid quantity: %d (must be >= 1)", quantity)
	}

	payload := map[string]any{
		"recipe_id": recipeID,
		"quantity":  quantity,
	}
	if deliverTo != "" {
		payload["deliver_to"] = deliverTo
	}

	msg := protocol.Message{
		Type:      "recycle",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTerminator(terminateOnActionOrOK), WithTimeout(SleepTick*3))
	if err == nil {
		_, err = h.Result(ctx)
	}
	return err
}
