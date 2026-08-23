# Mining Site Contention — Design

**Goal:** rank resource sites by how heavily *other* fleets are working them, so our
harvesting targets uncontested deposits instead of competing for picked-over ones.

**Status:** design agreed 2026-08-23. Not yet implemented.

---

## 1. The problem

Choosing a mining site by `poi_resources.remaining` gets it backwards, and does so
convincingly enough to fool a careful reader. A worked example from 2026-08-22:

| site | crystal `remaining` | correct reading |
|---|---|---|
| `wanderers_veil` (8 jumps from haven) | 338 | **being farmed** — low because it is close and contested |
| `forgotten_prism` (18 jumps) | 5,000 | **untouched** — high because nobody goes there |

The tell that the near sites are contested rather than poor: at those same POIs, iron and
copper sit at ~99,000 of a ~100,000 cap while energy_crystal sits at 54–338. That is a
resource being *taken*, not one that is absent.

A single reading cannot distinguish "low because contested" from "low because poor", and
`remaining` alone ranks the contested sites first. We need the derivative, not the level.

## 2. The model

Deposits regenerate at approximately **1 unit/tick × the resource's share of the site's
total richness** (operator estimate; corroborated live — `wanderers_veil` crystal read
255 → 289 → 338 in one session, ~0.28/tick against a 33% share predicting 0.33).

```
share          = resource_richness / sum(richness of all resources at that POI)
expected_gain  = min(share × elapsed_ticks, cap − remaining_prev)
actual_gain    = remaining_now − remaining_prev
contention     = expected_gain − actual_gain − our_own_extraction
```

`contention` is units per interval taken by everyone else. Normalised by `elapsed_ticks`
it becomes a rate comparable across sites observed at different cadences.

### The three states that matter

| observation | meaning | action |
|---|---|---|
| at/near cap, flat | uncontested and full | **prime target** |
| low, flat | extraction ≈ regeneration — someone is farming it | avoid |
| low, rising | recently worked, recovering | revisit later |

Distinguishing the first two requires knowing the site cap, since both look flat.

### Cap inference

No API field gives a cap. Infer it as **the maximum `remaining` ever observed** for that
(poi_id, resource_id). This is a lower bound that tightens over time, and it is
self-correcting: a site observed at a new high simply raises its own cap.

Observed caps cluster suggestively — common ores at ~100,000, energy_crystal at 5,000,
quantum_fragments 2,000, phase_crystal 1,500 — so a per-resource default may emerge later.
Do not hardcode one now.

## 3. Data sources

Two of the three inputs already exist.

| input | source | status |
|---|---|---|
| regen baseline (`share`) | `poi_resources.richness` | ✅ present, 2,075 rows |
| our own extraction | `action_log_events` where `event_type='mining.yield'` | ✅ present, 6,577 rows |
| `remaining` over time | `resource_history` | ❌ **0 rows — the only gap** |

### Our own extraction is already captured and joins cleanly

`mining.yield` events carry `resource_id`, `quantity`, `ticks`, `system_id` and `poi_name`
in `data_json`, timestamped by `created_at`. **All 6,613 events with a `poi_name` resolve
to a `pois.id` — 100% join coverage**, so this path has none of the id drift that afflicts
`public_facilities` and `agent_hulls.location_base_id`.

Subtracting our own extraction is what makes the metric measure *others* rather than
total demand.

## 4. Why `resource_history` is empty

`RecordResourceState` (`pkg/knowledge/analytics.go:12`) exists and is unit-tested. Nothing
in production calls it — only mocks.

Worse, the read side is live: `pkg/agentstate/refresh.go:40` calls `GetResourceHistory`
and has been silently receiving nothing. This is a reader wired to a writer nobody calls,
not merely a dormant table.

`poi_resources` itself is refreshed by the **`explore` / `auto-explore`** commands, which
land in `RememberPOI` (`pkg/knowledge/sqlite.go:335`). That is the only production writer,
and it explains the survey-age spread of hours to 195 days: a site's data is exactly as
fresh as the last time an explorer passed through. `poi_resources` is an **atrophied code
area** — little miner work in months.

## 5. Approach

**Hook the history write into `RememberPOI`'s per-resource loop**, inside the existing
transaction. Every observation the explorer fleet already makes becomes a history row, at
no additional game cost — no new commands, no new traffic, no fleet redeploy beyond the
usual rollout.

Three properties the hook must have:

1. **Append every observation, including ones the upsert discards.** `RememberPOI`
   deliberately refuses to let a weaker or older scan overwrite `remaining` (it gates on
   `excluded.last_updated_tick >= poi_resources.last_updated_tick`). History has no such
   concern — an older observation is still a real datapoint at its own tick, and the
   analysis orders by tick regardless.
2. **Deduplicate on `(poi_id, resource_id, game_tick)`.** Repeat scans within one tick
   would otherwise inflate row count without adding information.
3. **Never fail the POI write.** A history append that errors must not roll back an
   exploration capture. Best-effort, matching how the facility and asset captures behave.

The analysis then joins history against itself (consecutive observations per site), the
richness split, and our own `mining.yield` sum over the same window.

## 6. Deliverable

A ranking a mining dispatcher can consume, surfaced as a `play_as` command alongside the
existing mining tooling:

```
contention [resource_id] [--near <system>] [--max-jumps N]
```

Columns: site, system, jumps, `remaining`, inferred cap, `% of cap`, regen/day,
contention/day, observations, age of newest. Sorted by uncontested capacity, which is what
the caller actually wants — not by `remaining`.

## 7. Known limits

1. **It produces nothing from one observation.** Two are needed per site to compute any
   delta, and the interval must be long enough for regeneration to exceed rounding. Wiring
   the capture is therefore urgent even though the analysis can follow later — every day
   unrecorded is baseline that cannot be recovered retroactively.
2. **Resolution is bounded by explorer cadence.** A site nobody visits produces no history,
   so contention is unknowable exactly where data is thinnest. This measures *visited*
   sites well and says nothing about the rest.
3. **The regen constant is an estimate.** ~1 unit/tick × share fits one site's three
   readings. Accumulated history is what will confirm or correct it — and the model should
   be written so the constant is easy to re-fit, not baked into a query.
4. **Our own fleet mining a site pollutes its own reading** if extraction attribution is
   wrong. The `mining.yield` join is exact today; if drone-driven yields ever land without
   a `poi_name` (drone yields do carry `drone_id` — see the v0.531.4 `kind` discriminator),
   that assumption needs rechecking.
5. **Cap inference is a lower bound.** A site never observed at full will read as
   permanently contested until it is. Acceptable — it fails toward "look elsewhere", which
   is cheap, rather than toward sending a fleet to a picked-over site.

## 8. Relationship to the drone refit

[[project_fleet_drone_refit]] Phase 1 needs 3,607 `energy_crystal` and currently picks
sites by hand from a table whose two largest entries are 124 and 82 days stale. This is the
tool that would make that choice evidence-based. It is a separate feature with its own
value — every future mining campaign wants it — so it gets its own spec rather than being
folded into the refit.
