package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// CraftOutputs crafts NUM_OUTPUTS units of RECIPE's output item at STATION.
// facility "hand" hand-crafts at the docked station's local workshop (the
// server auto-routes when facility_id is omitted from the payload — Task 0
// findings, 2026-07-10, confirmed hand/workshop dry-run and craft ARE
// supported live); any other value is passed as an explicit facility
// instance id (crafting requires being docked at that facility's station —
// a remote facility_id errors no_facility). NUM_OUTPUTS is passed directly
// as the craft command's `quantity`: the server does the ceil-divide into
// production runs itself (live-verified: quantity 6 on a 3-per-run recipe
// produced "runs": 2), so no output-per-run math is needed for the craft
// call itself.
//
// Recompute-remaining: before crafting, any of the recipe's output item
// already sitting in the worker's own storage at STATION (or already
// carried in cargo) counts against NUM_OUTPUTS, so a resumed/retried node
// does not over-produce. The recipe's output item id is looked up from the
// KB (recipe_outputs) — the craft_node task's params (RECIPE, NUM_OUTPUTS,
// STATION, FACILITY) never carry it directly.
//
// Budget gate: a live dry-run always runs first — it is both the
// have_inputs/have_credits preflight and the 2x-replan gate. If the dry-run
// reports !HaveInputs or !HaveCredits, this fails immediately (no craft call)
// with an error naming which precondition failed plus the server's Message
// verbatim — live probes show the message names the nearest facility that
// CAN make the recipe when this station can't (Task 0 findings #6), which
// operators need in park details. Separately, the plan runner's budget
// admission already approved estFee (the planner's catalog fee estimate for
// this node); if the live dry-run's credits_total comes back more than 2x
// that, the catalog is stale and this fails with an error naming "replan"
// instead of queuing a possibly much more expensive job. estFee <= 0 disables
// the fee gate (no estimate to compare against).
//
// Crafting is asynchronous: the server queues the job and delivers output
// directly to the worker's own storage at STATION over subsequent ticks.
// Hand-crafts (the "hand" facility default) run at the Station Workshop —
// they advance only while docked there and PAUSE on undock (Task 0 findings
// #7). Since the plan runner marks a node done the moment this verb returns
// and may then have the worker undock for its next task, this verb does NOT
// return once the job is merely queued: it polls `craft action=queue` (via
// RawCommand — the only client-side surface that reports job progress
// without inventing a new server command) at game.SleepTick cadence, staying
// put, until the job reports complete for BOTH the hand and facility paths.
// The wait is bounded by the dry-run's EstCompletionTick plus a generous
// margin (GameClock only drifts forward — memory:
// reference_gameclock_forward_drift — so a tight bound risks a false-positive
// timeout); on timeout this returns an error, never a silent success.
func (d *WorkerDispatch) CraftOutputs(ctx context.Context, recipeID string, numOutputs int, station, facility string, estFee float64) error {
	if numOutputs < 1 {
		return fmt.Errorf("craft_node: numOutputs must be >= 1, got %d", numOutputs)
	}
	if facility == "" {
		facility = "hand"
	}

	sys, poi, err := resolveBase(ctx, d.KB, station)
	if err != nil {
		return fmt.Errorf("craft_node: resolve station %q: %w", station, err)
	}
	if err := d.autopilotAndDock(ctx, sys, poi); err != nil {
		return fmt.Errorf("craft_node: travel to station %q: %w", station, err)
	}

	itemID, err := recipeOutputItemID(ctx, d.KB, recipeID)
	if err != nil {
		return fmt.Errorf("craft_node: %w", err)
	}

	owned, err := d.craftOutputsOwned(ctx, itemID)
	if err != nil {
		return fmt.Errorf("craft_node: %w", err)
	}
	remaining := numOutputs - owned
	if remaining <= 0 {
		fmt.Fprintf(d.Out, "craft_node: %s already have %d of %d %s at %s — nothing to craft\n", //nolint:errcheck
			recipeID, owned, numOutputs, itemID, station)
		return nil
	}

	facilityID := ""
	if facility != "hand" {
		facilityID = facility
	}

	dry, err := d.Client.CraftDryRun(ctx, recipeID, remaining, facilityID)
	if err != nil {
		return fmt.Errorf("craft_node: dry run %s x%d: %w", recipeID, remaining, err)
	}
	if !dry.HaveInputs || !dry.HaveCredits {
		var missing []string
		if !dry.HaveInputs {
			missing = append(missing, "have_inputs=false")
		}
		if !dry.HaveCredits {
			missing = append(missing, "have_credits=false")
		}
		return fmt.Errorf("craft_node: %s x%d preflight failed (%s): %s",
			recipeID, remaining, strings.Join(missing, ", "), dry.Message)
	}
	if estFee > 0 && float64(dry.CreditsTotal) > 2*estFee {
		return fmt.Errorf("craft_node: %s x%d dry-run fee %d exceeds 2x planner estimate %v — stale catalog (replan)",
			recipeID, remaining, dry.CreditsTotal, estFee)
	}

	var jobID string
	if facility == "hand" {
		if err := d.Client.CraftWithQuantity(ctx, recipeID, remaining); err != nil {
			return fmt.Errorf("craft_node: craft %s x%d: %w", recipeID, remaining, err)
		}
		jobID, err = craftQueuedJobID(d.Client.GetRawJSON("_last"))
		if err != nil {
			return fmt.Errorf("craft_node: craft %s x%d: %w", recipeID, remaining, err)
		}
	} else {
		job := map[string]any{"recipe_id": recipeID, "quantity": remaining, "facility_id": facility}
		if err := d.Client.CraftBulk(ctx, []map[string]any{job}); err != nil {
			return fmt.Errorf("craft_node: craft %s x%d at facility %s: %w", recipeID, remaining, facility, err)
		}
		jobID, err = craftBulkJobID(d.Client.GetRawJSON("_last"))
		if err != nil {
			return fmt.Errorf("craft_node: craft %s x%d at facility %s: %w", recipeID, remaining, facility, err)
		}
	}

	if err := d.waitForCraftJob(ctx, jobID, dry.EstCompletionTick); err != nil {
		return fmt.Errorf("craft_node: craft %s x%d: %w", recipeID, remaining, err)
	}
	fmt.Fprintf(d.Out, "craft_node: completed %s x%d at %s (%d of %d already owned)\n", //nolint:errcheck
		recipeID, remaining, station, owned, numOutputs)
	return nil
}

// craftQueuedJobID decodes job_id from the raw JSON of a single (hand-craft)
// CraftWithQuantity queue response — the same shape as
// serverapi.CraftJobQueued, cached by the client under the "_last" raw-JSON
// key immediately after the queuing call returns.
func craftQueuedJobID(raw []byte) (string, error) {
	if raw == nil {
		return "", fmt.Errorf("no queue response captured (job_id unknown)")
	}
	var resp serverapi.CraftJobQueued
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", fmt.Errorf("decode queue response: %w", err)
	}
	if resp.JobID == "" {
		return "", fmt.Errorf("queue response carried no job_id")
	}
	return resp.JobID, nil
}

// craftBulkJobID decodes the job_id of the single job entry from a CraftBulk
// queue response (facility path). CraftOutputs always submits exactly one job
// per CraftBulk call, so only results[0] is examined.
func craftBulkJobID(raw []byte) (string, error) {
	if raw == nil {
		return "", fmt.Errorf("no queue response captured (job_id unknown)")
	}
	var resp serverapi.CraftBulkResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", fmt.Errorf("decode bulk queue response: %w", err)
	}
	if len(resp.Results) == 0 {
		return "", fmt.Errorf("bulk queue response carried no results")
	}
	r := resp.Results[0]
	if !r.Success {
		return "", fmt.Errorf("bulk craft job not queued: %s", r.Error)
	}
	if r.JobID == "" {
		return "", fmt.Errorf("bulk queue response carried no job_id")
	}
	return r.JobID, nil
}

// craftPollInterval is the poll cadence for waiting on an async craft job to
// complete: mutations resolve on the server's next tick (Task 0 findings
// #10), so there is no point polling faster than that.
const craftPollInterval = game.SleepTick

// craftPollTimeoutMarginTicks pads the dry-run's est_completion_tick before
// waitForCraftJob declares a timeout. GameClock only drifts forward (memory:
// reference_gameclock_forward_drift) — a bound sized tightly off
// est_completion_tick risks a false-positive timeout on a job that is still
// legitimately running a little behind the estimate.
const craftPollTimeoutMarginTicks = 30

// waitForCraftJob polls `craft action=queue` (the only client-side surface
// that reports in-flight craft job progress — server docs: "check progress
// with craft action=queue"; crafting_update push events are the
// server-recommended alternative but are wired as a callback on the concrete
// *game.Client, not the GameClient interface this package depends on) until
// jobID is no longer running, staying docked the whole time so a hand-craft
// (Station Workshop) job keeps advancing. estCompletionTick is the dry-run's
// estimate. CurrentTick is an absolute, ever-increasing counter (hundreds of
// thousands to millions), NOT a duration — so when estCompletionTick <= 0
// (the server gave no estimate), the deadline cannot be est + margin (that
// would be ~30, far below any real CurrentTick, and time out on the very
// first poll). Instead the deadline anchors to the tick observed when the
// wait started plus the flat margin. Returns an error — never a silent
// success — if the job is still running once the deadline passes.
func (d *WorkerDispatch) waitForCraftJob(ctx context.Context, jobID string, estCompletionTick int) error {
	sleep := d.craftPollSleep
	if sleep == nil {
		sleep = craftPollSleepFunc
	}
	deadlineTick := int64(estCompletionTick + craftPollTimeoutMarginTicks)
	if estCompletionTick <= 0 {
		deadlineTick = d.Client.GetState().CurrentTick + craftPollTimeoutMarginTicks
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := d.Client.RawCommand(ctx, "craft", map[string]any{"action": "queue"}); err != nil {
			return fmt.Errorf("poll job %s: %w", jobID, err)
		}
		done, runsRemaining, found := craftJobDone(d.Client.GetRawJSON("_last"), jobID)
		if done {
			return nil
		}
		currentTick := d.Client.GetState().CurrentTick
		if currentTick > deadlineTick {
			return fmt.Errorf("job %s timed out waiting for completion (tick %d > est_completion_tick %d + %d tick margin; runs_remaining=%d, still queued=%v)",
				jobID, currentTick, estCompletionTick, craftPollTimeoutMarginTicks, runsRemaining, found)
		}
		if err := sleep(ctx, craftPollInterval); err != nil {
			return err
		}
	}
}

// craftJobDone inspects a `craft action=queue` listing (the same shape as
// serverapi.CraftQueueListing) for jobID. done is true once the job has zero
// runs remaining, its status reads as terminal, or — the common case — it has
// simply dropped off the listing because it finished: the queue only lists
// in-flight jobs, and CraftOutputs never cancels its own job, so absence
// means completion, not loss. found reports whether jobID was present in this
// listing (for diagnostics on timeout).
//
// Discriminator (defense-in-depth against a "_last" clobber): a
// crafting_update push (serverapi.CraftingUpdateEvent: {"tick","jobs":[...]})
// shares the job_id/runs_remaining JSON tags with a genuine queue listing's
// job entries, so it decodes without error and — worse — the absence
// heuristic above would read a polled job's absence from that push as
// "finished" when its real status is simply unknown. serverapi.CraftJobQueued
// and CraftQueueListing both always carry a top-level "action" field (live
// fixture: `action":"queue"` — see serverapi/crafting_responses_test.go
// TestDecodeCraftQueueListing); CraftingUpdateEvent has no such field at all.
// A payload whose decoded Action isn't "queue" is therefore not a genuine
// queue listing and is treated as inconclusive — never as done.
func craftJobDone(raw []byte, jobID string) (done bool, runsRemaining int, found bool) {
	var listing serverapi.CraftQueueListing
	if err := json.Unmarshal(raw, &listing); err != nil {
		return false, 0, false
	}
	if listing.Action != "queue" {
		return false, 0, false
	}
	for _, j := range listing.Jobs {
		if j.JobID != jobID {
			continue
		}
		status := strings.ToLower(j.Status)
		if j.RunsRemaining <= 0 || status == "completed" || status == "done" {
			return true, j.RunsRemaining, true
		}
		return false, j.RunsRemaining, true
	}
	return true, 0, false
}

// craftPollSleepFunc sleeps for d, returning early with ctx.Err() if ctx is
// cancelled first. This is waitForCraftJob's default craftPollSleep; tests
// override the WorkerDispatch field with a zero-delay stand-in so polling
// retries don't add real wall-clock time to the suite.
func craftPollSleepFunc(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

// recipeOutputItemID looks up the output item id produced by recipeID from
// the KB's recipe_outputs table. Requires *knowledge.SQLiteKB, mirroring
// resolveBase's requirement — there is no SQL to run against other Base
// implementations.
func recipeOutputItemID(ctx context.Context, kb knowledge.Base, recipeID string) (string, error) {
	sqliteKB, ok := kb.(*knowledge.SQLiteKB)
	if !ok || sqliteKB == nil {
		return "", fmt.Errorf("recipe output lookup for %q: requires *knowledge.SQLiteKB, got %T", recipeID, kb)
	}
	var itemID string
	err := sqliteKB.DB().QueryRowContext(ctx, `SELECT item_id FROM recipe_outputs WHERE recipe_id = ? LIMIT 1`, recipeID).Scan(&itemID)
	if err != nil {
		return "", fmt.Errorf("recipe output lookup for %q: %w", recipeID, err)
	}
	return itemID, nil
}

// craftOutputsOwned returns how many units of itemID the worker already owns
// at the current (already-docked) station — own storage plus ship cargo.
func (d *WorkerDispatch) craftOutputsOwned(ctx context.Context, itemID string) (int, error) {
	if err := d.Client.ViewStorage(ctx); err != nil {
		return 0, fmt.Errorf("view storage: %w", err)
	}
	time.Sleep(game.SleepQuick)
	storageQty := storageItemCount(d.Client.GetRawJSON("storage"), itemID)
	if err := d.Client.GetCargo(ctx); err != nil {
		return 0, fmt.Errorf("refresh cargo: %w", err)
	}
	return storageQty + cargoCount(d.Client.GetState(), itemID), nil
}

// storageItemCount decodes a view_storage raw JSON payload — the same
// {"items": [{"item_id", "quantity", ...}]} shape as
// serverapi.ViewStorageResponse and pkg/game's private getStorageItems
// decode — and returns itemID's quantity, or 0 if absent or the payload is
// missing/unparseable.
func storageItemCount(raw []byte, itemID string) int {
	if raw == nil {
		return 0
	}
	var resp struct {
		Items []struct {
			ItemID   string  `json:"item_id"`
			Quantity float64 `json:"quantity"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return 0
	}
	for _, item := range resp.Items {
		if item.ItemID == itemID {
			return int(item.Quantity)
		}
	}
	return 0
}
