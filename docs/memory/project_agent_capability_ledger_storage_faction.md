---
name: project_agent_capability_ledger_storage_faction
description: "Slices 5-6 of the agent capability ledger (storage + faction + flap fix + ovdash panel) BUILT and reviewed on feat/agent-capability-ledger, 2026-08-06 — unmerged, unpushed, inert. Six known limitations recorded in the spec."
metadata: 
  node_type: memory
  type: project
  originSessionId: db74e972-dd15-4cd6-9408-f974d4fa7975
  modified: 2026-08-06T20:49:33.936Z
---

**STATUS 2026-08-06: ALL 11 TASKS BUILT, EVERY TASK REVIEWED, FINAL WHOLE-BRANCH
REVIEW CLEAN (0 Critical, no merge blockers). NOT MERGED, NOT PUSHED.** Branch
`feat/agent-capability-ledger`, worktree
`.claude/worktrees/feat+agent-capability-ledger`, 20 commits this run on top of
the 19 from slices 1-4. Still **inert until a worker gets `--assets-db-path`**.

Spec: `docs/superpowers/specs/2026-08-06-agent-ledger-storage-faction-design.md`
Plan: `docs/superpowers/plans/2026-08-06-agent-ledger-storage-faction.md`

**What shipped.** A read layer (`LoadProfile`/`LoadCarrier`/`LoadHulls`/`LoadStorage`
+ `BaseResolver` + `NewBaseResolver`); the eligibility **flapping fix** (capabilities
now fall back to stored rows, so one dropped frame no longer flips an agent to
ineligible, and `CarrierKnown`'s doc comment is finally true); `agent_storage`
(+`_items`); `faction_profile`/`faction_storage`(+`_items`); `capture_storage` and
`capture_faction` wired into the worker, `play_as`, and `roles.yaml` (daily, on all
6 roles that carry `capture_profile`); and the **ovdash panel that slices 1-4
specified but never built**.

**⭐ THE LIVE CANARY ANSWERED THE WIRE QUESTIONS — reuse this before planning.**
Run `play_as` built from the worktree with cwd = main repo,
`--debug=1 --debug-full-payload=true` (the pairing is required; `--debug-full-payload`
alone emits nothing).
- **The `view_storage` hint is AGENT-GLOBAL, not per-base.** Querying a station
  where the agent holds nothing still returns the full list of where it does. So
  base discovery is ONE seed call from anywhere — no docking, no per-station loop.
- **The hint does NOT truncate.** craftsman-1 returned all 20 bases in full; the
  `...` in the 2026-08-01 design doc was the transcriber's elision.
- **`"No items in storage at any station."` is a sentinel.** The spec's own parse
  recipe turns it into a base named `any station.`, which would be queried AND would
  suppress the deletion that should clear the agent's stale holdings.
- **Raw cache keys are `storage` / `faction_storage` / `faction_info`** — NOT the
  command names. [[reference_rawjson_key_drift]]
- **Alias trap, measured on real storage:** only **8 of craftsman-1's 20** storage
  bases resolve against `pois.id`. A naive join drops 12 of 20. [[reference_station_id_aliases]]
- **craftsman-1 is OFF-FLEET and safe to canary without a freeze** — it sits in
  `mission-learn-overrides.json`'s `removed` list and has no worker process.

**⭐🔴 SIX KNOWN LIMITATIONS ARE WRITTEN INTO THE SPEC** (appended 2026-08-06,
commit `d667e43`). Read that section before trusting any column. The two that
matter most:
1. **`faction_storage.fuel_reserve` and `agent_storage.credits` are only reliable
   for the base the capturing agent is DOCKED AT.** The hint enumerates bases with
   ITEMS, so a bunker-only or credits-only base is invisible, never swept, and
   DELETED each pass; the seed-base union rescues exactly one base. No cheap fix —
   `FactionInfoResponse.OwnedBases` is an int count, not a list.
2. **The coverage panel will latch permanently red the first time any agent is
   retired.** The alarm is `stale > 0 || age > cadence*2` and a retired agent's
   frozen row satisfies BOTH forever. A fix addressing only `oldest` would ship and
   still not work. Belongs in `pkg/ovdash` (has the roster), not `pkg/assets`.

**⭐ THE REVIEW LOOP EARNED ITS COST — one real data-loss bug, found by a reviewer
that BUILT the scenario and ran it.** `CaptureStorage` handed a whole-set writer a
set that omitted the agent's own docked base whenever the hint didn't name it, so a
credits-only dock was DELETED while its decoded contents sat in the seed response.
The bug was in the PLAN, not the implementation. Fixed by a seed-base union guarded
on non-empty (so an emptied dock still converges to zero), then **proactively carried
into `CaptureFaction` before it was dispatched** — where it bites harder, because a
fuel bunker with no items is entirely ordinary.

**Lesson that generalizes: read the target file before writing the brief.** Five
plan defects were caught pre-dispatch by checking reality (wrong test-fake names;
`MIN()` over zero rows returns a NULL row not `ErrNoRows`; the frontend uses
Tailwind not semantic CSS; `node_modules` is per-worktree). Each would have cost a
debugging round. **`.superpowers/` reports are gitignored — a reviewer's findings
vanish with the workspace unless you commit them somewhere real.**

**Still open / next:** the six limitations above (the panel-latch one first, since
it gates the captor-liveness mitigation); `spread:` scheduler jitter (all daily
captures fire at the same UTC midnight — judged safe, but unbatched); a read API
(this is still **substrate, not answers** — "what can we source for free" remains a
hand-written `sqlite3` query); rollout is canary → one live worker → fleet.

Related: [[project_agent_capability_ledger]] · [[project_fleet_role_interchangeability]] · [[project_fleet_asset_snapshots]]
