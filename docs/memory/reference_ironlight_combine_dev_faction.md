---
name: reference_ironlight_combine_dev_faction
description: "Ironlight Combine (IRLC) is the game developer's own in-game faction; its charter describes our exact marketbot architecture"
metadata: 
  node_type: memory
  type: reference
  originSessionId: db74e972-dd15-4cd6-9408-f974d4fa7975
  modified: 2026-08-13T20:25:12.969Z
---

**Ironlight Combine** (tag `IRLC`, faction id `9c7909aa45e39fc1b2df9b6c09e1718c`) is the
**main game dev's in-game faction** — told to me by the user 2026-08-13, not
derivable from any data we hold. `faction_members` has captured 0 rows for it, so
the roster is private; nothing in the KB reveals who flies it.

Its charter (in `factions.charter`, `data/spacemolt-knowledge.db`) describes our own
setup almost line for line: an all-AI freight/trading corporation, hub-and-spoke with
**Nova Terra as hub and nine spokes covered by permanently-assigned market
operators**, where "every docking generates a complete market snapshot shared across
the fleet in real time". Declared spokes include Procyon and Sirius. Claimed volume:
69,109 transactions / 147 commodities / 55 officers / 9,478 haul legs.

**Why it matters:** we run resident marketbots at nova_terra, procyon and sirius, so
our capture footprint overlaps the dev faction's declared territory. And
**Ironlight Crossroads** (poi `0321b3e4406021575337b7be26e53dd7`, system `lhs_1140`)
is one of the 16 stations the probe found open to us — but it is the ONLY open one
whose fuel desk is dry (`station_fuel_empty`), so a resident there cannot refuel
locally.

**How to apply:** POST A RESIDENT THERE. I argued for skipping it (dry desk + the
dev's own station); the user overruled on 2026-08-13 — "fuel or no, someone should
go" — so the dev station gets covered like any other. The dry desk is a real
constraint, not a reason to abstain: a resident that stays docked burns no fuel, and
the escape hatch is buying `fuel_cell` off the local market and burning it, the same
fix the four fuel-dead stronghold bots need. Do not assume Ironbelt Range
(`e4148510aa80…`, system `ascella`, which REFUSED our dock) is related; the similar
name is not evidence.

See [[reference_player_station_access]] for how open/denied is learned.
