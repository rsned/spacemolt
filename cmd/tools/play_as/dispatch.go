package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/craftbrain"
	"github.com/rsned/spacemolt/pkg/finditem"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/overmind/plans"
	"github.com/rsned/spacemolt/pkg/overmind/supervisor"
)

const (
	// craftQueueDir is where dispatch drops QueueFile JSON for the overmind
	// Runner to pick up (pkg/overmind/plans.Runner.QueueDir).
	craftQueueDir = "data/overmind/craft-queue"
	// craftFleetPath is the roster dispatch defaults --assembly from (its
	// first entry's station) when the operator doesn't pass one explicitly.
	craftFleetPath = "data/overmind/craft-fleet.yaml"

	// maxDryRunNodes caps how many facility craft nodes the verify pass
	// quotes live, so a large plan can't turn `dispatch` into a slow
	// per-node network round trip.
	maxDryRunNodes = 10
	// dryRunOverageFactor: a live dry-run quote more than this multiple of
	// the plan's own FeeTotal estimate means the plan is stale enough to be
	// worth re-running rather than trusting.
	dryRunOverageFactor = 2
	// budgetSlackFactor is the default budget cap headroom over the plan's
	// own fee/buy total, absent an explicit --budget.
	budgetSlackFactor = 1.25
)

// craftStateDir mirrors cmd/overmind's --plan-state-dir default: the
// flock-guarded PlanRun state the Runner advances and plan_* reads/edits.
// It is a var (not a const) so tests can point it at a temp directory.
var craftStateDir = "data/overmind/craft-plans"

// dispatchArgs is the parsed form of `dispatch <plan.json> [--budget=N]
// [--mine=item1,item2] [--assembly=BASE] [--skip-verify]`.
type dispatchArgs struct {
	planPath   string
	budget     int // 0 = compute the default; -1 = unbounded (no budget cap)
	mineItems  []string
	assembly   string // "" = resolve from craftFleetPath
	skipVerify bool
}

// parseDispatchArgs parses dispatch's flags. Unknown flags are rejected
// rather than ignored, matching build's parseBuildArgs: a typo'd flag would
// otherwise silently fall back to a default the operator didn't ask for.
func parseDispatchArgs(args []string) (dispatchArgs, error) {
	const usage = "usage: dispatch <plan.json> [--budget=N] [--mine=item1,item2] [--assembly=BASE] [--skip-verify]\nbudget: N > 0 = cap, N = -1 = unbounded (no cap), omit = computed default"
	if len(args) == 0 {
		return dispatchArgs{}, fmt.Errorf("%s", usage)
	}
	da := dispatchArgs{planPath: args[0]}
	for _, a := range args[1:] {
		switch {
		case a == "--skip-verify":
			da.skipVerify = true
		case strings.HasPrefix(a, "--budget="):
			v := strings.TrimPrefix(a, "--budget=")
			n, err := strconv.Atoi(v)
			if err != nil {
				return dispatchArgs{}, fmt.Errorf("dispatch: --budget must be an integer, got %q", v)
			}
			if n == 0 {
				return dispatchArgs{}, fmt.Errorf("dispatch: --budget=0 is invalid; use --budget=-1 for unbounded (no cap) or omit for computed default")
			}
			if n < -1 {
				return dispatchArgs{}, fmt.Errorf("dispatch: --budget must be >= -1, got %d", n)
			}
			da.budget = n
		case strings.HasPrefix(a, "--mine="):
			for item := range strings.SplitSeq(strings.TrimPrefix(a, "--mine="), ",") {
				if item = strings.TrimSpace(item); item != "" {
					da.mineItems = append(da.mineItems, item)
				}
			}
		case strings.HasPrefix(a, "--assembly="):
			da.assembly = strings.TrimPrefix(a, "--assembly=")
		case strings.HasPrefix(a, "--"):
			return dispatchArgs{}, fmt.Errorf("dispatch: unknown flag %q (supported: --budget=N, --mine=item1,item2, --assembly=BASE, --skip-verify)", a)
		default:
			return dispatchArgs{}, fmt.Errorf("dispatch: unexpected argument %q", a)
		}
	}
	return da, nil
}

// sellerLookup abstracts finditem.Find so dispatchTransform stays pure and
// testable: production wires it to a closure over the live market
// collector/knowledge base, tests wire it to a fake with canned results.
type sellerLookup func(ctx context.Context, itemID string, qty int) ([]finditem.Result, error)

// dispatchTransform is the pure decode -> resolve-assembly -> leaf-tag ->
// budget -> queue-file pipeline behind the dispatch command. It performs no
// I/O of its own — the plan bytes, the seller lookup, and the clock are all
// supplied by the caller — so it is driven entirely from byte literals and a
// fake lookup in tests.
//
// It returns the QueueFile ready to write, plus the subset of craft nodes
// needing CraftDryRun verification (facility crafts only — a non-empty
// FacilityID; hand-crafts have none — capped to maxDryRunNodes, in plan
// order) for the caller's separate, impure verify pass.
func dispatchTransform(ctx context.Context, planJSON []byte, args dispatchArgs, assembly string, lookup sellerLookup, now time.Time) (*plans.QueueFile, []craftbrain.Node, error) {
	if assembly == "" {
		return nil, nil, fmt.Errorf("dispatch: no assembly base resolved (pass --assembly=BASE or add an entry to %s)", craftFleetPath)
	}

	var plan craftbrain.Plan
	if err := json.Unmarshal(planJSON, &plan); err != nil {
		return nil, nil, fmt.Errorf("dispatch: decode plan json: %w", err)
	}

	resolveAssembly(&plan, assembly)

	mineSet := make(map[string]bool, len(args.mineItems))
	for _, id := range args.mineItems {
		mineSet[id] = true
	}

	for i := range plan.Nodes {
		n := &plan.Nodes[i]
		if n.Kind != craftbrain.KindMine || mineSet[n.ItemID] {
			continue
		}
		results, err := lookup(ctx, n.ItemID, n.Qty)
		if err != nil {
			plan.Diagnostics = append(plan.Diagnostics,
				fmt.Sprintf("dispatch: seller lookup for %s failed: %v (left as mine)", n.ItemID, err))
			continue
		}
		if len(results) == 0 {
			continue // no seller found; leave as mine
		}
		best := results[0] // finditem.Find's own ranking: nearest, then cheapest
		n.Kind = craftbrain.KindBuy
		n.StationID = best.StationID
		n.FeeTotal = int(math.Ceil(best.BestPrice * float64(n.Qty)))
	}

	priceNativeBuys(ctx, &plan, lookup)

	budget := args.budget
	switch budget {
	case 0:
		// Compute default: ceiling of slack factor times total fees
		sum := 0
		for _, n := range plan.Nodes {
			sum += n.FeeTotal
		}
		budget = int(math.Ceil(budgetSlackFactor * float64(sum)))
	case -1:
		// Unbounded: no budget cap
		budget = 0
	}
	// else: positive budget is used as-is (explicit cap)

	qf := &plans.QueueFile{
		Manifest: plans.Manifest{
			PlanID:       dispatchPlanID(plan, now),
			BudgetCap:    budget,
			MineItems:    append([]string(nil), args.mineItems...),
			Assembly:     assembly,
			DispatchedAt: now.UTC().Format(time.RFC3339),
		},
		Plan: plan,
	}

	var verify []craftbrain.Node
	for _, n := range plan.Nodes {
		if n.Kind != craftbrain.KindCraft || n.FacilityID == "" {
			continue
		}
		verify = append(verify, n)
		if len(verify) >= maxDryRunNodes {
			break
		}
	}

	return qf, verify, nil
}

// priceNativeBuys stamps FeeTotal on planner-native KindBuy nodes that arrived
// unpriced (FeeTotal==0). Without a fee, a buy node's MAX_UNIT_PRICE is 0 (no
// ceiling) and it contributes nothing to the budget gate — so a native buy
// could drain a wallet uncontrolled. It looks up a seller (preferring one at
// the node's existing planner StationID so the site is kept when possible),
// stamps FeeTotal = ceil(ask * qty), and notes any item with no seller in the
// plan diagnostics rather than silently leaving it uncontrolled.
func priceNativeBuys(ctx context.Context, plan *craftbrain.Plan, lookup sellerLookup) {
	for i := range plan.Nodes {
		n := &plan.Nodes[i]
		if n.Kind != craftbrain.KindBuy || n.FeeTotal != 0 || n.Qty <= 0 {
			continue
		}
		results, err := lookup(ctx, n.ItemID, n.Qty)
		if err != nil {
			plan.Diagnostics = append(plan.Diagnostics,
				fmt.Sprintf("dispatch: seller lookup for native buy %s failed: %v (left unpriced, no budget ceiling)", n.ItemID, err))
			continue
		}
		if len(results) == 0 {
			plan.Diagnostics = append(plan.Diagnostics,
				fmt.Sprintf("dispatch: no seller found for native buy %s (qty %d) — left unpriced, no budget ceiling", n.ItemID, n.Qty))
			continue
		}
		best := results[0] // finditem's ranking: nearest, then cheapest
		for _, r := range results {
			if r.StationID == n.StationID { // prefer re-pricing at the node's existing station
				best = r
				break
			}
		}
		n.StationID = best.StationID
		n.FeeTotal = int(math.Ceil(best.BestPrice * float64(n.Qty)))
	}
}

// filterDryRunNodes splits craft-node dry-run candidates by whether the
// operator can actually quote them from where they sit. CraftDryRun with an
// explicit facility_id is a no_facility error unless the caller is docked at
// that facility's station (live-verified 2026-07-10), so only nodes whose
// StationID equals the operator's current docked station can be verified here;
// the rest are returned separately to be warned-and-skipped (their planner fee
// is trusted). An empty dockedStation (operator not docked) verifies nothing.
func filterDryRunNodes(candidates []craftbrain.Node, dockedStation string) (verify, skipped []craftbrain.Node) {
	for _, n := range candidates {
		if dockedStation != "" && n.StationID == dockedStation {
			verify = append(verify, n)
		} else {
			skipped = append(skipped, n)
		}
	}
	return verify, skipped
}

// resolveAssembly rewrites every craftbrain.DefaultCraftBase
// ("any_docked_station") sentinel — in StationID, FromBase, and ToBase — to
// the resolved assembly base. The sentinel exists because the planner has no
// fixed hand-craft site to name (see DefaultCraftBase's doc comment);
// dispatch is where the operator finally pins one down, in all three fields
// a node could carry it in.
func resolveAssembly(plan *craftbrain.Plan, assembly string) {
	for i := range plan.Nodes {
		n := &plan.Nodes[i]
		if n.StationID == craftbrain.DefaultCraftBase {
			n.StationID = assembly
		}
		if n.FromBase == craftbrain.DefaultCraftBase {
			n.FromBase = assembly
		}
		if n.ToBase == craftbrain.DefaultCraftBase {
			n.ToBase = assembly
		}
	}
}

// dispatchPlanID builds the on-disk plan id: <sanitized-target>-<UTC
// timestamp, YYYYMMDD-HHMMSS>. craftbrain.Engine.Plan always sets Plan.Target,
// but a hand-edited or otherwise malformed plan file might not, so this
// falls back to the first node's ItemID (topological order puts the target's
// own node(s) first — consumers before producers), then to the literal
// string "plan" as a last resort, so a dispatch never fails purely over a
// missing id.
func dispatchPlanID(plan craftbrain.Plan, now time.Time) string {
	target := plan.Target
	if target == "" && len(plan.Nodes) > 0 {
		target = plan.Nodes[0].ItemID
	}
	if target == "" {
		target = "plan"
	}
	return sanitizeID(target) + "-" + now.UTC().Format("20060102-150405")
}

// sanitizeID lowercases s and replaces every rune outside [a-z0-9-] with
// '-'. Plan ids are used as filenames and as task-id components elsewhere in
// the overmind, where ':' and '/' are reserved.
func sanitizeID(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return b.String()
}

// runDispatch implements: dispatch <plan.json> [--budget=N]
// [--mine=item1,item2] [--assembly=BASE] [--skip-verify]
//
// Reads a craftbrain.Plan JSON file (as produced by `build <target> --json`),
// runs it through dispatchTransform, optionally verifies facility craft
// nodes against a live CraftDryRun quote, and atomically drops the resulting
// QueueFile into craftQueueDir for the overmind Runner to pick up.
func runDispatch(client game.GameClient, ctx context.Context, args []string) error {
	da, err := parseDispatchArgs(args)
	if err != nil {
		return err
	}

	raw, err := os.ReadFile(da.planPath)
	if err != nil {
		return fmt.Errorf("dispatch: read plan %s: %w", da.planPath, err)
	}

	assembly := da.assembly
	if assembly == "" {
		fleet, ferr := supervisor.LoadFleet(craftFleetPath)
		if ferr != nil || len(fleet) == 0 {
			return fmt.Errorf("dispatch: --assembly not given and default fleet %s is unavailable: %v", craftFleetPath, ferr)
		}
		assembly = fleet[0].Station
	}

	lookup := func(ctx context.Context, itemID string, qty int) ([]finditem.Result, error) {
		if globalMarketCollector == nil {
			return nil, fmt.Errorf("market DB not available (run with --market-db-path)")
		}
		origin := ""
		if st := client.GetState(); st != nil {
			origin = st.System.ID
		}
		return finditem.Find(ctx, globalMarketCollector, globalKB, itemID, float64(qty), origin, finditem.DefaultLimit)
	}

	qf, verify, err := dispatchTransform(ctx, raw, da, assembly, lookup, time.Now())
	if err != nil {
		return err
	}

	if !da.skipVerify {
		// CraftDryRun with an explicit facility_id requires being docked at
		// that facility's station, so only nodes at the operator's current
		// docked station can be quoted from here; the rest are trusted at their
		// planner fee (verify them by dispatching from their own station, or
		// re-run build).
		dockedStation := ""
		if st := client.GetState(); st != nil {
			dockedStation = st.Player.DockedAtBase
		}
		toVerify, skipped := filterDryRunNodes(verify, dockedStation)
		if len(skipped) > 0 {
			fmt.Printf("dispatch: skipping live dry-run verify for %d craft node(s) whose facility isn't at the operator's docked station %q (CraftDryRun requires docking there) — trusting their planner fees\n",
				len(skipped), dockedStation)
		}
		for _, n := range toVerify {
			resp, err := client.CraftDryRun(ctx, n.RecipeID, n.Qty, n.FacilityID)
			if err != nil {
				return fmt.Errorf("dispatch: dry-run verify node %s (%s): %w", n.ID, n.RecipeID, err)
			}
			if resp.CreditsTotal > dryRunOverageFactor*n.FeeTotal {
				return fmt.Errorf("dispatch: aborted — node %s (%s) live dry-run cost %d exceeds 2x planned fee %d; re-run `build %s --json` and re-dispatch",
					n.ID, n.RecipeID, resp.CreditsTotal, n.FeeTotal, qf.Plan.Target)
			}
		}
	}

	if err := os.MkdirAll(craftQueueDir, 0o755); err != nil {
		return fmt.Errorf("dispatch: mkdir %s: %w", craftQueueDir, err)
	}
	data, err := json.MarshalIndent(qf, "", "  ")
	if err != nil {
		return fmt.Errorf("dispatch: marshal queue file: %w", err)
	}
	path := filepath.Join(craftQueueDir, qf.Manifest.PlanID+".json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("dispatch: write tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("dispatch: rename %s -> %s: %w", tmp, path, err)
	}

	fmt.Printf("dispatched plan %s: %d node(s), budget cap %d, assembly %s -> %s\n",
		qf.Manifest.PlanID, len(qf.Plan.Nodes), qf.Manifest.BudgetCap, qf.Manifest.Assembly, path)
	return nil
}

// planStatePath returns the on-disk path for a plan-run's state file, e.g.
// data/overmind/craft-plans/<plan-id>.state.json.
func planStatePath(planID string) string {
	return filepath.Join(craftStateDir, planID+".state.json")
}

// knownPlanIDs lists the plan ids with a state file in craftStateDir, sorted.
// Errors are swallowed to "" — used only to build a helpful unknown-id error
// message, where an empty list is itself informative.
func knownPlanIDs() []string {
	matches, err := filepath.Glob(filepath.Join(craftStateDir, "*.state.json"))
	if err != nil {
		return nil
	}
	ids := make([]string, 0, len(matches))
	for _, m := range matches {
		ids = append(ids, strings.TrimSuffix(filepath.Base(m), ".state.json"))
	}
	sort.Strings(ids)
	return ids
}

// unknownPlanErr builds the "clear error listing known ids" required of
// every plan_* mutator and plan_status <plan-id>.
func unknownPlanErr(planID string) error {
	ids := knownPlanIDs()
	if len(ids) == 0 {
		return fmt.Errorf("plan %q not found; no plans are currently dispatched", planID)
	}
	return fmt.Errorf("plan %q not found; known plans: %s", planID, strings.Join(ids, ", "))
}

// loadPlanOrErr loads one plan-run's state, translating a missing state file
// into unknownPlanErr rather than LoadRun's generic os.ReadFile error.
func loadPlanOrErr(planID string) (*plans.PlanRun, error) {
	path := planStatePath(planID)
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, unknownPlanErr(planID)
		}
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}
	return plans.LoadRun(path)
}

// mutatePlan implements every plan_* mutator's LoadRun -> mutate Control ->
// SaveRun contract. The flock inside LoadRun/SaveRun makes this safe against
// the overmind Runner's own concurrent Tick writes. mutate returning an
// error aborts before SaveRun (e.g. plan_retry's unknown-node-id check).
func mutatePlan(planID string, mutate func(pr *plans.PlanRun) error) error {
	pr, err := loadPlanOrErr(planID)
	if err != nil {
		return err
	}
	if err := mutate(pr); err != nil {
		return err
	}
	if err := plans.SaveRun(craftStateDir, pr); err != nil {
		return fmt.Errorf("save plan %s: %w", planID, err)
	}
	return nil
}

// runPlanStatus implements: plan_status [plan-id]
// With no plan-id, lists every dispatched plan's summary. With one, also
// lists every parked node's reason/detail.
func runPlanStatus(args []string) error {
	if len(args) == 0 {
		return printAllPlanStatus()
	}
	if len(args) > 1 {
		return fmt.Errorf("usage: plan_status [plan-id]")
	}
	pr, err := loadPlanOrErr(args[0])
	if err != nil {
		return err
	}
	printPlanDetail(pr)
	return nil
}

func printAllPlanStatus() error {
	runs, errs := plans.LoadAllRuns(craftStateDir)
	for _, e := range errs {
		fmt.Fprintf(os.Stderr, "plan_status: %v\n", e)
	}
	if len(runs) == 0 {
		fmt.Println("no dispatched plans")
		return nil
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].Manifest.PlanID < runs[j].Manifest.PlanID })
	for _, pr := range runs {
		printPlanSummary(pr)
	}
	return nil
}

func printPlanSummary(pr *plans.PlanRun) {
	pr.Recompute()
	counts := nodeStateCounts(pr)
	fmt.Printf("%s  status=%s  spent=%d/%d  waiting=%d dispatched=%d done=%d parked=%d\n",
		pr.Manifest.PlanID, pr.Status, pr.Spent, pr.Manifest.BudgetCap,
		counts[plans.NodeWaiting], counts[plans.NodeDispatched], counts[plans.NodeDone], counts[plans.NodeParked])
}

func printPlanDetail(pr *plans.PlanRun) {
	printPlanSummary(pr)
	for _, n := range pr.Nodes {
		if n.State != plans.NodeParked {
			continue
		}
		fmt.Printf("  parked: %s (%s/%s) reason=%s detail=%s\n", n.Node.ID, n.Node.Kind, n.Node.ItemID, n.Park, n.ParkDetail)
	}
}

func nodeStateCounts(pr *plans.PlanRun) map[plans.NodeState]int {
	counts := map[plans.NodeState]int{}
	for _, n := range pr.Nodes {
		counts[n.State]++
	}
	return counts
}

// runPlanPause implements: plan_pause <plan-id>
func runPlanPause(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: plan_pause <plan-id>")
	}
	planID := args[0]
	if err := mutatePlan(planID, func(pr *plans.PlanRun) error {
		pr.Control.Pause = true
		return nil
	}); err != nil {
		return err
	}
	fmt.Printf("plan %s paused\n", planID)
	return nil
}

// runPlanResume implements: plan_resume <plan-id> [--raise-cap=N]
//
// Always clears Control.Pause: this is a load-bearing contract with the
// overmind Runner. The Runner's applyControl deliberately consumes RaiseCap
// and RetryNodes without ever touching Pause — un-pausing is the CLI's job,
// not the Runner's — so raising the cap alone, without plan_resume, leaves
// the plan paused.
func runPlanResume(args []string) error {
	const usage = "usage: plan_resume <plan-id> [--raise-cap=N]  (this clears the pause; raise-cap alone won't)"
	if len(args) < 1 {
		return fmt.Errorf("%s", usage)
	}
	planID := args[0]
	raiseCap := 0
	for _, a := range args[1:] {
		v, ok := strings.CutPrefix(a, "--raise-cap=")
		if !ok {
			return fmt.Errorf("plan_resume: unknown flag %q\n%s", a, usage)
		}
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return fmt.Errorf("plan_resume: --raise-cap must be a positive integer, got %q", v)
		}
		raiseCap = n
	}
	if err := mutatePlan(planID, func(pr *plans.PlanRun) error {
		pr.Control.Pause = false
		if raiseCap > 0 {
			pr.Control.RaiseCap = raiseCap
		}
		return nil
	}); err != nil {
		return err
	}
	if raiseCap > 0 {
		fmt.Printf("plan %s resumed, cap raised to %d\n", planID, raiseCap)
	} else {
		fmt.Printf("plan %s resumed\n", planID)
	}
	return nil
}

// runPlanCancel implements: plan_cancel <plan-id>
// Terminal: Control.Cancel has no corresponding un-cancel.
func runPlanCancel(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: plan_cancel <plan-id>  (terminal: a cancelled plan cannot be un-cancelled)")
	}
	planID := args[0]
	if err := mutatePlan(planID, func(pr *plans.PlanRun) error {
		pr.Control.Cancel = true
		return nil
	}); err != nil {
		return err
	}
	fmt.Printf("plan %s cancelled\n", planID)
	return nil
}

// runPlanRetry implements: plan_retry <plan-id> <node-id>
// Appends node-id to Control.RetryNodes; the Runner resets that node's
// Retries and re-dispatches it. It does NOT unpause a paused plan — if the
// plan is paused, also run plan_resume.
func runPlanRetry(args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: plan_retry <plan-id> <node-id>  (if the plan is paused, also run plan_resume)")
	}
	planID, nodeID := args[0], args[1]
	if err := mutatePlan(planID, func(pr *plans.PlanRun) error {
		if pr.NodeByID(nodeID) == nil {
			ids := make([]string, 0, len(pr.Nodes))
			for _, n := range pr.Nodes {
				ids = append(ids, n.Node.ID)
			}
			return fmt.Errorf("plan_retry: node %q not found in plan %s; known nodes: %s", nodeID, planID, strings.Join(ids, ", "))
		}
		pr.Control.RetryNodes = append(pr.Control.RetryNodes, nodeID)
		return nil
	}); err != nil {
		return err
	}
	fmt.Printf("plan %s: node %s queued for retry\n", planID, nodeID)
	return nil
}
