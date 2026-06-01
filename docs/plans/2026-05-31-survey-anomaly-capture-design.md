# Survey Anomaly Capture

**Date:** 2026-05-31

## Problem

`survey_system` responses carry an `AnomalyHint` string (e.g. *"Spatial anomaly
detected — faint readings toward Furud (3 jumps)."*). Today it is only printed:
`play_as` prints it in `explore.go surveySystem()`, and `auto-explorer`'s
`processSurveyResults()` ignores it entirely. After an explore/autoexplore
session there is no way to go back and review which anomalies were spotted —
especially the directional ones that need an explicit `travel`/`jump` to chase.

## Goal

Persist each survey anomaly hint to the existing `anomalies` table, parsing out
the target system and jump count so a later review can separate *"in this
system"* finds from *"travel to X (N jumps)"* finds.

## Design

### Parser — `pkg/knowledge/anomaly_survey.go`

`ParseSurveyAnomaly(hint, systemID, detectedBy string, tick int64) (Anomaly, bool)`

- Returns `ok=false` when `hint == ""`.
- `Type = "spatial_anomaly"`, `Severity = "opportunity"`, `Description = hint`,
  plus `SystemID` / `DetectedBy` / `LastUpdatedTick`.
- Regex `toward[s]?\s+(.+?)\s+\((\d+)\s+jumps?\)` extracts the target system name
  and jump count (handles the em-dash prefix and singular "1 jump").
- A case-insensitive `"in this system"` match sets `in_system=true`.
- `Details` is JSON: `{raw, target_system?, jumps?, in_system}`.

### Dedup + store helper — same file

`CaptureSurveyAnomaly(ctx, kb Base, hint, systemID, detectedBy string, tick int64) (bool, error)`

- Parses; returns `(false, nil)` when there is no hint.
- Reads `GetActiveAnomalies(ctx, systemID)`; if an active anomaly with the same
  `Type` + `Description` already exists, skips (returns `false`) so repeat
  surveys of the same system don't pile up duplicate rows.
- Otherwise `RecordAnomaly` and returns `(true, nil)`.

Both `RecordAnomaly` and `GetActiveAnomalies` are already on the `Base`
interface, so the helper works for any KB backend.

### Wiring (both survey sites, per decision)

- **auto-explorer** `processSurveyResults()`: after the existing POI/faint-sig
  loops, call `CaptureSurveyAnomaly(...)` with the agent id as `detectedBy` and
  the survey tick; log when a new anomaly is recorded.
- **play_as** `explore.go surveySystem()`: after printing the hint, capture via
  `globalKB` (current tick from client state, `detectedBy` = agent name). No-op
  when `globalKB` is nil.

### Out of scope (per decision)

No review command yet — rows live in the `anomalies` table for manual / later
query. A `play_as anomalies` listing command is a natural follow-up.
