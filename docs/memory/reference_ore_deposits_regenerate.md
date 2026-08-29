---
name: reference_ore_deposits_regenerate
description: "Deposits REGENERATE — poi_resources.remaining is a standing pool, not a lifetime budget. Round-trip time beats deposit size when picking a mining site."
metadata:
  node_type: memory
  type: reference
---

**Deposits refill.** Operator-confirmed 2026-08-22, observed live: `wanderers_veil`
(struve_1321, energy_crystal) read `remaining` 255, then **289 forty minutes later**.

**What this changes:** `poi_resources.remaining` is a **standing pool**, not a lifetime
budget — it is what has accumulated since the site was last worked, capped by the site.
Neither "biggest number" nor "nearest site" is the right selector on its own; see the
per-jump comparison below.

⚠️ **`poi_resources` is an ATROPHIED CODE AREA** (operator, 2026-08-22) — little miner work
in months, which is why survey ages range from hours to 195 days and why
`resource_history` was never wired. Treat every row as suspect until re-surveyed.

**Rate ≈ 1 unit/tick, SCALED BY THE SITE'S RESOURCE SPLIT** (operator estimate, and it
checks out numerically). `wanderers_veil` energy_crystal was read three times in one
session: **255 → 289 → 338**, i.e. ~83 units over ~300 ticks = **~0.28/tick**. Crystal is
**33% of that site's richness** (21 of 63 across iron 24 / crystal 21 / copper 18), and
1 × 0.33 = 0.33 — within noise. So per site:

    regen_per_tick(resource) ≈ resource_richness / sum(all richness at that POI)

At 10s/tick that is `share × 8,640 units/day`. A 33% share yields ~2,850/day; a 6% share
yields ~500/day. **Check the split before valuing a site** — a nebula whose crystal
richness is 3 against iron 25 and copper 22 is a 6% site and will trickle.

**⚠️ A LOW `remaining` AT A NEARBY SITE MEANS COMPETITION, NOT SCARCITY — and a STALE
reading at a REMOTE site is probably an UNDERSTATEMENT, not an overstatement.** Deposits
refill while unvisited, so 124-day-old `forgotten_prism` at 5,000 is likely still ~5,000;
whereas `wanderers_veil` reads 338 *because* it is 8 jumps from haven and gets worked. The
tell: at the near sites iron and copper sit at ~99,000 of a ~100,000 cap while crystal sits
at 54–338. That is a resource being taken, not one that is absent.

Consequence for site choice — compare **units per round-trip jump**, not raw remaining:
`wanderers_veil` ≈ 338/16 jumps ≈ 21/jump; `forgotten_prism` ≈ 5,000/36 ≈ 139/jump.

**The rate is otherwise UNRECORDED.** `resource_history` has **0 rows** — the
table and its writer exist, nothing calls it. Same dead-write-path shape as
[[reference_ship_modules_never_captured]]. To learn the rate: survey a site, mine it out,
re-survey next visit.

⚠️ **`poi_resources` survey data is wildly uneven in age.** Check `last_updated_tick`
before trusting any row — for energy_crystal the two largest entries (`forgotten_prism`
5,000 and `the_quiet_shimmer` 5,000) were **124 and 82 days stale**, while every site
surveyed within 30 days read **25–289**. Both stale rows read exactly `5000`, which looks
like a survey-time cap rather than a measurement. Seven fresh sites totalled 615 units.

```sql
SELECT p.system_id, r.poi_id, CAST(r.remaining AS INT) rem, r.richness,
       r.last_updated_tick, r.detected_by
FROM poi_resources r JOIN pois p ON p.id=r.poi_id
WHERE r.resource_id='<ore>' ORDER BY r.last_updated_tick DESC;
```

Related: [[project_fleet_drone_refit]] (needs 3,607 energy_crystal) ·
[[reference_gameclock_forward_drift]] · [[project_mining_fleet]]
