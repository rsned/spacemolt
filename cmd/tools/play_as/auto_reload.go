package main

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/rsned/spacemolt/internal/protocol"
)

// Keeping a magazine loaded has two possible shapes: count rounds locally and
// reload the moment a weapon reaches zero, or wait for the server to say so
// and answer that. Counting means modelling shots per tick, cooldowns, misses
// and every weapon's magazine independently, and it is wrong the first time
// any of those assumptions slips. The server already sends the fact — an
// error frame with code "out_of_ammo", repeated every tick for as long as the
// weapon is dry — so this answers the notification instead.

// isOutOfAmmo reports the server's dry-magazine refusal. The code is the
// contract; the human-readable message is not matched, since it is free to
// change wording.
func isOutOfAmmo(resp protocol.Response) bool {
	switch resp.Type {
	case protocol.TypeError, protocol.TypeActionError:
	default:
		return false
	}
	code, _ := resp.Payload["code"].(string)
	return code == "out_of_ammo"
}

// autoReloader turns repeated out_of_ammo frames into at most one reload pass
// at a time. The trigger channel has capacity 1, so the per-tick repeats that
// arrive while a reload is in flight collapse into a single follow-up pass
// rather than queueing a burst of tick-spending commands.
type autoReloader struct {
	enabled atomic.Bool
	trigger chan struct{}
	reload  func(context.Context) error
}

func newAutoReloader(reload func(context.Context) error) *autoReloader {
	return &autoReloader{
		trigger: make(chan struct{}, 1),
		reload:  reload,
	}
}

// SetEnabled arms or disarms the responder (`set_autoreload`).
func (a *autoReloader) SetEnabled(on bool) { a.enabled.Store(on) }

// Enabled reports the current arming state.
func (a *autoReloader) Enabled() bool { return a.enabled.Load() }

// Notify is called from the push handler for every server frame. It runs
// inside the router's dispatch path, so it must never block or issue a
// command — it only rings the bell.
func (a *autoReloader) Notify(resp protocol.Response) {
	if !a.enabled.Load() || !isOutOfAmmo(resp) {
		return
	}
	select {
	case a.trigger <- struct{}{}:
	default: // a pass is already queued; the repeats are the same fact
	}
}

// Run services triggers until ctx ends. One pass at a time.
func (a *autoReloader) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-a.trigger:
			if !a.enabled.Load() {
				continue
			}
			if err := a.reload(ctx); err != nil {
				fmt.Printf("\r%sauto-reload: %v%s\n", ansiYellow, err, ansiReset)
			}
		}
	}
}
