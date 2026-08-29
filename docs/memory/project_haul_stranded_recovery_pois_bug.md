---
name: project_haul_stranded_recovery_pois_bug
description: "haulRecoverIfStranded likely broken — galaxy FindNearest never populates NearestResult.POIs, so its len(dest.POIs)>0 filter drops everything"
metadata: 
  node_type: memory
  type: project
  originSessionId: 051adb3a-a06c-4dac-aed0-f51137d16814
---

**QUEUED follow-up (found 2026-07-16 during mission-runner smoke fixes):** `pkg/galaxy`'s FindNearest / FindNearestByPOIType never populates `NearestResult.POIs` for POI-type lookups (nearest_by_poi.go's own doc comment says "left nil for POI-type lookups"). The mission worker's reposition closure hit this live (0 targets every pass) and was fixed in `ea0c7e5` with a `missionStationPOI` helper (walks kb.GetPOIs + kb.GetBaseByPOI PublicAccess).

`pkg/worker/haul.go` `haulRecoverIfStranded` (~line 682) has the exact same `len(dest.POIs) > 0` filter and is very likely silently broken the same way — a stranded hauler would find 0 recovery stations. It's a rare path (only fires when stranded), which is why it's never been noticed. Fix by reusing `missionStationPOI` (same package). Add a regression test mirroring `TestMissionsDefaultRepositionFindsRealStationPOI`.

Related: [[project_idle_agent_income_paths]]
