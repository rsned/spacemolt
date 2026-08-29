---
name: reference_secondment_drain_and_rollback
description: "A fleet move that waits on a graceful drain must outlast RemoveDrainTimeout (4m) and must roll back on failure, or the agent ends up running in no fleet at all"
metadata: 
  node_type: memory
  type: reference
  originSessionId: db74e972-dd15-4cd6-9408-f974d4fa7975
  modified: 2026-08-13T13:34:20.841Z
---

Overmind roster removal is **graceful**: the worker finishes its current pass,
and the supervisor force-stops only at `RemoveDrainTimeout` (4m,
`DefaultRemoveDrainTimeout`). Measured live, a busy hauler's drain completed at
**4m05s**.

So anything that WAITS on a removal must outlast that window. The secondment
reconciler shipped with a 90s wait and could essentially never succeed on a
hauler. Both the package default and the `--stop-timeout` flag now derive from
`DefaultRemoveDrainTimeout + 1m`; do not restate the literal.

**The worse defect was the missing rollback.** `moveAgent` is
`remove from home → wait → release to away`. A failure anywhere after step 1 left
the agent in home's removed-set and absent from away's roster — it ran in **NO
fleet**, silently. On 2026-08-13 the very first live nomination (trader-1) and
then salvager-2 both went dark this way; the daemon had to be killed by hand and
both restored via `haul-overrides.json` + SIGHUP.

`restoreHome` now undoes step 1 on every failure path. It cannot create a
double-run, because away is only ever released after the home worker is confirmed
gone. Fixed in `490ec641`.

**Diagnosing this state:** `fleet-secondment --status` showing `phase=failed`
plus a non-empty `removed` list in the home overrides plus zero processes for the
agent = orphaned. Failed entries pin the agent by design and must be cleared by
hand (no CLI flag).

Related: [[project_fleet_pool_dynamic_membership]] · [[reference_deploy_verification]]
