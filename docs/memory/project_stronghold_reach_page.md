---
name: project_stronghold_reach_page
description: "Stronghold reach did-you-know page SHIPPED 2026-07-26 in the kb repo (main @ fc08da6bf) — BFS from the 9 strongholds, CSS-revealed radius frames; parked follow-ups listed"
metadata: 
  node_type: memory
  type: project
  originSessionId: e7789b50-07fd-46fe-856d-85f76b11e40d
  modified: 2026-07-27T01:59:09.410Z
---

**SHIPPED + PUSHED 2026-07-26.** Repo is the SIBLING `spacemolt-kb` at
`/home/robert/spacemolt/kb` (NOT `spacemolt/spacemolt`). `main` @ `a9c0f76ed`.
Page: `kb/did_you_know/stronghold_reach.html`, generator
`cmd/generate-stronghold-reach`, linked from `kb/did_you_know/index.html` + README.

**What it shows:** multi-source BFS from the 9 `is_stronghold` systems over
`connections`; a slider (1–14, opens at 5) grows red→white territory. Headline
numbers, re-verified at every step — systems per radius
`18 41 72 114 171 235 285 335 389 430 461 486 500 505`, separate blobs
`9 9 9 8 8 6 4 2 1 1 1 1 1 1`. Every system is within **14** jumps; blob count
only falls (proved: every reach component contains a stronghold). Algol is
nearest for 142 systems. Last 5 to fall at r=14: 82 Eridani, Blackthorn,
GSC-0036, Muscida, Windmere. Empire first-contact: Nebula+Voidborn r=4,
Solarian 6, Outer Rim 7, Crimson 9 — every one lands on a merge radius.

**Key design idea worth reusing:** reach is monotone, so blob geometry is
emitted ONCE with `rb-<n>` activation classes and frames are revealed purely by
generated CSS on a `data-r` attribute — ~1,570 elements for 14 frames instead of
14 copies of the map. `pkg/galaxymap` gained two optional fields for this,
`ReachBlob` (radius-layered) and `GroupBlobs` (static per-group territory blobs,
used for the 5 empires). Both nil-default, so the two pre-existing callers
(`cmd/generate-galaxy-map`, the Resources page) are byte-identical — verified.

**"Every System, By Jumps" index** (`a9c0f76ed`): page bottom carries all 505
systems grouped by EXACT distance 0–14 (0 = the strongholds), 5-column
alphabetical listing of system **IDs** (not names) so Ctrl-F matches the token
that appears in logs. Per-section counts
`9 9 23 31 42 57 64 50 50 54 41 31 25 14 5` sum to the cumulative row totals.
`JumpSections` in `stats.go`; omits empty radii rather than emitting them.

**Distances are jump-gate only.** Wormhole fixtures (3 entrances, 2 exits, 21
collapsed) live in `pois`, NOT `connections`, so BFS cannot traverse them. Page
carries an Analysis Note saying so. See [[reference_empire_field_semantics]] for
why only 70/505 systems are empire-colored.

**Parked, deliberately not dropped:**
- `RadiusRows`/`ComputeReach` take `edges` independently; passing mismatched
  graphs would silently produce wrong blob counts. Guarded by
  `TestEveryReachComponentContainsAStronghold` instead of a signature refactor
  (~10 test call sites of churn for a single-caller risk).
- union-find is duplicated between `componentCount` and the invariant test's
  `componentsOf` — judged justified (a test shouldn't depend on the code it
  verifies), but extractable.
- `mergeStory` prepends "and " whenever `len(parts)>1`, so a 2-part story reads
  "9 at 1, and a single blob at 2". Not hit by current data (6 parts).
- `main.go` `defer f.Close()` swallows the Close error; minor, `tmpl.Execute`
  surfaces write failures.
- Slider/prev/next buttons have no aria-labels.

Full per-task review reports + ledger are in the git-ignored
`kb/.superpowers/sdd/2026-07-26-stronghold-reach/` (deletion was denied, so it
still exists). Design + plan are committed under `docs/superpowers/`.
