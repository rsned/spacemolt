package tasks

import (
	"io"
	"log"
	"testing"

	"github.com/rsned/spacemolt/pkg/overmind/control"
	"github.com/rsned/spacemolt/pkg/overmind/supervisor"
)

type fakeSender struct{ sent map[string]control.Assign }

func (f *fakeSender) Send(agentID string, env control.Envelope) error {
	if f.sent == nil {
		f.sent = map[string]control.Assign{}
	}
	var a control.Assign
	_ = env.Into(&a)
	f.sent[agentID] = a
	return nil
}

func idleWorker(id, role string) supervisor.WorkerInfo {
	return supervisor.WorkerInfo{AgentID: id, Role: role, Healthy: true}
}

func newStore(ts ...Task) *Store {
	return NewStore(ts, log.New(io.Discard, "", 0))
}

func TestAssignByRoleToIdleWorker(t *testing.T) {
	s := newStore(Task{ID: "t1", Script: "mining_run", RoleRequired: "miner", Status: StatusPending})
	fs := &fakeSender{}
	s.AssignPending([]supervisor.WorkerInfo{idleWorker("explorer-1", "explorer"), idleWorker("miner-2", "miner")}, fs)
	if _, ok := fs.sent["miner-2"]; !ok {
		t.Fatalf("expected assign sent to miner-2, sent=%v", fs.sent)
	}
	if s.Snapshot()[0].Status != StatusAssigned || s.Snapshot()[0].AssignedTo != "miner-2" {
		t.Fatalf("task not marked assigned: %+v", s.Snapshot()[0])
	}
}

func TestAssignHonorsPin(t *testing.T) {
	s := newStore(Task{ID: "t1", Script: "mining_run", RoleRequired: "miner", AgentID: "miner-5", Status: StatusPending})
	fs := &fakeSender{}
	s.AssignPending([]supervisor.WorkerInfo{idleWorker("miner-2", "miner"), idleWorker("miner-5", "miner")}, fs)
	if _, ok := fs.sent["miner-5"]; !ok {
		t.Fatalf("expected assign to pinned miner-5, sent=%v", fs.sent)
	}
}

func TestAssignSkipsBusyWorker(t *testing.T) {
	s := newStore(Task{ID: "t1", Script: "mining_run", RoleRequired: "miner", Status: StatusPending})
	busy := idleWorker("miner-2", "miner")
	busy.LastStatus = control.Status{ActiveTaskID: "other"}
	fs := &fakeSender{}
	s.AssignPending([]supervisor.WorkerInfo{busy}, fs)
	if len(fs.sent) != 0 {
		t.Fatalf("expected no assignment to busy worker, sent=%v", fs.sent)
	}
	if s.Snapshot()[0].Status != StatusPending {
		t.Fatalf("task should stay pending, got %q", s.Snapshot()[0].Status)
	}
}

func TestHandleEventMarksDone(t *testing.T) {
	s := newStore(Task{ID: "t1", Script: "mining_run", RoleRequired: "miner", Status: StatusAssigned, AssignedTo: "miner-2"})
	s.HandleEvent("miner-2", control.Event{Kind: "task_done", Detail: "t1"})
	if s.Snapshot()[0].Status != StatusDone {
		t.Fatalf("task not marked done: %+v", s.Snapshot()[0])
	}
}

func TestHandleEventMarksFailed(t *testing.T) {
	s := newStore(Task{ID: "t1", Script: "mining_run", RoleRequired: "miner", Status: StatusAssigned, AssignedTo: "miner-2"})
	s.HandleEvent("miner-2", control.Event{Kind: "task_failed", Detail: "t1: jump failed"})
	if s.Snapshot()[0].Status != StatusFailed {
		t.Fatalf("task not marked failed: %+v", s.Snapshot()[0])
	}
}

func TestReassignsOnWorkerDeath(t *testing.T) {
	s := newStore(Task{ID: "t1", Script: "mining_run", RoleRequired: "miner", Status: StatusAssigned, AssignedTo: "miner-2"})
	fs := &fakeSender{}
	// miner-2 is gone from the snapshot (died); a fresh idle miner-9 is present.
	s.AssignPending([]supervisor.WorkerInfo{idleWorker("miner-9", "miner")}, fs)
	if _, ok := fs.sent["miner-9"]; !ok {
		t.Fatalf("expected reassignment to miner-9 after death, sent=%v", fs.sent)
	}
}

// TestReassignsOnWorkerUnhealthy covers the production case: Fleet is ADD-ONLY
// so a dead worker stays in the snapshot with Healthy=false. The absent-only
// check would never fire; we must also gate on !Healthy.
func TestReassignsOnWorkerUnhealthy(t *testing.T) {
	s := newStore(Task{ID: "t1", Script: "mining_run", RoleRequired: "miner", Status: StatusAssigned, AssignedTo: "miner-2"})
	fs := &fakeSender{}
	// miner-2 is present but unhealthy (died; Fleet kept the entry via MarkRestart).
	dead := supervisor.WorkerInfo{AgentID: "miner-2", Role: "miner", Healthy: false}
	healthy := idleWorker("miner-9", "miner")
	s.AssignPending([]supervisor.WorkerInfo{dead, healthy}, fs)
	// Task must be reassigned to the healthy worker, not the dead one.
	a, ok := fs.sent["miner-9"]
	if !ok {
		t.Fatalf("expected reassignment to miner-9, sent=%v", fs.sent)
	}
	if a.TaskID != "t1" {
		t.Fatalf("wrong task reassigned: %q", a.TaskID)
	}
	if _, bad := fs.sent["miner-2"]; bad {
		t.Fatalf("unhealthy worker miner-2 must not receive assignment")
	}
}
