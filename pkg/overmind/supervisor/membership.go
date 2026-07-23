package supervisor

import (
	"context"
	"time"

	"github.com/rsned/spacemolt/pkg/overmind/control"
)

// MembershipOp identifies a live-roster change.
type MembershipOp string

// Membership operations.
const (
	MembershipAdd    MembershipOp = "add"
	MembershipRemove MembershipOp = "remove"
	MembershipUpdate MembershipOp = "update"
)

// MembershipRequest asks the supervisor to change the roster for one agent.
// For MembershipRemove only Spec.AgentID is meaningful.
type MembershipRequest struct {
	Op   MembershipOp
	Spec WorkerSpec
}

// ControlSender delivers a control envelope to one connected worker.
// *Server satisfies it; tests inject fakes.
type ControlSender interface {
	Send(agentID string, env control.Envelope) error
}

// leavingState tracks one in-progress removal, owned by the reap goroutine.
type leavingState struct {
	deadline time.Time   // force-stop once past this
	relaunch *WorkerSpec // non-nil => rolling update: relaunch after the stop
}

// EnqueueMembership queues a roster change for the next reap tick. Safe from
// any goroutine.
func (s *Supervisor) EnqueueMembership(req MembershipRequest) {
	s.memberMu.Lock()
	defer s.memberMu.Unlock()
	s.pending = append(s.pending, req)
}

// Roster returns a copy of the current specs. Safe from any goroutine.
func (s *Supervisor) Roster() []WorkerSpec {
	s.specsMu.Lock()
	defer s.specsMu.Unlock()
	out := make([]WorkerSpec, len(s.specs))
	copy(out, s.specs)
	return out
}

func (s *Supervisor) drainMembership() []MembershipRequest {
	s.memberMu.Lock()
	defer s.memberMu.Unlock()
	out := s.pending
	s.pending = nil
	return out
}

// collapseRequests keeps the LAST request per agent, preserving first-seen
// agent order.
func collapseRequests(reqs []MembershipRequest) []MembershipRequest {
	last := make(map[string]MembershipRequest, len(reqs))
	var order []string
	for _, r := range reqs {
		if _, seen := last[r.Spec.AgentID]; !seen {
			order = append(order, r.Spec.AgentID)
		}
		last[r.Spec.AgentID] = r
	}
	out := make([]MembershipRequest, 0, len(order))
	for _, id := range order {
		out = append(out, last[id])
	}
	return out
}

// applyMembership dispatches queued roster changes. Reap-goroutine only.
func (s *Supervisor) applyMembership(now time.Time) {
	for _, req := range collapseRequests(s.drainMembership()) {
		switch req.Op {
		case MembershipAdd:
			s.memberAdd(req.Spec)
		case MembershipRemove:
			s.memberRemove(req.Spec.AgentID, nil, now)
		case MembershipUpdate:
			spec := req.Spec
			s.memberRemove(spec.AgentID, &spec, now)
		}
	}
}

// memberAdd records the spec; the launch itself happens in the main reap loop
// (no tracked proc -> tryRestart), which enforces the RestartBatch budget.
func (s *Supervisor) memberAdd(spec WorkerSpec) {
	// F2: an add for an agent whose removal is still draining CANCELS that
	// removal rather than racing it. applyMembership runs before
	// progressLeaving each tick, so clearing s.leaving here lands on the
	// correct side of the ordering — progressLeaving will not see the agent and
	// cannot complete the removal (which would strip the spec and vanish the
	// agent from both the live roster and the Removed section). The worker is
	// still alive; Resume undoes the drain it already received so it does not
	// sit idle after being kept.
	if s.leaving[spec.AgentID] != nil {
		delete(s.leaving, spec.AgentID)
		s.fleet.ClearLeaving(spec.AgentID)
		s.setSpec(spec)
		if s.Sender != nil {
			if err := s.Sender.Send(spec.AgentID, control.Envelope{Type: control.TypeResume, AgentID: spec.AgentID}); err != nil {
				s.logger.Printf("membership: resume to re-added %q failed (%v)", spec.AgentID, err)
			}
		}
		s.logger.Printf("membership: add %q cancelled its in-flight removal", spec.AgentID)
		return
	}
	if s.hasSpec(spec.AgentID) {
		s.setSpec(spec) // add of an existing agent refreshes its spec
		return
	}
	s.specsMu.Lock()
	s.specs = append(s.specs, spec)
	s.specsMu.Unlock()
	delete(s.restarts, spec.AgentID) // fresh life: reset the crash-loop counter
	s.logger.Printf("membership: added %q to roster", spec.AgentID)
}

// memberRemove starts (or immediately completes) a removal. relaunch non-nil
// makes this a rolling update. Reap-goroutine only.
func (s *Supervisor) memberRemove(agentID string, relaunch *WorkerSpec, now time.Time) {
	if !s.hasSpec(agentID) && relaunch == nil {
		s.logger.Printf("membership: remove %q ignored (not in roster)", agentID)
		return
	}
	if s.fleet.IsQuarantined(agentID) {
		// Already stopped; bookkeeping only.
		s.completeRemoval(agentID, relaunch)
		return
	}
	proc := procSnapshot(s, agentID)
	if proc == nil || !proc.alive() {
		s.completeRemoval(agentID, relaunch)
		return
	}
	if s.leaving[agentID] != nil {
		return // removal already in progress
	}
	s.fleet.MarkLeaving(agentID)
	s.leaving[agentID] = &leavingState{deadline: now.Add(s.RemoveDrainTimeout), relaunch: relaunch}
	if s.Sender != nil {
		if err := s.Sender.Send(agentID, control.Envelope{Type: control.TypeDrain, AgentID: agentID}); err != nil {
			s.logger.Printf("membership: drain to %q failed (%v); will force-stop at deadline", agentID, err)
		}
	}
	s.logger.Printf("membership: removing %q — drain sent, force-stop after %s", agentID, s.RemoveDrainTimeout)
}

// progressLeaving advances in-flight removals: stop when drained or past the
// deadline, then complete (and relaunch updates through the budget).
// Reap-goroutine only.
func (s *Supervisor) progressLeaving(ctx context.Context, now time.Time, budget *int) {
	if len(s.leaving) == 0 {
		return
	}
	status := make(map[string]WorkerInfo)
	for _, w := range s.fleet.Snapshot() {
		status[w.AgentID] = w
	}
	for agentID, st := range s.leaving {
		w, seen := status[agentID]
		drained := seen && w.LastStatus.Drained
		proc := procSnapshot(s, agentID)
		gone := proc == nil || !proc.alive()
		if !gone && !drained && now.Before(st.deadline) {
			continue // still draining
		}
		if !gone {
			s.kill(proc)
		}
		relaunch := st.relaunch
		delete(s.leaving, agentID)
		s.completeRemoval(agentID, relaunch)
		if relaunch != nil {
			// Relaunch through the budget so rolling updates never burst logins.
			s.tryRestart(ctx, *relaunch, true, budget)
		}
	}
}

// completeRemoval clears every trace of the agent; if relaunch is non-nil the
// spec is re-recorded so the relaunch (or a later reap tick) can spawn it.
func (s *Supervisor) completeRemoval(agentID string, relaunch *WorkerSpec) {
	s.specsMu.Lock()
	kept := s.specs[:0]
	for _, sp := range s.specs {
		if sp.AgentID != agentID {
			kept = append(kept, sp)
		}
	}
	s.specs = kept
	if relaunch != nil {
		s.specs = append(s.specs, *relaunch)
	}
	s.specsMu.Unlock()

	s.procMu.Lock()
	delete(s.procs, agentID)
	s.procMu.Unlock()

	delete(s.restarts, agentID)
	if relaunch == nil {
		s.fleet.Remove(agentID)
		s.logger.Printf("membership: %q removed from fleet", agentID)
	} else {
		s.fleet.ClearQuarantine(agentID) // also resets stall counters for the fresh life
		s.fleet.ClearLeaving(agentID)    // entry stays: drop the draining chip
		s.logger.Printf("membership: %q spec updated; relaunching", agentID)
	}
}

func (s *Supervisor) hasSpec(agentID string) bool {
	s.specsMu.Lock()
	defer s.specsMu.Unlock()
	for _, sp := range s.specs {
		if sp.AgentID == agentID {
			return true
		}
	}
	return false
}

func (s *Supervisor) setSpec(spec WorkerSpec) {
	s.specsMu.Lock()
	defer s.specsMu.Unlock()
	for i, sp := range s.specs {
		if sp.AgentID == spec.AgentID {
			s.specs[i] = spec
			return
		}
	}
}
