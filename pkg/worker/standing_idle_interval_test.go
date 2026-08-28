package worker

import (
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

// The idle interval defaulted to game.SleepShort, which is SleepTick/3 --
// 3.33 seconds. That ran every worker's idle pass THREE TIMES per game tick,
// and across a 144-worker fleet worked out to ~43 idle passes per second.
//
// Nothing can usefully run faster than the game advances: a tick is 10s, and a
// mutation is capped at 1 per tick per agent regardless. Every pass above that
// rate is pure overhead that can only produce redundant calls -- and on
// 2026-08-27 the fleet spent 4.5 hours IP-blocked, with find_route timeouts and
// "Your IP has been temporarily blocked" stranding seven miners.
//
// One tick is the floor: it matches the mutation limit exactly, and each
// command's own response time is added on top by the blocking dispatch.
func TestDefaultIdleInterval_IsNotFasterThanAGameTick(t *testing.T) {
	deps := StandingDeps{}
	applyStandingDefaults(&deps)

	if deps.IdleInterval < game.SleepTick {
		t.Errorf("default IdleInterval = %v, want >= one game tick (%v); "+
			"a faster loop cannot produce useful work and only burns rate budget",
			deps.IdleInterval, game.SleepTick)
	}
}

// An explicit interval must still win, so a caller can slow a fleet further.
func TestDefaultIdleInterval_RespectsExplicitValue(t *testing.T) {
	deps := StandingDeps{IdleInterval: 45 * game.SleepTick}
	applyStandingDefaults(&deps)

	if deps.IdleInterval != 45*game.SleepTick {
		t.Errorf("IdleInterval = %v, want the explicit 45 ticks", deps.IdleInterval)
	}
}
