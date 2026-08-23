package supervisor

import (
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/overmind/control"
)

// TestStalledMinerGetsExtendedWindow pins the miner exemption.
//
// Mining is the one activity whose successful execution is indistinguishable
// from being frozen: statusProgressed counts system, POI, credits and docked,
// and a working miner changes none of them. It sits undocked at one resource
// POI filling its hold, and ore pays nothing until it is delivered. Live
// 2026-08-23 the mining fleet carried 92 restarts against 0 for craft and
// hunt, with miner-4 killed at 88/100 iterations mid-mine every ~15 minutes.
func TestStalledMinerGetsExtendedWindow(t *testing.T) {
	now := time.Unix(100000, 0)
	stall := 15 * time.Minute

	worker := func(role string, frozenFor time.Duration, mod func(*WorkerInfo)) WorkerInfo {
		w := WorkerInfo{
			Role:         role,
			LastProgress: now.Add(-frozenFor),
			LastStatus:   control.Status{System: "treasure_cache", POI: "cache_mineral_fields", Docked: false},
		}
		if mod != nil {
			mod(&w)
		}
		return w
	}

	// The regression that motivated this: 20min > the 15min base window, so
	// the old code killed a miner that was working correctly.
	if Stalled(worker("miner", 20*time.Minute, nil), now, stall) {
		t.Error("miner frozen 20min is inside its extended window and must not be stalled")
	}
	// The exemption is a longer leash, not an exemption from the watchdog.
	if !Stalled(worker("miner", 100*time.Minute, nil), now, stall) {
		t.Error("miner frozen 100min exceeds the extended window and must be stalled")
	}
	// Every other role keeps the base window unchanged.
	for _, role := range []string{"", "hauler", "trader", "marketbot", "unlock"} {
		if !Stalled(worker(role, 20*time.Minute, nil), now, stall) {
			t.Errorf("non-miner role %q frozen 20min must still be stalled on the base window", role)
		}
	}
	// The existing docked/drained/quiesced exemptions still short-circuit first.
	if Stalled(worker("miner", 100*time.Minute, func(w *WorkerInfo) { w.LastStatus.Docked = true }), now, stall) {
		t.Error("docked miner must stay exempt regardless of the extended window")
	}
	// A disabled watchdog stays disabled.
	if Stalled(worker("miner", 100*time.Minute, nil), now, 0) {
		t.Error("stallTimeout 0 disables the watchdog for miners too")
	}
}

// TestStrandedInheritsMinerWindow guards the quarantine path. Stranded gates on
// Stalled, so a miner inside its extended window must not be quarantined as
// fuel-dead — quarantine is the expensive outcome, needing a rescue to undo.
func TestStrandedInheritsMinerWindow(t *testing.T) {
	now := time.Unix(100000, 0)
	stall := 15 * time.Minute

	miner := func(frozenFor time.Duration) WorkerInfo {
		return WorkerInfo{
			Role:         "miner",
			LastProgress: now.Add(-frozenFor),
			LastStatus: control.Status{
				System: "treasure_cache", POI: "cache_mineral_fields",
				Docked: false, Fuel: 2, MaxFuel: 130,
			},
		}
	}

	if stranded, reason := Stranded(miner(20*time.Minute), now, stall, 0.1, 10, 3); stranded {
		t.Errorf("miner inside its extended window must not be quarantined, got %q", reason)
	}
	if stranded, _ := Stranded(miner(100*time.Minute), now, stall, 0.1, 10, 3); !stranded {
		t.Error("miner past its extended window with 2/130 fuel is genuinely stranded")
	}
}
