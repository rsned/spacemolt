# Non-destructive, provenance-tracked POI merge

**Date:** 2026-06-01

## Problem

POI data lives in a **shared** knowledge base. Different agents fly ships with
different scan/survey power, so a weaker agent re-visiting a system already
surveyed richly by a stronger agent can **destroy the better data**:

- `RememberPOI` does `DELETE FROM poi_resources WHERE poi_id=?` then re-inserts,
  *whenever the incoming POI carries any resources* — no quality/freshness guard
  (`sqlite.go:347-370`). A weak scan that resolves fewer/poorer resources wipes
  the richer ones.
- On the `pois` row, `hidden` and `last_updated_tick` are overwritten blindly
  (name/type/description/position already COALESCE-guarded; `reveal_difficulty`
  already non-zero-guarded).
- No provenance: nothing records which agent observed the data, or lets a later
  capability-aware policy rank quality.

## Decisions

- **Track agent + tick provenance now; defer capability/scan-power.** We can't
  rank quality numerically yet, so the merge leans on freshness + being
  non-destructive, and records who/when so capability-gating can plug in later.
- **Same-resource conflict rule: max richness, latest remaining.** Richness is
  intrinsic — keep the highest ever observed (best detection wins). Remaining
  depletes — take it from the newest observation.

## Schema (migration 40, additive)

```sql
ALTER TABLE pois           ADD COLUMN detected_by TEXT;
ALTER TABLE poi_resources  ADD COLUMN detected_by TEXT;
```

`knowledge.POI` gains a `DetectedBy string` field, threaded by callers — so
`RememberPOI(ctx, poi)`'s signature is unchanged (no `Base`-interface / mock
breakage). Per-resource `detected_by` = the POI's `DetectedBy` (one agent per
write).

## Merge rules (`SQLiteKB.RememberPOI`)

**`poi_resources` — the real fix.** Drop the `DELETE`-all. Per-resource upsert,
keeping resources the incoming scan didn't mention:

```sql
INSERT INTO poi_resources (poi_id, resource_id, richness, remaining, last_updated_tick, detected_by)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(poi_id, resource_id) DO UPDATE SET
    richness          = MAX(poi_resources.richness, excluded.richness),
    remaining         = CASE WHEN excluded.last_updated_tick >= poi_resources.last_updated_tick
                             THEN excluded.remaining ELSE poi_resources.remaining END,
    detected_by       = CASE WHEN excluded.last_updated_tick >= poi_resources.last_updated_tick
                             THEN excluded.detected_by ELSE poi_resources.detected_by END,
    last_updated_tick = MAX(poi_resources.last_updated_tick, excluded.last_updated_tick);
```

A resource is only in the incoming set if the scan actually resolved it, so
"keep-missing" preserves the stronger scan's extra finds without zeroing them.

**`pois` row.** Add `detected_by`; tick-guard the previously-blind fields so an
older write can't flip `hidden` or stomp the tick:

```sql
hidden            = CASE WHEN excluded.last_updated_tick >= pois.last_updated_tick
                         THEN excluded.hidden ELSE pois.hidden END,
detected_by       = CASE WHEN excluded.last_updated_tick >= pois.last_updated_tick
                         THEN excluded.detected_by ELSE pois.detected_by END,
last_updated_tick = MAX(pois.last_updated_tick, excluded.last_updated_tick),
```

Existing non-empty COALESCE/CASE guards for name/type/class/description/
position/reveal_difficulty/expires_at stay as-is.

## Caller changes (set `DetectedBy`)

- `cmd/auto-explorer` `processSurveyResults` → `agentID`.
- `cmd/tools/play_as` `saveSurveyPOIs` / `kbUpdatePOI` / `kbUpdateAll` →
  `globalAgentID`.
- `pkg/agent` `KBMemory.RememberPOI` → `m.agentID`.

## Out of scope

- **Capability (scan/survey power) column + capability-gated overwrite** — the
  deferred next step; the `detected_by`/tick columns and the max-richness rule
  leave room for it.
- **`MemoryKB`** (in-memory, single-agent) keeps its simple overwrite; it just
  carries the new field so it compiles. The shared-DB clobber is the SQLite path.
