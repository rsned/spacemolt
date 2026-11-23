package worker

import (
	"sync"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// huntUnarmedGate stops a hunter that ended a sortie blocked_unarmed at a dock
// from provisioning again on every pass. It also holds a hunter whose sortie
// ended at the dock with no ground to fly to (blockNoGround).
//
// A blocked hunter has no weapon able to fire and found no fitting ammo in its
// storage or on the market under the cap, with nowhere reachable to rearm.
// Nothing the hunter does changes that; it waits on the operator (a depot
// refill, credits) or on the market. Without the gate the standing loop
// re-runs the pass every tick, and each one reads the missions, storage and
// market and writes another blocked_unarmed sortie record. Measured
// 2026-11-22: pirate-5 wrote 145 of them in 30 minutes.
//
// The hold is keyed on the station: docking somewhere else provisions at once.
// It ends after game.SleepMissionBoardPoll, the same bound a parked, dry
// hunter re-reads its board on.
//
// One instance per worker process, threaded in via HuntDeps. A nil gate never
// holds, so callers that do not thread one through (tests, play_as) behave as
// before.
type huntUnarmedGate struct {
	mu      sync.Mutex
	station string
	at      time.Time
	// noGround marks a hold set by a sortie that ended at the dock with no
	// ground to fly to, rather than by an unarmed hunter.
	noGround bool
}

// held reports whether the hunter was blocked at station recently enough that
// this pass should not provision again. noGround tells the two holds apart.
func (g *huntUnarmedGate) held(station string, now time.Time) (hold, noGround bool) {
	if g == nil {
		return false, false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.at.IsZero() || station == "" || station != g.station {
		return false, false
	}
	return now.Sub(g.at) < game.SleepMissionBoardPoll, g.noGround
}

// block records a sortie that ended blocked_unarmed at station.
func (g *huntUnarmedGate) block(station string, now time.Time) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.station, g.at, g.noGround = station, now, false
}

// blockNoGround records a sortie that ended at station's dock without ever
// leaving it: the search knew no ground for the job. Like an unarmed hunter it
// would otherwise open and close another sortie every pass (seen live
// 2026-11-22, pirate-10: one empty no_quarry sortie every 11 seconds). The
// knowledge base may learn a ground from another hunter's sighting; the hold's
// bound is how long that takes to be noticed.
func (g *huntUnarmedGate) blockNoGround(station string, now time.Time) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.station, g.at, g.noGround = station, now, true
}

// clear lifts the hold: the hunter armed itself.
func (g *huntUnarmedGate) clear() {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.station, g.at, g.noGround = "", time.Time{}, false
}
