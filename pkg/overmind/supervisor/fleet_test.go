package supervisor

import (
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/overmind/control"
)

func TestFleetApplyAndSnapshot(t *testing.T) {
	f := NewFleet()
	t0 := time.Unix(1000, 0)
	f.ApplyHello(control.Hello{AgentID: "b", Role: "resident", Station: "S2"}, 11, t0)
	f.ApplyHello(control.Hello{AgentID: "a", Role: "hauler", Station: "S1"}, 22, t0)
	f.ApplyStatus("a", control.Status{System: "SOL", Credits: 50}, t0.Add(time.Second))

	snap := f.Snapshot()
	if len(snap) != 2 || snap[0].AgentID != "a" { // sorted
		t.Fatalf("snapshot wrong: %+v", snap)
	}
	if snap[0].PID != 22 || snap[0].LastStatus.System != "SOL" {
		t.Fatalf("agent a info wrong: %+v", snap[0])
	}
}

func TestNeedsRestart(t *testing.T) {
	now := time.Unix(2000, 0)
	healthy := WorkerInfo{LastSeen: now.Add(-5 * time.Second)}
	stale := WorkerInfo{LastSeen: now.Add(-40 * time.Second)}
	if NeedsRestart(healthy, now, 30*time.Second) {
		t.Fatalf("healthy worker flagged for restart")
	}
	if !NeedsRestart(stale, now, 30*time.Second) {
		t.Fatalf("stale worker not flagged")
	}
}

// Stalled fires only for a worker that is heartbeating but frozen: undocked, with
// no progress for longer than the timeout. Docked/drained idle postures and healthy
// mobile workers are exempt.
func TestStalled(t *testing.T) {
	now := time.Unix(100000, 0)
	stall := 15 * time.Minute
	base := func(mod func(*WorkerInfo)) WorkerInfo {
		w := WorkerInfo{
			LastProgress: now.Add(-20 * time.Minute), // 20min since last progress
			LastStatus:   control.Status{System: "maplevale", Docked: false},
		}
		mod(&w)
		return w
	}

	if !Stalled(base(func(*WorkerInfo) {}), now, stall) {
		t.Error("undocked worker frozen 20min should be stalled")
	}
	if Stalled(base(func(w *WorkerInfo) { w.LastStatus.Docked = true }), now, stall) {
		t.Error("docked idle worker must be exempt (resident/camping shuttle)")
	}
	if Stalled(base(func(w *WorkerInfo) { w.LastStatus.Drained = true }), now, stall) {
		t.Error("drained worker must be exempt")
	}
	if Stalled(base(func(w *WorkerInfo) { w.LastProgress = now.Add(-2 * time.Minute) }), now, stall) {
		t.Error("worker that progressed 2min ago is within the window, not stalled")
	}
	if Stalled(base(func(w *WorkerInfo) { w.LastProgress = time.Time{} }), now, stall) {
		t.Error("never-seen worker (zero LastProgress) must not be stalled")
	}
	if Stalled(base(func(*WorkerInfo) {}), now, 0) {
		t.Error("non-positive timeout must disable the watchdog")
	}
}

// ApplyStatus advances LastProgress only when the status shows forward motion, so a
// worker that heartbeats forever from a frozen state stops advancing its progress
// clock — which is what lets Stalled catch it.
func TestApplyStatusTracksProgress(t *testing.T) {
	f := NewFleet()
	t0 := time.Unix(1000, 0)
	f.ApplyHello(control.Hello{AgentID: "a", Role: "shuttle"}, 1, t0)

	// First status: system set (progress from the Hello baseline).
	f.ApplyStatus("a", control.Status{System: "maplevale", Credits: 1374712}, t0.Add(time.Minute))
	lp := f.Snapshot()[0].LastProgress

	// Identical status 10min later: no progress -> LastProgress unchanged.
	f.ApplyStatus("a", control.Status{System: "maplevale", Credits: 1374712}, t0.Add(11*time.Minute))
	if got := f.Snapshot()[0].LastProgress; !got.Equal(lp) {
		t.Fatalf("frozen status advanced LastProgress: was %v, now %v", lp, got)
	}
	// Credits change: progress -> LastProgress advances.
	adv := t0.Add(12 * time.Minute)
	f.ApplyStatus("a", control.Status{System: "maplevale", Credits: 1400000}, adv)
	if got := f.Snapshot()[0].LastProgress; !got.Equal(adv) {
		t.Fatalf("credit change did not advance LastProgress: want %v, got %v", adv, got)
	}
}

func TestMarkRestartIncrements(t *testing.T) {
	f := NewFleet()
	t0 := time.Unix(1000, 0)
	f.ApplyHello(control.Hello{AgentID: "a"}, 1, t0)
	f.MarkRestart("a")
	f.MarkRestart("a")
	if f.Snapshot()[0].Restarts != 2 {
		t.Fatalf("restart count wrong: %+v", f.Snapshot()[0])
	}
}

func TestDrainProgressCountsHealthyDrained(t *testing.T) {
	f := NewFleet()
	now := time.Unix(1000, 0)
	for _, id := range []string{"a", "b", "c"} {
		f.ApplyHello(control.Hello{AgentID: id, Role: "hauler"}, 1, now)
	}
	f.ApplyStatus("a", control.Status{Drained: true}, now)
	f.ApplyStatus("b", control.Status{Drained: false}, now)
	// c never reports a status but is healthy from hello.
	idle, total, busy := f.DrainProgress()
	if total != 3 {
		t.Fatalf("total = %d, want 3", total)
	}
	if idle != 1 {
		t.Fatalf("idle = %d, want 1", idle)
	}
	if len(busy) != 2 || busy[0] != "b" || busy[1] != "c" {
		t.Fatalf("busy = %v, want [b c]", busy)
	}
}
