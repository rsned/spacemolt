---
name: reference_empire_field_semantics
description: "get_system.empire = regional space affiliation; get_map.empire (omitempty, ~70 systems) = OWNERSHIP — KB systems.empire rotted to 372 tagged rows by conflating them; 9 is_stronghold rows = the 9 pirate stronghold systems"
metadata: 
  node_type: memory
  type: reference
  originSessionId: 8c9098dc-7c7c-4768-bb10-b65a2ce84115
  modified: 2026-07-27T01:20:17.466Z
---

Two server fields share the wire key `empire` with DIFFERENT semantics:

- **get_map** `system.empire` (omitempty): actual empire OWNERSHIP. 2026-07-20 truth: 70 of 505 systems (solarian 15, crimson 16, nebula 17, outerrim 12, voidborn 10); 435 have no key.
- **get_system** `system.empire` (no omitempty): regional space affiliation — present for far more systems. NOT ownership.

**The rot (fixed 2026-07-20):** `RememberSystem` (pkg/knowledge/sqlite.go:131) wrote `empire = excluded.empire` unconditionally from `state.System.Empire` on every worker visit → 372 systems tagged. Plus a cross-system bleed: client.go:~3492 only copies empire when non-empty and never resets between systems, so a prior system's empire leaked onto systems that omit it. Data corrected from data/game-api/20260720/get_map.json (backup CSV in session scratchpad); code fix branch `fix/kb-empire-authority-and-market-index` makes get_map the sole authority. **Until that fix is deployed + fleet restarted, worker visits re-rot the column.**

**Pirate strongholds:** the KB's 9 `is_stronghold=1` rows ARE the 9 pirate stronghold SYSTEMS (Bellatrix, Alhena, Xamidimura, Gliese 581, Barnard 44, Sheratan, Zaniah, Algol, GSC-0008) — user-confirmed count. The pirate "bases" in [[reference_pirate_base_registry]] (sable_port etc.) are POIs inside systems, not system ids; they do not appear in get_map.

**Why:** any map/UI or planner reading systems.empire as ownership must ensure only get_map-sourced imports write it (cmd/data/import-map-data). [[project_overmind_dashboard_v1]] [[reference_pirate_base_registry]]

**Downstream artifact fixed 2026-07-26:** the kb repo's checked-in
`kb/galaxy-map.html` had been generated under the OLD rotted semantics — 364
systems empire-colored. Regenerating (`061b9aab0`) recolored **294 of 505 dots**
to unclaimed grey, leaving the correct 70. Any other checked-in artifact
generated before 2026-07-20 that colors or counts by `systems.empire` is
similarly wrong and needs regenerating — the symptom is a mostly-colorful map
rather than five small clusters. [[project_stronghold_reach_page]]

## ⭐ 2026-08-27 — BLANK empire is DATA, not a capture gap

435 of 505 systems have `empire=''`. That is **not** missing capture — it means
unaligned/lawless space, and the server says so itself: a live `get_location` at
Ashford returns `"empire": ""` alongside
`"security_status": "Lawless (no police protection)"`.

The correlation is near-total:

| empire | systems | lawless |
|---|---|---|
| (blank) | 435 | **407** |
| nebula | 17 | 0 |
| crimson | 16 | 0 |
| solarian | 15 | 0 |
| outerrim | 12 | 0 |
| voidborn | 10 | 0 |

**Zero** of the 70 empire-tagged systems are lawless. Only 28 blank systems have
no security_status recorded at all — those are the genuinely uncaptured ones.

So empire-scoped targeting is TRUSTWORTHY: "Nebula Federation territory" is
exactly those 17 systems, not "at least 17". Do not treat blank as unknown.
