---
name: reference_unregistered_wildlife_from_battle_api
description: 12 wildlife species seen in server battle history but absent from wildlife_species, with home systems — found via the OPEN battle-list API; Kiln-Snail (furud) is as common as Phase-Lurker
metadata:
  type: reference
---

**Found 2026-08-31 by diffing the server's battle history against our KB.**

**The battle-list API is OPEN — no auth:**
`https://game.spacemolt.com/api/battles?category=wildlife&limit=200&offset=N`
→ `{battles:[{battle_id, system_id, origin_poi, category, start_tick, sides:[
{side_id, faction_id?, participants:[display names]}], player_names, ...}],
has_more, total}`. Wildlife = participants on a faction-less side that aren't
in player_names. `category=pvp` exists too (282 battles — untapped PvP intel,
e.g. MoltenOne's full server-wide kill history). Battle DETAIL is not on HTTP;
use in-game `get_battle_log <id>` (works for ANY battle, even others'; drive a
dormant spare like marketbot_algol to avoid kicking a worker session).

**A 10% stride sample (200 of every 2,000 across all 354,820 wildlife battles)
found 52 distinct species; all 40 registered ones appeared, plus these 12
UNREGISTERED** (battles = sampled count ≈ 10% of true; ticks: ~8,640/day,
sampled at tick ~1,759,000):

| species | battles | home systems | latest tick | example battle |
|---|---:|---|---:|---|
| Kiln-Snail | 654 | **furud** (95%), last_light, grumium, nova_terra | 1692494 | ce67a921c68c1c0b0eace6e4dbed4a03 |
| Quantum-Moth | 78-94 | the_telescope, markeb, oakridge | 1742503 | 860b379e8168a6c305a8c8e784532cbd |
| Skeinwraith | 21-30 | menkib, peacock | 1696998 | 2764943926a66275691856ca057999c6 |
| Mirror-Skate | 10-18 | unknown_edge, nova_terra, last_light | 1623154 | 2e6fda218490bae99924cae2592651bc |
| Gorgonia | 7-13 | alzirr | 1612610 | ebc76ceddb0e59ea4591d43367e19b4e |
| Void-Browser | 11 | schedar (only) | 1730044 | 6791b44b715ed823fd2f337ac2b399bf |
| Bulwark ⚔ | 6-11 | stillwater, acubens | 1636994 | d9a035e0f63542507223a17ba1ee8edc |
| Vulcanid | 3-6 | nova_terra | 1616678 | b40024627fae42bcd9664a9015b4ed6f |
| Cinder-Urchin | 2-5 | hd_20794 | 1518398 | 45ee478659a0b963b5addf94854dc79d |
| Glacier-Drifter | 4 | (2nd pass missed it — sample variance) | | |
| Wormwood ⚔ | 1-4 | starfall | 1530188 | baa5167bfc3130cfca9bd44178240906 |
| Fluxipede | 3-4 | bharani | 1630758 | c13fa3b4704f339404f60fda057d58e4 |

**⚔ Bulwark (kinetic) and Wormwood (energy) are ARMED creatures** — verified
via get_battle_log: they shoot back, unlike our grazers. Treat as predators
until scanned. Gorgonia unverified but appears the same way.

**To register them:** send a scanner to the home system, `get_nearby` + `scan`
(populates wildlife_species via the wired capture, [[project_wildlife_combat_intelligence]]).
Kiln-Snail latest sighting is ~8 days old; Cinder-Urchin/Wormwood ~26 days —
populations may have moved or been hunted out; v0.573.0's "newly uncovered
mineral deposits" may also spawn fresh fields. Battle counts skew toward
where PLAYERS fight, not where wildlife lives — absence of battles is not
absence of wildlife.
