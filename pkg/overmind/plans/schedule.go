package plans

import "github.com/rsned/spacemolt/pkg/craftbrain"

// Progress is a done-vs-total quantity rollup for one item across all
// mine/buy/haul/craft nodes that produce it.
type Progress struct{ Done, Total int }

// ReadyNodes returns nodes whose deps are all done, state waiting, plan not
// paused/cancelled. Parked and synthetic-parked nodes never appear.
func (pr *PlanRun) ReadyNodes() []*NodeRun {
	pr.Recompute()
	if pr.Status != "running" {
		return nil
	}

	done := map[string]bool{}
	for _, n := range pr.Nodes {
		if n.State == NodeDone {
			done[n.Node.ID] = true
		}
	}

	var ready []*NodeRun
	for _, n := range pr.Nodes {
		if n.State != NodeWaiting {
			continue
		}
		depsSatisfied := true
		for _, dep := range n.Node.DependsOn {
			if !done[dep] {
				depsSatisfied = false
				break
			}
		}
		if depsSatisfied {
			ready = append(ready, n)
		}
	}
	return ready
}

// ItemProgress sums done vs total qty per item over mine/buy/haul/craft
// nodes. Synthetic nodes are included (they are real work); KindBlocked
// nodes are skipped.
func (pr *PlanRun) ItemProgress() map[string]Progress {
	prog := map[string]Progress{}
	for _, n := range pr.Nodes {
		if n.Node.Kind == craftbrain.KindBlocked {
			continue
		}
		p := prog[n.Node.ItemID]
		p.Done += n.DoneQty
		p.Total += n.Node.Qty
		prog[n.Node.ItemID] = p
	}
	return prog
}

// Recompute derives PlanRun.Status: "cancelled" if Control.Cancel;
// "paused" if Control.Pause; "done" when every node is done;
// "partial" when nothing is waiting/dispatched but parked nodes remain;
// otherwise "running".
func (pr *PlanRun) Recompute() {
	if pr.Control.Cancel {
		pr.Status = "cancelled"
		return
	}
	if pr.Control.Pause {
		pr.Status = "paused"
		return
	}

	var waitingOrDispatched, parked int
	for _, n := range pr.Nodes {
		switch n.State {
		case NodeWaiting, NodeDispatched:
			waitingOrDispatched++
		case NodeParked:
			parked++
		}
	}

	switch {
	case waitingOrDispatched == 0 && parked == 0:
		pr.Status = "done"
	case waitingOrDispatched == 0 && parked > 0:
		pr.Status = "partial"
	default:
		pr.Status = "running"
	}
}
