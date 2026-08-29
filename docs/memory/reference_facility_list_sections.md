---
name: reference_facility_list_sections
description: "`facility list` returns public production lines across THREE sections, and private lines omit production.public rather than setting it false."
metadata: 
  node_type: memory
  type: reference
  originSessionId: 82fc608b-c0b2-4c87-ad0b-296b44e4a4ff
---

A `facility list` payload splits facilities across four arrays. Public **production**
lines — the craft-for-hire sites the A1 catalog exists to record — appear in **three** of
them, and many stations return **no `public_facilities` key at all**:

| Section | Holds |
|---|---|
| `station_facilities[]` | NPC station-owned lines |
| `faction_facilities[]` | Your own faction's lines |
| `public_facilities[]` | **Other** factions' public-for-hire lines |
| `player_facilities[]` | Personal; never public production |

**The public predicate:** a private line signals privacy by **omitting `production.public`
entirely**, not by setting it `false`. voss_redoubt's "The Red Room" (`load_red_mist`,
`category:"production"`) has no `public` key and no `rental_fee_per_run`. So the keep-rule
must be `category=="production" && production.public == true` (explicit true). Every
genuinely public line sets it explicitly.

**Bug this caused:** `upsertPublicFromFacilityList` read only `public_facilities[]` and
used a keep-unless-explicitly-false predicate. Result: all 247 catalog rows were
faction-owned, zero station-owned; voss_redoubt captured 0 of its ~13 public lines.
Fixed 2026-07-09 in `8df24f8` (pkg/worker/capture.go) — reads all three sections, requires
explicit `public: true`. Regression test + fixture:
`pkg/worker/testdata/facility_list_sections.json`.

**Post-fix re-sweep (2026-07-09):** 247→**844 rows**, 6→**30 stations**, 0→**586
station-owned**, 149→196 recipes. But `facility_only` coverage only **25.6% → 31.9%
(101/317)** — most recovered lines produce already-covered recipes. **68% of facility_only
recipes still have NO known public facility**, so A2's buy-fallback is the common path, not
an edge case. The 4 fleet stations still absent re-swept and host no public production.

Note `pkg/worker/KBUpdateFacilities` (which backs play_as `update_facilities`) calls the
same upsert, so the play_as seed path shares the fix. The separate
`facilityDetail`/`RememberBase` → `base_facilities` path is lossy (strips level, fee,
production) but feeds the enriched-base view, **not** the catalog — don't confuse them.

Related: [[project_crafting_brain]], [[reference_facility_list_field_omissions]],
[[reference_facility_rent_cycle]].
