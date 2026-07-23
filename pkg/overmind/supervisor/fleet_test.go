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

func TestMarkRestartResetsProgressClock(t *testing.T) {
	f := NewFleet()
	now := time.Now()
	f.ApplyHello(control.Hello{AgentID: "a"}, 1, now)
	f.ApplyStatus("a", control.Status{System: "x"}, now)

	f.MarkRestart("a")

	w := f.Snapshot()[0]
	if !w.LastProgress.IsZero() {
		t.Fatalf("MarkRestart must zero LastProgress, got %v", w.LastProgress)
	}
	// A zero LastProgress disables Stalled until the new process reports in —
	// this is the double-fire regression guard.
	if Stalled(w, now.Add(time.Hour), time.Minute) {
		t.Fatal("freshly restarted worker must not be Stalled")
	}
}

func TestStallRestartCounter(t *testing.T) {
	f := NewFleet()
	now := time.Now()
	f.ApplyHello(control.Hello{AgentID: "a"}, 1, now)

	f.MarkStallRestart("a")
	f.MarkStallRestart("a")
	if got := f.Snapshot()[0].StallRestarts; got != 2 {
		t.Fatalf("StallRestarts = %d, want 2", got)
	}

	// Forward progress resets the counter.
	f.ApplyStatus("a", control.Status{System: "x"}, now)
	if got := f.Snapshot()[0].StallRestarts; got != 0 {
		t.Fatalf("progress must reset StallRestarts, got %d", got)
	}
}

func TestStallRestartCounterSurvivesIdenticalHeartbeat(t *testing.T) {
	f := NewFleet()
	now := time.Now()
	st := control.Status{System: "x", POI: "p", Fuel: 0, MaxFuel: 100}
	f.ApplyHello(control.Hello{AgentID: "a"}, 1, now)
	f.ApplyStatus("a", st, now)
	f.MarkStallRestart("a")
	f.MarkRestart("a")
	// worker relaunches: Hello then an identical (still-stranded) heartbeat
	f.ApplyHello(control.Hello{AgentID: "a"}, 2, now.Add(time.Second))
	f.ApplyStatus("a", st, now.Add(2*time.Second))
	if got := f.Snapshot()[0].StallRestarts; got != 1 {
		t.Fatalf("identical post-restart heartbeat must not reset counter, got %d", got)
	}
}

func TestStranded(t *testing.T) {
	now := time.Now()
	base := func(mut func(*WorkerInfo)) WorkerInfo {
		w := WorkerInfo{
			AgentID:      "a",
			Role:         "hauler",
			LastProgress: now.Add(-time.Hour),
			LastStatus:   control.Status{Docked: false, Fuel: 2, MaxFuel: 420},
		}
		if mut != nil {
			mut(&w)
		}
		return w
	}
	timeout := 15 * time.Minute
	cases := []struct {
		name string
		w    WorkerInfo
		want bool
	}{
		{"fuel dead big tank", base(nil), true}, // 2 < max(42, 10)
		{"fuel below floor small tank", base(func(w *WorkerInfo) { // 8 < max(5, 10)
			w.LastStatus.MaxFuel, w.LastStatus.Fuel = 50, 8
		}), true},
		{"fuel healthy", base(func(w *WorkerInfo) { w.LastStatus.Fuel = 60 }), false},
		{"not stalled yet", base(func(w *WorkerInfo) { w.LastProgress = now }), false},
		{"docked exempt", base(func(w *WorkerInfo) { w.LastStatus.Docked = true }), false},
		{"drained exempt", base(func(w *WorkerInfo) { w.LastStatus.Drained = true }), false},
		{"never seen exempt", base(func(w *WorkerInfo) { w.LastProgress = time.Time{} }), false},
		{"assist role exempt from fuel check", base(func(w *WorkerInfo) { w.Role = "assist" }), false},
		{"escalation trips regardless of fuel", base(func(w *WorkerInfo) {
			w.LastStatus.Fuel, w.StallRestarts = 400, 3
		}), true},
		{"escalation below limit", base(func(w *WorkerInfo) {
			w.LastStatus.Fuel, w.StallRestarts = 400, 2
		}), false},
		{"assist escalation still trips", base(func(w *WorkerInfo) {
			w.Role, w.StallRestarts = "assist", 3
		}), true},
	}
	for _, tc := range cases {
		got, reason := Stranded(tc.w, now, timeout, 0.10, 10, 3)
		if got != tc.want {
			t.Errorf("%s: Stranded = %v (reason %q), want %v", tc.name, got, reason, tc.want)
		}
		if got && reason == "" {
			t.Errorf("%s: stranded must carry a reason", tc.name)
		}
	}
}

func TestQuarantineLifecycle(t *testing.T) {
	f := NewFleet()
	now := time.Now()
	f.ApplyHello(control.Hello{AgentID: "a"}, 1, now)
	f.ApplyStatus("a", control.Status{System: "x"}, now)
	f.MarkStallRestart("a")

	f.Quarantine("a", "fuel-dead")
	w := f.Snapshot()[0]
	if !w.Quarantined || w.QuarantineReason != "fuel-dead" || w.Healthy {
		t.Fatalf("quarantine not recorded: %+v", w)
	}
	if !f.IsQuarantined("a") {
		t.Fatal("IsQuarantined false after Quarantine")
	}

	f.ClearQuarantine("a")
	w = f.Snapshot()[0]
	if w.Quarantined || w.QuarantineReason != "" {
		t.Fatalf("quarantine not cleared: %+v", w)
	}
	if w.StallRestarts != 0 || !w.LastProgress.IsZero() {
		t.Fatalf("ClearQuarantine must reset stall state, got restarts=%d progress=%v",
			w.StallRestarts, w.LastProgress)
	}
	// Quarantine on an unknown agent creates the entry (boot-restore path).
	f.Quarantine("newcomer", "restored from queue")
	if !f.IsQuarantined("newcomer") {
		t.Fatal("Quarantine must create missing entries for boot restore")
	}
}

func TestFleetLeavingAndRemove(t *testing.T) {
	f := NewFleet()
	f.ApplyHello(control.Hello{AgentID: "a1", Role: "missionrunner"}, 42, time.Now())
	f.MarkLeaving("a1")
	snap := f.Snapshot()
	if len(snap) != 1 || !snap[0].Leaving {
		t.Fatalf("after MarkLeaving: snapshot = %+v, want one entry with Leaving=true", snap)
	}
	f.ClearLeaving("a1")
	if snap = f.Snapshot(); snap[0].Leaving {
		t.Fatal("ClearLeaving did not clear the flag")
	}
	f.MarkLeaving("a1")
	f.Remove("a1")
	if got := f.Snapshot(); len(got) != 0 {
		t.Fatalf("after Remove: snapshot has %d entries, want 0", len(got))
	}
	// Remove of an unknown agent must not create an entry.
	f.Remove("ghost")
	if got := f.Snapshot(); len(got) != 0 {
		t.Fatalf("Remove(ghost) created an entry: %+v", got)
	}
}

func TestApplyHelloStoresBuildIdentity(t *testing.T) {
	f := NewFleet()
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	f.ApplyHello(control.Hello{
		AgentID: "hauler-3", Role: "hauler", Station: "ST-1",
		Version: "v0.3.0", Commit: "8016cd8abcde", BuiltAt: "2026-07-23T10:00:00Z",
		CodeDirty: true, Modified: true,
	}, 7, now)
	snap := f.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("want 1 worker, got %d", len(snap))
	}
	w := snap[0]
	if w.Version != "v0.3.0" || w.Commit != "8016cd8abcde" || w.BuiltAt != "2026-07-23T10:00:00Z" {
		t.Fatalf("build identity not stored: %+v", w)
	}
	if !w.CodeDirty || !w.Modified {
		t.Fatalf("dirty flags not stored: CodeDirty=%v Modified=%v", w.CodeDirty, w.Modified)
	}
}
