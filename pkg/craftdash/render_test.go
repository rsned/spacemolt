package craftdash

import (
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/craftbrain"
	"github.com/rsned/spacemolt/pkg/overmind/plans"
)

// fixturePlan builds a small 3-node PlanRun: one done leaf (mine-2), one
// active craft consuming it plus a parked node (craft-1), and one parked
// blocked node (blocked-3) also consumed by craft-1. This exercises all
// three renderable states plus multi-dep edges in one fixture.
func fixturePlan() *plans.PlanRun {
	return &plans.PlanRun{
		Manifest: plans.Manifest{PlanID: "plan-a", BudgetCap: 100},
		Spent:    42,
		Status:   "running",
		Nodes: []*plans.NodeRun{
			{
				Node: craftbrain.Node{
					ID: "craft-1", Kind: craftbrain.KindCraft, ItemID: "widget", Qty: 2,
					DependsOn: []string{"mine-2", "blocked-3"},
				},
				State: plans.NodeDispatched,
				Agent: "craftsman-2",
			},
			{
				Node:    craftbrain.Node{ID: "mine-2", Kind: craftbrain.KindMine, ItemID: "ore", Qty: 10},
				State:   plans.NodeDone,
				DoneQty: 4,
			},
			{
				Node:       craftbrain.Node{ID: "blocked-3", Kind: craftbrain.KindBlocked, ItemID: "rare_ore", Qty: 5},
				State:      plans.NodeParked,
				Park:       plans.ParkBlocked,
				ParkDetail: "no facility found",
			},
		},
	}
}

const wantMermaid = `flowchart TD
  classDef done fill:#eee,color:#999,stroke:#ccc
  classDef active fill:#fffbe6,stroke:#e6a700,stroke-width:3px
  classDef parked fill:#ffe6e6,stroke:#cc0000,stroke-width:2px
  craft-1["craft widget x2<br/>@craftsman-2"]:::active
  mine-2["mine ore x10"]:::done
  blocked-3["blocked rare_ore x5<br/>PARKED (blocked): no facility found"]:::parked
  craft-1 ==> mine-2
  craft-1 ==> blocked-3
`

func TestMermaidForPlan_Golden(t *testing.T) {
	got := MermaidForPlan(fixturePlan())
	if got != wantMermaid {
		t.Errorf("MermaidForPlan() mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, wantMermaid)
	}
}

// TestMermaidForPlan_EntirelyParked covers the A2 contract state where every
// node in a plan is parked (e.g. an all-cycle plan) — it must still render,
// not panic or produce an empty diagram.
func TestMermaidForPlan_EntirelyParked(t *testing.T) {
	pr := &plans.PlanRun{
		Manifest: plans.Manifest{PlanID: "plan-cycle"},
		Status:   "partial",
		Nodes: []*plans.NodeRun{
			{
				Node:       craftbrain.Node{ID: "craft-a", Kind: craftbrain.KindCraft, ItemID: "gizmo", Qty: 1, DependsOn: []string{"craft-b"}},
				State:      plans.NodeParked,
				Park:       plans.ParkCycle,
				ParkDetail: "cycle members: [craft-a craft-b]",
			},
			{
				Node:       craftbrain.Node{ID: "craft-b", Kind: craftbrain.KindCraft, ItemID: "widget", Qty: 1, DependsOn: []string{"craft-a"}},
				State:      plans.NodeParked,
				Park:       plans.ParkCycle,
				ParkDetail: "cycle members: [craft-a craft-b]",
			},
		},
	}
	got := MermaidForPlan(pr)
	// escapeLabel converts brackets to parens so a mermaid label with raw
	// "[...]" content (as ParkDetail's "cycle members: [...]" is) can't be
	// mistaken for mermaid's own node-shape delimiters.
	if !strings.Contains(got, `craft-a["craft gizmo x1<br/>PARKED (cycle): cycle members: (craft-a craft-b)"]:::parked`) {
		t.Errorf("expected parked craft-a node, got:\n%s", got)
	}
	if !strings.Contains(got, "craft-a --> craft-b") {
		t.Errorf("expected non-active (thin) edge for parked source, got:\n%s", got)
	}
	if strings.Contains(got, ":::active") || strings.Contains(got, ":::done") {
		t.Errorf("entirely-parked plan should have no active/done nodes, got:\n%s", got)
	}
}

func TestSanitizeID_ReplacesSlashAndOtherChars(t *testing.T) {
	pr := &plans.PlanRun{
		Manifest: plans.Manifest{PlanID: "plan-slash"},
		Nodes: []*plans.NodeRun{
			{Node: craftbrain.Node{ID: "haul/3:x", Kind: craftbrain.KindHaul, ItemID: "ore", Qty: 1}},
		},
	}
	got := MermaidForPlan(pr)
	if !strings.Contains(got, `haul_3_x["haul ore x1"]`) {
		t.Errorf("expected sanitized id haul_3_x, got:\n%s", got)
	}
	if strings.Contains(got, "haul/3:x") {
		t.Errorf("raw id with '/' leaked into mermaid source:\n%s", got)
	}
}

func TestNodeLabel_EscapesMermaidInjectionChars(t *testing.T) {
	pr := &plans.PlanRun{
		Manifest: plans.Manifest{PlanID: "plan-inj"},
		Nodes: []*plans.NodeRun{
			{
				Node:  craftbrain.Node{ID: "craft-1", Kind: craftbrain.KindCraft, ItemID: `evil"item[x]<y>`, Qty: 1},
				State: plans.NodeDispatched,
				Agent: `bad"agent<z>`,
			},
		},
	}
	got := MermaidForPlan(pr)
	if strings.Contains(got, `"item[`) || strings.Contains(got, "<z>") || strings.Contains(got, "<y>") {
		t.Errorf("mermaid injection characters leaked into output:\n%s", got)
	}
}

func TestSyntheticXferRendersAsXferKind(t *testing.T) {
	pr := &plans.PlanRun{
		Manifest: plans.Manifest{PlanID: "plan-xfer"},
		Nodes: []*plans.NodeRun{
			{
				Node:      craftbrain.Node{ID: "xfer-0", Kind: craftbrain.KindHaul, ItemID: "steel_plate", Qty: 50},
				State:     plans.NodeWaiting,
				Synthetic: true,
			},
		},
	}
	got := MermaidForPlan(pr)
	if !strings.Contains(got, `xfer-0["xfer steel_plate x50"]`) {
		t.Errorf("expected synthetic haul to render as xfer kind, got:\n%s", got)
	}
}

func TestPage_ContainsProgressRowAndHeader(t *testing.T) {
	pr := fixturePlan()
	page := string(Page([]*plans.PlanRun{pr}, 30))

	for _, want := range []string{
		"plan-a",
		"running",
		"42",
		"100",
		"4 / 10",
		"ore",
		"0 / 2",
		"widget",
		`<pre class="mermaid">`,
		"mermaid.initialize",
		`/assets/mermaid.min.js`,
		`content="30"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("Page() missing expected substring %q\n--- page ---\n%s", want, page)
		}
	}
}

func TestPage_UnboundedCapWhenZero(t *testing.T) {
	pr := &plans.PlanRun{
		Manifest: plans.Manifest{PlanID: "plan-b", BudgetCap: 0},
		Status:   "running",
		Nodes: []*plans.NodeRun{
			{Node: craftbrain.Node{ID: "mine-1", Kind: craftbrain.KindMine, ItemID: "ore", Qty: 5}, State: plans.NodeWaiting},
		},
	}
	page := string(Page([]*plans.PlanRun{pr}, 30))
	if !strings.Contains(page, "unbounded") {
		t.Errorf("expected 'unbounded' for zero BudgetCap, got:\n%s", page)
	}
}

func TestPage_EmptyRunsShowsFriendlyMessage(t *testing.T) {
	page := string(Page(nil, 30))
	if !strings.Contains(page, "no active plans") {
		t.Errorf("expected friendly empty-state message, got:\n%s", page)
	}
}

func TestPageWithErrors_RendersWarningBanner(t *testing.T) {
	page := string(PageWithErrors(nil, []error{errBad("plans: parse foo.state.json: unexpected EOF")}, 30))
	if !strings.Contains(page, "foo.state.json") {
		t.Errorf("expected corrupt-file warning banner to name the bad file, got:\n%s", page)
	}
}

type errBad string

func (e errBad) Error() string { return string(e) }
