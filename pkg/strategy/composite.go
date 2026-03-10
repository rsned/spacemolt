package strategy

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// CompositeConfig configures a CompositeStrategy.
type CompositeConfig struct {
	CleanupTimeout  time.Duration
	MinIdleDuration time.Duration
	Logger          *log.Logger
}

// CompositeStrategy orchestrates a primary strategy with a background strategy
// that runs during the primary's idle windows.
type CompositeStrategy struct {
	primary    Strategy
	background Strategy
	config     CompositeConfig
	logger     *log.Logger

	bgRunner *BackgroundRunner

	mu     sync.RWMutex
	status string

	idleCh chan bool
}

// NewCompositeStrategy creates a composite strategy.
func NewCompositeStrategy(primary, background Strategy, config CompositeConfig) *CompositeStrategy {
	logger := config.Logger
	if logger == nil {
		logger = log.Default()
	}

	return &CompositeStrategy{
		primary:    primary,
		background: background,
		config:     config,
		logger:     logger,
		idleCh:     make(chan bool, 1),
	}
}

func (c *CompositeStrategy) Name() string {
	return c.primary.Name() + "+" + c.background.Name()
}

func (c *CompositeStrategy) Description() string {
	return fmt.Sprintf("%s (background: %s)", c.primary.Description(), c.background.Name())
}

func (c *CompositeStrategy) CurrentStatus() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.status != "" {
		return c.status
	}
	primaryStatus := c.primary.CurrentStatus()
	if primaryStatus == "" {
		primaryStatus = "idle"
	}
	if c.bgRunner != nil && c.bgRunner.IsRunning() {
		return fmt.Sprintf("%s [bg: %s]", primaryStatus, c.background.CurrentStatus())
	}
	return primaryStatus
}

func (c *CompositeStrategy) Run(ctx context.Context, client game.GameClient, cfg Config) error {
	c.setStatus("starting")

	c.bgRunner = NewBackgroundRunner(c.background, BackgroundRunnerConfig{
		CleanupTimeout: c.config.CleanupTimeout,
		Logger:         c.logger,
	})

	bgCtx, bgCancel := context.WithCancel(ctx)
	defer bgCancel()

	var bgWg sync.WaitGroup
	bgWg.Add(1)
	go func() {
		defer bgWg.Done()
		c.manageBackground(bgCtx, client, cfg)
	}()

	c.setStatus("primary: running")
	err := c.primary.Run(ctx, client, cfg)

	bgCancel()

	if c.bgRunner.IsRunning() {
		c.logger.Printf("[composite] primary exited, interrupting background")
		c.bgRunner.Interrupt()
	}

	bgWg.Wait()

	if err != nil {
		c.setStatus(fmt.Sprintf("error: %v", err))
	} else {
		c.setStatus("completed")
	}
	return err
}

func (c *CompositeStrategy) manageBackground(ctx context.Context, client game.GameClient, cfg Config) {
	for {
		select {
		case <-ctx.Done():
			return
		case idle := <-c.idleCh:
			if idle {
				c.logger.Printf("[composite] primary entered idle — starting background")
				c.setStatus("primary: idle, bg: " + c.background.Name())
				c.bgRunner.Start(ctx, client, cfg)
			} else {
				if c.bgRunner.IsRunning() {
					c.logger.Printf("[composite] primary exiting idle — interrupting background")
					cp := c.bgRunner.Interrupt()
					if !cp.IsEmpty() {
						c.logger.Printf("[composite] background checkpointed at step %q", cp.CurrentStep)
					}
				}
				c.setStatus("primary: active")
			}
		}
	}
}

// NotifyIdle is called by the primary strategy to signal idle enter/exit.
// Pass true when entering idle, false when exiting. Drains any pending
// signal first to ensure the latest state always arrives.
func (c *CompositeStrategy) NotifyIdle(idle bool) {
	// Drain any stale signal so the new one is guaranteed to arrive.
	select {
	case <-c.idleCh:
	default:
	}
	c.idleCh <- idle
}

func (c *CompositeStrategy) setStatus(status string) {
	c.mu.Lock()
	c.status = status
	c.mu.Unlock()
}
