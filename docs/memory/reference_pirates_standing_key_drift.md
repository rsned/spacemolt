---
name: reference_pirates_standing_key_drift
description: "The generic \"pirates\" standings key is retired — the server sends nine per-stronghold keys, and stale fixtures hid the breakage"
metadata: 
  node_type: memory
  type: reference
  originSessionId: db74e972-dd15-4cd6-9408-f974d4fa7975
  modified: 2026-08-11T11:34:37.208Z
---

`get_status.standings` no longer carries a generic **`pirates`** key. It now has
**14 counterparties**: the five empires plus one per pirate stronghold —
`pirate_voss`, `pirate_kael`, `pirate_thane`, `pirate_mera`, `pirate_dross`,
`pirate_crix`, `pirate_sable`, `pirate_nyx`, `pirate_korr`. Per the standings
description in `server_docs/openapi.json`, each stronghold keeps its own books, so
attacking one crew leaves the others' opinion of you unchanged.

**Why it matters:** a lookup of the retired key returns a zero value rather than an
error, so it reads as "baseline 0" — indistinguishable from genuine hostility. It hit
three places on 2026-08-08 alone:

1. `pkg/assets/capability.go` — `stronghold_access` reported `baseline 0, needs 10`
   for an agent at baseline 10 / reputation 16-17 at all nine strongholds. Fixed in
   `fdb375e` to scan the `pirate_` prefix and be eligible if ANY stronghold qualifies.
2. `cmd/tools/play_as` status columns — the Reputation box grew 6 → 14 rows and
   unbalanced the two-column layout (`d544a6b`).
3. Both of the above had **test fixtures still using `pirates`**, so the suites stayed
   green while production was wrong.

**A FOURTH site surfaced 2026-08-11** — the partial-fix trap. `pkg/worker/
mission_standing.go`'s `smugglingUnlocked()` still keyed on `pirates`, so it could
never return true **for any agent, on any board, ever** — including the seven that
had completed the chain. `pkg/assets/capability.go` even carried a comment mirroring
this constant, so the 08-08 sweep read as complete when it had only covered
`pkg/assets`. Fixed in `c31371c0` (prefix scan, ANY crew at baseline ≥ 10 counts —
`an_introduction` is granted by one giver, so the unlock lands at one stronghold
first). Its fixture also used `pirates`. Blast radius was one framing line in
`mission.go` (unlocked agents were told their credits might not arrive), not a gate.

**How to apply:** when a standings lookup looks wrong, check the key before the logic.
Grep for `"pirates"` — and re-grep after you think you're done: this drift has now
produced two "fixed" declarations that each missed a site. **A cross-package comment
that mirrors a constant is not evidence the other package was fixed.** When a
capability or gate reads "baseline 0", suspect a missing key rather than a hostile
faction. Per-stronghold detail is always available from `agent_standings` in the asset
ledger, so aggregate flags need not carry it. Live shape verified 2026-08-11 against
`data/assets.db`: 122 agents × 9 `pirate_*` keys, **zero rows under `pirates`**.

Related: [[reference_rawjson_key_drift]] (same failure shape — a lookup that silently
finds nothing) · [[project_agent_capability_ledger_storage_faction]]
