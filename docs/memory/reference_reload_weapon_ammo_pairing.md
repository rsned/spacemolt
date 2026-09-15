---
name: reference_reload_weapon_ammo_pairing
description: "reload takes the get_ship MODULE INSTANCE id plus an ammo item_id; the pairing is weapon ammo_type == ammo item effect.subtype, one tick per module, and out_of_ammo repeats every tick"
metadata:
  type: reference
---

`reload <weapon_instance_id> <ammo_item_id>` — **operator-supplied
2026-09-15**, then verified against `data/game-api/latest/catalog_items.json`.

## The two ids
1. **weapon_instance_id** is the `id` on a `get_ship` `modules[]` entry — the
   fitted INSTANCE, not the `type_id`. There is no other command that gives it.
2. **ammo_item_id** is an item **in cargo** whose ammo type fits that weapon.

## The pairing is in the catalog, not guesswork
A weapon module carries `ammo_type`; an ammo item has `category: "ammo"` and
`effect.subtype`. **They are the same vocabulary**, so the join is exact:

| ammo_type | weapons | ammo items |
|---|---|---|
| `missile` | missile_launcher_i/ii | standard_guided_missiles, armor_buster_missiles, cluster_missiles, antimatter_warhead |
| `railgun` | railgun_* | ferrous_slug_case, cryo_slug_case |
| `autocannon` | autocannon_i/ii, blood_reaver | standard_rounds_box, armor_piercing_rounds_box, depleted_uranium_rounds_box, antimatter_core_rounds_box |
| `plasma` | — | corrosive/dispersal plasma_cell_pack |
| `torpedo` · `em_charge` · `mine` · `void_core` | incl. dark_matter_projector (void_core) | breacher/antimatter torpedoes, cascade_em_charge_pack, … |

`junk` is a weapon `ammo_type` with **no** matching ammo item subtype.
Energy weapons have an empty `ammo_type` and never reload.

Both halves live in the KB item catalog: `CatalogItem.Ammo.AmmoType` and
`CatalogItem.Module.Weapon.AmmoType`.

## Mechanics that shape any automation
- ⭐ **Each module's reload is its OWN TICK.** Never fan them out; sequence them.
- Reloading a **partial** magazine with a *different* ammo type discards the
  rounds still loaded — `ReloadResponse.rounds_discarded`. Rank the
  already-loaded `loaded_ammo_id` first, and leave partials alone mid-fight.
- The dry-magazine signal is an **error frame with `code: "out_of_ammo"`**,
  and it **repeats every tick** for as long as the weapon is empty. Match the
  code, never the prose.

## Two ways to keep magazines loaded
The operator named both: count rounds locally and reload at zero, or wait for
the notification and answer it. **We took the notification route** — local
counting means modelling shots/tick, cooldowns, misses and each magazine
separately, and it breaks the first time one assumption slips.

Built `aa511fca` in play_as: bare `reload` prints every ammo-using weapon
with the exact command, `reload all` sequences the dry ones, and
`autoReloader` answers `out_of_ammo` (repeats coalesce to one in-flight pass,
holds execMu, `set_autoreload off` disarms). **play_as only — the worker fleet
has none of this** and cannot even see the frame
([[reference_simplehandler_drops_every_push]]).

[[reference_combat_damage_pipeline]] · [[reference_ship_module_costs_scale_with_engineering]]
