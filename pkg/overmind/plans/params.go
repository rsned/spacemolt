package plans

import (
	"slices"
	"strconv"

	"github.com/rsned/spacemolt/pkg/craftbrain"
	"github.com/rsned/spacemolt/pkg/overmind/tasks"
)

// taskIDFor returns the store id for one dispatch attempt of a node: unique
// per retry so a re-added task after a failure never collides with the id
// the store just removed. Exported (lowercase, same package) for the runner
// test, which reconstructs ids to simulate worker events.
func taskIDFor(planID, nodeID string, retry int) string {
	return planID + "/" + nodeID + "/r" + strconv.Itoa(retry)
}

// recipientOrSelf returns recipient, or the literal "self" when empty.
// $RECIPIENT$ params (and handoff.Record.Recipient) always carry agent ids
// or "self" — never usernames; worker verbs resolve usernames later.
func recipientOrSelf(recipient string) string {
	if recipient == "" {
		return "self"
	}
	return recipient
}

// nodeTask maps a NodeRun to the tasks.Task the store should dispatch: the
// script (data/scripts/<name>.smolt) and its $PARAM$ substitutions. This is
// the single source of truth for the node-kind -> script/params mapping.
// It takes the owning PlanRun (not just the node) because the mine-node TO
// param is derived by scanning the plan for the node's craft consumer.
func nodeTask(pr *PlanRun, n *NodeRun) tasks.Task {
	t := tasks.Task{
		ID:           taskIDFor(pr.Manifest.PlanID, n.Node.ID, n.Retries),
		RoleRequired: "craftsman",
		AgentID:      n.Agent,
	}

	switch n.Node.Kind {
	case craftbrain.KindCraft:
		facility := n.Node.FacilityID
		if facility == "" {
			facility = "hand"
		}
		t.Script = "craft_node"
		t.Params = map[string]string{
			"RECIPE":      n.Node.RecipeID,
			"NUM_OUTPUTS": strconv.Itoa(n.Node.Qty),
			"STATION":     n.Node.StationID,
			"FACILITY":    facility,
			// EST_FEE feeds the craft_node verb's 2x-replan gate: a live
			// dry-run credits_total more than double this planner estimate
			// means the catalog is stale and the node should be replanned
			// rather than blindly queued.
			"EST_FEE": strconv.FormatFloat(float64(n.Node.FeeTotal), 'f', -1, 64),
		}

	case craftbrain.KindHaul:
		t.Script = "deliver_node"
		t.Params = map[string]string{
			"ITEM":      n.Node.ItemID,
			"QTY":       strconv.Itoa(n.Node.Qty),
			"FROM":      n.Node.FromBase,
			"TO":        n.Node.ToBase,
			"RECIPIENT": recipientOrSelf(n.Recipient),
		}

	case craftbrain.KindBuy:
		t.Script = "buy_node"
		maxPrice := "0"
		if n.Node.FeeTotal > 0 && n.Node.Qty > 0 {
			maxPrice = strconv.FormatFloat(float64(n.Node.FeeTotal)/float64(n.Node.Qty), 'f', -1, 64)
		}
		t.Params = map[string]string{
			"ITEM":           n.Node.ItemID,
			"QTY":            strconv.Itoa(n.Node.Qty),
			"STATION":        n.Node.StationID,
			"MAX_UNIT_PRICE": maxPrice,
			"RECIPIENT":      recipientOrSelf(n.Recipient),
		}

	case craftbrain.KindMine:
		t.Script = "mine_node"
		t.Params = map[string]string{
			"ITEM":      n.Node.ItemID,
			"QTY":       strconv.Itoa(n.Node.Qty),
			"TO":        mineDeliveryStation(pr, n),
			"RECIPIENT": recipientOrSelf(n.Recipient),
		}
	}

	return t
}

// mineDeliveryStation finds the craft node that consumes n (n.Node.ID
// appears in a craft node's DependsOn) and returns that consumer's
// StationID; falls back to the plan's resolved assembly base when no craft
// node depends on n.
func mineDeliveryStation(pr *PlanRun, n *NodeRun) string {
	for _, consumer := range pr.Nodes {
		if consumer.Node.Kind != craftbrain.KindCraft {
			continue
		}
		if slices.Contains(consumer.Node.DependsOn, n.Node.ID) {
			return consumer.Node.StationID
		}
	}
	return pr.Manifest.Assembly
}
