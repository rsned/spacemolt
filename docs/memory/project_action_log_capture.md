---
name: project_action_log_capture
description: Action-log capture into assets.db (action_log_events + action_log_cursor) — SHIPPED f2775c1d on two canaries only; fan-out to the other 158 agents needs a schedule entry per agent AND a worker restart
metadata: 
  node_type: memory
  type: project
  originSessionId: 9acefb51-d097-42a4-bd75-27e0bb652f32
  modified: 2026-08-17T18:42:37.757Z
---

**SHIPPED 2026-08-17 (`f2775c1d`), LIVE ON TWO CANARIES ONLY.** `pkg/assets`:
`ActionLogFrom` → `InsertActionLogEvents` → `PruneActionLog`, driven by
`CaptureActionLog`, dispatched as the worker command **`capture_action_log`**.
Tables `action_log_events(player_id, event_id, event_type, category, created_at,
data_json)` and `action_log_cursor` in **assets.db** (not the KB).

**Why it exists:** the server keeps ~85 days and we kept none. Every other
`pkg/assets` table is current-state-only, so reconstructing what salvager-7 was
carrying when a leviathan killed it meant reading one log by hand.
[[reference_battle_log_api_replay_data]] · [[project_per_death_loss_capture]]

## The walk is `since_id`, and it must not start at 0
`get_action_log` accepts **`since_id`** and answers with **`next_since_id`** —
oldest-first, gap-free. Page numbers shift under new events so a page cursor
re-reads or skips; an id cursor cannot. **`since_id=0` means "normal
newest-first paging"**, which strands the walk on the newest page — start at
**1**. `GetActionLogResponse` was missing `since_id`/`next_since_id`/`event_types`
until this commit even though the server documented them.
Budget: `actionLogPollsPerRun=20` × `page_size=100` = 2,000 events per pass.

## Retention was measured, not guessed
craftsman-1's 45,016 entries (63 distinct types) are **87% three types**:
`other.rent_paid` 35.3%, `trading.exchange_fill` 32.6%,
`trading.buy_order_created` 19.1%. Then `navigation.jumped` 5.2%,
`session.login` 0.7%, `ship.refuel` 0.5%.
- **72h TTL:** jumped / refuel / login / buy_order_created.
- **Downsampled to one row per calendar day:** `other.rent_paid`.
- **KEPT IN FULL: `trading.exchange_fill`** — it *is* the cargo manifest behind a
  loss (item_id, quantity, price, role buyer/seller), and it doubles as per-leg
  haul P&L. Never add it to the TTL list.
Pruning runs **inline in the capture pass**, never as a daemon
([[reference_market_db_prune]]: the unsupervised one died and hit 62GB).
Live proof: salvager-7's first pass fetched 2,000 and kept **196**.

## Numbers: two distinct traps, both measured
`data` is a per-event union over 63 types, so it can only arrive as
`map[string]any` and is stored as flat `string->string` JSON (query with
`json_extract`).
- **Without `UseNumber`** a value past 2^53 has already lost its last digit
  before any formatting: `9007199254740993` → `...992`, unrecoverable.
- **`%v` on a decoded number gives exponent form**: `1200000` → `"1.2e+06"`.
  `json.Marshal` does *not* (it gives `1200000`), so the exponent risk is
  specifically `%v` — which was the obvious way to write that function.
Both pinned by tests that were verified to fail when the guard is removed.

## ⭐ IT ALREADY PAID OFF: salvager-7's death, fully reconstructed
Three passes in, the capture answered what the battle log could not — **which POI**.

- **`the_gold_crest`** (the gold-only belt). Arrived `02:59:49Z`, destroyed
  `03:00:09Z` — **20 seconds**.
- `combat.ship_destroyed` = `{cause: wildlife, ship_class: shard, system_id:
  goldcrest, wreck_id: b43e4019…}` — the event carries **no POI**, so the POI
  comes from the preceding `navigation.jumped`. That is the whole reason the
  jump rows are worth keeping.
- `combat.respawned` = `{lost_ship_id: 1bcf5db9…, new_ship_class: shard,
  respawn_poi: war_citadel, respawn_system: krynn}`.
- **It crossed Goldcrest THREE times in 35 minutes and died on the third.**
  02:25 arrived `the_gold_crest` (lived), 02:36 arrived `goldcrest_star`
  (lived), 02:59 arrived `the_gold_crest` (died). **Risk is per-arrival, not
  deterministic** — and the pass that arrived at the STAR is the one that was
  never at risk, matching the 0-creature reading at `goldcrest_star`.
- Fuel tells its own story: top-ups of **15, 17 and 3 units** while looping the
  same westmark→mebsuta→adhara→pipirima→gudja→gsc_0017→goldcrest circuit twice.

**⚠ The 72h TTL on `navigation.jumped` nearly cost this.** The death was 15
hours old when captured. A death noticed 4+ days late has no jump trail and the
POI is unrecoverable. **Proposed fix, NOT built: exempt `navigation.jumped` rows
within ±1h of a `combat.ship_destroyed` from the prune.**

## ⭐ WHAT IS LEFT: the fan-out
Scheduled **hourly** on **craftsman-1** (richest log) and **salvager-7** (the
death being reconstructed) — both haul, both confirmed writing. The other 158
agents have the code but **no schedule entry**.

**`data/agents/<id>/schedule.json` is read ONCE at worker start**
(`cmd/worker/main.go` → `worker.LoadScheduler`) — there is no reload, so adding
an entry needs a worker restart. Fan-out is therefore: add the entry to every
agent, then roll the fleets again. Entry shape:
`{"id": <max+1>, "frequency": "hourly", "command": "capture_action_log", "created_at": "<RFC3339>"}`

Also unbuilt: a read/query tool (only `Store.ActionLogEventsByType` exists), and
joining `combat.respawned.lost_ship_id` to a pre-death hull/cargo manifest, which
still needs append-on-change *cargo* history ([[project_fleet_asset_snapshots]]).

**Bonus already landing:** `tax.income_paid`/`tax.property_paid` (the
undocumented `tax.*` category), `combat.pirate_attacking`, and
`salvage.ship_recovered` — the GSA tow event [[reference_gsa_ship_recovery]]
listed as "not yet captured" — all arrive for free, category derived from the
event_type prefix.
