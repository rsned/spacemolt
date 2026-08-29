---
name: project_haul_book_coordination_followups
description: Two deferred follow-ups after the haul book-coordination branch merged (2026-07-18)
metadata: 
  node_type: memory
  type: project
  originSessionId: 853398d3-bb3c-4431-bf12-2caa11653a5d
---

Branch `feat/haul-book-coordination` (base ce7ee82) shipped book-level haul coordination: `source_units` on opportunities, `haul_book_claims` roster, cap-K admission + destination fan-out, settle-at-buy, collapse invalidation, reaper. Final commit `affa2b7` fixed two review findings (resume cap-slot leak via new `Collector.GetActiveBookClaim`; scoped book invalidation to `"no live ask"` only). User deferred two items ("follow up"):

**(A) Stronghold-destination abandon leak.** `pkg/worker/haul.go` ~line 660-661: when a resumed held claim's `ToSystemName`/`FromSystemName` is a stronghold, `abandonClaim(...,"stronghold destination")` short-circuits BEFORE the `GetActiveBookClaim` recovery added in `affa2b7`, so it releases the opportunity but never releases the book-claim roster slot — leaks one cap slot until the 6h reaper (`ReapExpiredBookClaims`). Same leak class as review-finding F1, rarer path. Fix: recover the active book claim id and release it (route through `releaseBookAnd` or an explicit `ReleaseBookClaim`) before/instead of the bare `abandonClaim`. Surfaced by the affA2b7 re-review.

**(B) play_as book-aware opportunity view.** `cmd/tools/play_as/arbitrage_cmd.go` has `find_arbitrage`/`claim_arbitrage`/`release_arbitrage` (from `f670657`) but they predate book coordination and are NOT book-aware: no `source_units` depth column, no `haul_book_claims` roster display (agents on the book, phase, bought vs remaining, cap K), no per-`(item,from_station)` grouping fanned across destinations. Needs a new command (e.g. `book_status [item] [from_station]`) and/or a `find_arbitrage` depth column. New surface + own tests → own brainstorm/plan cycle. See [[reference_haul_fleet_capacity_ceiling]].
