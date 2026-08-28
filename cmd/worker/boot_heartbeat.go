package main

import (
	"context"
	"sync"
	"time"

	"github.com/rsned/spacemolt/pkg/overmind/control"
)

// bootActivity is what the fleet table shows for a worker that has greeted the
// supervisor but has not finished authenticating yet.
const bootActivity = "connecting to game server"

// statusSender sends one Status frame to the supervisor.
type statusSender func(control.Status) error

// startBootHeartbeat emits Status frames on interval until the returned stop is
// called, reporting the game connection as down.
//
// It closes a gap that cost us a self-sustaining login storm. The worker greets
// the supervisor at Step 4 and only starts its real heartbeat loop at Step 8,
// after game login -- so a worker slow to authenticate sent hello and then went
// completely quiet. The supervisor saw an ESTABLISHED worker (hello had set
// LastSeen) fall silent, and NeedsRestart's 90s SilenceTimeout killed it mid
// login: "initialize agent: context canceled" at cmd/worker/main.go's Step 5.
// The respawn then forced another fresh login into the same per-IP rate block
// that had stalled the first one, which deepened the block, which stalled the
// next login. Live 2026-08-28 that ran pirate-14 to 17 restarts and pirate-13
// to 14 inside an hour.
//
// The supervisor already knows how to handle this case correctly -- its
// DisconnectGrace branch leaves a game-disconnected but still-heartbeating
// worker to the fleet-wide reconnect gate for 30 minutes, precisely because "a
// restart here forces a fresh login that cannot succeed during a block and
// deepens it". That branch was simply unreachable during a first login, because
// reporting Disconnected requires a heartbeat and there was none. These frames
// make it reachable, and let BootTimeout govern cold start as intended.
//
// onErr, when non-nil, is called for each failed send. A send failure never
// stops the loop: losing the control socket must not also abandon the login.
func startBootHeartbeat(ctx context.Context, send statusSender, interval time.Duration, onErr func(error)) (stop func()) {
	ctx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := send(bootStatus(time.Now())); err != nil && onErr != nil {
					onErr(err)
				}
			}
		}
	}()
	// Synchronous by contract: once stop returns, no further frame can race the
	// real heartbeat loop, which takes ownership of the same encoder.
	return func() {
		cancel()
		wg.Wait()
	}
}

// bootStatus is the frame sent while authenticating. Disconnected is the load
// bearing field -- it is what selects the supervisor's DisconnectGrace branch
// over the silence kill. The rest is deliberately zero: we have no game state
// yet, and Activity says so rather than letting the fleet table imply a worker
// sitting at 0% hull.
func bootStatus(now time.Time) control.Status {
	return control.Status{
		Disconnected: true,
		Activity:     bootActivity,
		Timestamp:    now.Format(time.RFC3339Nano),
	}
}
