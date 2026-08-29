---
name: reference_mining_yield_is_random_from_site_mix
description: "A `mine` command returns a RANDOM resource from the site's mix — you cannot target an ore. The richness share therefore sets both the regen rate AND the fraction of your yields that are the ore you want."
metadata:
  node_type: memory
  type: reference
---

**You cannot mine a specific ore.** Each `mine` command yields a random resource from
whatever the POI carries (operator, 2026-08-22). So a site's richness share governs
**two** things at once:

    share = resource_richness / sum(richness of all resources at that POI)

1. **Regeneration** ≈ `1 unit/tick × share` — see [[reference_ore_deposits_regenerate]]
2. **Yield fraction** — roughly `share` of your `mine` actions return that resource

That double effect makes high-share sites far better than their `remaining` suggests, and
low-share sites nearly useless for a targeted campaign. For energy_crystal:

| site | crystal share | ~1 crystal per N mines |
|---|---|---|
| `wanderers_veil` | 33% | 3 |
| `forgotten_prism` | 24% | 4 |
| `garnet` / `botein` / `pherkad` / `ruchbah` | 4–7% | 15–25 |

**Practical consequences:**
- Cargo fills with the *mix*, not the target. Effective target-per-trip ≈ `cargo × share`
  unless you jettison the rest at the site and keep mining.
- Jettisoning the unwanted fraction converts the limit from *cargo* to *site pool + time*,
  which is what makes a big remote deposit worth the trip.
- **Check the split before valuing a site**, always:

```sql
SELECT resource_id, richness, CAST(remaining AS INT)
FROM poi_resources WHERE poi_id='<poi>' ORDER BY richness DESC;
```

**ORE COMPRESSION AND YIELD BONUSES LIVE IN `ships.inherent_capabilities`** — a JSON
array of `{Type, Value}`, NOT a top-level column. We already capture it and have never
parsed it. The top-level `*_cargo_efficiency` fields on `serverapi.Ship` read **0 for every
class in the 335-entry catalog and for every live get_ship we have**, so they are the wrong
place to look. 55 classes carry a real capability:

```sql
SELECT id, cargo_capacity, utility_slots, piloting_required, inherent_capabilities
FROM ships WHERE inherent_capabilities LIKE '%ore_cargo_efficiency%';
```

Two capabilities matter and they compound:
- **`ore_cargo_efficiency: 50`** = 50% space usage → **2x effective ore capacity**
- **`ore_yield_bonus: 10..35`** = more ore per `mine` action, which matters doubly given
  yields are random from the site mix

`effective_ore_capacity = cargo_capacity * 100 / ore_cargo_efficiency`

⚠️ **NOT ONE HULL THE MINING FLEET FLIES HAS ANY ORE CAPABILITY** (2026-08-22):
`excavator`, `drillship`, `prospector`, `cobble`, `theoria` all have none — zero
compression, zero yield bonus. Neither do `deeprock_harvester` / `mining_cruiser`. Those
six are **synthesised by `withLegacyShipClasses`** and absent from the real 335-class
catalog, which is why they look like plausible mining hulls and are not. **Do not rank
mining hulls by `cargo_capacity`** — rank by effective capacity + yield bonus.

| hull | cargo | eff | yield | effective | piloting | tier |
|---|---|---|---|---|---|---|
| `paydirt` | 2160 | 50 | +15 | **4320** | 30 | 3 |
| `pithead` | 1800 | 50 | +20 | 3600 | 30 | 3 |
| `lithosphere` | 1680 | 50 | +25 | 3360 | 30 | 3 |
| `ravager` | 1440 | 50 | **+30** | 2880 | 30 | 3 |
| `deep_survey` | 750 | 50 | +15 | **1500** | 20 | 2 |
| `bonanza_king` | 600 | 50 | +20 | 1200 | 20 | 2 |
| `siege_breaker` | 450 | 50 | **+35** | 900 | 20 | 2 |
| `workhorse` | 300 | 40 | +10 | 750 | **0** | 1 |

Miners had piloting 28-29 in 2026-08, so tier-2 (piloting 20) is open and tier-3
(piloting 30) is 1-2 levels away — miner-1 was at 29. Catalog `price` is 0 for all of
them (commission via `shipyard_tier`), but some appear in `ship_listings`: 2026-08-22 had
`siege_breaker` at ironhearth_station 411,045 and `deep_survey` at nova_terra_central
569,484. We owned exactly one `bonanza_king` (idle) and one `bedrock`.

Related: [[project_mining_fleet]] · [[project_fleet_drone_refit]]
