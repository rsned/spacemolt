# Crafting v0.389.0 — Gap Analysis & Design Decisions (handoff)

Reference input for the implementation plan. Server guide: `wiki/crafting.md`.
Server API: `server_docs/openapi.json` (→ `openapi.20260618.json`, v0.389.0).

## What changed on the server (authoritative)
- `craft` is now an **async queued job**, not instant. It returns a single
  `type=ok` frame carrying the job body (`job_id`, venue, ETA, escrow…). There is
  **no terminal `action_result`**. Output lands later via `crafting_update` push.
- Inputs come from **station storage** (not ship cargo); outputs go to storage.
  `deliver_to` enum is **`storage` (default) | `faction`** — **`cargo` removed**.
- `quantity` = number of **output items** wanted (server rounds up to whole runs).
  The old per-tick skill-limited batch concept (`MaxCraftBatchSize`) is obsolete.
- New `craft` payload fields: `facility_id`, `preset` (`fast`|`cheap`|`workshop`),
  `dry_run` (bool → cost+time quote), `action=queue` (list jobs), `jobs=[…]` bulk (≤50).
- New top-level **`recycle`** command (same shapes as craft; no `preset`).
- `facility` gains actions: `owned`, `upgrade`, `job_add`, `job_list`,
  `job_cancel`, `job_reorder`, `set_output_price`, `set_access`, resale market
  (`list_for_sale`/`browse_for_sale`/`buy_listing`/`cancel_listing`).
- New push event **`crafting_update`** (per tick): `{tick:int, jobs:[{job_id,
  recipe, mode, venue, storage, deposited:[{item_id,item_name,quantity}],
  runs_done, runs_remaining, completed}]}`. Schema: `Notification_crafting_update`.
- `get_guide guide="crafting"` already works (enum includes `crafting`).

## CraftJobResponse — the four `craft`/`recycle` response shapes (from openapi)
1. **Single job queued** (required: action, job_id, recipe, mode, venue,
   venue_type, facility_id, runs, effective_time_per_run, est_completion_tick,
   escrowed, message; optional: produces[], external).
   `escrowed = {fee:int, labor:int, inputs:[{item_id,name,quantity}]}`.
2. **Queue listing** (`action=queue`; required: action, jobs[]). Each job:
   job_id, recipe, mode, runs_total, runs_done, runs_remaining (required);
   progress:number, eta_ticks:int, position:int, orderer, status, facility_id,
   external:bool, venue, produces[].
3. **Bulk results** (required: action, mode, results[], summary). result:
   {index, success(req), job_id, recipe, runs, venue, message, error, error_code};
   summary: {total, succeeded, failed}.
4. **Dry-run quote** (required: action, dry_run, recipe, mode, quantity, runs,
   venue, venue_type, facility_id, cost, credits_total, have_inputs, have_credits,
   effective_time_per_run, est_completion_tick, message; optional produces[],
   external). `cost` same shape as `escrowed`.

The implementer MUST re-read these field names from `server_docs/openapi.json`
(search schema `CraftJobResponse`, `Notification_crafting_update`) before coding
structs — do not trust this summary over the file.

## Current client state (file:line)
- `pkg/game/crafting.go`
  - `MaxCraftBatchSize(state)` :19 — 3 callers: :122 (single craft), :479
    (CraftItems loop), `crafting_loop.go:431`.
  - `CraftWithOptions` :121 — builds payload :127-133, still doc-says `cargo`
    valid; awaits via `Submit(...WithTimeout(SleepTick*3))` :144 (default
    terminator = `terminateOnAction`, waits for action_result that never comes).
  - `CraftItems` :385-545 — withdraw-from-storage → craft-to-cargo → deposit loop.
- `crafting_loop.go` :199-462 — per-batch cargo loop; would double-spend now.
- `pkg/game/serverapi/responses.go` — `CraftResponse` :1394 (old instant shape:
  Outputs/FromStorage/XP…), `CraftOutput`, `CraftSourceItem` nearby.
- `pkg/game/client.go` — push events handled in `handleResponse`; mirror
  `case protocol.TypeMiningYield` (~:1320, :2347, :4668). Callback hooks:
  `SetOnChatMessage` :422 / `onChatMessage` field :134 / fire site ~:2593;
  `SetOnStorageUpdate` :413. No `crafting_update` anywhere.
- `pkg/game/client_api_monitor.go` :105 maps `"craft"→CraftResponse{}`;
  `eventExpectedFields` :306-555 has no crafting_update.
- `internal/protocol/messages.go` — no `TypeCraftingUpdate`.
- `cmd/tools/play_as/main.go` — craft REPL case :5767-5789 (offers
  `--deliver_to=cargo`); `formatCraft` :3565-3657 (old shape); formatter dispatch
  registry ~:709; `formatFacility` switch :1502-1537 (`types`:1509,
  `owned`:1515→`formatFacilityOwned`:1543); facility help :7214; craft help :8243.
- `pkg/game/interface.go` — GameClient; `pkg/game/mcp_game_client_commands.go`
  mirror methods.
- **Mock blast radius**: `pkg/agent/runner_test.go:79` mockGameClient and
  `pkg/skills/client_dispatcher_test.go:438` mockGameClient must gain a stub for
  every new GameClient interface method. `go build` does NOT catch this — only
  `go test ./...` does. (See memory `feedback_gameclient_interface_mocks.md`.)
- **Pre-existing, unrelated**: `pkg/galaxy/graph_test.go` mockKB lacks
  `RecordPassengers` — fails at branch base too. Do NOT try to fix; note and skip.

## Design decisions (binding — the plan must follow these)
1. **Async terminator**: craft & recycle use `WithTerminator(terminateOnActionOrOK)`
   (exists, used widely) so the queued-job `ok` frame is treated as terminal.
   Keep `WithTimeout(SleepTick*3)` for the single/dry-run/bulk submit.
2. **Struct strategy**: add NEW typed structs (`CraftJobQueued`, `CraftQueueListing`
   + `CraftJobEntry`, `CraftBulkResponse` + `CraftBulkResult`, `CraftDryRunResponse`,
   `EscrowCost` + `EscrowInput`, `CraftingUpdateEvent` + `CraftingUpdateJob`).
   `RecycleResponse` reuses the same shapes. Keep the old `CraftResponse` as a
   compile shim until Task 10 deletes its last use; `client_api_monitor.go:105`
   may keep pointing at a kept type until then.
3. **`deliver_to`**: remove `cargo` everywhere as a valid value (client docstring,
   play_as parser/help). Default empty → server uses `storage`.
4. **crafting_update**: add `TypeCraftingUpdate="crafting_update"`; handle in
   `handleResponse` mirroring `TypeMiningYield` (log + optional state); add an
   `OnCraftingUpdate(func(CraftingUpdateEvent))` callback mirroring
   `SetOnChatMessage` (field + setter + fire site). Add to `eventExpectedFields`.
5. **MaxCraftBatchSize**: stop using it in the single-craft path (Task 3); remove
   the function and its remaining two callers when rewriting `CraftItems` /
   `CraftingLoop` (Task 10). Don't delete the function while callers remain.
6. **Agent-loop rewrite (Task 10)**: replace withdraw→craft-to-cargo→deposit with
   queue-once-then-monitor (subscribe to `crafting_update` / poll `craft action=queue`).
   Never re-issue an identical craft to "make progress" — that double-spends.
7. Every task ends with `go build ./...` AND `go test ./...` (the latter catches
   mock breaks). New code must pass `golangci-lint` with no new findings.
