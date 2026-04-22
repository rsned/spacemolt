package game

import (
	"context"
	"log"
	"sync/atomic"
	"time"
)

const (
	// clockTickInterval is how often the clock advances by one tick locally.
	clockTickInterval = SleepTick // 10s, matches game server tick rate

	// clockSyncInterval is how often the clock re-syncs with the server.
	clockSyncInterval = 600 * time.Second // 5 minutes
)

// GameClock tracks the game server's tick count. It increments locally every
// 10 seconds and re-synchronizes with the server every 5 minutes via
// get_notifications.
type GameClock struct {
	tick   atomic.Int64
	cancel context.CancelFunc
}

// NewGameClock creates a GameClock, performs an initial sync via get_notifications,
// and starts background goroutines to increment and periodically re-sync.
func NewGameClock(ctx context.Context, client GameClient, logger *log.Logger) (*GameClock, error) {
	gc := &GameClock{}

	// Initial sync
	if err := client.GetNotifications(ctx); err != nil {
		return nil, err
	}
	initialTick := client.GetState().CurrentTick
	gc.tick.Store(initialTick)
	logger.Printf("Game clock initialized at tick %d", initialTick)

	// Start background goroutines
	clockCtx, cancel := context.WithCancel(ctx)
	gc.cancel = cancel

	go gc.tickLoop(clockCtx)
	go gc.syncLoop(clockCtx, client, logger)

	return gc, nil
}

// Tick returns the current estimated game tick.
func (gc *GameClock) Tick() int64 {
	return gc.tick.Load()
}

// Stop shuts down the background goroutines.
func (gc *GameClock) Stop() {
	if gc.cancel != nil {
		gc.cancel()
	}
}

// tickLoop increments the tick counter every 10 seconds.
func (gc *GameClock) tickLoop(ctx context.Context) {
	ticker := time.NewTicker(clockTickInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			gc.tick.Add(1)
		}
	}
}

// syncLoop periodically reconciles the local clock with the server tick
// observed on the client's State. Only adopts the server tick when it is
// *ahead* of our local counter — never rolls backward.
//
// Rationale: over MCP, GetNotifications actively refreshes state.CurrentTick,
// so this call typically pulls a fresh value forward. Over WS, GetNotifications
// is a no-op (the server rejects it and tick is delivered via push events).
// If no tick-bearing push arrived in the last interval, state.CurrentTick is
// stale and will appear *behind* the local counter; overwriting would undo
// real ticks (observed drift was exactly -clockSyncInterval/clockTickInterval
// every cycle). A monotonic clock is the right default — the 10s local ticker
// matches the server's tick rate (SleepTick), so unsynced drift is bounded and
// the next tick-bearing response will pull us forward.
func (gc *GameClock) syncLoop(ctx context.Context, client GameClient, logger *log.Logger) {
	ticker := time.NewTicker(clockSyncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := client.GetNotifications(ctx); err != nil {
				logger.Printf("Game clock sync failed: %v", err)
				continue
			}
			serverTick := client.GetState().CurrentTick
			localTick := gc.tick.Load()
			if serverTick > localTick {
				gc.tick.Store(serverTick)
				logger.Printf("Game clock synced: tick %d (drift %+d)", serverTick, serverTick-localTick)
			}
		}
	}
}
