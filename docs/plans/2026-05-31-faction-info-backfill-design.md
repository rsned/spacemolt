# Faction Info Backfill — Design

Date: 2026-05-31

## Goal

When a new `faction_id` is observed on another agent (via `get_nearby` /
`get_system_agents`), automatically fetch `faction_info` for that faction and
persist the full details to the knowledge base, so the `factions` (and
members/relations) tables fill in beyond the inline `faction_id` + `faction_tag`
we already store on `seen_players`.

## Decisions (confirmed)

- **Refresh policy**: fetch when the faction is missing from the `factions`
  table OR when the stored row is older than a threshold (24h, `FreshnessFaction`).
- **Depth**: full snapshot available for a non-member — header + members +
  relations (allies/enemies/wars) via the existing `parseFactionInfo`. Station-
  scoped data (bases/storage/orders/missions/rooms) and intel are own-faction/
  vantage-only and are intentionally skipped.
- **Trigger**: only the `get_nearby` / `get_system_agents` observer path (the
  one already feeding `seen_players`). Battle-side / scan faction_ids are a
  possible later extension.
- **Scope**: lives in play_as, the sole place `WirePlayerObserver` runs today.

## Reuse

`pkg/faction` already maps `serverapi.FactionInfoResponse` → knowledge records:
- `parseFactionInfo(info)` → `(FactionRecord, []FactionMember, []FactionRelation)`
  (pkg/faction/parse.go, tested).
- `Collector` owns `StoreFaction` / `ReplaceFactionMembers` /
  `ReplaceFactionRelations` (pkg/faction/collector.go).

## New pieces

### pkg/faction/collector.go — CollectFaction

```go
// CollectFaction fetches faction_info for an arbitrary factionID and persists
// header + members + relations. Unlike Collect it does not gather intel or
// station-scoped data (own-faction/vantage-only). For backfilling observed
// factions we are not a member of.
func (c *Collector) CollectFaction(ctx context.Context, client game.GameClient, factionID string) error
```

Submits `faction_info {faction_id, limit:200}` via the existing `readInto`,
runs `parseFactionInfo`, persists header/members/relations.

### pkg/knowledge/faction_store.go — freshness read

```go
// FactionCapturedAt returns the captured_utc of a stored faction and whether a
// row exists.
func (kb *SQLiteKB) FactionCapturedAt(ctx context.Context, factionID string) (time.Time, bool, error)
```

### pkg/faction/backfill.go — FactionBackfiller

```go
type factionFreshness interface {
    FactionCapturedAt(ctx context.Context, factionID string) (time.Time, bool, error)
}
type factionCollector interface {
    CollectFaction(ctx context.Context, client game.GameClient, factionID string) error
}

type FactionBackfiller struct { /* client, collector, fresh, threshold, logger, ch, seen, mu */ }

func NewFactionBackfiller(client game.GameClient, collector factionCollector, fresh factionFreshness, threshold time.Duration, logger *log.Logger) *FactionBackfiller
func (b *FactionBackfiller) Enqueue(ids ...string)   // non-blocking; session dedupe; bounded channel
func (b *FactionBackfiller) Start(ctx context.Context) // goroutine draining the channel
func (b *FactionBackfiller) process(ctx, id)          // freshness check -> CollectFaction; SleepQuick between fetches
```

- `Enqueue` drops empties and ids already enqueued this session (a `seen` set);
  non-blocking (drops if the bounded channel is full — backfill is best-effort).
- `process`: if `FactionCapturedAt` exists and is within `threshold`, skip;
  else `CollectFaction`. Errors logged, not fatal.

## Wiring

- `pkg/agent.WirePlayerObserver(c *game.Client, kb knowledge.Base, enq Enqueuer)`
  gains a nil-able `enq interface{ Enqueue(ids ...string) }`. The callback
  collects distinct non-empty `FactionID`s from the batch and calls
  `enq.Enqueue(...)` alongside `kb.RecordSightings`. Keeps `agent` from
  importing `faction`; `GameClient` interface untouched (no mock changes).
- `cmd/tools/play_as/main.go` (where `WirePlayerObserver(c, sqliteKB)` runs):
  build `faction.NewCollector(sqliteKB, logger)` and
  `faction.NewFactionBackfiller(c, collector, sqliteKB, FreshnessFaction, logger)`,
  `Start(ctx)`, and pass the backfiller as the enqueuer.

## Concurrency

The observer fires on the client's response goroutine, so it must not block —
it only does the non-blocking `Enqueue`. The backfiller's own goroutine does the
blocking `faction_info` network calls, sequentially, with a `SleepQuick` gap.

## Testing (TDD)

- `FactionBackfiller`: `Enqueue` dedupe (same id twice → one process); `process`
  skips when fresh, fetches when missing or stale (fake collector records calls,
  fake freshness returns controlled timestamps).
- `FactionCapturedAt`: real in-memory SQLite KB — store a faction, read back the
  captured time; missing id → exists=false.
- `WirePlayerObserver`: extend the existing test with a fake enqueuer; assert
  distinct non-empty faction ids are enqueued and `RecordSightings` still runs.

## Constants

- `FreshnessFaction = 24 * time.Hour` (pkg/game/constants.go).
- Inter-fetch delay reuses `game.SleepQuick`.
