# Crafting Brain B — Executor: design

Date: 2026-07-10
Status: approved design, pre-plan
Predecessors: A1 facility catalog (2026-07-09-facility-catalog-design.md),
A2 planner (2026-07-09-crafting-brain-a2-planner-design.md)

## Goal

Take a reviewed `pkg/craftbrain` plan (a JSON step-DAG of mine/buy/haul/craft
nodes) and execute it across a fleet of worker agents: dependency-ordered
dispatch, live progress tracking, budget enforcement, and honest parking of
work that needs the operator. A2 plans; B does.

## Decisions (made with the operator)

| Decision | Choice |
|---|---|
| Workforce | New dedicated craft fleet: **craftsman-2..10** (craftsman-1 excluded — it is the operator's interactive play_as agent) |
| Plan handoff | Queue directory + CLI control (rescue-queue file pattern), not a one-shot runner and not inline in play_as |
| Spend control | Per-plan budget cap, default = estimated fees+buys × 1.25, overridable at dispatch; craftsmen pay from their own wallets |
| Mine-vs-buy leaves | Default **buy** when a seller exists, fall back to mine; `--mine=item1,item2` overrides per dispatch |
| Holder-owned stock | Marketbot cooperation for overmind-managed resident holders; `needs_operator` parking for everyone else |
| Architecture | Plan store inside the overmind (approach 1) — reuse Assign/Event socket, tasks.Store, supervision; rejected: standalone executor daemon (duplicates the supervisor), compile-to-tasks.yaml (no dep gating, restart per plan) |

## Architecture

```
play_as build X --json > plan.json            (A2, exists)
play_as dispatch plan.json [--budget N] [--mine=a,b]
        │  validates (incl. craft --dry_run), tags leaves, sets budget,
        │  assigns plan id, writes one file
        ▼
data/overmind/craft-queue/<plan-id>.json
        │ polled (same cadence as pollRescues)
craft overmind = existing bin/overmind + new --plan-queue flag
  ├─ pkg/overmind/plans  PlanStore: node states, DAG gating, budget ledger
  │     releases READY nodes into tasks.Store at runtime (new runtime-Add)
  ├─ existing Assign/Event unix-socket protocol → craftsman workers
  │     node = one task; 4 param-substituted .smolt scripts → verbs
  │     craft / deliver / buy (new) + mine (exists)
  ├─ handoff-queue.json ◄── marketbot residents (standing-pass poll)
  └─ writes data/overmind/craft-plans/<plan-id>.state.json on every change
        │
cmd/tools/craft-dashboard (:8091) — mermaid DAG + progress table
```

New code: `pkg/overmind/plans`, 3 worker verbs + 1 handoff verb, a
`dispatch`/`plan_*` play_as command family, `craft-fleet.yaml` + `craftsman`
role, `cmd/tools/craft-dashboard`. Everything else (supervision, restarts,
reassignment, drain, heartbeats, status files) is reused as-is.

## Plan lifecycle

`dispatch` runs inside play_as (the KB is already open there). It:

1. Re-validates the plan against current data; uses server-side
   `craft --dry_run` on facility craft nodes to confirm site, fee, and
   duration against server truth (catches stale catalog data before money
   moves).
2. Applies mine/buy tags to raw leaves (default buy when a seller exists;
   `--mine=` list flips named items; no seller → the leaf stays a mine
   node).
3. Computes `budget_cap` (see Budget) and writes
   `craft-queue/<plan-id>.json` — plan JSON + dispatch manifest (tags, cap,
   dispatched_at, operator notes).

The craft overmind polls the queue dir, moves accepted plans to
`craft-plans/`, and owns them from there. On boot it reloads
`craft-plans/*.state.json` and resumes — plans survive overmind restarts and
the daily server restart.

Multiple concurrent plans are supported; task ids are `<plan-id>/<node-id>`.

**CLI control** (play_as commands mutating the state file under flock, honored
on the overmind's next pass — the operator-editable-file philosophy of the
rescue queue):

- `plan_status [id]` — summary or per-plan detail (incl. parked-node
  instructions)
- `plan_pause <id>` / `plan_resume <id> [--raise-cap=N]`
- `plan_cancel <id>` — running nodes finish their current verb, nothing new
  dispatches, plan archives as cancelled
- `plan_retry <id> <node>` — reset a parked(failed) node for another attempt

## DAG scheduling (pkg/overmind/plans)

Node states: `waiting → ready → dispatched → running → done | failed |
parked(reason)`. A node is `ready` when all `depends_on` are `done`.
Ready nodes are released into `tasks.Store` (extended with a runtime Add;
today it is boot-static YAML only) and flow through the existing
`AssignPending` matcher with `RoleRequired: craftsman`.

**A2 contract facts, honored explicitly:**

- **`Node.DependsOn` may be cyclic.** The scheduler never assumes a DAG: at
  plan load it runs its own cycle detection; nodes on a cycle park as
  `parked(cycle)` with the diagnostic attached — never scheduled, never able
  to wedge the ready-loop.
- **BLOCKED nodes are valid input.** They load as `parked(blocked)`;
  everything not downstream executes. A plan finishing with parked nodes
  completes as `partial` — a successful run of an incomplete plan.
- **`unknown_route` hauls have no Jumps.** Scheduled normally; couriers use
  autopilot. Missing jump counts gate nothing.

Progress is quantity-aware: nodes accumulate `done_qty` from task events, so
the dashboard can say `17,511 / 34,000 steel_plate`, not just "node running".

## Recipient pinning and synthetic transport (spec amendment, 2026-07-10)

Two facts discovered while planning:

1. **Storage is siloed per agent even inside the craft fleet.** A courier
   that deposits materials at station S leaves them in the *courier's*
   storage; a different craftsman running the craft node there cannot
   withdraw them.
2. **A2 plans have no haul nodes between craft nodes.** A craft at
   factory_belt feeding a craft at confederacy_central_command assumes
   transport that no node represents.

Resolution:

- **Pin craft nodes to agents at plan-accept.** The plan store assigns each
  craft node to a specific craftsman (load-balanced round-robin over the
  roster) before any dispatch. Task pinning already exists
  (`tasks.Task.AgentID`, honored by `pickWorker`).
- **Every node feeding a craft node carries `$RECIPIENT$`** = the pinned
  agent of its consumer. deliver/buy/mine verbs end with `send_gift` to the
  recipient at the consumer's station (goods land in the recipient's
  storage there) instead of deposit-and-leave. When the executing agent IS
  the recipient, gift degrades to a plain storage deposit. When a haul's
  `from_base == to_base`, the whole node collapses to a same-station gift —
  no flight (common: the live smoke showed most stock already sits at the
  assembly hub).
- **Synthetic transport nodes:** for each dependency edge between craft
  nodes at different stations, plan-accept inserts a synthetic deliver node
  (id `xfer-N`, flagged `synthetic`) pinned to the producer's agent, so the
  DAG on the dashboard shows the real work.
- **`any_docked_station` resolves at dispatch** to the plan's assembly base
  (`--assembly=<base_id>`, default: the first craft-fleet roster entry's
  station), so every node has a concrete station before the overmind sees
  it.
- Task 0 addition: the craft command's documented `deliver_to` job param
  may make the server deliver craft output cross-station; if so, synthetic
  transport nodes can be skipped where it applies. Verify live.

## Worker verbs

Nodes map to tasks via four tiny shared `.smolt` scripts using existing
`$PARAM$` substitution — `runTask` and the assign path need **zero changes**;
only `WorkerDispatch` grows.

- **`craft`** — `craft $RECIPE$ $NUM_OUTPUTS$ $STATION$ $FACILITY$`.
  Travel/dock at station; verify inputs on hand (cargo + own storage,
  withdrawing as needed); compute `runs = ceil(NUM_OUTPUTS /
  output_per_run)` — **the param is desired output units, never runs**
  (units-vs-runs is a known past bug class; keep the whole pipeline in
  output units). Dry-run first: actual cost must fit the remaining budget
  (see Budget). Craft in batches respecting skill-based
  `MaxCraftBatchSize`; deposit outputs to storage at the station; report
  per-batch progress via Events.
- **`deliver`** — `deliver $ITEM$ $QTY$ $FROM$ $TO$`. The directed haul the
  arbitrage `haul` verb never was: at FROM, acquire the goods (withdraw own
  storage — which is also where marketbot handoffs land, see Handoff);
  autopilot to TO; deposit. Cargo-capacity aware: loops trips when QTY
  exceeds the hold.
- **`buy`** — `buy $ITEM$ $QTY$ $STATION$ $MAX_UNIT_PRICE$`. Dock, buy up to
  the per-unit ceiling (from the plan estimate — one thin order book must
  not drain a wallet inside a single node), deposit to storage.
- **`mine`** — exists; reused with a quantity target.

**Shared retry invariant:** every verb begins by recomputing remaining need
from observable state — outputs on hand for craft, qty at destination for
deliver/buy. A node re-run after a worker death converges instead of
doubling. This is the one behavior all four verbs must implement.

## Marketbot handoff (holder-owned stock)

New `data/overmind/handoff-queue.json`, exact rescue-queue mechanics (flock
sidecar, atomic rename, `pending → done | failed`, operator-editable).

When the plan store schedules a `deliver` whose holder is an
overmind-managed resident marketbot, it first enqueues a handoff record:
holder, station, item, qty, recipient (the courier craftsman). Marketbot
residents poll the queue each standing pass. Because `send_gift` requires
only that the **sender** is docked, has no fee, and leaves the goods at the
sender's station (landing in the recipient's storage there), the marketbot
executes **immediately** — withdraw from storage, gift to the courier — no
courier-arrival synchronization at all. The courier later withdraws from its
own storage at that station whenever it arrives.

- v1 transfer: withdraw→gift loops in batches bounded by the marketbot's
  cargo hold. **Upgrade note:** a server patch adding
  `send_gift --source=storage` is expected; when it lands, the loop
  collapses to a single transfer (verb change only, no design change).
- Holders that are NOT overmind-managed (LLM agents: explorers, pirates,
  engineers…) → node parks as `parked(needs_operator)` carrying exact
  instructions (holder, base, item, qty, destination), surfaced in
  `plan_status` and red on the dashboard. The operator plays the holder via
  play_as, moves the stock, then `plan_retry`s the node (whose recompute
  sees the goods and completes).
- v2 option (deferred): if the craft fleet and marketbots shared a faction
  with storage, deposit-to-faction-storage / withdraw replaces gifting
  entirely. Fleets are factionless today, so v1 stays on `send_gift`.

## Budget

Dispatch manifest carries `budget_cap` (default: plan estimated fees + buy
costs × 1.25; `--budget=N` overrides). The plan store keeps a `spent` ledger
from task events carrying **actual** amounts (facility fees, buy fills).
Gate at dispatch time per spending node: `spent + node_cost > cap` → node
parks as `parked(over_budget)`, the plan pauses, the dashboard goes red.
`plan_resume --raise-cap=N` continues. For craft nodes `node_cost` comes from
the server-side `craft --dry_run` (authoritative), not the catalog estimate;
for buy nodes from qty × max unit price.

## Craft fleet

- `data/overmind/craft-fleet.yaml`: craftsman-2..10.
- New `craftsman` role in roles.yaml: idle = docked at home station with
  resident-style market/kb capture (idle craftsmen still contribute data);
  standard schedule entries.
- Launch: `bin/overmind --fleet data/overmind/craft-fleet.yaml --socket
  data/overmind/craft.sock --plan-queue data/overmind/craft-queue
  --status-file data/overmind/craft-status.json --history-file
  data/overmind/craft-history.jsonl --stagger 10s` (login-rate rules as
  usual).
- **Operator TODO before first run:** fund craftsman wallets (fees + buys
  are paid by the executing agent), fit cargo capacity; mining fits only
  where `--mine` will be used.

## Monitor: craft-dashboard (:8091)

New `cmd/tools/craft-dashboard`, same shape as haul-/faction-/market-
dashboards: re-reads `craft-plans/*.state.json` per request; no sockets.
Two views per plan:

- **DAG view** — mermaid flowchart (vendored `mermaid.min.js`, client-side
  render) regenerated from state each refresh. Color language: `running`
  bold/highlighted, annotated with the assigned agent; `done` faded grey;
  `waiting`/`ready` neutral; every `parked(*)` **red** with the reason as
  node subtitle; edges into running nodes highlighted. Fallback beyond ~300
  nodes: collapse done nodes into per-item summary nodes.
- **Progress table** — per item `done/total` quantities
  (`17,511 / 34,000 steel_plate`); spent vs budget cap; counts by node kind
  and state; the parked list with operator instructions; ETA from dry-run
  timings.

Auto-refresh + refresh-now button, as overmind-status does.

## Failure handling

- **Node failure** (`task_failed`): bounded retry — 2 automatic retries (the
  recompute invariant makes retries convergent), then `parked(failed)`.
  Failures never cascade silently: downstream nodes stay `waiting` until
  the operator acts (`plan_retry` / `plan_cancel`).
- **Worker death / quarantine mid-node:** existing store reconciliation
  reverts the task to pending; the plan store re-dispatches (counts as one
  retry).
- **Daily server restart:** workers reconnect via existing hardening;
  verbs resume via the recompute invariant.
- **Stale plan vs reality:** a verb finding preconditions gone (storage
  emptied, facility vanished, recipe changed) fails with a distinct
  `replan` reason so the parked message says *re-plan*, not *retry*.
- **Cancelled plans:** running verbs finish their current step; nothing new
  dispatches; state file archives as cancelled.

## Testing

- `pkg/overmind/plans`: golden plan-in/schedule-out fixtures like A2's,
  including the three contract cases (cyclic DependsOn, BLOCKED input,
  unknown_route haul) and budget exhaustion; pure logic, no I/O.
- Verbs: fake-GameClient unit tests; every new GameClient method ripples
  into pkg/agent + pkg/skills mocks — run full `go test ./...`, not just the
  package (known gotcha).
- Handoff queue: reuse rescue-queue test patterns (flock, transition CAS,
  corrupt-file behavior).
- Dashboard: fixture-state → HTML/mermaid-text golden tests.
- Live smoke before anything big: a small real plan (`air_recycler x2`)
  through craftsman-2..3, watched on the dashboard.

## Verify live before building (refuel --target lesson)

1. `send_gift` for **items**: exact syntax, batch limits, where goods land
   for the recipient (expected: recipient storage at sender's station), and
   whether recipient acceptance is needed.
2. Craft-at-public-facility: exact command shape for targeting a specific
   facility instance (A2 sites by instance for fee optimization) and what
   `--dry_run` reports for foreign-faction facilities.
3. `send_gift --source=storage` availability (expected server patch).
4. `craft` `deliver_to` job param semantics: does the server deliver craft
   output to another station/player? If yes, synthetic transport nodes can
   be skipped where it applies.

## Deferred (v2+)

- Faction-storage handoff (needs fleets in a shared faction).
- Auto re-plan of `parked(blocked)` nodes as facility coverage grows
  (hourly sweeps keep expanding the catalog).
- Cooperation with LLM-subsystem holders.
- Dashboard live-push (websocket) instead of refresh polling.
- Cross-fleet dispatch (e.g. offering big hauls to the haul fleet).

## Rollout

Operator runbook, in order. Commands are copy-pasteable from the repo root
unless noted.

### 1. Rebuild binaries

Nothing below runs against a stale binary. Rebuild all three; binaries live
in `bin/`, never the repo root:

```bash
go build -o bin/overmind ./cmd/overmind
go build -o bin/worker ./cmd/worker
go build -o bin/craft-dashboard ./cmd/tools/craft-dashboard
```

### 2. Redeploy the mb fleet (required for the handoff hook)

The marketbot fleet's `bin/worker` predates this branch and cannot run
`HandoffPass` until it's relaunched on the new binary. Drain, stop, relaunch:

```bash
kill -USR1 <mb-overmind-pid>     # graceful drain — see project_overmind_graceful_drain
# wait for drain quiescence, then:
kill -TERM <mb-overmind-pid>
```

The exact historical relaunch line isn't in shell history — reconstruct it
from `data/overmind/*-overmind.log` (the process logs its own flags on
startup) or from `reference_overmind_launch_commands` in ops memory. Relaunch
with the *rebuilt* `bin/overmind`.

**Rollout fact:** `--handoff-queue` defaults to
`data/overmind/handoff-queue.json` and is forwarded to *every* spawned
worker whenever it's non-empty (see `cmd/overmind/main.go`) — there is no
opt-out flag. That means any fleet restarted on the new binary, mb or
otherwise, gets handoff passes enabled by default. This is intended: the
holders that matter live in the mb fleet (the only fleet with resident
stock), and the pass is inert everywhere else — handoff records are
holder-addressed, so a worker that's never named in a record does nothing,
and a missing/empty queue file is a safe no-op. No special handling is
needed for the haul/shuttle/assist/rescue fleets picking up the new binary
on their own next restart.

### 3. First launch of the craft fleet

One-time setup, in order:

1. **Confirm stations.** `data/overmind/craft-fleet.yaml` marks its
   craftsman-2..10 station assignments **PROVISIONAL** — last-seen from the
   2026-07-02 KB sweep, now 8 days stale. Verify each still resolves to a
   real station (or `play_as` a live check) before funding wallets there.
2. **Fund wallets.** Craftsmen pay their own fees and buy costs — no
   funding, no dispatch. Fit cargo capacity on each; add mining fits only
   for craftsmen whose plans will use `--mine`.
3. **Launch with `--plan-queue` and `--stagger 10s`:**

   ```bash
   bin/overmind --fleet data/overmind/craft-fleet.yaml \
     --socket data/overmind/craft.sock \
     --plan-queue data/overmind/craft-queue \
     --plan-state-dir data/overmind/craft-plans \
     --status-file data/overmind/craft-status.json \
     --history-file data/overmind/craft-history.jsonl \
     --holders-roster data/overmind/mb-fleet.yaml \
     --stagger 10s
   ```

   Fresh logins are unmetered (only reconnects are rate-limited — see
   `reference_login_vs_reconnect_gating`), but keep `--stagger` conservative
   anyway; this is the fleet's first-ever boot.
4. **`craftsman-1` is excluded.** It's the operator's own interactive
   `play_as` session, not a fleet member — never add it to
   `craft-fleet.yaml`.

This is also the overmind that runs plan execution — the *only* one that
needs `--plan-queue`. It additionally requires `--holders-roster` (default
`data/overmind/mb-fleet.yaml`; **startup-fatal if unreadable** — verify the
path before launch, not after), and it `MkdirAll`s both
`--plan-queue`/`--plan-state-dir` itself, so those directories don't need to
pre-exist.

### 4. Live smoke test

```bash
play_as build air_recycler 2 --json > /tmp/plan.json
play_as dispatch /tmp/plan.json --budget 5000
```

Then watch `bin/craft-dashboard` (`:8091`, reads
`data/overmind/craft-plans/*.state.json`) until the plan completes or parks.
Verify, in order:

1. **Gift handoffs land.** Any `deliver` node sourced from an mb-fleet
   holder should show its handoff record transition `pending → done` and
   the courier's storage should actually hold the goods — this is
   `HandoffPass` fulfilling a real record end-to-end, unproven live before
   this smoke.
2. **Final goods reach the assembly base.** The plan's terminal craft
   output should land in storage at the assembly station
   (`--assembly=<base_id>` at dispatch, default: the first craft-fleet
   roster entry's station).

Additional points worth eyeballing while the plan runs, since none of these
were provable outside a live server:

- **`deliver`'s `GetCargo` round-trip vs tick pacing** — confirm the verb's
  read-after-write of cargo state isn't racing the server's own tick
  cadence and mis-reporting progress.
- **`craft`'s queue-absence-means-complete heuristic and its 30-tick
  timeout margin** — both are inferred behavior, not server-documented;
  watch at least one craft node finish cleanly and one that's slow enough
  to approach the timeout.
- **The dashboard renders in a real browser.** Its mermaid output was only
  string-tested (golden HTML/text fixtures); load `:8091` in an actual
  browser and confirm the DAG paints, colors, and refreshes.

### CLI control quick reference

- `plan_resume` is the **only** unpause. Neither `plan_retry` nor raising
  the budget cap alone resumes a paused plan — a budget breach always
  requires an explicit `plan_resume [--raise-cap=N]`.
- `plan_cancel` is terminal — there is no un-cancel; dispatch a new plan.
- `--budget`: `N > 0` sets the cap, `-1` means unbounded (no cap), `0` is
  rejected outright (use `-1` for "no cap", not `0`).

### Known issues

- **Pre-commit hook timeout vs `pkg/worker` runtime.** `scripts/setup-pre-commit.sh`
  gives most packages a 120s `-race` budget; `pkg/worker`'s full `-race` suite
  now runs ~200s. The tracked script's per-package table now grants `pkg/worker`
  300s alongside `pkg/game`/`pkg/knowledge` — but a hook already installed into
  `.git/hooks/pre-commit` from an older copy of the script still carries 120s
  until it is re-installed (`./scripts/setup-pre-commit.sh`). Until then, commits
  touching `pkg/worker` still need `--no-verify` (the worker suite is verified
  green out-of-band).
- **Two pre-existing red tests**, neither caused by this branch:
  `pkg/game TestServerCommandsCoveredByClient` (server drift — a new
  `espionage` command the client doesn't yet cover; skips locally if
  `data/game-api/latest/get_commands.json` isn't present, fails if it is)
  and `pkg/worker TestSeededCommandsAreDispatchable` (roles.yaml's
  `shuttle`/`assist` idle scripts resolve to no `<name>.smolt` file).
- **Stranded-cargo edge in `HandoffPass`.** If a holder's withdraw succeeds
  but the follow-up `send_gift` fails, the withdrawn stock is stranded in
  the holder's cargo (not storage) until the next pass retries. The pass
  never *over*-gifts — failure is always short, never double-sent — but the
  stranded state isn't yet surfaced anywhere; a schema follow-up to track
  it explicitly is open.

### Deviations

Honest gaps in the executor as shipped, so nobody reads more precision into
the state file than is there:

- **`DoneQty`/`Spent` are planner ESTIMATES, not measured actuals.** Task
  events (`task_done`) carry no per-node quantity or credits payload yet, so on
  completion the runner stamps `DoneQty = node.Qty` and `Spent += node.FeeTotal`
  from the plan, not from what the worker actually mined/bought/paid. The
  dashboard's `17,511 / 34,000` therefore reads plan intent at node granularity,
  not live server truth. Wiring real amounts through the task-event channel is
  the post-merge follow-up.
- **Progress is per-node binary.** A node is `waiting`→`dispatched`→`done`; its
  `DoneQty` jumps 0→full at completion. Mid-node partials (a haul on its third
  cargo-sized trip, a craft on batch 4 of 9) are invisible to the runner between
  ticks — the verb reports them to `d.Out`, not into the plan state.
- **A crash between task-remove and state-save can double-count one node's fee.**
  `syncTasks` removes the done task from the store and rolls its fee into
  `pr.Spent` in the same tick, then `SaveRun` persists. A process death after the
  store removal (which isn't itself persisted transactionally with the state
  file) but before/around `SaveRun` can, on reload, re-observe the node as still
  dispatched and re-collect its fee. The direction is deliberately conservative
  (over-count spend, never under-count), so the budget gate errs toward parking,
  never toward overspending. A single atomic task-outcome+state commit is the
  post-merge follow-up.
