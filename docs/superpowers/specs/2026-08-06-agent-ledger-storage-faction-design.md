# Agent Capability Ledger — Storage + Faction Slices — Design

Slices 5–6 of the agent asset + capability ledger
(`docs/superpowers/specs/2026-08-01-agent-asset-profile-design.md`), plus the
eligibility-flapping fix that shares their read layer. Continues on branch
`feat/agent-capability-ledger`; everything here inherits the parent spec's
rules (separate `assets.db`, wire-shaped tables, no FKs, per-table
`captured_at`, inert until `--assets-db-path`).

## Problem

The merged slices answer "who *is* this agent" (identity, skills, standings,
carrier, hulls). They cannot answer "what does the fleet *hold* and where" —
the question behind "what can we source for free", faction treasury health,
and the operator's fleet-asset-snapshot request (`agent_ships` has never held
a row; storage was last captured 2026-07-02). Separately, canary review found
that `CaptureProfile` computes eligibility only from the current pass, so one
transient `ListShips` failure flaps capabilities despite good rows sitting in
the tables. The readers that fix needs are the same ones storage capture
needs, so both land together.

## Decisions settled in this pass

Five questions were open when the parent spec shipped. All are now decided
(operator, 2026-08-06):

1. **Faction assets get their own tables**, not a `holder_type` discriminator
   on the agent tables. `player_id` and `faction_id` are different id spaces;
   faction storage carries fuel-bunker columns agents don't have; and no
   reader ever needs a `WHERE holder_type` filter it can forget.
2. **Base-id normalization happens at read time via an injected resolver.**
   Tables store the wire value verbatim, preserving the field-for-field
   canary check and keeping `pkg/assets` free of any
   spacemolt-knowledge.db dependency.
3. **The storage sweep is one paced pass per capture** — no chunking, no
   resume cursor. Worst case (~20 bases, craftsman-1) is ~40 seconds of free
   queries once a day.
4. **Eligibility falls back to stored rows** for sources not captured this
   pass. This makes `CarrierKnown`'s documented meaning ("no debt vs never
   captured") actually true.
5. **A minimal ovdash panel ships in this slice.** The coverage JSON already
   reaches the browser; nothing renders it, so the rollout step "watch the
   panel" has been unperformable. It has slipped once; it does not slip again.

One question from the hull-guard work resolves the opposite way from hulls:
**zero storage is legitimate.** An agent can never own zero ships (a
destroyed last hull respawns a Tier 0 starter), so an empty hull decode is
always a stale cache. But an agent genuinely can hold nothing at a base, so
an empty-but-successful sweep *deletes*. The fail-safes below keep "empty
because the call failed" from masquerading as "empty because it's empty".

## Schema

Five new tables. `base_id` is the **verbatim wire value** (base-id form, e.g.
`confederacy_central_command`) — see Read layer for why joins against
`pois.id` must go through the resolver.

| Table | Key | Columns (source) |
|---|---|---|
| `agent_storage` | `(player_id, base_id)` | `credits`, `captured_at` — `ViewStorageResponse` |
| `agent_storage_items` | `(player_id, base_id, item_id)` | `name`, `quantity`, `size` — `ViewStorageResponse.Items` |
| `faction_profile` | `faction_id` | `name`, `tag`, `leader_id`, `treasury`, `member_count`, `owned_bases`, `captured_at` — `FactionInfoResponse` |
| `faction_storage` | `(faction_id, base_id)` | `credits`, `fuel_reserve`, `fuel_capacity`, `captured_at` — `ViewFactionStorageResponse` |
| `faction_storage_items` | `(faction_id, base_id, item_id)` | `name`, `quantity`, `size` — `ViewFactionStorageResponse.Items` |

The parent spec's outline had a fourth faction table, `faction_fuel_bunkers`.
It is **dropped**: `faction_fuel_reserve` / `faction_fuel_capacity` ride the
`view_faction_storage` response per base, so bunker state is two columns on
`faction_storage`, not a table.

Not captured, by design: `ViewStorageResponse.Ships` (superseded by
`list_ships` — an `OwnedShip` is a strict superset and carries location),
`ViewStorageResponse.Gifts` (unclaimed gifts; deferred until something needs
them), `RecentActivity` on the faction response.

## `capture_storage`

A new dispatch command (worker + `play_as`), scheduled `daily`, all workers.

1. `view_storage` wherever the agent is → take `hint`.
2. Parse the hint's base list (split on `" in storage at "` then `", "`).
3. For each listed base: `ViewStorageAt(ctx, baseID)`, with
   `game.SleepQuick` between calls.
4. In **one transaction** at the end: upsert `agent_storage` +
   `agent_storage_items` for every base that answered, then apply both
   deletion invariants —
   - an item absent from a base's response is deleted;
   - a base absent from the successfully-parsed hint has its
     `(player_id, base_id)` rows deleted at both grains.

Fail-safes, in order of precedence:

- **Unparseable hint** → fall back to the previously-known base set
  (`SELECT base_id FROM agent_storage WHERE player_id=?`), loud log, and
  **skip the base-deletion invariant entirely** this pass. A parse failure
  must never read as "sold everything".
- **One `ViewStorageAt` fails** → skip that base: keep its old rows and old
  `captured_at`, exclude it from deletion, log, continue the loop. Partial
  capture is normal and recorded honestly, per the parent spec.
- **`view_storage` itself fails** → no capture this pass, nothing touched.
  Capture is never a new way for the pass to fail.

## `capture_faction`

A new dispatch command (worker + `play_as`), scheduled `daily`, no-op unless
`Player.FactionID != ""`.

**Designation without coordination.** Faction assets are per-faction, so one
member's capture covers all. The designated captor is the member whose
`player_id` is `MIN(player_id)` among `agent_profile` rows for that
`faction_id` with `captured_at` fresher than 24h; every other member no-ops.
Each worker evaluates this locally against its own `assets.db` — no shared
state, no claim file. During bootstrap (own profile row missing, or the
freshness set is empty) the worker captures anyway; the worst case is two
members capturing the same faction once, and upserts are idempotent.

Flow mirrors storage: `faction_info` → `faction_profile` row; then the
faction storage `Hint` drives a paced `ViewFactionStorageAt` loop with the
same parser, the same transaction shape, and the same invariants keyed on
`faction_id`.

## Eligibility fallback (the flap fix)

New per-agent readers in `pkg/assets`, each returning the value plus its
`captured_at`: `LoadProfile`, `LoadCarrier`, `LoadHulls`, `LoadStorage`.

`CaptureProfile` builds its `AgentSnapshot` from this pass where a source was
captured, and from stored rows where it was not. A transient
`ShippingProfile`/`ListShips` failure therefore no longer recomputes
eligibility as "never captured". `CarrierKnown` keeps its documented meaning.

Staleness stays honest in the output rather than the boolean: a rule fed by
stored data older than its source's cadence appends it to `blocking_reason`
(e.g. `cargo 45 < 100 (hulls stale 3d2h)`). Rules never silently trust old
data; they visibly trust it.

## Read layer and the resolver

```go
type BaseResolver func(baseID string) (poiID string)
```

Readers that return base ids accept an optional `BaseResolver`; nil means
verbatim. `pkg/worker` and `play_as` inject one backed by `bases(id, poi_id)`
in spacemolt-knowledge.db, which both already hold open. The map is 15 rows
where the forms differ: 5 empire capitals (genuine renames — suffix-stripping
does not work), 7 pirate strongholds, 3 player bases. A missing resolver
degrades to verbatim ids — never wrong ids, just unjoined ones.

`pkg/assets` itself never opens knowledge.db. That boundary is the point of
decision 2.

## Ovdash panel

One React component in `frontend/` rendering the coverage block the snapshot
JSON already carries, extended with the `storage` and `faction` sources: a
table of agents × sources (profile / carrier / hulls / storage / faction)
showing max age per cell, highlighted past 2× the source's cadence. Data
path follows the existing copy-under-RLock snapshot pattern; no new endpoint,
no polling change.

## Scheduling

Both captures are tier-2 ("eventual") per the parent spec's taxonomy: `daily`
entries in `roles.yaml` for all workers (`capture_faction` self-gates on
membership). The known consequence — every worker fires at the same daily
boundary — is accepted for the same reason as before: free queries and small
transactions. `spread: true` jitter remains a scheduler follow-on, not a
workaround inside capture.

## Testing

TDD, proven red first, fake `GameClient` per the `pkg/worker` fakes. The
standing fixture rule, learned twice on this branch at cost: **fixtures come
from captured live payloads** (`--debug=1 --debug-full-payload=true`),
**never composed** — an invented wrapper is exactly how `owned_ships` stayed
dead under a green suite.

- Hint-parser goldens from live frames: normal list, single base, and the
  parse-failure fallback (falls back to known set, deletes nothing).
- Both deletion invariants at both grains: items `{x,y}`→`{x}` leaves no `y`;
  bases `{A,B}`→`{A}` leaves no `B` in either table.
- Zero-is-legitimate: an empty successful sweep deletes all rows; a failed
  `view_storage` deletes none.
- Per-base failure: base B's `ViewStorageAt` errors → B keeps old rows and
  old `captured_at`, A updates, B not deleted.
- Designation election: min-player_id wins; stale profiles excluded;
  bootstrap (no rows) captures anyway.
- Flap regression: pass 1 captures everything; pass 2 fails `ListShips` →
  eligibility unchanged, `blocking_reason` gains the staleness suffix.
- Resolver: nil → verbatim; present → the 5-capitals fixture maps correctly.

## Canary verification — before the plan freezes

Two wire behaviors only a live agent can answer, both front-loaded because a
wrong assumption here invalidates the hint design:

- **(a)** Does `ViewStorageAt` / an undocked `view_storage` response carry
  `hint`? If the hint only appears when docked at one's own station, the
  base-list source changes.
- **(b)** Does the hint truncate for very large holders? Verify against
  craftsman-1 (~20 bases, the largest). A silent truncation would
  under-sweep and then *delete* the truncated bases — worth one canary run
  to rule out. Also capture the faction-side `Hint` raw, which has never
  been parsed.

Canary recipe is unchanged from the parent effort: build `play_as` from the
worktree, run from the main repo with a scratch db, off-fleet agents
(databot, prophet-1/2, craftsman-1 with supervisor freeze).

## Build order

1. Readers (`LoadProfile`/`LoadCarrier`/`LoadHulls`) + the flap fix — pure
   `pkg/assets`, no new wire code, immediately mergeable.
2. Canary runs (a) and (b); adjust the hint design if either surprises.
3. `agent_storage` + `agent_storage_items` + hint parser + `capture_storage`
   (+ `LoadStorage`).
4. Faction tables + designation + `capture_faction`.
5. Coverage extension + ovdash panel.

## Deferred, unchanged from the parent spec

Faction ship garages (shape unverified, none built), module/fitting capture,
`spread:` jitter, unclaimed gifts, assignment. The worker's own gate remains
authoritative; this ledger screens, it does not promise.

---

## Known limitations as built (2026-08-06, from the final whole-branch review)

These survived review deliberately. None blocks merge; all are real, and each is
recorded here rather than in a scratch file so the next reader finds them.

**`faction_storage.fuel_reserve` is only reliable for the captor's own dock.**
This is the sharpest one, and it dents a headline deliverable. The `view_storage`
hint enumerates bases **with items**, so a faction base holding a stocked fuel
bunker and no items is invisible to it — never swept, never observed, and
therefore deleted by the whole-set writer on every pass. The seed-base union
rescues exactly one base: the one the designated captor happens to be docked at.
The same applies to agent bases holding only credits, which makes
`agent_storage.credits` similarly reliable only for the seed base.

There is no cheap fix from the current wire surface: `FactionInfoResponse.OwnedBases`
is an `int` count, not a list, so there is nothing to union against. Closing this
needs a base-enumerating call the client does not have. **Do not read these two
columns as fleet-wide coverage.**

**A persistently failing per-base query is invisible to the coverage panel.**
When one base's `ViewStorageAt` fails, its previously stored rows are carried
forward — but `ReplaceStorage` stamps every row it writes with `now`, and
`StorageBase` carries no per-base `CapturedAt` to preserve. So a base that has
failed every day for a month reads as "captured minutes ago" forever, and the
coverage query can never flag it. The parent spec's promise that partial capture
is "recorded honestly" does not hold on this one path. Fixing it needs a per-base
`CapturedAt` on the struct plus preservation in the writer.

**The coverage panel will latch red once any agent is retired.** The alarm is
`stale > 0 || age > cadence*2`. Nothing prunes rows for agents that leave the
fleet, and a retired agent's frozen row satisfies *both* terms permanently — so
the first retirement turns the panel red for every source that agent has rows in,
and it never recovers. Note this carefully: an earlier draft of this note blamed
only `oldest`, and a fix addressing only `oldest` would not work, because `stale`
latches on the same rows. The fix belongs in `pkg/ovdash`, which already has the
fleet roster, rather than in `pkg/assets`, which deliberately has no such
dependency. This matters more than it looks: the panel is also the stated
mitigation for the captor-liveness caveat below, so it must be fixed first or that
mitigation is meaningless.

**Captor liveness is not monitored.** A faction's designated captor is the
lowest `player_id` among members with a fresh *profile*. Freshness proxies "this
worker's `CaptureProfile` is running", not "its `CaptureFaction` is succeeding".
A captor whose faction capture silently fails while its profile keeps refreshing
stalls that faction's ledger indefinitely, and no other member steps in.

**One narrow duplicate-fetch hole remains in the sweep.** The skip-if-observed
guard tests the *requested* base id while a successful fetch marks the *response's*.
Under a persistent id-form mismatch combined with a hint that lists the same base
twice, the base is fetched twice and appended twice, violating the primary key and
erroring that capture. Both preconditions are unobserved in the wild, and this is
strictly better than the pre-fix state (which had no guard at all). Test coverage
for the id-form fix is also asymmetric: it exists for storage, not for faction, and
no test constructs a successful response whose `base_id` differs from the request.
