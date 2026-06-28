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
