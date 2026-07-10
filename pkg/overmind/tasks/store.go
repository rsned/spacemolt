package tasks

import (
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/rsned/spacemolt/pkg/overmind/control"
	"github.com/rsned/spacemolt/pkg/overmind/supervisor"
)

// Sender routes a control envelope to a named worker (satisfied by
// *supervisor.Server).
type Sender interface {
	Send(agentID string, env control.Envelope) error
}

// Store holds tasks in memory and matches pending ones to idle workers.
type Store struct {
	mu     sync.Mutex
	tasks  []Task
	logger *log.Logger
}

// NewStore wraps a loaded task slice.
func NewStore(ts []Task, logger *log.Logger) *Store {
	return &Store{tasks: ts, logger: logger}
}

// AssignPending reconciles assignments against the current fleet snapshot, then
// sends an Assign for each pending task to an eligible idle worker. Called each
// status tick. Tasks assigned to a worker absent from the snapshot (died) are
// returned to pending for reassignment.
func (s *Store) AssignPending(workers []supervisor.WorkerInfo, sender Sender) {
	s.mu.Lock()
	defer s.mu.Unlock()

	present := make(map[string]supervisor.WorkerInfo, len(workers))
	for _, w := range workers {
		present[w.AgentID] = w
	}

	// Reconcile: revert tasks whose assigned worker is gone (died/unregistered)
	// OR whose worker is present but unhealthy (died and Fleet kept the entry).
	// Fleet is ADD-ONLY: MarkRestart sets Healthy=false but never removes the
	// entry, so absent-only was never true in production.
	for i := range s.tasks {
		t := &s.tasks[i]
		if t.Status == StatusAssigned || t.Status == StatusRunning {
			w, ok := present[t.AssignedTo]
			if !ok || !w.Healthy {
				s.logger.Printf("task %q: worker %q gone or unhealthy, returning to pending", t.ID, t.AssignedTo)
				t.Status = StatusPending
				t.AssignedTo = ""
			}
		}
	}

	// Track which workers are already busy this pass so we don't double-assign.
	busy := make(map[string]bool)
	for _, w := range workers {
		if w.LastStatus.ActiveTaskID != "" {
			busy[w.AgentID] = true
		}
	}
	for i := range s.tasks {
		if s.tasks[i].Status == StatusAssigned || s.tasks[i].Status == StatusRunning {
			busy[s.tasks[i].AssignedTo] = true
		}
	}

	for i := range s.tasks {
		t := &s.tasks[i]
		if t.Status != StatusPending {
			continue
		}
		worker, ok := s.pickWorker(t, workers, busy)
		if !ok {
			continue // none eligible this pass; retried next tick
		}
		env, err := control.NewEnvelope(control.TypeAssign, worker, control.Assign{
			TaskID: t.ID, Script: t.Script, Params: t.Params,
		})
		if err != nil {
			s.logger.Printf("task %q: build assign envelope: %v", t.ID, err)
			continue
		}
		if err := sender.Send(worker, env); err != nil {
			s.logger.Printf("task %q: send assign to %q: %v", t.ID, worker, err)
			continue
		}
		t.Status = StatusAssigned
		t.AssignedTo = worker
		busy[worker] = true
		s.logger.Printf("task %q assigned to %q", t.ID, worker)
	}
}

// pickWorker returns an eligible idle worker for t: the pinned AgentID if set
// and free, otherwise the first healthy, non-busy worker of the required role.
func (s *Store) pickWorker(t *Task, workers []supervisor.WorkerInfo, busy map[string]bool) (string, bool) {
	if t.AgentID != "" {
		for _, w := range workers {
			if w.AgentID == t.AgentID && w.Healthy && !busy[w.AgentID] {
				return w.AgentID, true
			}
		}
		return "", false
	}
	for _, w := range workers {
		if w.Role == t.RoleRequired && w.Healthy && !busy[w.AgentID] {
			return w.AgentID, true
		}
	}
	return "", false
}

// HandleEvent updates task status from a worker's task_done / task_failed event.
// The event Detail begins with the task id (see the worker's OnTaskResult).
func (s *Store) HandleEvent(agentID string, ev control.Event) {
	if ev.Kind != "task_done" && ev.Kind != "task_failed" {
		return
	}
	taskID := ev.Detail
	if i := strings.IndexByte(taskID, ':'); i >= 0 {
		taskID = taskID[:i]
	}
	taskID = strings.TrimSpace(taskID)

	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.tasks {
		t := &s.tasks[i]
		if t.ID != taskID {
			continue
		}
		if ev.Kind == "task_done" {
			t.Status = StatusDone
		} else {
			t.Status = StatusFailed
		}
		s.logger.Printf("task %q -> %s (worker %q)", t.ID, t.Status, agentID)
		return
	}
}

// Snapshot returns a copy of all tasks.
func (s *Store) Snapshot() []Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Task, len(s.tasks))
	copy(out, s.tasks)
	return out
}

// Add inserts a runtime task (plan-runner injection path). Same validation as
// LoadTasks; Status is forced to pending regardless of input.
func (s *Store) Add(t Task) error {
	switch {
	case t.ID == "":
		return fmt.Errorf("tasks: empty id")
	case strings.Contains(t.ID, ":"):
		return fmt.Errorf("tasks: id %q must not contain ':'", t.ID)
	case t.Script == "":
		return fmt.Errorf("tasks: task %q has empty script", t.ID)
	case t.RoleRequired == "":
		return fmt.Errorf("tasks: task %q has empty role_required", t.ID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.tasks {
		if s.tasks[i].ID == t.ID {
			return fmt.Errorf("tasks: duplicate id %q", t.ID)
		}
	}
	t.Status = StatusPending
	t.AssignedTo = ""
	s.tasks = append(s.tasks, t)
	return nil
}

// Remove deletes the task with the given id. It reports whether a task was
// found and removed.
func (s *Store) Remove(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.tasks {
		if s.tasks[i].ID == id {
			s.tasks = append(s.tasks[:i], s.tasks[i+1:]...)
			return true
		}
	}
	return false
}

// Get returns a copy of the task with the given id, if present.
func (s *Store) Get(id string) (Task, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.tasks {
		if s.tasks[i].ID == id {
			return s.tasks[i], true
		}
	}
	return Task{}, false
}
