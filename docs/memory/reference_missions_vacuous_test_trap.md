---
name: reference_missions_vacuous_test_trap
description: "Tests calling Missions()/worker passes with a bare &game.State{} silently early-return before the code under test — three shipped green while asserting nothing. Prove discrimination by neutering the target line."
metadata: 
  node_type: memory
  type: reference
  originSessionId: 8c9098dc-7c7c-4768-bb10-b65a2ce84115
  modified: 2026-07-20T18:51:01.918Z
---

`Missions()` (`pkg/worker/mission.go`) returns early — around the "current system unknown"
guard, ~line 245 — whenever `state.System.ID == ""`. A test built on
`&fakeClient{state: &game.State{}}` therefore exits **three lines in**, before the nav
default, before the board read, before any feature code.

Such a test still PASSES. Assertions of the form "no shipping calls were made" or "the pass
did not error" are trivially true of a function that never ran. The fixture the test
carefully sets up is never read.

**This shipped three separate times on one branch** (Shipping Sub-project B,
[[project_shipping_carrier]]) and I twice reported the green result as evidence of
correctness before a reviewer caught it. Passing-but-unreachable tests are worse than no
tests: they consume the reviewer's trust budget and mark the path as covered.

**Two defenses, both cheap:**

1. **Populate a real docked state.** See `mission_test.go:1225` for the established fixture
   shape, or use a helper like `freightBoardClient`.
2. **Carry a reachability guard inside the test**, so it fails loudly if the pass short-circuits:
   ```go
   if !slices.Contains(f.calls, "get_missions") {
       t.Fatalf("test is vacuous — the pass never reached the board read: %v", f.calls)
   }
   ```

**The discipline that actually catches it:** never trust a green test on a function with an
early-return guard. Neuter the specific line the test names, confirm the test goes RED, then
restore. Two tests on that branch are proven discriminating this way — one by replacing
`missionFreightOrDry` with `missionDryPass` (failed with `calls were [profile profile list]`),
one by deleting a single `ClearRawJSON` line (failed by exhibiting the stale contract). A test
that passes both with and without the code it names is worthless.

Related trap, same family: a fake whose `GetState()` returns the live pointer while the real
client returns `state.Clone()` (`pkg/game/client.go:2095`). With a shared pointer, a stale
snapshot and a fresh read are the same object, so code that wrongly reuses a stale snapshot
still looks correct. `fakeClient.cloneState` is opt-in for exactly this — it cannot be made
unconditional, because cloning deadlocks `TestKBUpdateMissionsUpsertsHandAuthoredOnly`, which
relies on observing its own mutations through that pointer.
