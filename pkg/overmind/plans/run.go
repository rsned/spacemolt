// Package plans is the dispatch-time envelope around a craftbrain.Plan:
// the accept transform (park cycles/blocked nodes, pin crafts to a roster,
// route feeder recipients, insert synthetic cross-station transfers) and the
// flock-guarded on-disk PlanRun state that Executor B advances tick by tick.
package plans

import (
	"fmt"
	"slices"

	"github.com/rsned/spacemolt/pkg/craftbrain"
)

// NodeState is a NodeRun's position in the executor's per-node lifecycle.
type NodeState string

const (
	NodeWaiting    NodeState = "waiting"    // deps not yet done
	NodeDispatched NodeState = "dispatched" // task in the store (assigned or running)
	NodeDone       NodeState = "done"
	NodeParked     NodeState = "parked"
)

// ParkReason explains why a NodeRun is parked instead of progressing.
type ParkReason string

const (
	ParkCycle         ParkReason = "cycle"
	ParkBlocked       ParkReason = "blocked"
	ParkNeedsOperator ParkReason = "needs_operator"
	ParkOverBudget    ParkReason = "over_budget"
	ParkFailed        ParkReason = "failed"
	ParkReplan        ParkReason = "replan"
)

// RosterAgent is one craft-capable agent available to pin plan nodes to.
type RosterAgent struct {
	AgentID string `yaml:"agent_id" json:"agent_id"`
	Station string `yaml:"station" json:"station"`
}

// Manifest is the dispatch-time envelope around a craftbrain plan.
type Manifest struct {
	PlanID       string   `json:"plan_id"`
	BudgetCap    int      `json:"budget_cap"`
	MineItems    []string `json:"mine_items,omitempty"` // leaves tagged mine at dispatch
	Assembly     string   `json:"assembly"`             // any_docked_station resolved to this base
	DispatchedAt string   `json:"dispatched_at"`
}

// QueueFile is the JSON dropped into the craft-queue dir by play_as dispatch.
type QueueFile struct {
	Manifest Manifest        `json:"manifest"`
	Plan     craftbrain.Plan `json:"plan"`
}

// NodeRun is one plan node's executor-side lifecycle wrapper.
type NodeRun struct {
	Node        craftbrain.Node `json:"node"`
	State       NodeState       `json:"state"`
	Park        ParkReason      `json:"park,omitempty"`
	ParkDetail  string          `json:"park_detail,omitempty"`
	Agent       string          `json:"agent,omitempty"`     // pin (crafts + synthetic xfers) or assignee
	Recipient   string          `json:"recipient,omitempty"` // consumer's pinned agent for feeder nodes
	DoneQty     int             `json:"done_qty"`
	Retries     int             `json:"retries"`
	Synthetic   bool            `json:"synthetic,omitempty"`
	SpentActual int             `json:"spent_actual"` // fees/buys actually paid
}

// Control carries operator-issued mutations for the next tick to apply.
type Control struct {
	Pause      bool     `json:"pause,omitempty"`
	Cancel     bool     `json:"cancel,omitempty"`
	RaiseCap   int      `json:"raise_cap,omitempty"`   // new absolute cap; 0 = unchanged
	RetryNodes []string `json:"retry_nodes,omitempty"` // node ids to reset from parked(failed)
}

// PlanRun is the full on-disk executor state for one dispatched plan.
type PlanRun struct {
	Manifest    Manifest   `json:"manifest"`
	Nodes       []*NodeRun `json:"nodes"` // plan order + synthetic xfers appended
	Spent       int        `json:"spent"`
	Status      string     `json:"status"` // running | paused | done | partial | cancelled
	Control     Control    `json:"control"`
	Diagnostics []string   `json:"diagnostics,omitempty"`
}

// NodeByID returns the NodeRun with the given node id, or nil if absent.
func (pr *PlanRun) NodeByID(id string) *NodeRun {
	for _, n := range pr.Nodes {
		if n.Node.ID == id {
			return n
		}
	}
	return nil
}

// NewRun applies the accept transform: park cycle/blocked nodes, pin craft
// nodes round-robin over roster, set Recipient on feeder nodes, insert
// synthetic xfer nodes for cross-station craft->craft edges.
func NewRun(qf QueueFile, roster []RosterAgent) *PlanRun {
	pr := &PlanRun{
		Manifest:    qf.Manifest,
		Status:      "running",
		Diagnostics: append([]string(nil), qf.Plan.Diagnostics...),
	}
	// Manifest is a value copy, but MineItems is a slice: it still shares
	// the caller's backing array until explicitly cloned.
	pr.Manifest.MineItems = slices.Clone(qf.Manifest.MineItems)

	// Rule 1: copy nodes in plan order into NodeRuns. DependsOn is cloned
	// (not just the struct) so rule 6's in-place edge rewrite never leaks
	// back into the caller's qf.Plan — Go struct copy shares slice backing
	// arrays by default.
	pr.Nodes = make([]*NodeRun, 0, len(qf.Plan.Nodes))
	for _, n := range qf.Plan.Nodes {
		n.DependsOn = append([]string(nil), n.DependsOn...)
		pr.Nodes = append(pr.Nodes, &NodeRun{Node: n, State: NodeWaiting})
	}

	// Rule 2: cycle park via Kahn's algorithm over DependsOn edges.
	cycleMembers := detectCycleMembers(pr.Nodes)
	for _, n := range pr.Nodes {
		if cycleMembers[n.Node.ID] {
			members := make([]string, 0, len(cycleMembers))
			for _, m := range pr.Nodes {
				if cycleMembers[m.Node.ID] {
					members = append(members, m.Node.ID)
				}
			}
			n.State = NodeParked
			n.Park = ParkCycle
			n.ParkDetail = fmt.Sprintf("cycle members: %v", members)
		}
	}

	// Rule 3: blocked park.
	for _, n := range pr.Nodes {
		if n.State == NodeParked {
			continue
		}
		if n.Node.Kind == craftbrain.KindBlocked || n.Node.Status == craftbrain.StatusBlocked {
			n.State = NodeParked
			n.Park = ParkBlocked
			n.ParkDetail = n.Node.Reason
		}
	}

	// Rule 4: pin crafts round-robin over roster.
	craftIdx := 0
	for _, n := range pr.Nodes {
		if n.Node.Kind != craftbrain.KindCraft {
			continue
		}
		if n.State == NodeParked {
			continue
		}
		if len(roster) == 0 {
			n.State = NodeParked
			n.Park = ParkNeedsOperator
			n.ParkDetail = "no craft roster"
			continue
		}
		n.Agent = roster[craftIdx%len(roster)].AgentID
		craftIdx++
	}

	// Rule 5: recipients for consumer->producer edges.
	// A producer feeding multiple consumers keeps the first consumer's
	// recipient; iterate consumers in plan order so "first" is deterministic.
	assigned := map[string]bool{}
	for _, consumer := range pr.Nodes {
		if consumer.Agent == "" || consumer.Node.Kind != craftbrain.KindCraft {
			continue
		}
		for _, depID := range consumer.Node.DependsOn {
			producer := pr.NodeByID(depID)
			if producer == nil {
				continue
			}
			if assigned[producer.Node.ID] {
				pr.Diagnostics = append(pr.Diagnostics,
					fmt.Sprintf("node %s feeds multiple crafts; recipient pinned to first", producer.Node.ID))
				continue
			}
			producer.Recipient = consumer.Agent
			assigned[producer.Node.ID] = true
		}
	}

	// Rule 6: synthetic xfers for cross-station craft->craft edges.
	insertSyntheticXfers(pr)

	return pr
}

// detectCycleMembers runs Kahn's algorithm over DependsOn edges (consumer
// depends on producer) and returns the set of node ids that are on, or
// downstream of, a cycle: anything not emitted by the topological sort.
// Iteration is over the node slice in plan order (never map order) so the
// result is deterministic across runs.
func detectCycleMembers(nodes []*NodeRun) map[string]bool {
	ids := make([]string, 0, len(nodes))
	index := map[string]int{}
	for i, n := range nodes {
		ids = append(ids, n.Node.ID)
		index[n.Node.ID] = i
	}

	// inDegree[i] = number of unresolved dependencies for node i.
	// forward[p] = consumers that depend on producer p (edge p -> consumer).
	inDegree := make([]int, len(nodes))
	forward := make([][]int, len(nodes))
	for i, n := range nodes {
		for _, dep := range n.Node.DependsOn {
			pi, ok := index[dep]
			if !ok {
				continue // dangling dependency id; not this rule's concern
			}
			inDegree[i]++
			forward[pi] = append(forward[pi], i)
		}
	}

	visited := make([]bool, len(nodes))
	queue := make([]int, 0, len(nodes))
	for i := range nodes {
		if inDegree[i] == 0 {
			queue = append(queue, i)
		}
	}
	for len(queue) > 0 {
		i := queue[0]
		queue = queue[1:]
		if visited[i] {
			continue
		}
		visited[i] = true
		for _, ci := range forward[i] {
			inDegree[ci]--
			if inDegree[ci] == 0 {
				queue = append(queue, ci)
			}
		}
	}

	members := map[string]bool{}
	for i, id := range ids {
		if !visited[i] {
			members[id] = true
		}
	}
	return members
}

// insertSyntheticXfers walks the DependsOn edges into every craft consumer and
// inserts a synthetic haul NodeRun wherever the producer's output would
// otherwise be stranded out of the consumer's reach, then rewrites the
// consumer's DependsOn to point at the xfer instead of the producer directly.
// Consumers are walked in plan order so xfer ids are stable across runs.
//
// Three stranding cases are bridged:
//
//   - craft->craft, different stations: producer deposits at its station; the
//     xfer carries the output to the consumer's station. Courier = producer's
//     agent (holds the output), recipient = consumer's agent.
//   - craft->craft, SAME station but different pinned agents (round-robin
//     gives adjacent crafts different agents): the producer deposits into its
//     OWN storage; the consumer (a different agent) can't withdraw it. A
//     same-station xfer gifts it across — Deliver handles FROM==TO as a
//     no-flight gift. Courier = producer's agent, recipient = consumer's agent.
//   - buy->craft, different stations: BuyDirected gifts the bought goods to the
//     consumer craft's pinned agent AT the seller station (rule 5 set the buy's
//     Recipient to that agent). The xfer carries them to the craft station,
//     couriered by that same agent and self-deposited. Courier = the buy's
//     recipient, recipient = self.
func insertSyntheticXfers(pr *PlanRun) {
	n := 0
	// Snapshot the pre-xfer node list; new synthetic nodes are appended to
	// pr.Nodes as they are created, but we only ever scan the original craft
	// nodes as consumers.
	consumers := append([]*NodeRun(nil), pr.Nodes...)
	for _, consumer := range consumers {
		if consumer.Node.Kind != craftbrain.KindCraft {
			continue
		}
		deps := consumer.Node.DependsOn
		for i, depID := range deps {
			producer := pr.NodeByID(depID)
			if producer == nil {
				continue
			}
			xfer := buildXfer(producer, consumer, &n)
			if xfer == nil {
				continue
			}
			pr.Nodes = append(pr.Nodes, xfer)
			deps[i] = xfer.Node.ID
		}
	}
}

// buildXfer returns the synthetic haul NodeRun bridging a producer->consumer
// edge, or nil when the producer's output already sits where the consumer can
// reach it (same station AND same agent for crafts; same station for buys).
// idx is the shared xfer-id counter, advanced only when an xfer is built.
func buildXfer(producer, consumer *NodeRun, idx *int) *NodeRun {
	switch producer.Node.Kind {
	case craftbrain.KindCraft:
		if producer.Node.StationID == "" {
			return nil
		}
		sameStation := producer.Node.StationID == consumer.Node.StationID
		sameAgent := producer.Agent == consumer.Agent
		if sameStation && sameAgent {
			return nil
		}
		return newXfer(idx, producer.Node.ItemID, producer.Node.Qty,
			producer.Node.StationID, consumer.Node.StationID,
			producer.Node.ID, producer.Agent, consumer.Agent)

	case craftbrain.KindBuy:
		// BuyDirected already gifted to the consumer's agent at the seller
		// station; only a cross-station buy needs carrying onward.
		if producer.Node.StationID == "" || producer.Node.StationID == consumer.Node.StationID {
			return nil
		}
		return newXfer(idx, producer.Node.ItemID, producer.Node.Qty,
			producer.Node.StationID, consumer.Node.StationID,
			producer.Node.ID, producer.Recipient, "")

	default:
		return nil
	}
}

// newXfer constructs one synthetic haul NodeRun and advances the id counter.
func newXfer(idx *int, itemID string, qty int, from, to, dep, courier, recipient string) *NodeRun {
	xferID := fmt.Sprintf("xfer-%d", *idx)
	*idx++
	return &NodeRun{
		Node: craftbrain.Node{
			ID:        xferID,
			Kind:      craftbrain.KindHaul,
			ItemID:    itemID,
			Qty:       qty,
			FromBase:  from,
			ToBase:    to,
			DependsOn: []string{dep},
		},
		State:     NodeWaiting,
		Agent:     courier,
		Recipient: recipient,
		Synthetic: true,
	}
}
