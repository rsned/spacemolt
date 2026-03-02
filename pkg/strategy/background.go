package strategy

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// BackgroundRunnerConfig configures a BackgroundRunner.
type BackgroundRunnerConfig struct {
	CleanupTimeout time.Duration
	Logger         *log.Logger
}

// BackgroundRunner wraps a Strategy with interrupt/checkpoint support for
// running as a background skill during idle windows of a primary skill.
type BackgroundRunner struct {
	strategy Strategy
	config   BackgroundRunnerConfig
	logger   *log.Logger

	mu         sync.Mutex
	running    bool
	cancel     context.CancelFunc
	done       chan struct{}
	checkpoint *SkillCheckpoint
}

// NewBackgroundRunner creates a background runner for the given strategy.
func NewBackgroundRunner(strategy Strategy, config BackgroundRunnerConfig) *BackgroundRunner {
	logger := config.Logger
	if logger == nil {
		logger = log.Default()
	}
	return &BackgroundRunner{
		strategy:   strategy,
		config:     config,
		logger:     logger,
		checkpoint: &SkillCheckpoint{},
	}
}

// Start begins executing the background strategy in a goroutine.
func (b *BackgroundRunner) Start(parent context.Context, client *game.Client, cfg Config) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.running {
		return
	}

	ctx, cancel := context.WithCancel(parent)
	b.cancel = cancel
	b.running = true
	b.done = make(chan struct{})

	go func() {
		defer func() {
			b.mu.Lock()
			b.running = false
			b.mu.Unlock()
			close(b.done)
		}()

		b.logger.Printf("[bg:%s] background skill started", b.strategy.Name())
		err := b.strategy.Run(ctx, client, cfg)
		if err != nil && ctx.Err() == nil {
			b.logger.Printf("[bg:%s] background skill error: %v", b.strategy.Name(), err)
		} else if err == nil {
			b.logger.Printf("[bg:%s] background skill completed naturally", b.strategy.Name())
			b.mu.Lock()
			b.checkpoint.Clear()
			b.mu.Unlock()
		} else {
			b.logger.Printf("[bg:%s] background skill interrupted", b.strategy.Name())
		}
	}()
}

// Interrupt signals the background skill to stop and waits for cleanup.
func (b *BackgroundRunner) Interrupt() *SkillCheckpoint {
	b.mu.Lock()
	if !b.running {
		cp := *b.checkpoint
		b.mu.Unlock()
		return &cp
	}

	cancel := b.cancel
	done := b.done
	b.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	timeout := b.config.CleanupTimeout
	if timeout == 0 {
		timeout = 2 * time.Minute
	}

	select {
	case <-done:
		b.logger.Printf("[bg:%s] background skill stopped cleanly", b.strategy.Name())
	case <-time.After(timeout):
		b.logger.Printf("[bg:%s] cleanup timeout exceeded, force-stopping", b.strategy.Name())
		b.mu.Lock()
		b.running = false
		b.mu.Unlock()
	}

	b.mu.Lock()
	cp := *b.checkpoint
	b.mu.Unlock()
	return &cp
}

// IsRunning returns whether the background skill is currently executing.
func (b *BackgroundRunner) IsRunning() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.running
}

// GetCheckpoint returns a copy of the current checkpoint.
func (b *BackgroundRunner) GetCheckpoint() SkillCheckpoint {
	b.mu.Lock()
	defer b.mu.Unlock()
	return *b.checkpoint
}

// SetCheckpoint updates the stored checkpoint.
func (b *BackgroundRunner) SetCheckpoint(cp SkillCheckpoint) {
	b.mu.Lock()
	defer b.mu.Unlock()
	*b.checkpoint = cp
}

// Strategy returns the wrapped strategy.
func (b *BackgroundRunner) Strategy() Strategy {
	return b.strategy
}
