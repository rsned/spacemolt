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
//
// That frame only comes when the whole volley is refused. A rack where one
// gun is dry and the others still fire never gets one, and the dry gun stays
// silent for the rest of the fight. So the battle frames ring the bell too:
// once per battle tick, and at the start and end of a battle. The pass reads
// get_ship (no tick spent) and reloads only magazines that are already empty,
// since a reload spends a whole ammo item whatever is left in the magazine.

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

// battleCheckTick reports whether resp should trigger an empty-magazine check:
// battle_started and battle_ended always, battle_update once per battle tick.
// tick is the update's tick, 0 when the frame carries none.
func battleCheckTick(resp protocol.Response) (check bool, tick int64) {
	switch resp.Type {
	case protocol.TypeBattleStarted, protocol.TypeBattleEnded:
		return true, 0
	case protocol.TypeBattleUpdate:
		if t, ok := resp.Payload["tick"].(float64); ok {
			return true, int64(t)
		}
		return true, 0
	}
	return false, 0
}

// autoReloader turns repeated out_of_ammo frames into at most one reload pass
// at a time. The trigger channel has capacity 1, so the per-tick repeats that
// arrive while a reload is in flight collapse into a single follow-up pass
// rather than queueing a burst of tick-spending commands.
type autoReloader struct {
	enabled  atomic.Bool
	lastTick atomic.Int64 // last battle tick that rang the bell
	trigger  chan struct{}
	reload   func(context.Context) error
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
	if !a.enabled.Load() {
		return
	}
	if !isOutOfAmmo(resp) {
		check, tick := battleCheckTick(resp)
		if !check {
			return
		}
		if tick != 0 && a.lastTick.Swap(tick) == tick {
			return // another snapshot of a tick already checked
		}
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
