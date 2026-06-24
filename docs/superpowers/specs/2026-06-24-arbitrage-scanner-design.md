# Arbitrage Scanner — Design

**Date:** 2026-06-24
**Status:** Approved (design), pending implementation plan
**Phase:** 4 of the Market Intelligence System
**Related:** `docs/superpowers/specs/2026-06-19-market-intelligence-system-design.md` (system design, §Arbitrage Scanner + §Implementation Phases); `docs/superpowers/specs/2026-06-22-market-dashboard-design.md` (Phase 2 dashboard, merged via PR #125); `pkg/market` (collection + reads, merged).

## Goal

A cross-station arbitrage scanner that reads the latest market captures from `data/market.db`, detects profitable buy-low/sell-high spreads, and writes them to the existing `arbitrage_opportunities` table — making spreads visible in the market dashboard and claimable by trading agents (Phase 5). This is **Phase 4**, delivered in the workstream's validate-first style (the dashboard was explicitly a validation tool; this scanner validates that collected prices form credible opportunities before any agent acts on them).

## Scope

**In scope:**
- `pkg/market`: a new arbitrage-native read (`GetItemStationPrices`), detection+persist (`ScanArbitrage`), claiming atoms (`ClaimOpportunity` / `CompleteOpportunity`), a dashboard read (`GetOpportunities`), and `Opportunity` / `ItemStationPrice` / `ScanOptions` / `ScanResult` types.
- `cmd/arbitrage-scanner`: a thin CLI binary (subcommands: `scan` / `list` / `claim` / `complete`) over the package methods.
- `cmd/market-dashboard`: an additive **Opportunities** view (`GET /api/opportunities` + a fourth UI tab).
- TDD throughout (temp-DB unit tests + httptest), mirroring the dashboard plan.

**Explicitly out of scope (deferred):**
- **Logistics realism** — no fuel, distance, travel-ticks, or taxes. Real distance/fuel lives in `knowledge.db` (`connections.distance`, `connection_metrics.avg_fuel_cost` / `avg_travel_time`), keyed by **system**, not station, while `market.db` is deliberately standalone (`pkg/market/doc.go`). Bridging that is its own architectural step. Persisted rows store `fuel_cost = 0`, `travel_ticks = 0`, `cargo_required = quantity` (unit-count proxy), and `notes = 'logistics:deferred'`. → **Phase 4b.**
- **Scheduling** — on-demand binary only; no resident `ScheduledTask`. → later slice.
- **Agent consumption** — the claiming *mechanism* (SQL + methods + CLI) ships now; no agent claims until Phase 5.
- `sell_then_buy` action type — MVP emits only `buy_then_sell`.
- Multi-hop routing (`maxJumps`) — direct station pairs only.
- A `failed` opportunity status — the existing `status` CHECK allows only `available / claimed / completed / expired`. A failed run leaves the row `claimed`; adding `failed` is a later migration.

## Architecture

**Approach (chosen):** all scanner logic lives in `pkg/market` as methods on `*market.Collector`; `cmd/arbitrage-scanner` is a thin flag-parsing wrapper; the dashboard calls the same package methods. This mirrors the established pattern (`pkg/market` is the single home for market-DB logic; `cmd/market-dashboard`, `cmd/tools/market-stats` are thin shells) and gives the logic temp-DB unit tests at the package level while letting the dashboard reuse it.

**Rejected alternative:** logic inside `cmd/arbitrage-scanner/main.go` querying the DB directly — untestable at the package level, not reusable by the dashboard, diverges from convention.

**Runtime:** `arbitrage-scanner [--market-db-path data/market.db] <subcommand> [flags]`. Opens one `*market.Collector`, fails fast on an unreadable DB (mirrors `market-stats`), defers `Close`. Pure local-DB read/write — never connects to the game; safe to run any time.

### Price basis (locked)

The scanner uses each station's **best price in its latest capture** (the genuinely transactable best ask / best bid), **not** latest-bucket VWAP:
- A sell order is an ask; the **cheapest** sell order at a station is where you **buy**.
- A buy order is a bid; the **highest** buy order at a station is where you **sell**.
- Best ask/bid is the realizable spread; VWAP is an average and would understate it.

Basement orders (e.g. 1cr bait asks) are excluded by a per-order **price floor** (`MinPrice`, default 10cr) **and** a **depth floor** (`MinQuantity`, default 1) — both sides must independently clear. This complements the workstream's existing "VWAP filters basement orders" handling for analysis, but for *actionable* spreads the transactable best price is correct.

### `pkg/market` API additions (all additive, TDD)

**Types** (`types.go`):

```go
// ItemStationPrice is one item's best ask/bid per station, from the latest capture.
// The arbitrage-native primitive: correct "best price in latest capture" semantics
// with both-side depth. (GetMatrix cells lack buy-side volume; FindBestPrices returns
// the latest single order, not the best order in the latest capture, and is not
// per-station grouped.)
type ItemStationPrice struct {
    StationID   string
    StationName string
    SystemID    string
    SystemName  string
    BestAsk     float64 // cheapest sell order in latest capture (where you BUY)
    AskQty      float64 // total quantity of orders tying at BestAsk
    BestBid     float64 // highest buy order in latest capture  (where you SELL)
    BidQty      float64 // total quantity of orders tying at BestBid
    HasSell     bool
    HasBuy      bool
    CapturedAt  time.Time
}

// ScanOptions parameterizes a scan.
type ScanOptions struct {
    MinProfit   float64       // gross_profit floor (default 1000)
    MinPrice    float64       // per-order price floor, kills basement orders (default 10)
    MinQuantity float64       // per-order depth floor (default 1)
    ExpiresIn   time.Duration // opportunity TTL (default 6h)
    Items       []string      // allowlist; empty = all items
    Limit       int           // cap rows inserted (default 500)
}

// ScanResult reports what a scan did.
type ScanResult struct {
    Expired     int
    Inserted    int
    GeneratedAt time.Time
}

// (Reuses the existing ArbitrageOpportunity type in types.go — extend it rather
// than introduce a parallel type. Additive string fields, populated by
// GetOpportunities via joins; persisted rows store IDs only):
//   FromStationName, FromSystemName, ToStationName, ToSystemName, ItemName
// Timestamps stay string (RFC3339) as in the existing type.
```

**Methods** (on `*Collector`):

- `GetItemStationPrices(ctx, itemID string) ([]ItemStationPrice, error)` — latest-capture CTE (same shape as `GetMatrix`'s cell query) computing `MIN(sell price)` / `MAX(buy price)` **and the quantity at that price**, per station. Empty slice when none.
- `ScanArbitrage(ctx, opts ScanOptions) (ScanResult, error)` — detect + persist (algorithm below).
- `ClaimOpportunity(ctx, id int, agentID string) (claimed bool, err error)` — atomic `UPDATE … SET status='claimed', claimed_by=?, claimed_at=? WHERE id=? AND status='available'`; returns whether the claim succeeded (false = already gone/expired).
- `CompleteOpportunity(ctx, id int, agentID string) (completed bool, err error)` — atomic `UPDATE … SET status='completed' WHERE id=? AND claimed_by=?`; zero rows → `false`, no error.
- `GetOpportunities(ctx, status string, limit int) ([]ArbitrageOpportunity, error)` — for the dashboard; joins `stations`/`items` for names, `ORDER BY gross_profit DESC`. `status == ""` → all statuses.

### Detection algorithm (`ScanArbitrage`)

1. `BEGIN` transaction. Expire stale: `UPDATE arbitrage_opportunities SET status='expired' WHERE status='available'` (claimed/completed rows persist).
2. Resolve the item set (all `items`, or `opts.Items` allowlist when non-empty).
3. For each item: `prices = GetItemStationPrices(item)`.
   - **buy sources** = stations with `HasSell && BestAsk >= MinPrice && AskQty >= MinQuantity`.
   - **sell dests** = stations with `HasBuy && BestBid >= MinPrice && BidQty >= MinQuantity`.
4. For each `(src, dst)` with `src.StationID != dst.StationID` and `dst.BestBid > src.BestAsk`:
   - `qty = min(src.AskQty, dst.BidQty)` (never exceeds either depth).
   - `gross = (dst.BestBid - src.BestAsk) * qty`.
   - Keep if `gross >= opts.MinProfit`.
5. Sort candidates by `gross DESC`, cap at `opts.Limit`, INSERT each with `action_type='buy_then_sell'`, `fuel_cost=0`, `travel_ticks=0`, `cargo_required=qty`, `expires_at = now + opts.ExpiresIn`, `discovered_at = now`, `discovered_by='arbitrage_scanner'`, `notes='logistics:deferred'`.
6. `COMMIT`. Return `ScanResult{Expired, Inserted, GeneratedAt: now}`.

On any query error: rollback and return `fmt.Errorf("...: %w", err)` (house style).

**Why a new `GetItemStationPrices`** rather than reusing `FindBestPrices` or `GetMatrix`: `GetMatrix` cells carry sell-side `Volume` but not buy-side depth, and `FindBestPrices` ranks the latest single order per station rather than the best order within the latest capture and is not per-station grouped. The scanner needs both sides' best price **and** depth per station — hence a focused primitive.

### `cmd/arbitrage-scanner` CLI

Subcommand-based; `scan` runs when no subcommand is given.

| Subcommand | Flags | Behavior |
|---|---|---|
| `scan` *(default)* | `--market-db-path` (default `data/market.db`), `--min-profit` (1000), `--min-price` (10), `--min-quantity` (1), `--expires` (6h), `--items` (comma list, optional), `--limit` (500), `--json` | Calls `ScanArbitrage`; prints `expired/inserted/generated_at`. With `--json`, full JSON including the top-20 inserted rows (for eyeballing credibility). |
| `list` | `--market-db-path`, `--status` (default `available`), `--limit` (50), `--json` | Calls `GetOpportunities`; prints a readable table (item, from→to, buy/sell/qty/gross, status, expires). |
| `claim` | `--market-db-path`, `--id`, `--agent` | `ClaimOpportunity`; prints `claimed` or `unavailable`. |
| `complete` | `--market-db-path`, `--id`, `--agent` | `CompleteOpportunity`; prints `completed` or `not-owned`. |

### `cmd/market-dashboard` — Opportunities view (additive)

- **Endpoint:** `GET /api/opportunities?status=&limit=` → `GetOpportunities`. Returns `200` with `[]` when empty (nil-safe, matching the dashboard convention).
- **UI:** a fourth tab **"Opportunities"** in the embedded UI — one `<table>`: item · from→to (station / system) · buy · sell · qty · **gross** · status · expires · discovered, sorted by gross desc. Reuses the existing `getJSON` / `fmt` / `relTime` helpers and tab-switch machinery: one new render function + one handler + one route + one tab button. Refreshes with the existing Refresh button.

## Data Flow

```
data/market.db (market_orders, items, stations, arbitrage_opportunities)
        │
        ▼
  pkg/market: GetItemStationPrices → ScanArbitrage (writes arbitrage_opportunities)
        │                                      │
        ▼                                      ▼
  cmd/arbitrage-scanner (CLI)          cmd/market-dashboard (/api/opportunities + tab)
        │                                      │
        ▼                                      ▼
  human/operator eyeballs              dashboard Opportunities view
        │
        ▼
  ClaimOpportunity / CompleteOpportunity  ◄── Phase 5 agents (future)
```

No game connection. The scanner reads `market_orders` and writes `arbitrage_opportunities`; the dashboard reads `arbitrage_opportunities`.

## Edge Cases & Error Handling

- **Basement orders:** excluded by both `MinPrice` and `MinQuantity`; each side must independently clear.
- **Same-station:** `src.StationID != dst.StationID` enforced. Same-system / different-station pairs are valid (zero travel) and may show strong spreads — acceptable while logistics is deferred.
- **No profitable pairs / unknown items in `--items`:** `ScanResult{Inserted: 0}`, no error; dashboard renders its empty state.
- **Quantity:** `min(AskQty, BidQty)`; `cargo_required = qty`.
- **Expire lifecycle:** scan start marks all `available` → `expired` (so re-scans don't accumulate stale dupes); `claimed`/`completed` persist. `expires_at` is set; `idx_arbitrage_status(status, expires_at)` supports a later background sweeper (none in MVP — on-demand scans carry freshness).
- **Claim race / wrong agent:** `ClaimOpportunity` and `CompleteOpportunity` are atomic conditional `UPDATE`s; zero rows affected → `false`, no error.
- **DB errors:** `ScanArbitrage` runs in one transaction; any error → rollback + wrapped error. Read methods return empty slices / nil on no data (matches existing `GetLatestSnapshot` semantics).
- **No game coupling:** the scanner is pure local-DB read/write; safe to run any time.

## Testing (TDD per slice, temp DB)

- **`GetItemStationPrices`:** multi-station ask/bid/qty; latest-capture-wins (older capture ignored); `HasSell`/`HasBuy`; empty when none.
- **`ScanArbitrage`:** happy-path spread (correct buy/sell/qty/gross/`action_type`/`notes='logistics:deferred'`); basement filtered by `MinPrice`; `MinQuantity` filtered; sub-`MinProfit` excluded; same-station excluded; expire-lifecycle (`available`→`expired`, `claimed`/`completed` preserved); item allowlist; `Limit` cap; `qty = min` depth.
- **`ClaimOpportunity`:** available→claimed; double-claim→`false`; claim on expired→`false`.
- **`CompleteOpportunity`:** matching `claimed_by`→`completed`; wrong agent→`false`.
- **`GetOpportunities`:** status filter, joined station/system names, `gross_profit DESC` ordering, empty.
- **`cmd/arbitrage-scanner`:** table-driven flag→`ScanOptions` mapping; a `scan`→`list` round-trip against a temp DB. (Heavy logic is pkg-level.)
- **`cmd/market-dashboard` `opportunitiesHandler`:** `httptest` asserting JSON shape + empty-array case (mirrors existing handler tests).
- After each series of changes: `go build ./...`, `go test ./...`, `golangci-lint run ./...` (per project rules — interface/struct changes break things the build alone misses).

## Sequencing / Coordination

1. Spec (this doc) + plan on a Phase 4 branch off `main`.
2. Implement in order: (a) `pkg/market` types + `GetItemStationPrices`; (b) `ScanArbitrage`; (c) `ClaimOpportunity` / `CompleteOpportunity` / `GetOpportunities`; (d) `cmd/arbitrage-scanner`; (e) dashboard endpoint + tab. Tests at each step.
3. Smoke against the real `data/market.db`: `scan`, then `list`, then open the dashboard Opportunities tab, then a `claim`→`complete` round-trip.

No coordination gate: all changes are additive to `pkg/market` (new methods/types) plus two new/extended `cmd/` packages. Nothing conflicts with in-flight work.

## Open Items (follow-on, not this work)

- **Phase 4b — logistics:** real `fuel_cost` / `travel_ticks` / net profit by reading `knowledge.db` (`connections.distance`, `connection_metrics.avg_fuel_cost` / `avg_travel_time`), resolving the system-vs-station key mismatch; tax estimation; `cargo_required` from real per-item cargo volume.
- **Multi-hop routing** (`maxJumps`) and `sell_then_buy`.
- **Scheduling:** register the scanner as a resident `ScheduledTask` (hourly, after captures).
- **`failed` status:** migrate the `status` CHECK to add `failed`; wire `CompleteOpportunity(success=false)`.
- **Phase 5 — agent integration:** agents query `arbitrage_opportunities`, evaluate, claim, execute, and report results.
