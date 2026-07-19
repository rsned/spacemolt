# Depth-Aware Price Gate — Missions + Hauler Hardening

**Date:** 2026-07-19
**Status:** Design approved, pending spec review
**Related:** `2026-06-25-hauler-prebuy-profit-gate.md`, `2026-07-16-mission-runner-fleet-design.md`, `2026-07-18-haul-book-coordination-design.md`

## Problem

The server generates TRADING / procurement missions ("buy N of item X, deliver to Z")
**without consulting the market** — reward and required quantity do not reflect real
acquisition cost or order-book depth (player-reported 2026-07-19). A mission-runner that
trusts the mission economics will overpay for inputs and strand itself.

**Live casualty — `fighter-4` (2026-07-19):**
- Accepted "Steel Plate Order".
- **Bought 25× Iron Ore @ 2000 each = 50,000 credits** (iron_ore's real price is ~5–10 cr;
  a 200–400× markup).
- Drained ~132,238 cr → 0, ended fuel-dead (3/240) at Procyon holding 295 units of cargo,
  full hull (not a death), then abandoned the run.

**Root cause:** the mission-runner's existing cost gate uses `MissionStore.GetReferenceAsk`
(single **best-ask**, top of book) at **accept time only**. When it accepted, the reference
was cheap; by the time it traveled and bought, the cheap depth was gone and there was **no
buy-time re-check**, so it filled the fixed quantity by walking up the book to 2000/unit.
Two blind spots:
1. **No depth awareness** — best-ask × qty assumes the cheapest price holds for the whole
   order; it doesn't when the book is thin.
2. **No buy-time gate** — the accept-time reference goes stale between accept and buy.

Haulers share blind spot (1) in `haulGate` (`pkg/worker/haul.go`), but survive it because
they size the buy *down* (`sizeBuy`) instead of committing to a fixed quantity.

## Goals

- Missions never overpay for inputs: gate procurement against the current station's real
  order-book depth before buying; skip the mission rather than overpay.
- Harden haulers: `haulGate`/`sizeBuy` price against the depth-walked fill, not best-ask.
- Defense-in-depth: an absolute surge ceiling that fires even if a mission's reward is
  nonsensically high.
- Shared, well-tested pricing primitive used by both roles.

## Non-Goals

- Recovering cargo already stranded (that is the separate cargo-liquidation / cut-losses
  feature, `project_cargo_liquidation_cut_losses`).
- Changing server mission generation (out of our control).
- Partial-delivery fulfillment (explicitly rejected: buy-time breach → abandon, buy nothing).

## Design

### 1. Shared primitive — depth-walked acquisition cost (`pkg/market`)

A pure, table-tested helper (no I/O):

```go
// AskLevel is one price level of the sell side of the book.
type AskLevel struct { PriceEach, Quantity float64 }

// CostToAcquire walks asks cheapest-first, filling up to qty.
// Returns total cost, quantity actually fillable, volume-weighted avg price,
// and whether the ladder had enough depth to fill qty in full.
func CostToAcquire(asks []AskLevel, qty float64) (totalCost, filled, avgPrice float64, enoughDepth bool)
```

Backed by a ladder query that extends the existing best-ask logic in
`GetItemStationPrices` (`pkg/market/arbitrage.go:16`): instead of collapsing sell orders
to a single `BestAsk`, return the **sorted ask ladder** per station from that station's
latest capture. New method, e.g.:

```go
// GetAskLadder returns the sorted (ascending price) sell-side levels for an item
// at a station's latest capture. Empty when the item has no sell orders there.
func (c *Collector) GetAskLadder(ctx context.Context, itemID, stationID string) ([]AskLevel, error)
```

(Reuses the same `market_orders` + latest-capture-per-station pattern already in
`GetItemStationPrices`.)

### 2. Reference price for the absolute surge ceiling (`pkg/market`)

```go
// GetReferencePrice returns a robust "cheap" price for an item: a low-percentile
// (e.g. 20th pct) of recent best-asks across stations over a lookback window,
// so a single gouging station (iron_ore @ 2000) is an outlier, not the reference.
func (c *Collector) GetReferencePrice(ctx context.Context, itemID string, lookback time.Duration) (float64, bool, error)
```

Source = recent per-station best-asks (from `market_orders`, or `market_ohlcv.low/close`
as a fallback). Low-percentile, not min, to avoid a single stale 1-unit lowball setting an
unrealistically cheap reference.

### 3. Mission gate (`pkg/worker/mission.go`)

**Correction to the original assumption (verified in code 2026-07-19):** the mission-runner
does **not** source cross-station. It buys inputs **at the station it is already docked at**
while reading the board (`Buy` at `mission.go:539`; availability gate at `:452-461` even
says "only X on market **here**"). At accept time `buildMissionCandidate` estimates item
cost from `GetReferenceAsk` — a **global** cross-station reference best-ask, not the local
price — and at buy time there is **no price check at all**. That is the exact fighter-4
mechanism: cheap global reference passed the accept net-calc, then it bought locally at
2000/unit unchecked.

So the mission gate is a **single local depth-walk gate at the current station**, not an
accept-vs-buy-time split. It slots into the existing availability-gate block, which already
fetches the current station's live `view_market` in the same pass (seconds before the buy).

- Extend `missionFetchMarketSupply` (`mission.go:983`) to return the **ask ladder** per item
  from the same `view_market` response it already parses (currently it returns only total
  quantity per item). New return type carries `map[itemID][]market.AskLevel` alongside (or
  instead of) the totals.
- For each selected candidate with `BuyQty > 0`, run
  `CostToAcquire(localLadder[itemID], BuyQty)` → `localCost, filled, avgFill, enoughDepth`.
- **Accept the buy only if** `enoughDepth` **and** `net ≥ netFloor` (where
  `net = reward − localCost − fuel(current→dest)`, using the existing `fuelCostFor`)
  **and** `avgFill ≤ surgeMult × referencePrice` (from `GetReferencePrice`).
- Otherwise **skip the mission and `markAttempted`** (as the availability gate already does
  on `:462`). The gate runs *before* any `Buy`, so a rejected mission buys nothing — no
  partial cargo, satisfying "abandon + buy nothing" for free.
- `MissionStore` gains `GetReferencePrice`; keep `GetReferenceAsk` (still fine as the
  accept-time coarse filter in `buildMissionCandidate`). The authoritative economic gate is
  the local depth-walk above.
- Missing/empty local ladder for an item → treat as not acquirable here (skip), consistent
  with the existing quantity availability gate.

### 4. Hauler hardening (`haulGate`, `sizeBuy` — `pkg/worker/haul.go`)

Swap the single-`BestAsk` sizing for the depth-walk: size the buy so the effective ask is
the depth-walked average for the intended quantity, not the top-of-book price. Haulers keep
sizing *down* (they are not fixed-quantity), so the practical effect is they buy less when
the book is thin instead of overpaying on the assumption best-ask holds. The existing
`haulMinMargin` / `haulMinNetProfit` thresholds are evaluated against the depth-walked
spread. Optionally apply the same `surgeMult × referencePrice` ceiling for consistency.

### 5. Parameters

- Reuse hauler constants for missions: `haulMinMargin = 0.03`, `haulMinNetProfit = 1000`
  (floor-adjusted for cargo via `netProfitFloor`).
- New: `surgeMult` (absolute ceiling multiple), default **4×**; `referenceLookback`
  default **24h**; reference percentile **20th**. Named constants near the gate, tunable.

## Data Flow

```
mission:  at current dock ──▶ view_market (already fetched by availability gate)
                          ──▶ ladder per item ──▶ CostToAcquire(BuyQty)
                          ──▶ net vs floor + surge vs reference + enoughDepth
                          ──▶ buy locally  |  skip + markAttempted (buys nothing)
hauler:   at buy station ──▶ RecaptureBuyMarket into store ──▶ GetAskLadder(item, here)
                         ──▶ CostToAcquire(intended qty) ──▶ sizeBuy on depth-walked avg
                         ──▶ margin/net gate (+ optional surge ceiling)
```
`GetAskLadder` (store-backed, section 1) serves the hauler path; the mission path reads the
ladder from the live `view_market` response, not the store.

## Error Handling

- Missing/empty local ladder for a mission item → not acquirable here; skip + `markAttempted`
  (same as the existing quantity availability gate).
- `view_market` unavailable this pass → the existing gate already skips the whole availability
  check ("view_market unavailable ... availability gate skipped"); the depth-walk rides on the
  same fetch, so it is skipped too (fail-open for that pass, no buy commitment change).
- `GetReferencePrice` unavailable → fall back to the reward-relative `net` + `enoughDepth`
  gate only (log once); do not block purely on a missing reference.
- Rejected missions are never bought (gate precedes `Buy`), so no abandon/partial-cargo path
  is needed on the mission side.

## Testing

- **`CostToAcquire`** (unit, table-driven): empty ladder, thin book (partial fill,
  `enoughDepth=false`), exact fill, surge ladder (avg > best), single level.
- **`GetReferencePrice`** (unit): outlier gouging station excluded by percentile.
- **Mission gate** (unit, synthetic local ladders): accept on healthy depth; skip on
  thin/missing depth; skip on surge over the reference ceiling; reproduce the fighter-4 case
  (local iron_ore ladder @ 2000, BuyQty 25, reference ~7 → skip, zero spend).
- **`haulGate`/`sizeBuy`** (unit): thin book sizes down vs. old best-ask behavior;
  surge ceiling rejects a gouging source.
- No live-fleet change until unit coverage is green; roll out to the mission-learn pool
  first (lowest-priority fleet), then haulers.

## Rollout

1. Land `pkg/market` primitives (`CostToAcquire`, `GetAskLadder`, `GetReferencePrice`) + tests.
2. Wire the mission gate; deploy to mission-learn pool; watch for over-rejection
   (dry-pass rate) vs. the fighter-4 class of overspend.
3. Harden `haulGate`; canary one hauler, then the fleet.
