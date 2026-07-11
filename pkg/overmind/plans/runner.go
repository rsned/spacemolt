package plans

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"

	"github.com/rsned/spacemolt/pkg/craftbrain"
	"github.com/rsned/spacemolt/pkg/handoff"
	"github.com/rsned/spacemolt/pkg/overmind/tasks"
)

// MaxNodeRetries is how many times a node's task may fail before it parks
// (needs an operator retry via Control.RetryNodes).
const MaxNodeRetries = 2

// Runner drives all plan runs one Tick at a time. Construct once in
// cmd/overmind. Tick is single-threaded — it is called from the overmind
// main select loop and never spawns goroutines of its own.
type Runner struct {
	QueueDir string
	StateDir string
	Store    *tasks.Store
	Handoff  *handoff.Queue
	Roster   []RosterAgent     // craft fleet (pin targets)
	Managed  map[string]string // holder agent_id -> its home station (marketbot roster)
	Logger   *log.Logger

	runs map[string]*PlanRun // loaded lazily; keyed by plan id

	// courierIdx round-robins courier pins for handoff-gated haul nodes
	// (FromBase != ToBase). It reuses NewRun's round-robin-over-Roster
	// idea, but on its own counter since it advances at handoff-enqueue
	// time rather than at NewRun time (see gateHandoff).
	courierIdx int
}

// Tick advances every loaded plan run by one step: intake queue files, sync
// node states from the task store, retry/park failed nodes, gate and
// dispatch ready nodes (handoff-first, then budget), apply operator Control,
// and persist. Runs are processed in plan-id order for determinism.
func (r *Runner) Tick() {
	r.ensureLoaded()
	r.intake()

	ids := make([]string, 0, len(r.runs))
	for id := range r.runs {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, id := range ids {
		r.tickRun(r.runs[id])
	}
}

// ensureLoaded populates r.runs from StateDir on the first Tick only.
func (r *Runner) ensureLoaded() {
	if r.runs != nil {
		return
	}
	r.runs = map[string]*PlanRun{}
	runs, errs := LoadAllRuns(r.StateDir)
	for _, err := range errs {
		r.Logger.Printf("plans: load run: %v", err)
	}
	for _, pr := range runs {
		r.runs[pr.Manifest.PlanID] = pr
	}
}

// intake globs QueueDir for freshly dispatched plans, applies the accept
// transform (NewRun), saves state, and removes the consumed queue file.
// Decode failures are never silently dropped or retried: the offending file
// is renamed to <file>.rejected so it stops being picked up.
func (r *Runner) intake() {
	matches, err := filepath.Glob(filepath.Join(r.QueueDir, "*.json"))
	if err != nil {
		r.Logger.Printf("plans: intake glob %s: %v", r.QueueDir, err)
		return
	}

	for _, path := range matches {
		raw, err := os.ReadFile(path)
		if err != nil {
			r.Logger.Printf("plans: intake read %s: %v", path, err)
			continue
		}

		var qf QueueFile
		if err := json.Unmarshal(raw, &qf); err != nil {
			r.Logger.Printf("plans: intake decode %s: %v (rejecting)", path, err)
			if rerr := os.Rename(path, path+".rejected"); rerr != nil {
				r.Logger.Printf("plans: intake reject-rename %s: %v", path, rerr)
			}
			continue
		}

		pr := NewRun(qf, r.Roster)
		if err := SaveRun(r.StateDir, pr); err != nil {
			r.Logger.Printf("plans: intake save %s: %v", pr.Manifest.PlanID, err)
			continue
		}
		r.runs[pr.Manifest.PlanID] = pr

		if err := os.Remove(path); err != nil {
			r.Logger.Printf("plans: intake remove %s: %v", path, err)
		}
		r.Logger.Printf("plans: intake plan %q (%d nodes)", pr.Manifest.PlanID, len(pr.Nodes))
	}
}

// tickRun advances one plan run: apply Control, sync task-store outcomes
// into node state, gate/budget/dispatch ready nodes, recompute status, save.
func (r *Runner) tickRun(pr *PlanRun) {
	r.adoptDiskControl(pr)
	r.applyControl(pr)
	r.syncTasks(pr)
	r.dispatchReady(pr)

	pr.Recompute()
	if err := SaveRun(r.StateDir, pr); err != nil {
		r.Logger.Printf("plans: save run %q: %v", pr.Manifest.PlanID, err)
	}
}

// adoptDiskControl re-reads the on-disk state file's Control block — written
// by the play_as plan_* mutators under flock since the last tick — and adopts
// it into the in-memory PlanRun before applyControl runs. The Runner stays
// authoritative for every other field (node states, Spent, Status); only
// Control is operator-owned. Without this re-read, the Runner's own end-of-tick
// SaveRun would clobber any plan_pause/cancel/resume/retry the operator issued
// between ticks (the C1 operator-facing hole). A freshly intaken plan whose
// state file matches the in-memory copy re-reads its own (empty) Control — a
// harmless no-op.
func (r *Runner) adoptDiskControl(pr *PlanRun) {
	path := statePath(r.StateDir, pr.Manifest.PlanID)
	if _, err := os.Stat(path); err != nil {
		return // no on-disk file yet: in-memory Control stands
	}
	disk, err := LoadRun(path)
	if err != nil {
		r.Logger.Printf("plans: adopt control %q: %v (keeping in-memory Control)", pr.Manifest.PlanID, err)
		return
	}
	pr.Control = disk.Control
}

// applyControl consumes one-shot operator directives. Pause/Cancel are left
// in place for Recompute (and ReadyNodes, which calls Recompute) to turn
// into plan status; RaiseCap and RetryNodes are consumed here because they
// mutate node/manifest state directly rather than just a status label.
func (r *Runner) applyControl(pr *PlanRun) {
	if pr.Control.RaiseCap > 0 {
		pr.Manifest.BudgetCap = pr.Control.RaiseCap
		pr.Control.RaiseCap = 0
	}

	for _, id := range pr.Control.RetryNodes {
		n := pr.NodeByID(id)
		if n == nil || n.State != NodeParked {
			continue
		}
		switch n.Park {
		case ParkFailed, ParkOverBudget, ParkNeedsOperator:
			n.State = NodeWaiting
			n.Park = ""
			n.ParkDetail = ""
			n.Retries = 0 // an explicit operator retry grants a fresh MaxNodeRetries budget
		}
	}
	pr.Control.RetryNodes = nil
}

// syncTasks reconciles every dispatched node against the task store: done
// tasks roll DoneQty/Spent into the node and run, failed (or missing —
// treated as failed defensively) tasks retry or park the node.
func (r *Runner) syncTasks(pr *PlanRun) {
	for _, n := range pr.Nodes {
		if n.State != NodeDispatched {
			continue
		}

		taskID := taskIDFor(pr.Manifest.PlanID, n.Node.ID, n.Retries)
		task, ok := r.Store.Get(taskID)
		switch {
		case !ok:
			r.Logger.Printf("plans: node %s/%s: task %s missing from store, treating as failed",
				pr.Manifest.PlanID, n.Node.ID, taskID)
			r.failNode(n)

		case task.Status == tasks.StatusDone:
			n.State = NodeDone
			n.DoneQty = n.Node.Qty
			n.SpentActual = n.Node.FeeTotal
			pr.Spent += n.Node.FeeTotal
			r.Store.Remove(taskID)

		case task.Status == tasks.StatusFailed:
			r.Store.Remove(taskID)
			r.failNode(n)
		}
	}
}

// failNode records a failed attempt: back to waiting for another try, or
// parked once MaxNodeRetries is exceeded.
func (r *Runner) failNode(n *NodeRun) {
	n.Retries++
	if n.Retries > MaxNodeRetries {
		n.State = NodeParked
		n.Park = ParkFailed
		n.ParkDetail = fmt.Sprintf("failed after %d attempts", n.Retries)
		return
	}
	n.State = NodeWaiting
}

// handoffOutcome is gateHandoff's verdict for a haul node.
type handoffOutcome int

const (
	// handoffNotNeeded means the node isn't handoff-gated (no Holder, or
	// the node's own agent already holds the stock): proceed to dispatch.
	handoffNotNeeded handoffOutcome = iota
	// handoffPending means a handoff record exists (or was just enqueued)
	// and hasn't reached "done" yet: leave the node waiting.
	handoffPending
	// handoffCollapsedDone means a same-station handoff completed and the
	// node was marked NodeDone directly (no courier flight needed).
	handoffCollapsedDone
	// handoffReadyToDispatch means a courier-carried handoff completed:
	// proceed to dispatch the courier leg.
	handoffReadyToDispatch
	// handoffBlocked means the node was parked (unmanaged holder, no
	// roster to pin a courier to, or the handoff itself failed).
	handoffBlocked
)

// gateHandoff implements the handoff-gating rule for a haul node whose
// Holder is a managed marketbot: the node may not dispatch until the holder
// has gifted the stock onward.
//
// Same-station collapse (FromBase == ToBase): no courier flight is needed,
// so the gift's recipient is the node's own Recipient (the consumer's
// agent), and once the handoff reads done the node is marked NodeDone
// directly with DoneQty = the record's MovedQty.
//
// Courier-carried (FromBase != ToBase): unpinned haul nodes have no AgentID,
// so there is no single "executing agent" to receive the gift. Per the
// binding simplification: pin the haul node to a roster agent at
// handoff-enqueue time (round-robin over Roster via r.courierIdx, mirroring
// NewRun's craft-pin order), set the handoff Record.Recipient to that same
// pin, and dispatch the node's own task to that pin once the handoff is
// done — so the courier who received the gift is the courier who flies it.
func (r *Runner) gateHandoff(pr *PlanRun, n *NodeRun) handoffOutcome {
	holder := n.Node.Holder
	if holder == "" || holder == n.Agent {
		return handoffNotNeeded
	}

	if _, managed := r.Managed[holder]; !managed {
		n.State = NodeParked
		n.Park = ParkNeedsOperator
		n.ParkDetail = fmt.Sprintf("holder %s at %s: move %d %s to %s",
			holder, n.Node.FromBase, n.Node.Qty, n.Node.ItemID, recipientOrSelf(n.Recipient))
		return handoffBlocked
	}

	sameStation := n.Node.FromBase == n.Node.ToBase
	recordID := pr.Manifest.PlanID + "/" + n.Node.ID

	recs, err := r.Handoff.List()
	if err != nil {
		r.Logger.Printf("plans: handoff list: %v", err)
		return handoffPending
	}

	var rec *handoff.Record
	for i := range recs {
		if recs[i].ID == recordID {
			rec = &recs[i]
			break
		}
	}

	if rec == nil {
		recipient := n.Recipient
		if !sameStation {
			if n.Agent == "" {
				if len(r.Roster) == 0 {
					n.State = NodeParked
					n.Park = ParkNeedsOperator
					n.ParkDetail = "no craft roster to pin courier"
					return handoffBlocked
				}
				n.Agent = r.Roster[r.courierIdx%len(r.Roster)].AgentID
				r.courierIdx++
			}
			recipient = n.Agent
		}
		if _, err := r.Handoff.Enqueue(handoff.Record{
			ID:        recordID,
			Holder:    holder,
			Station:   n.Node.FromBase,
			ItemID:    n.Node.ItemID,
			Qty:       n.Node.Qty,
			Recipient: recipientOrSelf(recipient),
		}); err != nil {
			r.Logger.Printf("plans: handoff enqueue %s: %v", recordID, err)
		}
		return handoffPending
	}

	switch rec.Status {
	case handoff.StatusDone:
		if sameStation {
			n.State = NodeDone
			n.DoneQty = rec.MovedQty
			return handoffCollapsedDone
		}
		return handoffReadyToDispatch
	case handoff.StatusFailed:
		n.State = NodeParked
		n.Park = ParkNeedsOperator
		n.ParkDetail = fmt.Sprintf("handoff %s failed: %s", recordID, rec.Error)
		return handoffBlocked
	default: // pending
		return handoffPending
	}
}

// hasSpend reports whether dispatching n could cost money and therefore
// needs the budget gate: a nonzero FeeTotal (crafts), or any buy node
// (its ceiling comes from MAX_UNIT_PRICE, not FeeTotal).
func hasSpend(n *NodeRun) bool {
	return n.Node.FeeTotal > 0 || n.Node.Kind == craftbrain.KindBuy
}

// inFlightSpend sums FeeTotal over every node currently NodeDispatched: fees
// already committed by a dispatch but not yet rolled into pr.Spent (that
// only happens when the node completes — see syncTasks). The budget gate
// must count these alongside pr.Spent, or two spending nodes dispatched
// while both are still in flight can jointly overshoot BudgetCap even though
// neither alone would.
func inFlightSpend(pr *PlanRun) int {
	total := 0
	for _, n := range pr.Nodes {
		if n.State == NodeDispatched && n.Node.FeeTotal > 0 {
			total += n.Node.FeeTotal
		}
	}
	return total
}

// dispatchReady walks the plan's ready nodes (deps satisfied, plan running)
// and, for each: gates handoffs, budget-gates spend, and dispatches to the
// task store. Once a node parks (budget or handoff) it may pause the whole
// run, so remaining ready nodes are left for a later tick.
func (r *Runner) dispatchReady(pr *PlanRun) {
	// inFlight starts from nodes already dispatched in a prior tick and
	// accumulates further as this loop dispatches more nodes, so a node
	// dispatched earlier in this same tick counts as in-flight for the
	// budget check on the next candidate.
	inFlight := inFlightSpend(pr)

	for _, n := range pr.ReadyNodes() {
		if pr.Control.Pause || pr.Control.Cancel {
			return
		}

		if n.Node.Kind == craftbrain.KindHaul {
			switch r.gateHandoff(pr, n) {
			case handoffPending, handoffCollapsedDone, handoffBlocked:
				continue
			case handoffNotNeeded, handoffReadyToDispatch:
				// fall through to the budget gate + dispatch below.
			}
		}

		if hasSpend(n) {
			projected := pr.Spent + inFlight + n.Node.FeeTotal
			if pr.Manifest.BudgetCap > 0 && projected > pr.Manifest.BudgetCap {
				n.State = NodeParked
				n.Park = ParkOverBudget
				n.ParkDetail = fmt.Sprintf("projected spend %d exceeds cap %d", projected, pr.Manifest.BudgetCap)
				pr.Control.Pause = true
				return
			}
		}

		t := nodeTask(pr, n)
		if err := r.Store.Add(t); err != nil {
			r.Logger.Printf("plans: dispatch node %s/%s: %v", pr.Manifest.PlanID, n.Node.ID, err)
			continue
		}
		n.State = NodeDispatched
		if n.Node.FeeTotal > 0 {
			inFlight += n.Node.FeeTotal
		}
	}
}
