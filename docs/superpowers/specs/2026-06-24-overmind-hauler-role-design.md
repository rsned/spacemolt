# Overmind Hauler Role — Design

**Date:** 2026-06-24
**Status:** Approved (design), pending implementation plan
**Phase:** Overmind Phase 2b mobile roles (hauler) — and **Market Intelligence System Phase 5** ("Agent Integration")
**Related:**
- `docs/superpowers/specs/2026-06-19-market-intelligence-system-design.md` (§Implementation Phases → Phase 5; §Claiming API)
- `docs/superpowers/specs/2026-06-24-arbitrage-scanner-design.md` (Phase 4 producer, merged via PR #127 → `origin/main` @ `c0840bb`)
- `docs/superpowers/specs/2026-06-23-overmind-explorer-role-design.md` (the standing-role pattern this mirrors)
- `pkg/worker` (worker runtime), `pkg/market` (arbitrage reads/atoms), `pkg/navigation` (jump graph)

## Goal

Give the overmind fleet a **hauler** role that autonomously consumes the arbitrage
opportunities produced by the Phase 4 scanner: pick the best reachable spread, claim
it, move the goods buy-station → sell-station, and report completion. This closes the
market-intelligence loop **detect → haul → profit** and is exactly Phase 5 of the
Market Intelligence System ("agents query `arbitrage_opportunities`, evaluate and
claim, execute trades and report results").

Haulers exist to move goods for the arbitrage work specifically — they are the
consumer counterpart to the `marketbot_*` resident fleet (the real station agents that
capture market data hourly via `update_market`).

## Scope

**In scope:**
- `pkg/worker/haul.go`: `SelectHaulOpportunity` (pure chooser) + `Haul` (one-step run engine), mirroring `explore.go`'s `NextExploreTarget` + `Explore`.
- `WorkerDispatch`: add `haul` to the `Supports` allowlist + a `case "haul"` dispatch arm. Buy/sell/autopilot happen **inside** the engine (direct client calls), not as dispatch commands.
- Config: `data/scripts/haul.smolt` (gitignore-allowlisted), `hauler` role in `data/overmind/roles.yaml`, hauler agents added to `data/overmind/fleet.yaml`.
- TDD: table tests for selection, fake-driven tests for the run engine, drift-guard coverage.

**Explicitly out of scope (deferred):**
- **Any `pkg/market` change.** The hauler uses only the atoms shipped in PR #127:
  `GetOpportunities`, `ClaimOpportunity`, `CompleteOpportunity`. No new reads, no
  schema change, no migration.
- **Stuck-claim recovery** — a failed or dead-mid-run hauler leaves its row `claimed`.
  This is harmless cruft (see Recovery below) and is deferred to the market team as a
  **Phase 5b** item (orphaned-claim sweeper + a `failed` status migration — both
  already gestured at by the system design's "Claiming API" and the Phase 4 design's
  note that "adding `failed` is a later migration" and that `idx_arbitrage_status`
  "supports a later background sweeper").
- **Logistics realism in selection** — `gross_profit` from the scanner is a gross
  spread with `fuel_cost=0`, `travel_ticks=0` (Phase 4b deferral). Selection ranks on
  gross profit and a jump-distance tiebreak only; it does not net out fuel.
- **Ship module management** (buy/fit a larger cargo hold) — separate cross-role
  prerequisite, income-gated, tracked elsewhere.
- **Salvager role** — the next mobile role after this one.

## Background: what's already in place

No new plumbing is required to read opportunities or trade:

- `WorkerDispatch.Market *market.Collector` is **already wired** (the `--market-db-path`
  flag in `cmd/worker`, opened best-effort) because residents already run
  `update_market`. The hauler engine reads opportunities through the same handle.
- The game client already exposes `Buy(ctx, itemID, qty)`, `Sell(ctx, itemID, qty)`,
  and `SellAllBulk`. `State.GetCredits()`, `Ship.CargoUsed`, `Ship.CargoCapacity`
  provide the budget for sizing a buy.
- `Autopilot(ctx, AutopilotDeps{Client, Out, OnWaypoint}, targetSystem, targetPOI)` is
  reused verbatim for both legs of the run.
- The standing-role runtime (`RunStanding`, idle behavior re-invoked each pass, all
  game work serialized on one `ExecMu`) already exists from Plan B. The hauler is a
  **standing role** like `explorer` — no new runtime.

## Architecture

```
hauler role (roles.yaml: idle: haul)
        │  RunStanding idle pass
        ▼
   case "haul"  ──►  Haul(ctx, HaulDeps{Client, KB, Market, Out})
                          │
        ┌─────────────────┼───────────────────────────────┐
        ▼                 ▼                                ▼
 GetOpportunities   SelectHaulOpportunity            Autopilot ×2
 ("available", N)   (pure: opps + currentSys         (buy leg, sell leg)
   [Market]          + KB jump graph)                  [reused]
                          │
                  ClaimOpportunity ──► Buy ──► Sell ──► CompleteOpportunity
                          [Market]    [Client]        [Market]
```

`SelectHaulOpportunity` is a **pure function** (no I/O) so it is exhaustively table-
testable, exactly like `NextExploreTarget`. `Haul` is the impure engine that wires the
client, KB, and market collector around it.

## Component 1 — `SelectHaulOpportunity` (selection)

**Signature (indicative):**
```go
func SelectHaulOpportunity(
    opps []market.ArbitrageOpportunity, // status == "available"
    currentSystemID string,
    nameToID map[string]string,         // system display-name → system id
    graph navigation.JumpGraph,
) (chosen market.ArbitrageOpportunity, ok bool)
```

**System-ID resolution (this is what keeps `pkg/market` untouched).**
`ArbitrageOpportunity` carries `FromSystemName` / `ToSystemName` (display names joined
on read) but **no system IDs**, while the jump graph and `currentSystemID` key on
lowercase system **IDs**. The caller builds `nameToID` from `kb.GetSystems()` (each
`System` has `ID` and `Name`) and passes it in. An opportunity whose buy-system name
does not resolve to a known ID, or whose buy-system is unreachable
(`BFSJumps` distance `>= navigation.RouteInf`), is **skipped** — the hauler cannot
route to it.

**Ranking (profit-dominant, proximity/chaining as tiebreak):**
1. Primary: `gross_profit` **descending**.
2. Among opportunities within **10%** of the current best `gross_profit` (the
   "near-tie band"), break ties by, in order:
   a. fewer jumps from `currentSystemID` to the buy-system (reposition cost),
   b. **chaining bonus** — the sell-system is at or adjacent (≤ 1 jump) to *another*
      available opportunity's buy-system, so the next run starts hot,
   c. opportunity `id` (deterministic).
3. `ok == false` when no opportunity is reachable/resolvable — the hauler idles.

Jump distances come from a single `navigation.BFSJumps(graph, currentSystemID, nodes)`
over the candidate buy/sell systems, matching `NextExploreTarget`.

## Component 2 — `Haul` (run engine)

**Signature (indicative):**
```go
type HaulDeps struct {
    Client game.GameClient
    KB     knowledge.Base
    Market *market.Collector
    Out    io.Writer // nil -> io.Discard
    AgentID string    // claim owner
    PoolLimit int     // GetOpportunities limit; 0 -> default (e.g. 50)
}

func Haul(ctx context.Context, deps HaulDeps) error
```

**One idle step (all under the single `ExecMu` held by `RunStanding`):**

1. Resolve current system from `deps.Client.GetState()`. Use `state.System.ID` (the
   id, not the display name — the same id/name lesson that bit `Explore`). Nil state or
   empty system id → log + return nil (idle).
2. **Producer: scan-when-empty.** `GetOpportunities("available", PoolLimit)`. If the
   pool is empty, run one `ScanArbitrage` (haulers already hold the `Collector`), then
   re-query once. `ScanArbitrage` is idempotent under the write lock (expire-all-
   available + reinsert), so an occasional redundant scan from concurrent haulers is
   harmless. Still empty → log + return nil (idle).
3. Build `nameToID` from `kb.GetSystems()`; build the jump graph from `kb.GetConnections()`.
4. `SelectHaulOpportunity(...)`. `!ok` → idle.
5. `ClaimOpportunity(id, AgentID)`:
   - `true` → proceed.
   - `false` (lost the race to another hauler) → drop that candidate and re-select from
     the remaining pool; if the pool is exhausted, idle.
6. `Autopilot` → (buy-system id, `FromStationID` as POI). *Assumption:* the `market.db`
   `station_id` is the same identifier the game client docks at. **Flagged for
   verification in the implementation plan** (live-check on the first real run).
7. **Size the buy from live state:** `qty = min(opp.Quantity, cargoFree, floor(credits / buyPriceEach))`
   where `cargoFree = Ship.CargoCapacity - Ship.CargoUsed`. If `qty < 1` (can't afford
   even one / hold full) → leave the row `claimed`, log, return nil (idle). The buy
   uses the current/live ask the server charges; `opp.BuyPrice` is the snapshot guide.
8. `Buy(itemID, qty)`.
9. `Autopilot` → (sell-system id, `ToStationID` as POI).
10. `Sell(itemID, qty)` (live bid; realized profit, not the snapshot).
11. `CompleteOpportunity(id, AgentID)`.

**Failure handling (per the approved recovery scope):** any error at steps 6–10 is
logged and the step returns nil so the worker stays alive and idles. The claimed row is
**left `claimed`** — it is harmless orphan cruft, not a lost opportunity (see Recovery).
No release atom is called (none exists; deferred to Phase 5b).

## Recovery — why leaving rows `claimed` is safe

`ScanArbitrage` marks every `available` row `expired` and **regenerates a fresh
opportunity set on each scan**. So a real spread that a hauler failed to complete
reappears as a brand-new `available` row (new `id`) on the next scan and is picked up
by some hauler. The stuck `claimed` row is orphaned database/dashboard cruft, not a
spread lost to the fleet. Cleaning that cruft (a stale-`claimed` sweeper and/or a
`failed` status) is the market team's **Phase 5b**, already anticipated by the existing
`idx_arbitrage_status` index and the system-design "Claiming API" sketch.

## Configuration

- `data/scripts/haul.smolt` — the standing script. Minimal: `haul` (the engine does the
  full claim→buy→sell run internally), then `update_market` to refresh local prices
  after docking. Add to the `.gitignore` script allowlist (the `!data/scripts/...`
  negations) alongside `explore.smolt` / `mine_local.smolt`.
- `data/overmind/roles.yaml` — new role:
  ```yaml
  hauler:
    idle: haul
  ```
  (No scheduled commands needed; `update_market` runs as the second script line, and
  the scan is engine-driven scan-when-empty.) Do **not** add an `idle: <command>` that
  collides with the drift guard — `haul` is a real dispatchable command and a real
  script name, consistent with `explore`.
- `data/overmind/fleet.yaml` — add hauler agents (`hauler-1..N`, `role: hauler`,
  `station: ""` — mobile, position emerges from where the work is). Exact roster /
  credentials TBD with the user before launch.

## Testing

- **`SelectHaulOpportunity`** (pure, table-driven, mirrors `TestNextExploreTarget...`):
  profit-descending order; the 10% near-tie band breaking to fewer-jumps; the chaining
  bonus (sell adjacent to another live buy); name that doesn't resolve → skipped;
  unreachable buy-system → skipped; empty/`!ok`; determinism by `id`.
- **`Haul`** (fakes for client/KB/market): happy path → claim, buy sized correctly,
  sell, complete; claim-race (`ClaimOpportunity` returns false) → falls through to the
  next candidate; can't-afford (`qty < 1`) → leaves row claimed, idles; scan-when-empty
  populates the pool then proceeds; nil-KB / no-system → idle, no crash.
- **Drift guard:** `TestSeededCommandsAreDispatchable` must cover `haul.smolt` (the
  `haul` command is in `Supports`).
- Full `go build ./...` + `go test ./...` green; `golangci-lint` clean.

## Open items carried to the plan

- Verify the `market.db` `station_id` == game-client dock POI id assumption on a live
  run before trusting blind autopilot-to-station.
- Confirm `Buy`/`Sell` server semantics for partial fills (does `Buy(qty)` fill `qty`
  exactly or up to availability?) and adjust sizing/`Sell` quantity accordingly.
- Final hauler roster + credentials in `fleet.yaml`.
- Base branch: implementation must build on `origin/main` (which has PR #127's
  `pkg/market` atoms); `feat/overmind-hauler-role` is to be rebased onto `origin/main`
  before implementation.
