# Haul Book Coordination — Design

**Date:** 2026-07-18
**Status:** Approved (design); implementation plan pending
**Area:** `pkg/worker/haul.go`, `pkg/market/arbitrage.go`, `pkg/market/schema.sql`

## Problem

Two symptoms observed on the hauler overmind status page:

1. **Impossible quantities.** A line reads `Opportunity #921069 4847 Steel Plate from
   Ironhearth Station to Ramen's Rest` — no ship can carry 4847 units. This is a
   display artifact: `haulActivityLabel` (haul.go:461) prints the raw `opp.Quantity`,
   which is order-book depth, not what the ship will buy. The actual buy is capped at
   execution by `sizeBuy` (haul.go:357). Harmless to execution, misleading on screen.

2. **Phantom depth / fleet pileup.** Many haulers all working the same two items —
   e.g. 12 agents claimed rows for `processing_core` from `starfall_salvage_station`
   whose real supply is a **single 42-unit book** (best-ask depth, ~2 days stale), and
   4 agents on a 40-unit `control_node` book. Verified against live `market.db`.

### Root cause

`ScanArbitrage` (arbitrage.go:125-154) emits **one opportunity row per
`(item, source, destination)`** — a cross product. Every destination row off the same
source book copies the **same source-supply figure** (`quantity = min(src.AskQty,
dst.BidQty)`), and nothing reserves or decrements the shared source inventory. The
claim lock (`ClaimOpportunity`, arbitrage.go:267) is scoped to a **row id** (one
source→dest pair), not to the source book. So N haulers each legitimately claim a
*different* row that all draw on the *same* physical supply. Only the first to arrive
wins; the rest reposition, find the book empty, and abandon — wasting fuel and time.

Worse, `abandonClaim` (haul.go:762) calls `ReleaseOpportunity`, which sets the row
**back to `available`** (arbitrage.go:395). A collapsed book's rows are therefore
*republished* on every abandon, actively pulling the next hauler in until the next full
scan or TTL expiry — a churn loop.

## Goal

Make the **book** = `(item_id, from_station_id)` the unit of coordination, so that:

- A genuinely deep book (4367 Steel Plate) is **shared** across multiple haulers,
  fanned out across destinations.
- A thin book (42 Processing Core) admits only as many haulers as it can supply.
- A collapsed book is **cleared** so no further haulers are drawn in.
- The status page shows an honest, ship-relative quantity.

## Key decisions (from brainstorming)

| # | Decision | Choice |
|---|----------|--------|
| 1 | Who shares a fat book | **Proximity-ranked** (nearest claim first, soft; reuses existing jump-ranking, no hard cutoff) |
| 2 | Destination assignment | **Fan out across destinations** by remaining bid depth |
| 3 | When to subtract a slice from the pool | **Settle-only at buy** — commit actual bought qty; no fictional pre-reservation (other players share the book) |
| 4 | Anti-pileup limit | **Cap by book size** — K = ceil(source_units / ship cargo) concurrent claimants per book |
| — | Structure | **Approach A**: book-claims sidecar + one column; `arbitrage_opportunities` rows retained as the destination enumerator |

## Data model (Section 1)

The unit of coordination becomes the book `(item_id, from_station_id)`. Two changes;
`arbitrage_opportunities` rows are otherwise untouched (they still enumerate
destinations and hold the per-dest claim lock).

### 1a. New column on `arbitrage_opportunities`

`source_units REAL` — the book's source-side supply (`src.AskQty`), identical across
all rows sharing an `(item, from_station)`. Today that value is lost (each row stores
only `quantity = min(AskQty, BidQty)`). The scanner fills it. It gives us the book size
for the cap and an honest status label.

Added via the `ensure*Cols` pattern, **not** a numbered migration (per the
ships-table-migration trap: `reference_ships_table_migration_trap`).

### 1b. New table `haul_book_claims`

One row per active claim (the live claimant roster):

```sql
CREATE TABLE IF NOT EXISTS haul_book_claims (
    claim_id        INTEGER PRIMARY KEY AUTOINCREMENT,
    item_id         TEXT NOT NULL,
    from_station_id TEXT NOT NULL,
    opp_id          INTEGER NOT NULL,        -- the specific (dest) row this claim took
    to_station_id   TEXT NOT NULL,           -- assigned destination (fan-out)
    agent_id        TEXT NOT NULL,
    phase           TEXT NOT NULL DEFAULT 'claimed'
                       CHECK (phase IN ('claimed','bought','released','done')),
    bought_units    REAL NOT NULL DEFAULT 0, -- actual qty, written at buy (settle)
    claimed_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL,
    expires_at      TEXT NOT NULL,
    UNIQUE(item_id, from_station_id, agent_id)   -- one active claim per agent per book
);
CREATE INDEX IF NOT EXISTS idx_bookclaims_book
    ON haul_book_claims(item_id, from_station_id, phase, expires_at);
```

**No mutable `remaining` field is stored** — it is *derived*, which is the
write-lock-safe form:

- **Concurrent claimants** (for the cap) =
  `COUNT(*) WHERE book AND phase IN ('claimed','bought') AND expires_at > now`.
- **Remaining units others see** = `source_units − SUM(bought_units)` over non-released
  claims of the book. A hauler "subtracts actual amount used" simply by writing its
  `bought_units` at buy; every later reader's `SUM` reflects it. Release/abandon flips
  `phase='released'` and the units return automatically — no counter to corrupt.

`market.db` is already the shared multi-process store; SQLite `BEGIN IMMEDIATE` is the
write-lock for the count-then-insert and the settle write.

## Claim admission (Section 2)

Keep the existing net-of-fuel + proximity-tie ranking (haul.go:298-315). Replace the
per-row claim primitive with a **book-aware admission** inside one `IMMEDIATE`
transaction (atomic across worker processes). For the next ranked opp's book:

1. **Cap check.** `K = ceil(source_units / slice)`, where `slice` = the claiming ship's
   `CargoCapacity` (adaptive; small hauler → larger K, freighter → smaller K). If
   `active_claimants(book) >= K`, skip this book and fall through to the next ranked
   opp. A 42-unit book with a 300-cargo ship → `K=1`; the 4367 book → K in the double
   digits.
2. **Proximity gate (soft).** No new distance rule. Ranking already prefers nearer buy
   stations within a net-tie band and hard-caps at `MaxJumps`. "Nearest claims first"
   falls out of claimants arriving in rank order and the cap closing the book once full.
3. **Destination fan-out.** Among the book's opp rows, pick the best-net `to_station_id`
   **not already held** by an active claimant of this book (exclude the set from
   `SELECT to_station_id FROM haul_book_claims WHERE book AND phase IN
   ('claimed','bought')`). Degrades to reusing the best dest when distinct dests < K
   (never rejects admission for lack of a fresh dest — that would strand source depth).
4. **Admit.** `INSERT` the `haul_book_claims` row (`phase='claimed'`, chosen dest,
   `opp_id`, `expires_at`) and keep the existing per-row `ClaimOpportunity` flip on that
   `opp_id` as the **destination lock**. `COMMIT`.

If the transaction loses the race, the count re-check / `UNIQUE` fails the insert → roll
back, try the next ranked opp (exactly how `claimBest` already tolerates a lost claim).

## Concurrency, deadlock, and fallback-collision (Section 2b)

`market.db` runs **WAL + `busy_timeout`** (collector.go:88-90), but `_txlock` is **not**
set today, so `database/sql` transactions are *deferred*.

- **Deadlock is structurally impossible with `IMMEDIATE`.** SQLite (even WAL) has one
  database-wide write lock; no row-level locking. The single classic deadlock is two
  *deferred* transactions that read (shared lock) then both try to *upgrade* to write.
  An `IMMEDIATE` transaction grabs the write lock at `BEGIN` and never upgrades → no
  lock ordering, no hold-and-wait. Worst case degrades to `SQLITE_BUSY`, which the
  existing `busy_timeout` absorbs as a clean block-then-proceed.
  **Requirement:** add `_txlock=immediate` to the admission connection's DSN (a
  dedicated `*sql.Conn` or DSN variant).
- **No lock is held across the haul.** The claim is a *committed row*, not a held lock;
  the write lock lives only for the microseconds of count+insert. Reposition/buy/sell
  hold nothing. The "each holds part, the other's lock blocks it" trap cannot form.
- **Fallback does not re-collide.** Because admission is fully serialized, hauler A's
  count + dest-pick + insert **commits before** B's transaction begins. B's cap-count
  and taken-dests query already see A's committed row, so B picks a genuinely-free
  slot/dest in one pass — never the one A just took. This holds **only because the whole
  decision is inside the one transaction** (no stale-snapshot straddle). If the book is
  genuinely full, B advances to the next ranked book; the ranked list is finite and
  books only fill during a pass, so the loop terminates.

**Two semantic (liveness, not deadlock) traps designed against:**

1. **Crashed hauler holding a cap slot** (`kill -9` between claim and release): the cap
   count filters `expires_at > now`, so a stale claim ages out automatically; a lazy
   reaper also marks expired rows `released`. Book self-heals.
2. **Fewer distinct destinations than K**: fan-out degrades to reusing the best dest
   rather than refusing admission (see Section 2.3).

## Buy/settle + status label (Section 3)

**Settle at buy.** Flow unchanged through reposition. At the buy leg (`runClaimedHaul`,
haul.go:~850-874), `haulGate`/`sizeBuy` still compute the real quantity from the **live
re-read** market (authoritative — other players may have eaten the book), cargo, and
credits. Immediately after `Client.Buy` returns the filled quantity, one short
`IMMEDIATE` transaction:

```sql
UPDATE haul_book_claims
   SET phase='bought', bought_units=<actual>, updated_at=now
 WHERE claim_id=? AND agent_id=?;
```

That is the entire "subtract actual amount used" — later readers computing
`source_units − SUM(bought_units)` see the reduced remainder.

**Terminal transitions:**
- Sell completes → `phase='done'` (frees the cap slot; `bought_units` retained for the
  remainder math until the book is re-scanned).
- Collapse-confirmed abandon on arrival → `phase='released'` + book invalidation
  (Section 3b).
- Expiry/reaper → `phase='released'`.

**Status-label fix.** `haulActivityLabel` (haul.go:461) shows what this hauler will
attempt plus book context:

> `Opportunity #921069 · buying up to 480 of 4367 Steel Plate · Ironhearth → Ramen's Rest`

where `480 = min(shipCargo, affordable)` and `4367 = source_units`. Thin books read
`up to 42 of 42`. No more physically-impossible quantities.

**Interface additions** (`OpportunityStore`, haul.go:376), all satisfied by
`*market.Collector`; tests use the existing fake:
`AdmitBookClaim(ctx, book, oppID, dest, agent, slice, cap) (claim, ok, err)`,
`SettleBookClaim(ctx, claimID, agent, boughtUnits)`,
`ReleaseBookClaim(...)`,
`BookState(ctx, item, from) (sourceUnits, activeClaimants, takenDests)`,
`InvalidateBook(...)` (Section 3b), plus the reaper.

## Signal-back: clear a collapsed book (Section 3b)

The arriving hauler is the best-positioned observer — `haulGate` already does a live
recapture of the source market before buying (the fresh, now-empty snapshot lands in
`market_orders`). On a **collapse-confirmed** abandon, instead of republishing the row,
invalidate the whole book.

**New store method** `InvalidateBook(ctx, item, fromStation, observedBy, reason)`, one
`IMMEDIATE` transaction:

- `UPDATE arbitrage_opportunities SET status='expired',
  notes='source collapsed @<ts> by <agent>'
  WHERE item_id=? AND from_station_id=? AND status='available'` — every sibling
  destination row off that dead source leaves the pool at once; no new hauler is ranked
  into it.
- Mark the book's `haul_book_claims` rows `released` (frees cap + remainder).

**Scope:** only `available` rows. En-route `claimed` haulers are already committed; each
confirms-and-invalidates on its own arrival (idempotent), so we do not yank in-flight
ships.

**Durability without a permanent blacklist:** A's recapture already wrote the fresh
empty snapshot, so even the on-demand re-scan (`loadAvailable` fires `ScanArbitrage`
when the pool drains, haul.go:393) reads real emptiness and won't regenerate the book.
When supply genuinely returns, a later capture → scan brings it back. Self-limiting.

**Confidence guard — do not over-invalidate:** call `InvalidateBook` *only* when the
live recapture **succeeded and confirmed** collapse (best-ask depth below a floor /
spread gone). Per-hauler abandons — unroutable, gate-rejected, dock failure,
insufficient credits/cargo — are **not** the book's fault and keep today's single-row
`ReleaseOpportunity` behavior. The abandon path splits by reason:
*book-collapse → invalidate book*; *my-problem → release my row*.

## Scanner changes (Section 4)

`ScanArbitrage` (arbitrage.go:96-199), minimal additions:

- `GetItemStationPrices` already computes `src.AskQty` per source. Carry it into each
  inserted row's new `source_units` column (identical across a book's rows). Only new
  write in the insert at arbitrage.go:189-193.
- No cap or claim state computed by the scanner — `K` is derived at claim time; the
  roster lives in `haul_book_claims`. The scanner's existing "expire all available,
  re-insert fresh" sweep (arbitrage.go:174) resets books each cycle; the reaper lazily
  clears `haul_book_claims` rows whose `opp` no longer exists.

**Migration:** `source_units` via `ensure*Cols`; `haul_book_claims` via
`CREATE TABLE IF NOT EXISTS` in `schema.sql` + the migrations path; `_txlock=immediate`
on the admission connection DSN.

## Test plan (Section 4)

All in `pkg/worker` + `pkg/market`, table-driven, fake store where possible:

1. **Cap math** — `source_units=42, cargo=300 → K=1`; `4367/480 → K≈10`. Boundaries:
   exact multiples, `source_units=0`.
2. **Admission serialization** — two concurrent admits on a `K=1` book against a real
   temp SQLite (`_txlock=immediate`): exactly one wins, loser advances to next book;
   assert no deadlock, no double-admit.
3. **Fan-out** — 3 admits on a fat book with 3 dests → 3 distinct dests; 4th
   (dests < K) → degrades to best dest, not rejected.
4. **Settle** — after buy, `bought_units` written; `source_units − Σbought` reflects it;
   a released claim drops out of the sum.
5. **Collapse invalidate** — collapse-confirmed abandon expires *all* the book's
   available rows and does **not** republish; a second hauler no longer sees them.
   My-problem abandon still releases just the one row.
6. **Reaper** — a `phase='claimed'` row past `expires_at` is excluded from the cap count
   and swept to `released`.
7. **Label** — `haulActivityLabel` renders `up to <slice> of <source_units>`, never raw
   book depth.

Gate before commit: `go build ./... && go test ./... && golangci-lint` (per CLAUDE.md).

## Future work / non-goals

- **Destination reroute mid-flight.** If a hauler is en route to sell and the
  destination's buy orders are eaten (by another player or our own hauler A) before it
  arrives, reroute to the next-best remaining destination instead of dumping into a
  crashed bid or abandoning. Leans on the same book-coordination state
  (`dest_assignments` + remaining-demand view). Deferred; settle-only + fan-out-at-claim
  first.
- **Demand-side reservation.** Symmetric decrement of destination bid depth across our
  own haulers. Not needed for this iteration (fan-out-at-claim spreads them; other
  players make a stored demand reservation fiction, same argument as source).
- **Per-unit item volume** in `sizeBuy` (existing `TODO(logistics)`, haul.go:355) —
  unchanged here.
