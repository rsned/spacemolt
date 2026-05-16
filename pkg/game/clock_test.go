package game

import (
	"context"
	"log"
	"os"
	"testing"
	"time"

	"github.com/rsned/spacemolt/internal/protocol"
)

func TestGameClockInitialSync(t *testing.T) {
	client := newTestClient()
	client.state.CurrentTick = 500
	logger := log.New(os.Stderr, "[test] ", 0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	gc, err := NewGameClock(ctx, client, logger)
	if err != nil {
		t.Fatal(err)
	}
	defer gc.Stop()

	if tick := gc.Tick(); tick != 500 {
		t.Errorf("initial tick = %d, want 500", tick)
	}
}

func TestGameClockIncrementsLocally(t *testing.T) {
	client := newTestClient()
	client.state.CurrentTick = 100
	logger := log.New(os.Stderr, "[test] ", 0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	gc, err := NewGameClock(ctx, client, logger)
	if err != nil {
		t.Fatal(err)
	}
	defer gc.Stop()

	// Manually advance tick to simulate what tickLoop does
	gc.tick.Add(3)

	if tick := gc.Tick(); tick != 103 {
		t.Errorf("tick after 3 increments = %d, want 103", tick)
	}
}

func TestGameClockSync(t *testing.T) {
	client := newTestClient()
	client.state.CurrentTick = 100
	logger := log.New(os.Stderr, "[test] ", 0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	gc, err := NewGameClock(ctx, client, logger)
	if err != nil {
		t.Fatal(err)
	}
	defer gc.Stop()

	// Simulate drift: local clock has advanced but server is ahead
	gc.tick.Store(105)
	client.state.CurrentTick = 110

	// Manual sync (what syncLoop does)
	_ = client.GetNotifications(ctx)
	serverTick := client.GetState().CurrentTick
	gc.tick.Store(serverTick)

	if tick := gc.Tick(); tick != 110 {
		t.Errorf("tick after sync = %d, want 110", tick)
	}
}

func TestGameClockStop(t *testing.T) {
	client := newTestClient()
	client.state.CurrentTick = 50
	logger := log.New(os.Stderr, "[test] ", 0)
	ctx := context.Background()

	gc, err := NewGameClock(ctx, client, logger)
	if err != nil {
		t.Fatal(err)
	}

	gc.Stop()
	// Give goroutines time to exit
	time.Sleep(50 * time.Millisecond)

	// After stop, Tick still returns last value
	if tick := gc.Tick(); tick != 50 {
		t.Errorf("tick after stop = %d, want 50", tick)
	}
}

// newTestClient creates a minimal Client suitable for clock tests.
// sendOverride stubs out the WebSocket send so GetNotifications doesn't fail,
// and simulates an immediate "ok" response with current_tick from state.
func newTestClient() *Client {
	ctx, cancel := context.WithCancel(context.Background())
	c := &Client{
		state: &State{
			CurrentTick: 0,
			Ship:        Ship{Cargo: []CargoItem{}},
			System:      SystemData{POIs: []POI{}, Connections: []ConnectionInfo{}},
		},
		debugLogger:     log.New(os.Stderr, "", 0),
		readyChan:       make(chan struct{}),
		stopCh:          make(chan struct{}),
		router:          newResponseRouter(),
		goroutineCtx:    ctx,
		goroutineCancel: cancel,
	}
	// Stub Send to simulate an ok response with the current tick.
	// Dispatches via the router after a brief delay so subscribers are registered.
	c.sendOverride = func(_ context.Context, _ protocol.Message) error {
		go func() {
			time.Sleep(20 * time.Millisecond)
			c.router.dispatch(protocol.Response{
				Type:    protocol.TypeOK,
				Payload: map[string]any{"current_tick": float64(c.state.CurrentTick)},
			})
		}()
		return nil
	}
	return c
}
