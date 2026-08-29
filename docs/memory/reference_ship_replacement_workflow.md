---
name: reference_ship_replacement_workflow
description: How to rebuy + refit a hauler after a destruction (browse_ships → buy_listed_ship → strip utility mods → insure); station-local availability + scarcity constraints
metadata: 
  node_type: memory
  type: reference
  originSessionId: 868ac572-f106-4923-9823-e82622bf66b9
---

The in-game procedure to replace a destroyed hauler with an equipped one. On death of an UNINSURED ship the agent clones into a STARTER hull at home_base and loses the fitted hull + cargo_expanders + in-transit cargo (the expensive case the rebuy feature targets). Starter hulls are NOT insurable. NOTE (2026-06-28): "starter" is a CLASS family, not a fixed 50-cargo hull — Cobble (75), Theoria (70), Prospect (100) are all starter classes. So a respawn line like `Ready! … | Ship: Prospect | Cargo: 0/100` with credits UNCHANGED = an uninsured-death starter respawn (lost the fitted hauler), NOT an insured ship restored. `NeedsReplacement` (Task 3) keys off starter/sub-spec hull + low utility slots, so it correctly catches these regardless of the starter's cargo number. There is NO upgrade/rebuy logic in the worker fleet (`pkg/worker`); `pkg/game/upgrades.go` exists but only the legacy `auto-*` agents use it.

Hull-selection target (which listed ship to buy):
- **Tier is gated by piloting skill: Tier 2+ ships require piloting skill ≥ 10.** Most fleet pilots are below that, so for now replacement hulls are effectively **Tier 1 only** for most agents. Filter listings by the agent's piloting skill before choosing.
- **Utility-slot count is the primary criterion** — at least 2–3 utility slots, and MORE IS BETTER. Each `cargo_expander_*` fills one utility slot, so slot count is the cargo ceiling.
- Plus a **decent base `cargo_capacity`** to start from.
- (Freight-class hulls are the ideal but rarely stocked — see scarcity below.)

Workflow:
1. **`browse_ships`** — primary path to a new ship; shows ONLY what the shipyard at the CURRENT station stocks (station-local). ~400 hull choices exist game-wide.
2. **`buy_listed_ship`** — buy a suitable hull. Budget **~3× the `catalog` list_price** (shipyard markup).
3. **`uninstall_mod`** every `utility_slot` module that is NOT `cargo_expander_*` or `afterburner_i` (strip junk to free slots), then fit **`cargo_expander_*`** for max hold.
4. **`buy_insurance`** on the new ship to cover the next destruction.

Scarcity / availability constraints (why this is a procurement-routing problem, not a buy-on-dock):
- **Freight-class hulls are rarely available**; non-empire / non-capital systems carry even less stock and fewer choices.
- **`cargo_expander_iii`** (+100 cargo) is manufactured in only a couple of stations → the scarce bottleneck. `cargo_expander_i/ii` more common.
- A stranded agent often can't rebuy a good hauler where it stands; it must route to a shipyard that actually has freight hulls / cargo_expander_iii.

Procurement intelligence ALREADY EXISTS (no new collection needed) — the resident marketbots in every station are distributed scouts:
- **Hull availability** → `ship_listings` table (knowledge DB). Populated by `worker.KBUpdateStation` (`pkg/worker/capture.go:495`), which runs `browse_ships` → `StoreShipListings`. The resident schedule's hourly **`kb_update`** (→ `KBUpdateAll` → `KBUpdateStation` when docked) captures it fleet-wide.
- **cargo_expander_* availability** → `market.db` sell orders, populated by the residents' hourly **`update_market`**. So we already know which stations SELL cargo_expander_iii.

So a casualty's procurement-routing is a QUERY against existing data: find nearest station stocking a qualifying Tier-1 hull (≥2–3 utility slots, decent base cargo) AND selling cargo_expander_iii, then autopilot there → buy_listed_ship → strip non-cargo utility mods → fit expanders → buy_insurance.

Catalog data source for hull specs (tier/utility_slots/cargo/piloting): the KB catalog table is named **`ships`** (NOT `ship_classes` — that name in CLAUDE.md/the old schema docs is wrong). `kb.GetShipClasses(ctx)` reads `FROM ships`. Live worker KB (`data/spacemolt-knowledge.db`) has it populated (~320 classes) but STALE vs the latest fetch. Authoritative full catalog = `data/game-api/latest/catalog_ships.json` (`{"items":[...]}`, 331 items as of 2026-06-26). `cmd/data/import-catalog-ships <file>` parses it → `StoreShipClasses` → `ships` table (DELETE-then-insert; default DB data/spacemolt-knowledge.db, env SPACEMOLT_DB override). Re-run it to refresh. Sibling import-catalog-{items,skills,recipes} tools refresh the other catalogs.

Plan written: `docs/superpowers/plans/2026-06-28-haul-ship-replacement.md` (Task 0 = refresh import; Tasks 1–9 = auto-replacement in the haul behavior, gated OFF by `RebuyConfig.Enable`).

Context: surfaced 2026-06-28 after a Crix-stronghold routing bug destroyed several haulers, which respawned in ~50-cargo starters with insurance credits but no way to spend them on an equipped ship. Building auto-replacement is the pending recovery feature.
