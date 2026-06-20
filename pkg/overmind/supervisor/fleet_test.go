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
