package plans

import (
	"testing"

	"github.com/rsned/spacemolt/pkg/craftbrain"
)

// A plan node's RoleRequired must follow the node kind, not be uniformly
// "craftsman". Only craft nodes are pinned to an agent (NewRun rule 4);
// haul/buy/mine nodes always dispatch with Agent == "", so RoleRequired is
// the ONLY thing tasks.Store.pickWorker can match on. Hardcoding
// "craftsman" made those nodes undispatchable on any fleet without
// craftsman workers -- a haul fleet of 23 haulers matched nothing and every
// node parked.
func TestNodeTaskRoleFollowsKind(t *testing.T) {
	for _, tc := range []struct {
		kind     craftbrain.Kind
		wantRole string
		wantScr  string
	}{
		{craftbrain.KindCraft, "craftsman", "craft_node"},
		{craftbrain.KindHaul, "hauler", "deliver_node"},
		{craftbrain.KindBuy, "hauler", "buy_node"},
		{craftbrain.KindMine, "miner", "mine_node"},
	} {
		t.Run(string(tc.kind), func(t *testing.T) {
			pr := &PlanRun{Manifest: Manifest{PlanID: "p1"}}
			n := &NodeRun{Node: craftbrain.Node{ID: "n1", Kind: tc.kind, ItemID: "iron_ore", Qty: 5}}
			pr.Nodes = []*NodeRun{n}

			got := nodeTask(pr, n)
			if got.RoleRequired != tc.wantRole {
				t.Errorf("kind %s: RoleRequired = %q, want %q", tc.kind, got.RoleRequired, tc.wantRole)
			}
			if got.Script != tc.wantScr {
				t.Errorf("kind %s: Script = %q, want %q", tc.kind, got.Script, tc.wantScr)
			}
		})
	}
}

// An explicit pin must still win: craft nodes carry Agent from the roster,
// and pickWorker dispatches by AgentID when set regardless of role.
func TestNodeTaskKeepsAgentPin(t *testing.T) {
	pr := &PlanRun{Manifest: Manifest{PlanID: "p1"}}
	n := &NodeRun{
		Node:  craftbrain.Node{ID: "c1", Kind: craftbrain.KindCraft, RecipeID: "forge_titanium_alloy", Qty: 2},
		Agent: "craftsman-3",
	}
	pr.Nodes = []*NodeRun{n}

	if got := nodeTask(pr, n); got.AgentID != "craftsman-3" {
		t.Errorf("AgentID = %q, want craftsman-3", got.AgentID)
	}
}
