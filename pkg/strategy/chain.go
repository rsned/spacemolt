package strategy

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/rsned/spacemolt/pkg/game"
)

// ChainStrategy runs multiple strategies in sequence.
type ChainStrategy struct {
	name  string
	steps []Strategy

	mu     sync.RWMutex
	status string
}

// NewChainStrategy creates a chain that runs the given strategies in order.
func NewChainStrategy(name string, steps ...Strategy) *ChainStrategy {
	return &ChainStrategy{
		name:  name,
		steps: steps,
	}
}

func (c *ChainStrategy) Name() string { return c.name }

func (c *ChainStrategy) Description() string {
	names := make([]string, len(c.steps))
	for i, s := range c.steps {
		names[i] = s.Name()
	}
	return fmt.Sprintf("Chain: %s", strings.Join(names, " → "))
}

func (c *ChainStrategy) CurrentStatus() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.status != "" {
		return c.status
	}
	return "chain:idle"
}

func (c *ChainStrategy) Run(ctx context.Context, client game.GameClient, cfg Config) error {
	for i, step := range c.steps {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		c.setStatus(fmt.Sprintf("chain[%d/%d]: %s", i+1, len(c.steps), step.Name()))

		if err := step.Run(ctx, client, cfg); err != nil {
			c.setStatus(fmt.Sprintf("chain error at %s: %v", step.Name(), err))
			return fmt.Errorf("chain step %q: %w", step.Name(), err)
		}
	}

	c.setStatus("chain:completed")
	return nil
}

func (c *ChainStrategy) setStatus(status string) {
	c.mu.Lock()
	c.status = status
	c.mu.Unlock()
}
