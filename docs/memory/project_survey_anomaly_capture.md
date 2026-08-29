---
name: project_survey_anomaly_capture
description: Persist survey_system spatial-anomaly hints to the anomalies table (explore/autoexplore)
metadata: 
  node_type: memory
  type: project
  originSessionId: a9e877bc-b5aa-417f-b398-a7b8f6f7c432
---

Built 2026-05-31 (merge 52d026f). `survey_system` responses carry an
`AnomalyHint` (e.g. "Spatial anomaly detected — faint readings toward Furud
(3 jumps).") that used to be printed and lost. Now both survey sites persist it
to the existing `anomalies` table so a session's anomalies can be reviewed.

- **`pkg/knowledge/anomaly_survey.go`**: `ParseSurveyAnomaly(hint, systemID, detectedBy, tick) (Anomaly, bool)`
  — Type=`spatial_anomaly`, Severity=`opportunity`, Description=raw hint; regex
  `(?i)towards?\s+(.+?)\s+\((\d+)\s+jumps?\)` pulls target system + jump count,
  "in this system" → `in_system=true`; Details JSON = `{raw,target_system?,jumps?,in_system}`.
  `CaptureSurveyAnomaly(ctx, kb, ...)` dedups against `GetActiveAnomalies` by
  Type+Description so repeat surveys of a system don't pile up rows.
- **Wiring** (per decision, both sites): `auto-explorer` `processSurveyResults`
  (gained an `agentID` param) and `play_as` `explore.go saveSurveyAnomaly`
  (detected_by = new `globalAgentID`, set from args[0] in main). Both gated on a
  non-nil KB.
- **No review command yet** (explicit decision) — rows live in `anomalies` for
  manual/later query. A `play_as anomalies` listing (flagging directional ones
  needing travel) is the natural follow-up.
- Prior to this, the only `RecordAnomaly` caller was `pkg/agent/base.go` Learn()
  (rich-deposit / depleting-resource, from POI resource data — not survey).
- Design doc: `docs/plans/2026-05-31-survey-anomaly-capture-design.md`.
