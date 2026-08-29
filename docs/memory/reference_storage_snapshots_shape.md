---
name: reference_storage_snapshots_shape
description: "storage_snapshots is upserted (UNIQUE agent_id+base_id), not append-only; quantities are REAL not INTEGER"
metadata: 
  node_type: memory
  type: reference
  originSessionId: 82fc608b-c0b2-4c87-ad0b-296b44e4a4ff
---

The per-agent storage capture tables in `spacemolt-knowledge.db`, verified read-only against live data on 2026-07-09:

```sql
CREATE TABLE storage_snapshots (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  agent_id TEXT NOT NULL,
  base_id TEXT NOT NULL,
  credits INTEGER NOT NULL DEFAULT 0,
  captured_at TEXT NOT NULL,
  UNIQUE(agent_id, base_id)        -- <-- upserted, ONE row per (agent, base)
);
CREATE TABLE storage_snapshot_items (
  snapshot_id INTEGER NOT NULL,
  item_id TEXT NOT NULL,
  quantity REAL NOT NULL DEFAULT 0, -- <-- REAL, not INTEGER
  ...
);
```

Two things that are easy to get wrong:

1. **It is NOT append-only.** The `UNIQUE(agent_id, base_id)` constraint means each agent/base pair holds exactly one current snapshot — confirmed live: 227 groups, all with exactly 1 snapshot, and 0 duplicate `(snapshot_id, item_id)` rows. So a plain `JOIN` + `SUM(quantity)` per `(agent, base)` cannot double-count across history. Do not add "pick the latest snapshot" logic to guard against inflation — there is no history to pick from. (I asserted the opposite while building craftbrain A2 and made it a hard requirement on the SQL Source; it was defending a bug that cannot occur. The resulting `GROUP BY` is harmless.)

2. **`quantity` is `REAL`, not `INTEGER`** — in both `storage_snapshot_items` and `faction_storage_items`. All 53,898 live item rows are integral today, so anything converting to `int` is latent-safe. `cmd/tools/play_as/source_sql.go` does `Qty: int(qty)`, which **truncates**. For inventory that is the *safe* direction: flooring never claims stock the fleet doesn't have. If fractional quantities ever appear, keep the floor — don't "fix" it to rounding without reasoning about over-claiming.

Related: [[project_crafting_brain]]. The passive capture itself lives at `pkg/agent/storage_capture.go`.
