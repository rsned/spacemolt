package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/craftbrain"
	"github.com/rsned/spacemolt/pkg/finditem"
	"github.com/rsned/spacemolt/pkg/market"
	"github.com/rsned/spacemolt/pkg/overmind/plans"
)

func TestParseDispatchArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    dispatchArgs
		wantErr bool
	}{
		{
			name: "plan path only",
			args: []string{"plan.json"},
			want: dispatchArgs{planPath: "plan.json"},
		},
		{
			name: "all flags",
			args: []string{"plan.json", "--budget=500", "--mine=iron,copper", "--assembly=grand_exchange_station", "--skip-verify"},
			want: dispatchArgs{
				planPath:   "plan.json",
				budget:     500,
				mineItems:  []string{"iron", "copper"},
				assembly:   "grand_exchange_station",
				skipVerify: true,
			},
		},
		{
			name: "mine list trims whitespace and drops empties",
			args: []string{"plan.json", "--mine= iron , , copper "},
			want: dispatchArgs{planPath: "plan.json", mineItems: []string{"iron", "copper"}},
		},
		{name: "no args", args: nil, wantErr: true},
		{name: "bad budget", args: []string{"plan.json", "--budget=abc"}, wantErr: true},
		{name: "budget=0 rejected", args: []string{"plan.json", "--budget=0"}, wantErr: true},
		{name: "budget=-1 unbounded", args: []string{"plan.json", "--budget=-1"}, want: dispatchArgs{planPath: "plan.json", budget: -1}},
		{name: "budget=-5 rejected", args: []string{"plan.json", "--budget=-5"}, wantErr: true},
		{name: "unknown flag rejected", args: []string{"plan.json", "--jsonn"}, wantErr: true},
		{name: "unexpected positional", args: []string{"plan.json", "extra"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseDispatchArgs(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatal("want error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.planPath != tt.want.planPath || got.budget != tt.want.budget ||
				got.assembly != tt.want.assembly || got.skipVerify != tt.want.skipVerify {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
			if len(got.mineItems) != len(tt.want.mineItems) {
				t.Fatalf("mineItems = %v, want %v", got.mineItems, tt.want.mineItems)
			}
			for i := range got.mineItems {
				if got.mineItems[i] != tt.want.mineItems[i] {
					t.Fatalf("mineItems = %v, want %v", got.mineItems, tt.want.mineItems)
				}
			}
		})
	}
}

func TestSanitizeID(t *testing.T) {
	tests := []struct{ in, want string }{
		{"iron_plate", "iron-plate"},
		{"Fusion Core", "fusion-core"},
		{"already-lower-case", "already-lower-case"},
		{"weird:chars/here", "weird-chars-here"},
		{"UPPER123", "upper123"},
		{"Café Crate", "caf--crate"}, // accented characters and spaces become dashes
		{"日本語テキスト", "-------"},       // CJK characters become dashes
		{"café-123", "caf--123"},     // mix of accented and valid chars
	}
	for _, tt := range tests {
		got := sanitizeID(tt.in)
		if got != tt.want {
			t.Errorf("sanitizeID(%q) = %q, want %q", tt.in, got, tt.want)
		}
		// Verify output stays within [a-z0-9-]
		for _, r := range got {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
				t.Errorf("sanitizeID(%q) = %q contains invalid rune %q (must be [a-z0-9-])", tt.in, got, r)
			}
		}
	}
}

func TestDispatchPlanID(t *testing.T) {
	now := time.Date(2026, 7, 10, 15, 4, 5, 0, time.UTC)
	tests := []struct {
		name string
		plan craftbrain.Plan
		want string
	}{
		{
			name: "uses plan target",
			plan: craftbrain.Plan{Target: "Fusion Core"},
			want: "fusion-core-20260710-150405",
		},
		{
			name: "falls back to first node item id when target empty",
			plan: craftbrain.Plan{Nodes: []craftbrain.Node{{ItemID: "iron_ore"}}},
			want: "iron-ore-20260710-150405",
		},
		{
			name: "falls back to plan when nothing else available",
			plan: craftbrain.Plan{},
			want: "plan-20260710-150405",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := dispatchPlanID(tt.plan, now); got != tt.want {
				t.Errorf("dispatchPlanID() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveAssembly(t *testing.T) {
	plan := craftbrain.Plan{
		Nodes: []craftbrain.Node{
			{ID: "n1", StationID: craftbrain.DefaultCraftBase},
			{ID: "n2", FromBase: craftbrain.DefaultCraftBase, ToBase: "somewhere_else"},
			{ID: "n3", FromBase: "elsewhere", ToBase: craftbrain.DefaultCraftBase},
			{ID: "n4", StationID: "already_pinned"},
		},
	}
	resolveAssembly(&plan, "grand_exchange_station")

	want := map[string]craftbrain.Node{
		"n1": {ID: "n1", StationID: "grand_exchange_station"},
		"n2": {ID: "n2", FromBase: "grand_exchange_station", ToBase: "somewhere_else"},
		"n3": {ID: "n3", FromBase: "elsewhere", ToBase: "grand_exchange_station"},
		"n4": {ID: "n4", StationID: "already_pinned"},
	}
	for _, n := range plan.Nodes {
		w := want[n.ID]
		if n.StationID != w.StationID || n.FromBase != w.FromBase || n.ToBase != w.ToBase {
			t.Errorf("node %s: got StationID=%q FromBase=%q ToBase=%q, want StationID=%q FromBase=%q ToBase=%q",
				n.ID, n.StationID, n.FromBase, n.ToBase, w.StationID, w.FromBase, w.ToBase)
		}
	}
}

// fakeSeller returns a canned sellerLookup: itemID -> results (nil means no
// sellers found, error is never returned by this fake).
func fakeSeller(byItem map[string][]finditem.Result) sellerLookup {
	return func(_ context.Context, itemID string, _ int) ([]finditem.Result, error) {
		return byItem[itemID], nil
	}
}

func TestDispatchTransform_LeafTagging(t *testing.T) {
	plan := craftbrain.Plan{
		Target: "widget",
		Nodes: []craftbrain.Node{
			{ID: "mine-iron", Kind: craftbrain.KindMine, ItemID: "iron_ore", Qty: 10},
			{ID: "mine-copper", Kind: craftbrain.KindMine, ItemID: "copper_ore", Qty: 5},
			{ID: "mine-titanium", Kind: craftbrain.KindMine, ItemID: "titanium_ore", Qty: 3},
			{ID: "craft-widget", Kind: craftbrain.KindCraft, ItemID: "widget", Qty: 1},
		},
	}
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}

	lookup := fakeSeller(map[string][]finditem.Result{
		"copper_ore": {
			{ItemSeller: market.ItemSeller{StationID: "near_stn", BestPrice: 12.5}},
			{ItemSeller: market.ItemSeller{StationID: "far_stn", BestPrice: 9.0}},
		},
		// titanium_ore: no sellers -> stays mine
	})

	args := dispatchArgs{mineItems: []string{"iron_ore"}} // iron explicitly kept as mine
	qf, _, err := dispatchTransform(context.Background(), raw, args, "grand_exchange_station", lookup, time.Now())
	if err != nil {
		t.Fatalf("dispatchTransform: %v", err)
	}

	byID := map[string]craftbrain.Node{}
	for _, n := range qf.Plan.Nodes {
		byID[n.ID] = n
	}

	if got := byID["mine-iron"].Kind; got != craftbrain.KindMine {
		t.Errorf("mine-iron kind = %q, want mine (explicitly excluded from conversion)", got)
	}
	if got := byID["mine-titanium"].Kind; got != craftbrain.KindMine {
		t.Errorf("mine-titanium kind = %q, want mine (no seller found)", got)
	}

	copperNode := byID["mine-copper"]
	if copperNode.Kind != craftbrain.KindBuy {
		t.Fatalf("mine-copper kind = %q, want buy", copperNode.Kind)
	}
	if copperNode.StationID != "near_stn" {
		t.Errorf("mine-copper StationID = %q, want near_stn (first/best Find result)", copperNode.StationID)
	}
	// FeeTotal = ceil(12.5 * 5) = 63
	if copperNode.FeeTotal != 63 {
		t.Errorf("mine-copper FeeTotal = %d, want 63", copperNode.FeeTotal)
	}
}

func TestDispatchTransform_BudgetDefault(t *testing.T) {
	plan := craftbrain.Plan{
		Target: "widget",
		Nodes: []craftbrain.Node{
			{ID: "n1", Kind: craftbrain.KindCraft, ItemID: "widget", Qty: 1, FeeTotal: 100},
			{ID: "n2", Kind: craftbrain.KindMine, ItemID: "iron_ore", Qty: 10, FeeTotal: 0},
		},
	}
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	lookup := fakeSeller(nil)

	t.Run("computed default is ceil(1.25 * fee sum)", func(t *testing.T) {
		qf, _, err := dispatchTransform(context.Background(), raw, dispatchArgs{}, "base", lookup, time.Now())
		if err != nil {
			t.Fatalf("dispatchTransform: %v", err)
		}
		// sum FeeTotal = 100 -> ceil(1.25*100) = 125
		if qf.Manifest.BudgetCap != 125 {
			t.Errorf("BudgetCap = %d, want 125", qf.Manifest.BudgetCap)
		}
	})

	t.Run("explicit budget overrides entirely", func(t *testing.T) {
		qf, _, err := dispatchTransform(context.Background(), raw, dispatchArgs{budget: 9000}, "base", lookup, time.Now())
		if err != nil {
			t.Fatalf("dispatchTransform: %v", err)
		}
		if qf.Manifest.BudgetCap != 9000 {
			t.Errorf("BudgetCap = %d, want 9000", qf.Manifest.BudgetCap)
		}
	})

	t.Run("budget includes buy-converted leaves", func(t *testing.T) {
		buyLookup := fakeSeller(map[string][]finditem.Result{
			"iron_ore": {{ItemSeller: market.ItemSeller{StationID: "s", BestPrice: 2.0}}},
		})
		qf, _, err := dispatchTransform(context.Background(), raw, dispatchArgs{}, "base", buyLookup, time.Now())
		if err != nil {
			t.Fatalf("dispatchTransform: %v", err)
		}
		// craft fee 100 + buy fee ceil(2.0*10)=20 -> sum 120 -> ceil(1.25*120) = 150
		if qf.Manifest.BudgetCap != 150 {
			t.Errorf("BudgetCap = %d, want 150 (buy-converted leaf must count toward the sum)", qf.Manifest.BudgetCap)
		}
	})

	t.Run("budget=-1 expresses unbounded (cap=0)", func(t *testing.T) {
		qf, _, err := dispatchTransform(context.Background(), raw, dispatchArgs{budget: -1}, "base", fakeSeller(nil), time.Now())
		if err != nil {
			t.Fatalf("dispatchTransform: %v", err)
		}
		if qf.Manifest.BudgetCap != 0 {
			t.Errorf("BudgetCap = %d, want 0 (unbounded)", qf.Manifest.BudgetCap)
		}
	})
}

// TestDispatchTransform_PricesNativeBuyNodes (I3): planner-native KindBuy nodes
// arriving with FeeTotal==0 escaped the budget controls entirely (MAX_UNIT_PRICE
// 0 = no ceiling, +0 to the gate). dispatchTransform must price them via the
// seller lookup — preferring a seller at their existing station — and stamp
// FeeTotal, so the default budget includes them. A buy with no seller anywhere
// is left unpriced with a diagnostic.
func TestDispatchTransform_PricesNativeBuyNodes(t *testing.T) {
	plan := craftbrain.Plan{
		Target: "widget",
		Nodes: []craftbrain.Node{
			{ID: "buy-1", Kind: craftbrain.KindBuy, ItemID: "gizmo", Qty: 4, FeeTotal: 0, StationID: "existing_stn"},
			{ID: "buy-nostock", Kind: craftbrain.KindBuy, ItemID: "rare", Qty: 2, FeeTotal: 0},
		},
	}
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	lookup := fakeSeller(map[string][]finditem.Result{
		"gizmo": {
			{ItemSeller: market.ItemSeller{StationID: "cheap_stn", BestPrice: 3.0}},
			{ItemSeller: market.ItemSeller{StationID: "existing_stn", BestPrice: 5.0}},
		},
		// rare: no seller
	})

	qf, _, err := dispatchTransform(context.Background(), raw, dispatchArgs{}, "base", lookup, time.Now())
	if err != nil {
		t.Fatalf("dispatchTransform: %v", err)
	}

	byID := map[string]craftbrain.Node{}
	for _, n := range qf.Plan.Nodes {
		byID[n.ID] = n
	}

	b1 := byID["buy-1"]
	if b1.StationID != "existing_stn" {
		t.Errorf("buy-1 StationID = %q, want existing_stn (a seller exists there — prefer re-pricing in place)", b1.StationID)
	}
	if b1.FeeTotal != 20 { // ceil(5.0 * 4)
		t.Errorf("buy-1 FeeTotal = %d, want 20 (priced at the existing-station seller)", b1.FeeTotal)
	}
	if bn := byID["buy-nostock"]; bn.FeeTotal != 0 {
		t.Errorf("buy-nostock FeeTotal = %d, want 0 (no seller — left unpriced)", bn.FeeTotal)
	}
	// Budget default now includes the priced buy: ceil(1.25 * 20) = 25.
	if qf.Manifest.BudgetCap != 25 {
		t.Errorf("BudgetCap = %d, want 25 (priced native buy must count toward the default)", qf.Manifest.BudgetCap)
	}
	// The no-seller buy must be surfaced in diagnostics.
	foundNote := false
	for _, d := range qf.Plan.Diagnostics {
		if strings.Contains(d, "rare") {
			foundNote = true
		}
	}
	if !foundNote {
		t.Errorf("diagnostics = %v, want a note about the unpriced native buy 'rare'", qf.Plan.Diagnostics)
	}
}

// TestFilterDryRunNodes (I4): CraftDryRun with an explicit facility_id needs
// the operator docked at that facility's station, so only craft nodes whose
// StationID matches the operator's current docked station can be verified from
// where the operator sits; the rest are warned-and-skipped. Pure filtering.
func TestFilterDryRunNodes(t *testing.T) {
	candidates := []craftbrain.Node{
		{ID: "here", StationID: "dock_stn"},
		{ID: "remote", StationID: "far_stn"},
		{ID: "here2", StationID: "dock_stn"},
	}

	verify, skipped := filterDryRunNodes(candidates, "dock_stn")
	if len(verify) != 2 || verify[0].ID != "here" || verify[1].ID != "here2" {
		t.Errorf("verify = %+v, want [here here2]", verify)
	}
	if len(skipped) != 1 || skipped[0].ID != "remote" {
		t.Errorf("skipped = %+v, want [remote]", skipped)
	}

	// Operator not docked anywhere: nothing can be verified locally.
	v2, s2 := filterDryRunNodes(candidates, "")
	if len(v2) != 0 || len(s2) != 3 {
		t.Errorf("empty docked station: verify=%d skipped=%d, want 0/3", len(v2), len(s2))
	}
}

func TestDispatchTransform_NoAssembly(t *testing.T) {
	raw, _ := json.Marshal(craftbrain.Plan{Target: "widget"})
	_, _, err := dispatchTransform(context.Background(), raw, dispatchArgs{}, "", fakeSeller(nil), time.Now())
	if err == nil {
		t.Fatal("want error for empty assembly base, got nil")
	}
}

func TestDispatchTransform_QueueFileShape(t *testing.T) {
	plan := craftbrain.Plan{
		Target: "Widget Mk2",
		Nodes: []craftbrain.Node{
			{ID: "craft-a", Kind: craftbrain.KindCraft, ItemID: "widget", Qty: 2, RecipeID: "recipe_widget", FacilityID: "fac-1", FeeTotal: 40, StationID: craftbrain.DefaultCraftBase},
			{ID: "craft-hand", Kind: craftbrain.KindCraft, ItemID: "gizmo", Qty: 1, RecipeID: "recipe_gizmo", FeeTotal: 10}, // hand-craft: no FacilityID
			{ID: "mine-a", Kind: craftbrain.KindMine, ItemID: "iron_ore", Qty: 4},
		},
	}
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)

	qf, verify, err := dispatchTransform(context.Background(), raw, dispatchArgs{mineItems: []string{"iron_ore"}}, "grand_exchange_station", fakeSeller(nil), now)
	if err != nil {
		t.Fatalf("dispatchTransform: %v", err)
	}

	if qf.Manifest.PlanID != "widget-mk2-20260710-120000" {
		t.Errorf("PlanID = %q", qf.Manifest.PlanID)
	}
	if qf.Manifest.Assembly != "grand_exchange_station" {
		t.Errorf("Assembly = %q, want grand_exchange_station", qf.Manifest.Assembly)
	}
	if qf.Manifest.DispatchedAt != now.Format(time.RFC3339) {
		t.Errorf("DispatchedAt = %q", qf.Manifest.DispatchedAt)
	}
	if len(qf.Manifest.MineItems) != 1 || qf.Manifest.MineItems[0] != "iron_ore" {
		t.Errorf("MineItems = %v, want [iron_ore]", qf.Manifest.MineItems)
	}
	if len(qf.Plan.Nodes) != 3 {
		t.Fatalf("Plan.Nodes len = %d, want 3", len(qf.Plan.Nodes))
	}
	// any_docked_station resolved on the facility craft node's StationID.
	for _, n := range qf.Plan.Nodes {
		if n.ID == "craft-a" && n.StationID != "grand_exchange_station" {
			t.Errorf("craft-a StationID = %q, want grand_exchange_station", n.StationID)
		}
	}

	// Only the facility craft node (non-empty FacilityID) needs dry-run
	// verification; the hand-craft and mine nodes must not appear.
	if len(verify) != 1 || verify[0].ID != "craft-a" {
		t.Errorf("verify = %+v, want exactly [craft-a]", verify)
	}

	// Round-trip through JSON to pin the on-disk shape the Runner's intake
	// glob (pkg/overmind/plans.Runner.intake) decodes.
	data, err := json.Marshal(qf)
	if err != nil {
		t.Fatalf("marshal QueueFile: %v", err)
	}
	var roundTrip plans.QueueFile
	if err := json.Unmarshal(data, &roundTrip); err != nil {
		t.Fatalf("unmarshal QueueFile: %v", err)
	}
	if roundTrip.Manifest.PlanID != qf.Manifest.PlanID {
		t.Errorf("round-tripped PlanID = %q, want %q", roundTrip.Manifest.PlanID, qf.Manifest.PlanID)
	}
}

func TestDispatchTransform_VerifyCap(t *testing.T) {
	plan := craftbrain.Plan{Target: "widget"}
	for i := range maxDryRunNodes + 5 {
		plan.Nodes = append(plan.Nodes, craftbrain.Node{
			ID: "craft-" + string(rune('a'+i)), Kind: craftbrain.KindCraft, ItemID: "widget",
			Qty: 1, RecipeID: "r", FacilityID: "fac-1",
		})
	}
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	_, verify, err := dispatchTransform(context.Background(), raw, dispatchArgs{}, "base", fakeSeller(nil), time.Now())
	if err != nil {
		t.Fatalf("dispatchTransform: %v", err)
	}
	if len(verify) != maxDryRunNodes {
		t.Errorf("verify len = %d, want %d (capped)", len(verify), maxDryRunNodes)
	}
}

func TestDispatchTransform_DecodeError(t *testing.T) {
	_, _, err := dispatchTransform(context.Background(), []byte("not json"), dispatchArgs{}, "base", fakeSeller(nil), time.Now())
	if err == nil {
		t.Fatal("want decode error, got nil")
	}
}

// --- plan_* mutator tests (LoadRun/SaveRun round trip against a temp dir) ---

func withTempStateDir(t *testing.T) {
	t.Helper()
	orig := craftStateDir
	craftStateDir = t.TempDir()
	t.Cleanup(func() { craftStateDir = orig })
}

func TestUnknownPlanErr(t *testing.T) {
	withTempStateDir(t)
	err := unknownPlanErr("nope")
	if err == nil {
		t.Fatal("want error")
	}
}

func TestMutatePlan_PauseResumeCancelRetry(t *testing.T) {
	withTempStateDir(t)

	pr := &plans.PlanRun{
		Manifest: plans.Manifest{PlanID: "test-plan"},
		Nodes: []*plans.NodeRun{
			{Node: craftbrain.Node{ID: "n1"}, State: plans.NodeParked, Park: plans.ParkFailed},
		},
	}
	if err := plans.SaveRun(craftStateDir, pr); err != nil {
		t.Fatalf("seed SaveRun: %v", err)
	}

	if err := runPlanPause([]string{"test-plan"}); err != nil {
		t.Fatalf("plan_pause: %v", err)
	}
	loaded, err := plans.LoadRun(planStatePath("test-plan"))
	if err != nil {
		t.Fatalf("LoadRun: %v", err)
	}
	if !loaded.Control.Pause {
		t.Error("Control.Pause = false after plan_pause, want true")
	}

	if err := runPlanResume([]string{"test-plan", "--raise-cap=500"}); err != nil {
		t.Fatalf("plan_resume: %v", err)
	}
	loaded, err = plans.LoadRun(planStatePath("test-plan"))
	if err != nil {
		t.Fatalf("LoadRun: %v", err)
	}
	if loaded.Control.Pause {
		t.Error("Control.Pause = true after plan_resume, want false (resume must clear pause)")
	}
	if loaded.Control.RaiseCap != 500 {
		t.Errorf("Control.RaiseCap = %d, want 500", loaded.Control.RaiseCap)
	}

	if err := runPlanRetry([]string{"test-plan", "n1"}); err != nil {
		t.Fatalf("plan_retry: %v", err)
	}
	loaded, err = plans.LoadRun(planStatePath("test-plan"))
	if err != nil {
		t.Fatalf("LoadRun: %v", err)
	}
	if len(loaded.Control.RetryNodes) != 1 || loaded.Control.RetryNodes[0] != "n1" {
		t.Errorf("Control.RetryNodes = %v, want [n1]", loaded.Control.RetryNodes)
	}

	if err := runPlanRetry([]string{"test-plan", "does-not-exist"}); err == nil {
		t.Error("plan_retry with unknown node id: want error, got nil")
	}

	if err := runPlanCancel([]string{"test-plan"}); err != nil {
		t.Fatalf("plan_cancel: %v", err)
	}
	loaded, err = plans.LoadRun(planStatePath("test-plan"))
	if err != nil {
		t.Fatalf("LoadRun: %v", err)
	}
	if !loaded.Control.Cancel {
		t.Error("Control.Cancel = false after plan_cancel, want true")
	}

	if err := runPlanPause([]string{"unknown-plan-id"}); err == nil {
		t.Error("plan_pause on unknown plan id: want error, got nil")
	}
}
