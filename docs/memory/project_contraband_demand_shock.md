---
name: project_contraband_demand_shock
description: Contraband bids rose 17-188x from 2026-10-17 and were still climbing on 10-22. The BUY BOOK UNDERSTATES REAL DEMAND ~100x — craftsman-1 cleared 881 moonshine in one HOUR at 1500. We own 5 contraband lines at Voss.
metadata:
  node_type: memory
  type: project
  originSessionId: 2d7bbf54-7b88-4de5-ba93-9477eb1cb891
  modified: 2026-10-22T08:07:51.376Z
---

## ⭐🔴 The buy book is a FLOOR, not a ceiling

I twice sized this off standing buy orders and was wrong both times. Posting an
**ask** discovers demand the bid book never shows.

Proof, craftsman-1's own sell orders at Treasure Cache (`market_ohlcv`,
side='sell' — OHLCV is not pruned, `market_orders` is):

```
10-19T07   902 units @1500
10-19T08    21 units @1500      <- 881 CLEARED IN ONE HOUR
10-18T02   350 @1500 -> 10-18T03  70    <- 280 in one hour
```

~3,000 units sold over a few days at one station, against a **galaxy-wide
standing bid book of 814 units**. So production is the constraint, not demand.

Also beware **1-credit junk walls**: Voss shows ~50,000 units bid for every
contraband item at 1cr. Real Voss depth for ghost_rounds is **21 units @71,330**.
Always band by price before summing depth.

## The spike (VWAP, buy side, `low_price>=100`)

| item | 10-11 | 10-22 | × |
|---|---|---|---|
| ghost_rounds | 383 | 72,128 | **188×** |
| starshine | 625 | 27,650 | 44× |
| gutter_flux | 137 | 2,848 | 21× |
| pirate_moonshine | 105 | 1,786 → top bid 5,655 | 17×+ |

Started ~10-17 and still rising on 10-22. Depth thins as price climbs
(starshine standing units 1,204 → ~50), i.e. buyers lifting an empty book.

## What we own — FIVE contraband lines at Voss Redoubt (Alhena, lawless)

| facility | recipe | output |
|---|---|---|
| Unmarked Munitions Line | `forge_ghost_rounds` | 2 hot_cell + 3 titanium_ingot → 2 |
| Bootleg Cell Shop | `crack_hot_cells` | 4 copper_wiring + 6 plasma_gas → 4 hot_cell |
| Crystal Infusion Still | `infuse_starshine` | 1 **energy_crystal** + 3 moonshine → 1 |
| Backwater Still | `distill_moonshine` | 10 water_ice + 2 pharma_culture → 6 |
| Backstreet Chem Lab | `cook_gutter_flux` | 8 chlorine_compound + 12 chlorine_gas → 8 |

Five of the eight contraband items in the game. Missing: nerve_burn, void_dust.
**Do not craft starshine** — it burns [[project_drone_project_crystal_reserve]]
crystals and we hold 1,059 against thin demand.

Upstream: `run_pharmaceutical_lab` = 3 purified_water + 1 liquid_nitrogen → 3
culture, at **6 public labs @28/run** (Starfall, Sirius, **Sol**, Central Nexus
L2 @660, +2). `synthesize_purified_water` = 4 hydrogen_gas + 2 oxygen_gas → 2.
**purified_water is buyable at 37 with 50,006 on offer** — skip the gas step.

## Selling: strongholds have NO skill gate

Empire sales need **smuggling ≥ 5 × station market tier** (we store tier
NOWHERE — a capture gap). **Strongholds and most player-faction stations are
always sellable.** Our haul fleet (hauler-0, salvager-1..10, trader-1/2/10) and
six stronghold-resident marketbots (algol/Dross, bellatrix/Crix,
gliese_581/Korr, sheratan/Thane Keep, xamidimura/Kael, barnard_44/Sable) all
hold pirate baseline 10. Empire premium is only ~13% (80,460 vs 71,382).

**But `resident_market` is `ensure_home; dock; refuel` — residents NEVER post
asks.** The 100× channel has no fleet implementation. That shopkeeper behaviour
is the single highest-value missing piece.

## Zaniah / Mera Sanctum (2026-10-22)

Bids **5,000 × 2,868 pirate_moonshine (14.3M)** and **3,000 × 3,263
titanium_ingot (9.8M)** — ~25.9M at one station. Caveats: Zaniah is a **kill
zone** (15 of 22 haulers lost hulls there + goldcrest), Mera Sanctum is
**wrecked** (no refuel/repair — arrive with fuel to leave), and it is
`mera_sanctum` in market.db but `mera_sanctum_station` in the KB.

**Realized 2026-10-22:** "Sold 756x Pirate Moonshine for 3780000 credits from
Marlowe" — the 5,000 bid is REAL and filled (3.78M, nearly all the 798 on
hand). ~2,112 units of that bid were left after this fill, if no one else took them.

## Stock on hand (2026-10-22)

water_ice 296,963 · hydrogen_gas 169,254 · titanium_ingot 6,844 (6,003 at
Crimson War Citadel) · hot_cell 7,774 (6,609 Treasure Cache, 202 Voss) ·
liquid_nitrogen 12,394 · oxygen_gas 10,411 · purified_water 3,844 ·
starshine 1,059 · moonshine 798 · pharma_culture 510 · ghost_rounds 123 ·
gutter_flux 0.

Ghost rounds need only **titanium imported** — the Bootleg Cell Shop makes its
own hot_cell. 1,101 ingots from Krynn → 734 rounds.
