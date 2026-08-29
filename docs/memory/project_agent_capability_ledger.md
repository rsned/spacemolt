---
name: project_agent_capability_ledger
description: "pkg/assets agent capability ledger BUILT on branch feat/agent-capability-ledger (16 commits, unmerged) — identity/skills/standings/carrier/hulls + eligibility. Two operator decisions still open."
metadata: 
  node_type: memory
  type: project
  originSessionId: 2671530b-761a-4dbe-b378-62f725016c20
  modified: 2026-08-02T16:37:16.825Z
---

**STATUS 2026-08-01 (operator away several days; resume here): BUILT, CANARY-
VALIDATED ON LIVE AGENTS, NOT MERGED, NOT DEPLOYED.** Branch
`feat/agent-capability-ledger` at `3fab0a5`, worktree
`.claude/worktrees/feat+agent-capability-ledger`, 19 commits.
All 19 commits pushed to origin (`f344422..3fab0a5`, 2026-08-02); nothing
outstanding locally.
Green uncached at handoff: `go build ./...`, `pkg/game` 114s, `pkg/worker`
152s, `pkg/assets`, `golangci-lint` = 0 issues. **Inert until a worker gets
`--assets-db-path`** — merging changes no runtime behaviour.

**▶ NEXT STEP: a design pass for spec slices 5-6 (storage + faction), which the
operator asked for and which is the larger half of the original design.** Three
questions to settle first, all named and none answered:
1. Does `agent_storage` normalize base ids at WRITE time or READ time? (Write
   time gives `pkg/assets` a dependency on spacemolt-knowledge.db that the
   separate-DB design deliberately avoids.)
2. Faction assets — own tables, or a `holder_type` discriminator on the agent
   ones? Factions have credits, storage AND ships: the same three shapes.
3. How is the per-base sweep scheduled? craftsman-1 needs ~20 calls where most
   agents need 1.

Spec: `docs/superpowers/specs/2026-08-01-agent-asset-profile-design.md`
Plan: `docs/superpowers/plans/2026-08-01-agent-capability-ledger.md`
Ledger with every finding: `<worktree>/.superpowers/sdd/2026-08-01-agent-capability-ledger/progress.md`

**What it is.** Sub-project B of [[project_fleet_role_interchangeability]]. A
separate `data/assets.db` (blast radius, not size — market.db cost a recovery
day) holding `agents` (the three-way player_id/username/agent_id map),
`agent_profile`, `agent_skills`, `agent_standings`, `agent_carrier`,
`agent_hulls`, `agent_capability`. Workers self-capture hourly via a new
`capture_profile` dispatch command; `play_as` has it too.

**✅ HULL GUARD FIXED (`8de8c11`).** `HullsFrom` now returns
`([]Hull, bool, error)` mirroring `CarrierFrom`; ok=false means "raw empty,
nothing captured". Premise, from the operator: **an agent can NEVER own zero
ships — you must always hold at least one, and a destroyed last hull respawns
you in a Tier 0 starter.** So an empty decode is always a stale cache, never a
gone fleet. Falsified: reverting the guard reddens the new
`TestCaptureProfileHullsSurviveEmptyRawCache`.
Still open: does the same "cannot reach zero" rule apply to storage items at a
base, or faction storage? Matters for spec slices 5-6.

**⭐🔴 FIRST LIVE CANARY (databot, off-fleet, 2026-08-01) FOUND A REAL BUG IN
`pkg/game` ON RUN ONE — `agent_hulls` captured 0 rows for an agent that owns 2
ships.** Fixed in `42116dc`. **A live `list_ships` reply carries NO `action`
field** — `{"active_ship_class":…,"active_ship_id":…,"count":2,"ships":[…]}` —
but `storeRawJSON` only reached the `owned_ships` key through the action-based
switch (`case "list_ships"`). The payload always fell through to the generic
`hasShips` branch and landed under `"ships"`, so **`owned_ships` was empty from
the day it was added**. `cmd/tools/daily-summary` had already papered over it
with a read-side fallback to `"ships"` instead of fixing the key. Now
classified on `active_ship_id` (only an owned-fleet listing names the active
hull) and stored under BOTH keys, since `cmd/auto-trader` and daily-summary
read the generic one.
**This is the SECOND instance of the exact drift the `browse_ships` fix called
out** (ship listings dead 2026-02-18..2026-07-04): a content-based classifier
silently claiming a payload the action switch was written for. **Suspect any
other `GetRawJSON` key that is only reachable via the action switch.**
Lesson: my golden fixture *invented* the `"action":"list_ships"` wrapper the
server never sends, which is precisely why a fully green suite never revealed
the dead key. Fixtures must come from a captured live payload, never composed.

**Canary recipe (reusable, zero fleet risk).** Credentials live only in the
main repo, so build from the worktree and run with cwd = main repo:
`go build -o bin/play_as-canary ./cmd/tools/play_as`, then
`printf 'capture_profile\nquit\n' | bin/play_as-canary --assets-db-path <scratch>.db --debug=1 --debug-full-payload=true <agent>`.
**`--debug-full-payload` emits NOTHING without `--debug=1`** — that pairing is
what surfaced the raw frames. Off-fleet agents with credentials: databot,
prophet-1/2, random-clark, random-7, pirate-*, empire-crimson (architect-*/
spark-* have none). Verified post-fix against databot: identity, profile,
10 skills, 14 standings, carrier and 2 hulls all match the raw payloads
field-for-field.
**⭐🔴 REAL FINDING FROM THE SAME RUN — STATION IDS ARE DUAL-NAMED, AND THE
LEDGER STORES BOTH FORMS IN DIFFERENT COLUMNS.** databot showed
`docked_at_base=confederacy_central_command` but `current_poi=sol_central`.
**Those are the SAME PLACE** (operator: a legacy name they could not remove, so
both are live and used interchangeably) — I first misread it as a
docked_at_base semantics bug; it is not.
**The authoritative alias map already exists: `bases(id, poi_id)` in
spacemolt-knowledge.db — 15 rows where they differ.** The five empire capitals:
`confederacy_central_command`→`sol_central` (solarian),
`central_nexus`→`the_core` (voidborn), `frontier_station`→`mobile_capital`
(outerrim), `grand_exchange_station`→`grand_exchange` (nebula),
`crimson_war_citadel`→`war_citadel` (crimson). Plus all 7 pirate strongholds on
a mechanical `_station` suffix (`crix_stronghold_station`→`crix_stronghold`
etc.) and 3 hex-id player bases.
**Consequence for the ledger:** `agent_profile.docked_at_base` and
`agent_hulls.location_base_id` hold the BASE-id form, `current_poi` holds the
POI-id form, and slices 5-6 `agent_storage` will hold base-id. Joining any of
them against `pois.id` silently returns nothing for all 5 capitals and all 7
pirate strongholds — and `pkg/assets` has no FKs by design, so it will not
error, just quietly under-report. Normalize in the read layer via
`bases(id, poi_id)`; do NOT hand-roll suffix-stripping (2 of the 5 capitals are
genuine renames, not suffixes).

**🔴 TWO OPERATOR DECISIONS OPEN — this is where to resume.**
1. **Capability flapping.** `CaptureProfile` builds its `AgentSnapshot` only
   from *this pass*, so one transient `ShippingProfile`/`ListShips` failure
   recomputes eligibility as if never captured, though good data sits in the
   tables. Final review recommends **(b) fall back to stored values**:
   `CarrierKnown` is *documented* as "no debt vs never captured" but under (a)
   means "not captured this pass", so the comment is false; and the
   `LoadCarrier`/`LoadHulls` readers (b) needs are owed to spec slices 5-6
   anyway. Severity is lower than it sounds — rules degrade to honest
   `blocking_reason` strings, not a bare false, and nothing reads
   `agent_capability` yet. If the answer is (a), fix the doc comment instead.
2. **There is no dashboard panel.** The plan's Step 5 was titled "add the ovdash
   panel" but only ever specified the Go loader + `Snapshot` field. No React
   component was planned. Data reaches the browser as JSON and nothing renders
   it. Rollout step 4 ("watch the panel") cannot be performed.

**⭐ THIS BRANCH SHIPS THE SUBSTRATE, NOT THE ANSWERS.** `pkg/assets` has **no
read API** — nothing reads the tables back out. "Who has left freight
probation", "who is at smuggling L3" are answerable only by hand-written
`sqlite3` queries. Defensible v1 boundary, not a rewrite risk (the readers
slices 5-6 need are the same ones option (b) needs), but do not expect answers
from it as merged.

**⭐ CANARY VALIDATED ON 3 AGENTS (databot, prophet-1, craftsman-1), one shared
`assets.db`, all fields matching the raw frames.** craftsman-1's `player_id`
came back `a50924913cef881c5e4d14257589d9ba` — exactly the value the operator
pasted from a live `get_status` weeks earlier. Identity map proven with a real
`username != agent_id` case (prophet-1 = "The Prophet"). craftsman-1: 20 hulls
across 4 stations, carrier **1 successful delivery from `licensed`** (4 done,
delivered_value 67,276), smuggling L12 so that capability flips eligible.
**Hard number for the alias trap: 3 of craftsman-1's 4 station ids do not exist
in `pois` — a naive `JOIN pois ON id=location_base_id` returns 2 of 20 hulls
(10%)** and silently drops the rest.

**✅ REMOTE STORAGE READ WORKS — verified live, no docking needed.**
`Client.ViewStorageAt(ctx, stationID)` (`client_commands.go:732`, payload
`station_id`) — databot docked at sol_central returned
`base_id=grand_exchange_station`. play_as exposes it as
`storage --station_id <id>`. So the storage slice is: one `view_storage` for
the `hint` (which enumerates ONLY bases that actually hold items, so the call
count is holdings, not ~50 stations) → parse → one `ViewStorageAt` per base.
No travel, no per-station scheduling.
**Faction data confirmed present too:** `faction_info` returns treasury
`credits` + `member_count`; `view_faction_storage` returns a Faction Lockbox
facility; `faction_garages` exists in the API (no garage built yet).

**Deferred by design:** `agent_storage`/`_items` (the `view_storage` `hint`
parser + per-base sweep) and the faction tables — spec slices 5-6, a second
plan. Also `spread: true` scheduler jitter, ship-class capacity lookup (the
`freight` rule currently over-reports for small hulls because `OwnedShip` has
`CargoUsed`, not capacity), module/fitting capture, faction ship garages.

**⭐ FIVE DEFECTS IN MY OWN PLAN that review caught — none would have failed a
test run.** Reusable lesson: a passing suite proves nothing about a fixture
that decodes to zeros.
- `ShippingProfileResponse.Progression` (json `"progression"`) is NOT
  `TierProgress`/`"tier_progress"`. **A wrong JSON key decodes to an all-zero
  struct with NO error.** I fixed one fixture in self-review and missed a
  second; the implementer caught it.
- `haulMinCredits=20000` sat in a block advertised as mirrored from
  `pkg/worker` but has **no original** — `haul.go` has no credit floor at all,
  it gates on margin (`haulMinMargin=0.03`, `haulMinNetProfit=1000.0`,
  `haulSmallHoldNetProfit=250.0`). False provenance is worse than an openly
  arbitrary constant.
- A `len(hs) > 0` guard meant `agent_hulls` could never be **cleared**, so an
  agent that sold its last ship reported haul/freight eligible forever.
- The ovdash sketch **mutated an already-published snapshot** — the handler
  copies under RLock then marshals after RUnlock, so it raced. Fixed by
  mirroring the existing `s.acct` pattern.
- `Coverage.Stale` **summed ROWS while Agents counted DISTINCT agents**, so on
  the multi-row tables Stale could exceed Agents (15 skills = 15 "stale").

**⭐ Operational lessons worth keeping.**
- **A subagent's idle signal means "not running", NOT "finished".** Reviewers
  repeatedly went idle without sending their report; one implementer went idle
  having done nothing. Always verify against the tree/`git log`, and have
  reviewers **write reports to a file** as the reliable channel.
- **`(cached)` in a `go test` result is meaningless after a signature change** —
  use `-count=1`. A cached PASS nearly hid a real compile break.
- Editor diagnostics fire constantly mid-TDD (test lands before impl). Verify
  with `go build`/`go vet` before believing them.

Related: [[project_fleet_role_interchangeability]] · [[project_fleet_asset_snapshots]] · [[project_worker_storage_capture_gap]] · [[reference_deploy_verification]]
