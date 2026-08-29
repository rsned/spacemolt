---
name: reference_secondment_overrides_are_removed_sets
description: "Fleet overrides sidecars can only SUBTRACT from a yaml roster, so commenting out a rotating agent's yaml line silently makes the secondment reconciler's release step a no-op and the agent runs in NO fleet."
metadata: 
  node_type: memory
  type: reference
  originSessionId: 00cd813a-f76a-48cf-bb7a-0c47c76e1566
  modified: 2026-08-24T22:34:05.210Z
---

The haul<->unlock secondment reconciler (`pkg/overmind/supervisor/reconcile.go`,
run by the `bin/fleet-secondment --watch 5m` daemon) moves an agent between
fleets by editing the two fleets' **overrides sidecars**
(`data/overmind/<fleet>-overrides.json`). Those sidecars are `{"removed": [...]}`
— a removed-SET. They can only subtract from the yaml roster, never add to it.

`moveAgent` is: `fromOv.Add(id)` + reload → wait for the worker to stop →
`toOv.Delete(id)` + reload. **That last step is a no-op if the destination
fleet's yaml has no line for the agent.**

**So every agent that rotates must be listed in BOTH fleet yamls permanently,
with the removed-sets as the only switch.**

## How it bit (2026-08-24)

On 08-23 I commented out the 21 haul-owned entries in `unlock-fleet.yaml`
(`# QUEUED-NOT-OWNED`) and hauler-0's line in `haul-fleet.yaml`
(`# SECONDED-OUT`), as belt-and-braces on top of the removed-sets.

At 07:31Z the reconciler graduated hauler-0 (unlock→haul) and seconded trader-1
(haul→unlock). Both fleets **removed** their worker; neither fleet could
**start** one. Both agents ran in NO fleet for 14 hours.

**No health check catches this.** A worker that is in no roster is not
unhealthy — it is absent. Status files, `restarts`, `healthy`, fleet-watch
census: all normal, because nothing is claiming the agent at all. It surfaced
only from `ps` showing zero worker processes for two agent ids that the
secondment ledger said were mid-rotation.

Diagnostic: for each fleet, compare `yaml agent_ids - overrides.removed` against
the actually-running `bin/worker --agent` processes. Any ledger entry in phase
`seconded` or `home` whose agent appears in neither fleet is this bug.

Note the comments were not redundant safety — they were a **second writer for
state the reconciler owns**, which is what broke it. Same class of bug as any
two-source-of-truth split.

Related: [[project_pirate_reputation_unlock_campaign]] ·
[[reference_worker_quiesce_park]] · [[project_fleet_pool_dynamic_membership]]
