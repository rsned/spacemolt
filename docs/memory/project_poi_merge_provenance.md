---
name: project_poi_merge_provenance
description: Non-destructive provenance-tracked POI merge in the shared KB (weak scans no longer clobber rich data)
metadata: 
  node_type: memory
  type: project
  originSessionId: a9e877bc-b5aa-417f-b398-a7b8f6f7c432
---

Built 2026-06-01 (merge 4616e5c). The shared SQLite KB let a weaker agent
re-surveying a system clobber a stronger agent's richer POI data:
`RememberPOI` did `DELETE`-all + re-insert on `poi_resources` and blindly
overwrote `pois.hidden`/`last_updated_tick`.

New merge (`pkg/knowledge/sqlite.go RememberPOI`):
- **poi_resources**: per-resource `UPSERT` on `(poi_id,resource_id)`, never
  deleting resources the incoming scan didn't mention. Conflict rule =
  **MAX richness** (intrinsic, best detection wins) + **newest remaining**
  (depletes) + newest provenance. Chosen interim because capability/scan-power
  ranking is deferred.
- **pois row**: tick-guard `hidden`/`detected_by`, `MAX()` the tick so a stale /
  out-of-order write can't downgrade a fresher one. Existing non-empty COALESCE
  guards (name/type/desc/position/reveal_difficulty/expires_at) unchanged.

Provenance: `detected_by TEXT` on `pois` + `poi_resources` (migration **40**),
threaded via new `knowledge.POI.DetectedBy` (no `Base`-interface change → no mock
breakage, see [[feedback_gameclient_interface_mocks]]). Set by auto-explorer
`processSurveyResults`, play_as `saveSurveyPOIs`/`saveFaintSignatures`/
`kbUpdatePOI`/`kbUpdateAll` (= `globalAgentID`), and `KBMemory.RememberPOI`
(= `m.agentID`).

Gotchas for future migrations here:
- Migration 40 is **table-guarded** (`m.version==40` special case): narrow
  migration-test fixtures fake "migration 1 applied" without creating `pois`, so
  a blind `ALTER pois` fails. Skip+record when `pois` is absent.
- After any migration change, run `./scripts/sql/regenerate_initialize_database.sh`
  or `TestInitializeDatabaseSQLInSync` fails (the artifact is byte-compared).

Deferred next step: a capability/scan-power column + capability-gated overwrite.
Design doc: `docs/plans/2026-06-01-poi-merge-provenance-design.md`. Relates to
[[project_survey_anomaly_capture]].
