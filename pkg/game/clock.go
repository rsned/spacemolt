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

// syncLoop calls get_notifications every 5 minutes to re-sync with the server.
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
			gc.tick.Store(serverTick)
			if drift := serverTick - localTick; drift != 0 {
				logger.Printf("Game clock synced: tick %d (drift %+d)", serverTick, drift)
			}
		}
	}
}
