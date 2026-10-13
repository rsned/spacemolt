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

## ⭐🔴 2026-10-13: two more ways this bites, both hit in one operation

Seconding the nine idle craftsmen craft -> unlock for the pirate unlock.

### 1. A sweep applies the CLI `--home` to EVERY ledger entry — FIXED `e63b02d2`

`ReconcileSecondments` iterated all entries and used the configured home fleet
for each, ignoring the entry's own `HomeFleet`. A **haul**-homed sweep run to
clear six stale August returns also picked up the **craft**-homed craftsman
entries and wrote them into `haul-overrides.json`, a fleet that never rostered
them. Six craftsmen ended up ACTIVE in craft AND unlock at once.

The ledger itself was always right (`home=craft`); only the file being edited
was wrong. Fixed: a sweep now skips entries whose `HomeFleet` is not its own.
**Still: run one sweep per home fleet, and read the log lines — "seconded
haul -> unlock" for a craft-homed agent is the tell.**

### 2. ⭐🔴 SIGHUP does NOT apply the overrides sidecar

Editing `<fleet>-overrides.json` and sending SIGHUP reloads the **yaml** but
leaves the removed agents' workers running. The craft supervisor kept five
workers alive through a SIGHUP. What actually stops a worker is the control
API:

```
POST http://localhost:8091/api/overmind/fleets/<fleet>/agents/<id>/remove
```

(returns `{"status":"accepted"}`, then drains up to `DefaultRemoveDrainTimeout`
= 240s; some took >8 min). `GET /api/overmind/fleets` is 404 — only the
documented per-agent routes exist.

### 3. Detecting a double-run

Status files, `healthy`, and `restarts` all look NORMAL — both fleets happily
report the same worker. The only ground truth is counting processes per agent:

```bash
for p in /proc/[0-9]*; do [ "$(cat $p/comm 2>/dev/null)" = worker ] || continue
  cl=$(tr '\0' ' ' < $p/cmdline); pp=$(awk '/^PPid:/{print $2}' $p/status)
  case " $cl " in *" <agent-id> "*) echo "<agent-id> ppid=$pp";; esac
done | sort | uniq -c   # count >1, or two different ppids = DOUBLE-RUN
```

Two ppids for one agent_id is the signature; map ppid to fleet by reading the
supervisor's `/proc/<ppid>/cmdline`. Never `pgrep -f` — it matches your own
shell. 56 `session_replaced` events were logged while the doubles ran.

