# Hull production costs

**Status:** for operator review, 2026-11-02
**Related:** the item production cost model (`docs/superpowers/specs/2026-11-02-item-production-cost-model-design.md`), built in the KB repo as `pkg/prodcost` + `cmd/generate-production-costs`. It is live, and its private page is `~/spacemolt/kb/private/production-costs/`. Also related: the KB build-cost matrix (`kb/build-costs`), which already prices ships by **buying** every material at market at each station.

## Why

CRFT now owns a Congregation license (bought 11-02 for 1M) and built its first hull at Conjunction Junction. The operator wants to know **how much the materials for a ship cost us to make**, for every hull, next to the other costs of building it and next to what buying would cost. The question it answers: is it cheaper to build this hull at a station of ours than to buy it?

Worked example from the conversation, which the page must reproduce:

| input | qty | how it's made | cost/u | total |
|---|---|---|---|---|
| aluminum_sheet | 50 | HEXC L1 at Sol (rented) | 8 | 400 |
| flex_polymer | 14 | hand | 0 | 0 |
| cargo_container | 12 | hand (36 sheet at 8 inside) | 24 | 288 |
| fuel_tank | 5 | hand (5 titanium_alloy at 37, BUSY L1 at The Obsidian Well) | 37 | 185 |
| hull_plating | 3 | hand (3 titanium_alloy at 37) | 37 | 111 |
| **materials** | | | | **984** |

Labor 1,000 (quoted). Royalty under the operator's rule, 10% × 984 = 98. Make total about 2,082. The same materials at the cheapest asks: about 6,329. Cheapest Congregation listing seen in the last 3 days: 12,202.

## The cost rules (operator)

- **Material make cost** uses the item engine's numbers and rules unchanged. Raw resources cost 0. Hand crafting costs 0. Every facility run pays labor, plus the fee when the facility isn't ours. Each material is costed on its cheapest route.
- **Time costs are out.** Mining, crafting and hauling time are assumed to be amortized ahead of time. The page shows credits only.
- **Royalty = 10% of the computed material make cost.** That's the number above, not the server's market-based figure. The server actually charged 723 on the Congregation; the page notes that the server's royalty differs, but the column uses the rule.
- **Labor is assumed to be a fixed amount per ship tier.** Known: tier 1 = 1,000 (Congregation, quoted twice at Conjunction Junction). The other tiers show "unknown" until a quote at that tier has been seen.
- **License:** empire and pirate hulls can only be built at a station of ours if the faction holds that design's license. Per the server's `buy_ship_license` notes (`get_commands.json`): cost comes from the treasury by `tier` (T1 1M, T2 5M, T3 10M, T4 20M, T5 50M); pirate hulls can be licensed (their royalty is split among pirate station managers); starter, prestige and pirate boss flagships cannot. Show whether CRFT owns the license. Owned today: congregation only. The license is shown as information and is **not** added into the per-hull make total.
  - **"Tier" means the catalog `tier` field** everywhere on this page (license cost and the labor bucket), not `shipyard_tier`. Check: congregation is tier 1 → 1M, and frankenhauler tier 4 → 20M; both match the server's refusals.
  - **The License column has these states:**
    - owned
    - license T*n* cost (not owned), for empire and pirate hulls
    - not licensable, for the 12 faction-less hulls (all carry a `prestige_lock`), for starter hulls and for pirate boss flagships
    - not buildable, for the 5 tier-0 `reclaim_only` hulls (cobble, prospect, shard, theoria, threshold), which also get no labor
  - These hulls are still costed and listed, so the materials number stays visible.

## Costing every hull: flagged steps

Every hull gets a make cost, even where the item pages' private rules would leave a material partial. Steps that need something special are **costed and flagged**, not dropped:

1. **Energy crystal recipes are allowed here.** The crystal reservation only limits CRFT's own crafting; nobody else has it. Steps that use energy_crystal are costed normally and flagged "uses energy crystal". This touches 142 hulls (power_core, phase_matrix, void_condensate, neural_interface_gel and everything made from them).
2. **Facility-only steps with no usable line** (no empire or faction rental captured, no CRFT facility) are costed at the catalog's L1 own-route labor and flagged "facility not built". Example: capital_ship_frame, 103 hulls. Pirate-base-only facilities get the same treatment plus the "pirate base only" tag, as on the item pages.
3. **Salvage is flagged and left unpriced.** salvage_components, salvage_metal and rare_salvage have no recipe, and 42 hulls list them **directly** in `build_materials` (33 of them pirate hulls; checked in `catalog_ships.json` dated 11-01). The operator expected no hull to need salvage, so this is the catalog as of 11-01. Those hulls show their make cost as a floor and are marked "needs salvage". Recipes with a salvage or wildlife alternative always also have a non-salvage, non-wildlife recipe, so the cheapest-route choice must prefer the complete one. Salvage should only surface where a hull lists it directly.

Before these rules, 153 of 370 hulls costed completely. After them, all hulls except the 42 that list salvage directly should cost completely.

## What gets built

**A separate private page for hulls**, next to the item production-cost page, linked both ways. Not published to the KB site.

**One table, one row per hull (all 370).** Columns:

- Hull · Tier · Empire (catalog `faction`) · Cargo
- Materials (make cost) · Labor · Royalty · **Make total** (materials + labor + royalty)
- License: owned / not owned + its tier cost
- Materials at market: each material at its cheapest ask (walking book depth, as the item page's buy-now does), summed. Mark it when a material has no ask.
- Cheapest listing: the lowest finished-hull price in `ship_listings` within the last 3 days, plus how many were listed.
- Flags: uses energy crystal · facility not built · needs salvage · labor unknown
- Sortable by any column.

**Each row expands** to show the hull's materials, as in the worked example: qty, how it's made (the route label), cost/u, total, flag, and that material's cheapest ask with station and jumps from home. Each material links to its row or tree on the item production-cost page.

**The make total is boxed** when it's lower than both the market materials total and the cheapest listing.

## Labor quotes: capture from now on

Quotes depend on where they're asked. A shipyard only quotes hulls it's licensed for, so learning every tier's labor means quotes from empire capitals and strongholds, and nobody will make trips just for that.

- The game client (spacemolt repo) **records every `commission_quote` and `commission_ship` response** any agent gets into a KB table. Today they're only kept in memory for play_as formatting (`pkg/game/client.go`, the `can_commission` + `ship_class` detection).
  - **Fields:** ship class, tier, station, labor, royalty, material_cost, provide_materials_total, yard_margin, shipyard_tier_here, mode (quote / commission), tick. Also keep the raw reply.
  - **Labor is the `labor_cost` field.** In the Congregation quote, `labor_cost` = 1000, and `provide_materials_total` 12,975 = labor 1,000 + `yard_margin` 11,975. So the yard fee is already separate and `labor_cost` is pure labor at any yard.
  - **Royalty has no structured field.** It appears only in text: the quote's `blockers`/`message` ("treasury pays 1000 labor + 734 royalty") and the `commission_ship` `message` ("the treasury paid 1000 labor + 723 royalty"). Parse it from that text; leave it empty when absent, which is the case at non-faction yards. The `commission_ship` figure is what was actually charged.
- **`buy_ship_license` replies are recorded too** (ship class, tier, cost_paid, tick), in the same capture. The page reads owned licenses from that table. Seed it with the 11-02 Congregation purchase from `data/stations/cloverfield/events.jsonl`, tick 2301382.
- The hull page takes labor per tier from the recorded quotes and marks it "quoted". It also shows each recorded quote's royalty next to the rule's royalty.
- If quotes show labor varies within a tier, the page uses the per-class quote where one exists and shows the tier's range otherwise. This is our fallback; the operator's working assumption is a fixed per-tier labor.

## Constraints

- Read-only for the page: nothing calls the game. The capture is passive, recording replies to calls agents already make.
- Reuse the `pkg/prodcost` engine. A hull costs exactly what its materials cost on the item page, plus the three hull-only rule changes above (crystals allowed, unbuilt facilities at L1 labor, salvage unpriced).
- Data sources: `catalog_ships.json` (build_materials, tier, faction), the knowledge DB's `ship_listings`, `market.db` asks, the new quote table, and the item engine's inputs.
- Every page shows each data source's age.
- golangci-lint clean, Go 1.24 idioms, `go build ./...` and `go test ./...` pass in both repos. Binaries go in `bin/`.

## Non-goals

- Time cost (mining, crafting, hauling).
- Spreading the license cost over hulls built (no payback math).
- Module/loadout costs. The hull as delivered is what's costed; `bare_hull` is ignored.
- Building or commissioning anything.
- A public KB version.

## Anti-goals: a failure even if it technically works

- A hull silently showing a low make cost because a material was dropped. Every unpriced or special step must be flagged.
- A material costed differently on the hull page than on the item page, other than through the three documented rule changes.
- Royalty computed from market prices instead of our make cost.
- Labor shown as a number for a tier we have no quote for.

## Calls made by Claude, not the operator

- The hull page lives in the same generator and module as the item page (`cmd/generate-production-costs`), with a new output file beside `private/production-costs/index.html`, so the two pages always share one engine run.
- "Facility not built" steps are costed at **L1** own-route labor (the cheapest level to build), not at a higher level.
- Cheapest listing uses a 3-day window, the same freshness as the counts quoted in the conversation.
- Make total does not include the license.
- The quote capture goes into the knowledge DB that `play_as` and the workers already write.
- License ownership comes from the recorded `buy_ship_license` replies plus the seeded Congregation row, not a hand-kept list.
- Materials whose tree uses energy crystal **link to the public item page**, which does not skip crystals, instead of the private one. The private item page is unchanged.
- Labor and License states for prestige, starter, boss and reclaim-only hulls follow the server's license notes, as listed above.

## Left to the builder

- Table name and schema for recorded quotes, plus its migration (never edit an applied migration).
- Page styling, following the existing private page.
- How the engine is parameterized for the three hull rule changes. Options are one alternate `Compute` run, or per-step overrides.
