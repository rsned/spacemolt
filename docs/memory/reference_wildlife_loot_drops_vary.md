---
name: wildlife-loot-drops-vary
description: "wildlife carcass loot is not static: item quantities differ kill to kill for the same species (operator 2026-11-23)"
metadata:
  node_type: memory
  type: reference
  originSessionId: 734f5d40-f51d-46b3-9086-72af3ca8b239
  modified: 2026-11-23T21:23:35.706Z
---

Carcass loot is not static (operator-confirmed 2026-11-23). Three Pilot-Whale carcasses the same afternoon held whale_oil 2, 4 and 2, with raw_xeno_meat 3, 5 and 3 plus two other items each.

**How to apply:** never plan a Station Need as "N kills"; count the part in the hold (the hunt worker does). Treat `wildlife_kill_drops` as a sample of a range, not a recipe. A comment above `huntPickQuarry` in `pkg/worker/hunt.go` still claims loot does not scale with the quarry, from two early carcasses; do not rely on it.

Related: [[index_combat_wildlife]], [[reload-replaces-whole-magazine]]
