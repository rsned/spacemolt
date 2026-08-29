---
name: reference_smuggling_spare_marketbot_accounts
description: "Nine dormant marketbot accounts (credentials present, in no fleet yaml) the operator designated for smuggling-threshold work"
metadata: 
  node_type: memory
  type: reference
  originSessionId: 2f3b8937-e63d-42aa-8015-c67d52bc5fd2
  modified: 2026-07-25T22:43:38.053Z
---

**2026-07-25 — operator-supplied accounts for the smuggling mission thresholds:**

```
marketbot_algol       marketbot_alhena      marketbot_barnard_44
marketbot_bellatrix   marketbot_gliese_581  marketbot_gsc_0008
marketbot_sheratan    marketbot_xamidimura  marketbot_zaniah
```

All nine verified 2026-07-25: `data/agents/<id>/` exists **with `credentials.json`**, and none appear in any `data/overmind/*-fleet.yaml` — they are **dormant, not fleet-managed**. That means no supervisor owns their game session, so `go run ./cmd/tools/play_as <id>` can drive them directly with **no SIGSTOP/freeze dance** (unlike a live worker — see [[feedback_play_as_go_run]]).

**Why this matters:** smuggling progression is per-character, and until now `engineer-2` was the single smuggling pilot — parked out of the mission-learn fleet precisely so it could be driven by hand. These nine give parallel capacity to grind the tier chain without pulling any earner out of a fleet.

Thresholds to climb (from the proven engineer-2 run): **L0→1 = 60, L1→2 = 165, L2→3 = 340**. The chain that worked: `no_questions_asked` (+100) → `across_the_line` (+200), cross-border into Sol, clean, on a freighter → smuggling L2 → courier accept succeeds (it returns `skill_required` at L1).

Caveats before using them: they are old accounts (KB `storage_snapshots` shows several last touched 2026-07-03), so verify credits, hull, and current station before planning a run — several may need refit or fuel. Adding any of them to a fleet requires editing the fleet yaml + SIGHUP (the overrides sidecar can only REMOVE).

See [[project_smuggling_enablement]] and [[reference_customs_mechanics]] (continuous travel = zero confiscation risk; only a 10-tick stop at a border triggers a scan).
