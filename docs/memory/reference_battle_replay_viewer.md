---
name: reference_battle_replay_viewer
description: Every battle gets a battle_id that replays visually at https://spacemolt.com/battles/<battle_id>
metadata: 
  node_type: memory
  type: reference
  originSessionId: db74e972-dd15-4cd6-9408-f974d4fa7975
  modified: 2026-08-09T17:36:06.568Z
---

When a battle begins the server assigns a **`battle_id`**, and that id replays
the whole fight in a web viewer:

```
https://spacemolt.com/battles/<battle_id>
```

Operator-supplied 2026-08-09.

**Why it matters:** it is the only way to *watch* what a combat behaviour
actually did, rather than infer it from log lines. For the hunt fleet
[[project_pirate_bands]] that makes it the natural check on the two things
hardest to prove from text:

- did the executor actually CLOSE RANGE (`zone_distance` vs `max_weapon_reach`)
  rather than fire from out of reach — the original `attack` bug
- did the quarry flee, and did the pursuit keep it in reach

**Capture the id at battle start** — grab it from the `battle_started` /
`battle_update` frame while the session is live; reconstructing it afterwards
from a transcript is fiddly and sometimes impossible.

Complements `get_battle_log`, which returns per-tick structured entries for
**any** battle including other players' (weapon pipeline, hit/crit rolls,
resist percentages, damage breakdown). Use the log for analysis and the viewer
for seeing it. See [[reference_v0536_wildlife_combat]].
