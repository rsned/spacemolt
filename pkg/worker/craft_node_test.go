package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

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

	// queueJobID is the job_id carried in the queued-craft response cached
	// under "_last" right after CraftWithQuantity/CraftBulk. Defaults to
	// "job-1" so tests that don't care about polling still get a decodable
	// queue response.
	queueJobID string

	// pollResponses is a FIFO of raw `craft action=queue` listing JSON
	// bodies: RawCommand("craft", {"action":"queue"}) returns
	// pollResponses[pollCallCount] each call (repeating the last entry once
	// exhausted). Left unset, RawCommand returns an empty jobs listing —
	// the queued job is immediately "not found" == done, so tests that don't
	// exercise polling never sleep.
	pollResponses [][]byte
	pollCallCount int
	pollErr       error
}

type craftQuantityCall struct {
	recipeID string
	quantity int
}

func (f *craftFakeClient) ViewStorage(ctx context.Context) error {
	f.calls = append(f.calls, "view_storage")
	return nil
}

// setQueuedRaw caches a decodable queued-craft response under "_last",
// mirroring what the real client does after a craft/craft-bulk call returns —
// craft_node.go reads it back immediately to learn the job_id.
func (f *craftFakeClient) setQueuedRaw(bulk bool) {
	jobID := f.queueJobID
	if jobID == "" {
		jobID = "job-1"
	}
	var body []byte
	if bulk {
		body, _ = json.Marshal(serverapi.CraftBulkResponse{ //nolint:errcheck
			Action:  "craft",
			Mode:    "bulk",
			Results: []serverapi.CraftBulkResult{{Index: 0, Success: true, JobID: jobID}},
			Summary: serverapi.CraftBulkSummary{Total: 1, Succeeded: 1},
		})
	} else {
		body, _ = json.Marshal(serverapi.CraftJobQueued{Action: "craft", JobID: jobID}) //nolint:errcheck
	}
	if f.raw == nil {
		f.raw = map[string][]byte{}
	}
	f.raw["_last"] = body
}

func (f *craftFakeClient) CraftWithQuantity(ctx context.Context, recipeID string, quantity int) error {
	f.calls = append(f.calls, fmt.Sprintf("craft:%s:%d", recipeID, quantity))
	f.craftQuantityCalls = append(f.craftQuantityCalls, craftQuantityCall{recipeID: recipeID, quantity: quantity})
	f.setQueuedRaw(false)
	return nil
}

func (f *craftFakeClient) CraftBulk(ctx context.Context, jobs []map[string]any) error {
	f.calls = append(f.calls, "craft_bulk")
	f.craftBulkCalls = append(f.craftBulkCalls, jobs)
	f.setQueuedRaw(true)
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
	return &serverapi.CraftDryRunResponse{HaveInputs: true, HaveCredits: true}, nil
}

// RawCommand intercepts `craft action=queue` polling (from
// WorkerDispatch.waitForCraftJob) and serves pollResponses in order; every
// other command falls through to the embedded fakeClient.
func (f *craftFakeClient) RawCommand(ctx context.Context, command string, args map[string]any) error {
	if command == "craft" && args["action"] == "queue" {
		f.calls = append(f.calls, "raw:craft:queue")
		if f.pollErr != nil {
			return f.pollErr
		}
		var body []byte
		switch {
		case f.pollCallCount < len(f.pollResponses):
			body = f.pollResponses[f.pollCallCount]
		case len(f.pollResponses) > 0:
			body = f.pollResponses[len(f.pollResponses)-1]
		default:
			body = []byte(`{"action":"queue","jobs":[]}`)
		}
		f.pollCallCount++
		if f.raw == nil {
			f.raw = map[string][]byte{}
		}
		f.raw["_last"] = body
		return nil
	}
	return f.fakeClient.RawCommand(ctx, command, args)
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

// TestCraftOutputsPreflightFailsOnMissingInputs is the regression test for
// finding #2: the dry-run's HaveInputs/HaveCredits preflight was previously
// ignored entirely — only CreditsTotal was read. have_inputs=false must fail
// CraftOutputs immediately, surfacing the server's Message verbatim (Task 0
// findings #6: the message names the nearest facility that CAN make the
// recipe — operators need that text in park details) — without ever queuing
// a craft.
func TestCraftOutputsPreflightFailsOnMissingInputs(t *testing.T) {
	kb := newCraftTestKB(t)
	const wantMsg = "'Assemble Widget' is made in a Widget Assembler, and no facility here can make it. Nearest public one: Hub B (3 jump(s) away)."
	client := &craftFakeClient{
		fakeClient: &fakeClient{state: &game.State{}},
		dryRunResult: &serverapi.CraftDryRunResponse{
			CreditsTotal: 5, HaveInputs: false, HaveCredits: true, Message: wantMsg,
		},
	}
	d := NewWorkerDispatch(client, kb, nil, io.Discard)

	err := d.CraftOutputs(context.Background(), "make_widget", 5, "hub_a", "hand", 0)
	if err == nil {
		t.Fatal("expected an error when the dry-run reports have_inputs=false")
	}
	if !strings.Contains(err.Error(), wantMsg) {
		t.Fatalf("err = %q, want it to contain the server message %q", err.Error(), wantMsg)
	}
	if !strings.Contains(err.Error(), "have_inputs=false") {
		t.Fatalf("err = %q, want it to name have_inputs=false", err.Error())
	}
	if len(client.craftQuantityCalls) != 0 || len(client.craftBulkCalls) != 0 {
		t.Fatalf("craft must not be called after a failed preflight, calls = %v", client.calls)
	}
}

// TestCraftOutputsPreflightFailsOnMissingCredits mirrors the have_inputs
// case for have_credits=false.
func TestCraftOutputsPreflightFailsOnMissingCredits(t *testing.T) {
	kb := newCraftTestKB(t)
	client := &craftFakeClient{
		fakeClient: &fakeClient{state: &game.State{}},
		dryRunResult: &serverapi.CraftDryRunResponse{
			CreditsTotal: 5, HaveInputs: true, HaveCredits: false, Message: "not enough credits",
		},
	}
	d := NewWorkerDispatch(client, kb, nil, io.Discard)

	err := d.CraftOutputs(context.Background(), "make_widget", 5, "hub_a", "hand", 0)
	if err == nil {
		t.Fatal("expected an error when the dry-run reports have_credits=false")
	}
	if !strings.Contains(err.Error(), "have_credits=false") || !strings.Contains(err.Error(), "not enough credits") {
		t.Fatalf("err = %q, want it to name have_credits=false and the server message", err.Error())
	}
	if len(client.craftQuantityCalls) != 0 || len(client.craftBulkCalls) != 0 {
		t.Fatalf("craft must not be called after a failed preflight, calls = %v", client.calls)
	}
}

// TestCraftOutputsWaitsForJobCompletion is the regression test for finding
// #1: CraftOutputs must not return the instant CraftWithQuantity/CraftBulk
// accepts the job — it must poll `craft action=queue` until the job is no
// longer running. The fake reports the job still running (runs_remaining=1)
// on the first poll and gone (completed) on the second, so a premature
// return would be caught by asserting both polls happened.
func TestCraftOutputsWaitsForJobCompletion(t *testing.T) {
	kb := newCraftTestKB(t)
	client := &craftFakeClient{
		fakeClient:   &fakeClient{state: &game.State{CurrentTick: 50}},
		dryRunResult: &serverapi.CraftDryRunResponse{HaveInputs: true, HaveCredits: true, EstCompletionTick: 100},
		pollResponses: [][]byte{
			[]byte(`{"action":"queue","jobs":[{"job_id":"job-1","runs_remaining":1,"status":"running"}]}`),
			[]byte(`{"action":"queue","jobs":[]}`),
		},
	}
	d := NewWorkerDispatch(client, kb, nil, io.Discard)
	d.craftPollSleep = func(ctx context.Context, dur time.Duration) error { return nil } // no real wall-clock wait

	if err := d.CraftOutputs(context.Background(), "make_widget", 5, "hub_a", "hand", 0); err != nil {
		t.Fatalf("CraftOutputs: %v", err)
	}
	if client.pollCallCount != 2 {
		t.Fatalf("poll calls = %d, want 2 (verb must not return before the job reports complete)", client.pollCallCount)
	}
}

// TestCraftOutputsWaitTimesOut is the regression test for the bounded-wait
// half of finding #1: if the job never reports complete before
// est_completion_tick + margin, CraftOutputs must fail (never a silent
// success) so the runner can retry/park the node. CurrentTick is set well
// past the deadline up front so this asserts without ever needing to sleep.
func TestCraftOutputsWaitTimesOut(t *testing.T) {
	kb := newCraftTestKB(t)
	client := &craftFakeClient{
		fakeClient:   &fakeClient{state: &game.State{CurrentTick: 100000}},
		dryRunResult: &serverapi.CraftDryRunResponse{HaveInputs: true, HaveCredits: true, EstCompletionTick: 100},
		pollResponses: [][]byte{
			[]byte(`{"action":"queue","jobs":[{"job_id":"job-1","runs_remaining":1,"status":"running"}]}`),
		},
	}
	d := NewWorkerDispatch(client, kb, nil, io.Discard)
	d.craftPollSleep = func(ctx context.Context, dur time.Duration) error { return nil } // no real wall-clock wait

	err := d.CraftOutputs(context.Background(), "make_widget", 5, "hub_a", "hand", 0)
	if err == nil {
		t.Fatal("expected a timeout error when the job never completes before the deadline")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %q, want it to mention a timeout", err.Error())
	}
}

// TestCraftOutputsWaitSucceedsWhenEstCompletionTickIsZero is the regression
// test for the deadline degeneration bug: deadlineTick was computed as
// estCompletionTick + craftPollTimeoutMarginTicks with NO reference to the
// absolute current tick. When the dry-run gives no estimate (est_completion_tick
// <= 0 — the server's documented "no estimate" sentinel), that produced a
// deadline of ~30, while CurrentTick is an absolute counter in the hundreds of
// thousands to millions (memory: current tick ~1.3M as of this writing) — so
// the very first poll always finds currentTick > deadlineTick and times out
// immediately, failing a perfectly good craft. The fix must anchor the
// fallback deadline to the tick observed when the wait started, not to zero.
// CurrentTick is set high (1,300,000-ish) up front to prove the fallback
// isn't accidentally working only because CurrentTick happens to be small.
func TestCraftOutputsWaitSucceedsWhenEstCompletionTickIsZero(t *testing.T) {
	kb := newCraftTestKB(t)
	client := &craftFakeClient{
		fakeClient:   &fakeClient{state: &game.State{CurrentTick: 1_300_000}},
		dryRunResult: &serverapi.CraftDryRunResponse{HaveInputs: true, HaveCredits: true, EstCompletionTick: 0},
		pollResponses: [][]byte{
			[]byte(`{"action":"queue","jobs":[{"job_id":"job-1","runs_remaining":2,"status":"running"}]}`),
			[]byte(`{"action":"queue","jobs":[{"job_id":"job-1","runs_remaining":1,"status":"running"}]}`),
			[]byte(`{"action":"queue","jobs":[]}`), // poll 3: job gone, done
		},
	}
	d := NewWorkerDispatch(client, kb, nil, io.Discard)
	d.craftPollSleep = func(ctx context.Context, dur time.Duration) error { return nil }

	if err := d.CraftOutputs(context.Background(), "make_widget", 5, "hub_a", "hand", 0); err != nil {
		t.Fatalf("CraftOutputs: %v, want success (est_completion_tick=0 must not degenerate to an instant timeout)", err)
	}
	if client.pollCallCount != 3 {
		t.Fatalf("poll calls = %d, want 3", client.pollCallCount)
	}
}

// TestCraftOutputsFacilityWaitsForJobCompletion mirrors
// TestCraftOutputsWaitsForJobCompletion for the facility (CraftBulk) path,
// confirming the wait applies to BOTH facility and hand paths.
func TestCraftOutputsFacilityWaitsForJobCompletion(t *testing.T) {
	kb := newCraftTestKB(t)
	client := &craftFakeClient{
		fakeClient:   &fakeClient{state: &game.State{CurrentTick: 10}},
		dryRunResult: &serverapi.CraftDryRunResponse{HaveInputs: true, HaveCredits: true, EstCompletionTick: 50},
		pollResponses: [][]byte{
			[]byte(`{"action":"queue","jobs":[{"job_id":"job-1","runs_remaining":2,"status":"running"}]}`),
			[]byte(`{"action":"queue","jobs":[{"job_id":"job-1","runs_remaining":0,"status":"completed"}]}`),
		},
	}
	d := NewWorkerDispatch(client, kb, nil, io.Discard)
	d.craftPollSleep = func(ctx context.Context, dur time.Duration) error { return nil }

	facilityID := "workshop:abc123:hub_a"
	if err := d.CraftOutputs(context.Background(), "make_widget", 5, "hub_a", facilityID, 0); err != nil {
		t.Fatalf("CraftOutputs: %v", err)
	}
	if client.pollCallCount != 2 {
		t.Fatalf("poll calls = %d, want 2", client.pollCallCount)
	}
}

// TestCraftJobDone_CraftingUpdateShapeIsInconclusive is the regression test
// for the craft_node half of the "_last" clobber bug (pkg/game/client.go
// storeRawJSON now excludes crafting_update from ever landing in "_last", but
// this is defense-in-depth in case a clobber slips through some other path).
// A crafting_update push (serverapi.CraftingUpdateEvent: {"tick","jobs":[...]})
// has no top-level "action" field, but its per-job entries share the
// job_id/runs_remaining tags with serverapi.CraftQueueListing's CraftJobEntry,
// so it decodes cleanly as a (bogus) queue listing. If jobID being polled
// isn't one of the jobs named in that push (the common case — a crafting_update
// tick for some OTHER job clobbering the read), the old absence-means-done
// heuristic reported done=true for a job whose real status is completely
// unknown. craftJobDone must instead recognize the payload isn't a genuine
// queue listing (Action != "queue") and report inconclusive (done=false,
// found=false) so the caller polls again instead of declaring silent success.
func TestCraftJobDone_CraftingUpdateShapeIsInconclusive(t *testing.T) {
	// Verbatim crafting_update shape (serverapi.CraftingUpdateEvent) for a
	// DIFFERENT job than the one being polled.
	raw := []byte(`{"tick":1300000,"jobs":[{"job_id":"job-other-999","recipe":"r","mode":"craft","venue":"v","storage":"station","deposited":[],"runs_done":1,"runs_remaining":3,"completed":false}]}`)

	done, runsRemaining, found := craftJobDone(raw, "job-1")
	if done {
		t.Fatalf("craftJobDone must not report done for a crafting_update-shaped payload naming a different job; got done=true runsRemaining=%d found=%v", runsRemaining, found)
	}
	if found {
		t.Errorf("found = true, want false (job-1 was never actually looked up in a genuine queue listing)")
	}
}

// TestCraftOutputsWaitSurvivesCraftingUpdateShapedPoll is the integration
// counterpart: waitForCraftJob must keep polling (not falsely conclude done)
// when a poll response is crafting_update-shaped, and only conclude success
// once a genuine `action=queue` listing shows the job gone.
func TestCraftOutputsWaitSurvivesCraftingUpdateShapedPoll(t *testing.T) {
	kb := newCraftTestKB(t)
	client := &craftFakeClient{
		fakeClient:   &fakeClient{state: &game.State{CurrentTick: 10}},
		dryRunResult: &serverapi.CraftDryRunResponse{HaveInputs: true, HaveCredits: true, EstCompletionTick: 100},
		pollResponses: [][]byte{
			// Poll 1: a crafting_update push clobbered "_last" for a different job.
			[]byte(`{"tick":11,"jobs":[{"job_id":"job-other-999","recipe":"r","mode":"craft","venue":"v","storage":"station","deposited":[],"runs_done":1,"runs_remaining":3,"completed":false}]}`),
			// Poll 2: genuine queue listing, job still running.
			[]byte(`{"action":"queue","jobs":[{"job_id":"job-1","runs_remaining":1,"status":"running"}]}`),
			// Poll 3: genuine queue listing, job gone (done).
			[]byte(`{"action":"queue","jobs":[]}`),
		},
	}
	d := NewWorkerDispatch(client, kb, nil, io.Discard)
	d.craftPollSleep = func(ctx context.Context, dur time.Duration) error { return nil }

	if err := d.CraftOutputs(context.Background(), "make_widget", 5, "hub_a", "hand", 0); err != nil {
		t.Fatalf("CraftOutputs: %v", err)
	}
	if client.pollCallCount != 3 {
		t.Fatalf("poll calls = %d, want 3 (must not conclude done on the crafting_update-shaped poll)", client.pollCallCount)
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
