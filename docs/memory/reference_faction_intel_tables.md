---
name: reference-faction-intel-tables
description: CRFT Intel Center pool is mirrored into the shared KB (faction_intel_* tables, migration 76) by the capture_intel worker command; hazards merge into pois
metadata:
  type: reference
---

Shipped 2026-11-15 (2e11f050). Migration 76 (applied to live KB) adds
faction_intel_systems / _pois / _resources / _connections, keyed by faction_id.
`capture_intel` pages faction_query_intel (limit 100, offset-checked) and
replaces each system's rows; POI hazards (v0.716.0) merge into `pois` through
RecordPOIHazard (hazard-only, so `hidden` and other fields stay as they are).

Scheduled twice_daily on craftsman-3 (CJ officer). Its running worker predates the
command, so the task logs "unsupported command" until that worker restarts.

Also fixed then: SQLite GetPOIs never returned hazard columns, so sweep hazard
checks against the KB were dead. See [[reference-intel-center-autosync]] and
[[reference-environmental-hazards]].
