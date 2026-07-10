package worker

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// craftFakeClient wraps the package's fakeClient (dispatch_test.go) with
// craft-specific simulation: ViewStorage/GetCargo are no-ops that rely on
// pre-populated f.raw["storage"] / f.state.Ship.Cargo (mirroring
// deliverFakeClient's convention), and CraftDryRun/CraftWithQuantity/
// CraftBulk are recorded for assertion instead of doing anything live.
type craftFakeClient struct {
	*fakeClient
	dryRunResult *serverapi.CraftDryRunResponse
	dryRunErr    error

	craftQuantityCalls []craftQuantityCall
	craftBulkCalls     [][]map[string]any
}

type craftQuantityCall struct {
	recipeID string
	quantity int
}

func (f *craftFakeClient) ViewStorage(ctx context.Context) error {
	f.calls = append(f.calls, "view_storage")
	return nil
}

func (f *craftFakeClient) CraftWithQuantity(ctx context.Context, recipeID string, quantity int) error {
	f.calls = append(f.calls, fmt.Sprintf("craft:%s:%d", recipeID, quantity))
	f.craftQuantityCalls = append(f.craftQuantityCalls, craftQuantityCall{recipeID: recipeID, quantity: quantity})
	return nil
}

func (f *craftFakeClient) CraftBulk(ctx context.Context, jobs []map[string]any) error {
	f.calls = append(f.calls, "craft_bulk")
	f.craftBulkCalls = append(f.craftBulkCalls, jobs)
	return nil
}

func (f *craftFakeClient) CraftDryRun(ctx context.Context, recipeID string, quantity int, facilityID string) (*serverapi.CraftDryRunResponse, error) {
	f.calls = append(f.calls, fmt.Sprintf("craft_dry_run:%s:%d:%s", recipeID, quantity, facilityID))
	if f.dryRunErr != nil {
		return nil, f.dryRunErr
	}
	if f.dryRunResult != nil {
		return f.dryRunResult, nil
	}
	return &serverapi.CraftDryRunResponse{}, nil
}

// newCraftTestKB builds an in-memory SQLiteKB with a "hub_a" base and a
// make_widget -> widget (2 per run) recipe_outputs row, so resolveBase and
// recipeOutputItemID have real rows to resolve.
func newCraftTestKB(t *testing.T) *knowledge.SQLiteKB {
	t.Helper()
	kb, err := knowledge.NewSQLiteKB(knowledge.Config{DBPath: ":memory:"})
	if err != nil {
		t.Fatalf("NewSQLiteKB: %v", err)
	}
	ctx := context.Background()
	db := kb.DB()
	rows := []string{
		`INSERT INTO pois (id, system_id, name, type, position_x, position_y)
		 VALUES ('hub_a_poi','hub_a_sys','Hub A Poi','station',0,0)`,
		`INSERT INTO bases (id, poi_id, name) VALUES ('hub_a','hub_a_poi','Hub A Base')`,
		`INSERT INTO recipe_outputs (recipe_id, item_id, quantity, quality_mod)
		 VALUES ('make_widget','widget',2,1.0)`,
	}
	for _, q := range rows {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatalf("seed KB: %v", err)
		}
	}
	return kb
}

// TestCraftOutputsHandCraftsFullQuantity is the regression test for finding
// #4 (2026-07-10 live mechanics): NUM_OUTPUTS is passed directly as the
// craft `quantity` — the server does the ceil-divide into runs itself — so
// with nothing already owned, CraftWithQuantity must be called with the
// full requested quantity, not some runs-adjusted number.
func TestCraftOutputsHandCraftsFullQuantity(t *testing.T) {
	kb := newCraftTestKB(t)
	client := &craftFakeClient{
		fakeClient:   &fakeClient{state: &game.State{}},
		dryRunResult: &serverapi.CraftDryRunResponse{CreditsTotal: 5, HaveInputs: true, HaveCredits: true},
	}
	d := NewWorkerDispatch(client, kb, nil, io.Discard)

	if err := d.CraftOutputs(context.Background(), "make_widget", 5, "hub_a", "hand", 0); err != nil {
		t.Fatalf("CraftOutputs: %v", err)
	}
	if len(client.craftQuantityCalls) != 1 {
		t.Fatalf("CraftWithQuantity calls = %d, want 1 (%+v)", len(client.craftQuantityCalls), client.craftQuantityCalls)
	}
	got := client.craftQuantityCalls[0]
	if got.recipeID != "make_widget" || got.quantity != 5 {
		t.Fatalf("CraftWithQuantity call = %+v, want make_widget x5", got)
	}
	if len(client.craftBulkCalls) != 0 {
		t.Fatalf("CraftBulk must not be called for facility=hand, got %+v", client.craftBulkCalls)
	}
}

// TestCraftOutputsFacilityUsesCraftBulkWithFacilityID: a real facility
// instance id (not "hand") must route through CraftBulk with facility_id set
// (dry_run is not compatible with CraftBulk, so the dry-run itself still
// goes through the separate CraftDryRun call with facility_id).
func TestCraftOutputsFacilityUsesCraftBulkWithFacilityID(t *testing.T) {
	kb := newCraftTestKB(t)
	client := &craftFakeClient{
		fakeClient:   &fakeClient{state: &game.State{}},
		dryRunResult: &serverapi.CraftDryRunResponse{CreditsTotal: 0, HaveInputs: true, HaveCredits: true},
	}
	d := NewWorkerDispatch(client, kb, nil, io.Discard)

	facilityID := "workshop:abc123:hub_a"
	if err := d.CraftOutputs(context.Background(), "make_widget", 5, "hub_a", facilityID, 0); err != nil {
		t.Fatalf("CraftOutputs: %v", err)
	}
	if len(client.craftQuantityCalls) != 0 {
		t.Fatalf("CraftWithQuantity must not be called for a real facility, got %+v", client.craftQuantityCalls)
	}
	if len(client.craftBulkCalls) != 1 || len(client.craftBulkCalls[0]) != 1 {
		t.Fatalf("CraftBulk calls = %+v, want one job", client.craftBulkCalls)
	}
	job := client.craftBulkCalls[0][0]
	if job["recipe_id"] != "make_widget" || job["quantity"] != 5 || job["facility_id"] != facilityID {
		t.Fatalf("CraftBulk job = %+v", job)
	}
	// The dry-run call must have carried the same facility_id.
	found := false
	for _, c := range client.calls {
		if c == fmt.Sprintf("craft_dry_run:make_widget:5:%s", facilityID) {
			found = true
		}
	}
	if !found {
		t.Fatalf("dry run call not found with facility_id, calls = %v", client.calls)
	}
}

// TestCraftOutputsReplanGateOnStaleFee is the regression test for the
// 2x-replan budget gate: the runner's budget admission already approved
// estFee (the planner's catalog estimate); a live dry-run credits_total more
// than double that means the catalog is stale, and CraftOutputs must fail
// with an error naming "replan" instead of queuing a job — without ever
// calling CraftWithQuantity/CraftBulk.
func TestCraftOutputsReplanGateOnStaleFee(t *testing.T) {
	kb := newCraftTestKB(t)
	client := &craftFakeClient{
		fakeClient:   &fakeClient{state: &game.State{}},
		dryRunResult: &serverapi.CraftDryRunResponse{CreditsTotal: 26, HaveInputs: true, HaveCredits: true}, // 2.6x of estFee=10
	}
	d := NewWorkerDispatch(client, kb, nil, io.Discard)

	err := d.CraftOutputs(context.Background(), "make_widget", 5, "hub_a", "hand", 10)
	if err == nil {
		t.Fatal("expected an error for a dry-run fee more than 2x the estimate")
	}
	if !strings.Contains(err.Error(), "replan") {
		t.Fatalf("err = %q, want it to contain %q", err.Error(), "replan")
	}
	if len(client.craftQuantityCalls) != 0 || len(client.craftBulkCalls) != 0 {
		t.Fatalf("craft must not be called after the gate rejects, calls = %v", client.calls)
	}
}

// TestCraftOutputsGateDisabledWhenEstFeeZero: estFee 0 means the planner had
// no fee estimate for this node (or the gate is simply off) — an
// arbitrarily large dry-run credits_total must not block the craft.
func TestCraftOutputsGateDisabledWhenEstFeeZero(t *testing.T) {
	kb := newCraftTestKB(t)
	client := &craftFakeClient{
		fakeClient:   &fakeClient{state: &game.State{}},
		dryRunResult: &serverapi.CraftDryRunResponse{CreditsTotal: 100000, HaveInputs: true, HaveCredits: true},
	}
	d := NewWorkerDispatch(client, kb, nil, io.Discard)

	if err := d.CraftOutputs(context.Background(), "make_widget", 5, "hub_a", "hand", 0); err != nil {
		t.Fatalf("CraftOutputs: %v", err)
	}
	if len(client.craftQuantityCalls) != 1 {
		t.Fatalf("CraftWithQuantity calls = %d, want 1", len(client.craftQuantityCalls))
	}
}

// TestCraftOutputsRecomputesAlreadyOwned is the regression test for the
// recompute-remaining step: 3 of the 5 requested outputs are already sitting
// in the worker's own storage at STATION, so CraftOutputs must craft only
// the remaining 2 — not re-craft the full NUM_OUTPUTS and over-produce on a
// resumed/retried node.
func TestCraftOutputsRecomputesAlreadyOwned(t *testing.T) {
	kb := newCraftTestKB(t)
	client := &craftFakeClient{
		fakeClient: &fakeClient{
			state: &game.State{},
			raw:   map[string][]byte{"storage": []byte(`{"items":[{"item_id":"widget","name":"Widget","quantity":3}]}`)},
		},
		dryRunResult: &serverapi.CraftDryRunResponse{CreditsTotal: 0, HaveInputs: true, HaveCredits: true},
	}
	d := NewWorkerDispatch(client, kb, nil, io.Discard)

	if err := d.CraftOutputs(context.Background(), "make_widget", 5, "hub_a", "hand", 0); err != nil {
		t.Fatalf("CraftOutputs: %v", err)
	}
	if len(client.craftQuantityCalls) != 1 || client.craftQuantityCalls[0].quantity != 2 {
		t.Fatalf("CraftWithQuantity calls = %+v, want quantity 2 (5 requested - 3 already owned)", client.craftQuantityCalls)
	}
	// The dry run itself must also have been scoped to the remaining amount.
	found := false
	for _, c := range client.calls {
		if c == "craft_dry_run:make_widget:2:" {
			found = true
		}
	}
	if !found {
		t.Fatalf("dry run not scoped to remaining 2, calls = %v", client.calls)
	}
}

// TestCraftOutputsCountsCargoToo: already-owned counts storage AND cargo —
// 2 in storage + 2 in cargo covers all 4 requested, so nothing is crafted.
func TestCraftOutputsCountsCargoToo(t *testing.T) {
	kb := newCraftTestKB(t)
	client := &craftFakeClient{
		fakeClient: &fakeClient{
			state: &game.State{Ship: game.Ship{Cargo: []game.CargoItem{{ItemID: "widget", Quantity: 2}}}},
			raw:   map[string][]byte{"storage": []byte(`{"items":[{"item_id":"widget","name":"Widget","quantity":2}]}`)},
		},
	}
	d := NewWorkerDispatch(client, kb, nil, io.Discard)

	if err := d.CraftOutputs(context.Background(), "make_widget", 4, "hub_a", "hand", 0); err != nil {
		t.Fatalf("CraftOutputs: %v", err)
	}
	if len(client.craftQuantityCalls) != 0 {
		t.Fatalf("CraftWithQuantity must not be called when already-owned covers the full request, got %+v", client.craftQuantityCalls)
	}
	for _, c := range client.calls {
		if strings.HasPrefix(c, "craft_dry_run:") {
			t.Fatalf("dry run must not be called when nothing remains to craft, calls = %v", client.calls)
		}
	}
}

// TestCraftOutputsUnknownRecipeErrors: a recipe absent from recipe_outputs
// (stale or wrong KB) must fail clearly instead of crafting item id "".
func TestCraftOutputsUnknownRecipeErrors(t *testing.T) {
	kb := newCraftTestKB(t)
	client := &craftFakeClient{fakeClient: &fakeClient{state: &game.State{}}}
	d := NewWorkerDispatch(client, kb, nil, io.Discard)

	err := d.CraftOutputs(context.Background(), "no_such_recipe", 5, "hub_a", "hand", 0)
	if err == nil {
		t.Fatal("expected an error for a recipe with no recipe_outputs row")
	}
	if len(client.craftQuantityCalls) != 0 || len(client.craftBulkCalls) != 0 {
		t.Fatalf("craft must not be called for an unresolvable recipe, calls = %v", client.calls)
	}
}

// TestDispatchCraftNode verifies the Run() dispatch case parses
// RECIPE NUM_OUTPUTS STATION FACILITY EST_FEE positionally and forwards them
// to CraftOutputs, including the optional trailing EST_FEE.
func TestDispatchCraftNode(t *testing.T) {
	kb := newCraftTestKB(t)
	client := &craftFakeClient{
		fakeClient:   &fakeClient{state: &game.State{}},
		dryRunResult: &serverapi.CraftDryRunResponse{CreditsTotal: 5, HaveInputs: true, HaveCredits: true},
	}
	d := NewWorkerDispatch(client, kb, nil, io.Discard)

	if err := d.Run(context.Background(), []string{"craft_node", "make_widget", "5", "hub_a", "hand", "10"}); err != nil {
		t.Fatalf("Run(craft_node): %v", err)
	}
	if len(client.craftQuantityCalls) != 1 || client.craftQuantityCalls[0].quantity != 5 {
		t.Fatalf("CraftWithQuantity calls = %+v", client.craftQuantityCalls)
	}
}

// TestDispatchCraftNodeMissingArgs: fewer than 4 args must error before
// touching the client.
func TestDispatchCraftNodeMissingArgs(t *testing.T) {
	kb := newCraftTestKB(t)
	client := &craftFakeClient{fakeClient: &fakeClient{state: &game.State{}}}
	d := NewWorkerDispatch(client, kb, nil, io.Discard)

	err := d.Run(context.Background(), []string{"craft_node", "make_widget", "5", "hub_a"})
	if err == nil {
		t.Fatal("expected an error for missing FACILITY arg")
	}
	if len(client.calls) != 0 {
		t.Fatalf("no client calls expected before validation, got %v", client.calls)
	}
}
