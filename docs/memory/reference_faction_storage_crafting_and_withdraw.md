---
name: reference_faction_storage_crafting_and_withdraw
description: "Live-verified 2026-10-24: a facility craft falls back to CRFT FACTION storage for inputs (and delivers output there) only for agents with the faction Treasury permission (Officer+; Member is refused with cannot_craft). faction_withdraw_items lands in CARGO, so moving output to personal storage is withdraw + deposit."
metadata:
  type: reference
---

**Craft with inputs in faction storage (Voss Backwater Still, job `2452b6fe…`, 2026-10-24 02:25 PDT):**
`craft distill_moonshine 60` as **craftsman-10 (faction role Officer)** queued 10 runs with the server note
"Faction storage and treasury are covering this job because your own can't; outputs go to faction storage".
The identical command as **craftsman-8 (Member)** was refused: `cannot_craft: Not enough materials in your
station storage`. The operator's roles table: Leader/Officer have Treasury [x], Member/Recruit do not. So a
crafter that is meant to run on faction stock must be an Officer (`faction_promote <player-id> officer`, from the
leader craftsman-boss). Do NOT give the Member role Treasury — all 31 miningbots are Members.
`craft … --dry_run` quotes runs/inputs/labor and says `Have inputs: ❌` for a Member even when faction storage is
full. Facility rate at Voss is 2.7 ticks/run (10 runs ≈ 27 ticks); outputs appeared in `view_faction_storage`.

**Getting output back out:** `faction_withdraw_items pirate_moonshine 72` puts the goods in **CARGO** (the error
text for a bad `--target` claims the default "self" is personal storage — it is not; `--target=storage` is
rejected). Then `deposit pirate_moonshine 72` moves cargo → personal storage at the docked station. Only personal
storage is read by `ReportConsignStock` (`consign_offer`), so a crafter's sell leg is withdraw → deposit → offer.

**Changing a worker's yaml spec (e.g. adding `consign_offer: [item]`) and SIGHUPing the overmind** logs
`membership: removing X — drain sent` then `membership: "X" spec updated; relaunching` ~10 s later — the worker
restarts with the new flags; no manual kill needed.

Related: [[project_consignment_haul_relay]] · [[reference_handoff_gifts_from_storage]] ·
docs/superpowers/specs/2026-10-24-contraband-production-pipeline-design.md
