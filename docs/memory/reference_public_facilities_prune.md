---
name: reference_public_facilities_prune
description: "public_facilities is prune-at-write: a station's listing is COMPLETE (measured, no server cap), so rows the scrape omits are deleted. Fixed 280 ghost rows overstating recipe coverage by 7."
metadata:
  node_type: memory
  type: reference
---

`public_facilities` was upsert-only and had **no delete path**, so a facility that got
dismantled — or made private — stayed on file forever, still answering "you can build
this recipe here". Live audit 2026-08-22: **280 ghost rows across 49 stations**, oldest
43 days, overstating facility-only recipe coverage **153 → 146 of 317**.

Shipped `SQLiteKB.ReplacePublicFacilitiesAtStation(ctx, stationID, rows) (pruned, err)` —
one transaction: upsert what the scrape saw, delete that station's rows it did not.
`UpsertPublicFacilities` remains the insert-only primitive.

**The measurement the delete's safety rests on — re-verify before trusting it again.**
A station's facility listing is **complete, not truncated**:

| evidence | value |
|---|---|
| `crimson_war_citadel` | returns all **104/104** rows in one capture |
| two largest stations | **231** vs **223** newest-capture rows — different, so no shared cap |

If a server-side cap ever appears, this method starts deleting **live** rows — the same
shape as [[reference_legacy_ship_classes_erased_by_refresh]].

**The completeness guard lives in the caller**, `upsertPublicFromFacilityList`
(pkg/worker/capture.go). It only prunes when the payload decoded, named its `base_id`,
AND carried at least one facility section. A reply failing any of those is a failed or
mis-routed capture — `GetRawJSON("_last")` really can hand back another command's reply
([[reference_rawjson_key_drift]]). A listing that DOES decode with sections but returns
zero public production lines **is** a real observation and prunes.

Only four roles run the `facilities` capture — `resident`, `resident_gas`, `resident_ice`,
`craftsman` — so this ships to **mb + craft fleets only**. See
[[reference_facility_list_sections]] for the three-section parse.

Result on the first sweep (2026-08-22 19:05): 1954 → **1674 rows, 0 stale**, coverage
153 → **146**, stations unchanged at 49. Two prunes dominated: **178** at
`confederacy_central_command` (faction `1582cf58…` tore down its production wing
Aug 11-12) and **55** at `cca9e51e…`.
