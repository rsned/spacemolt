# Dynamic Fleet Membership — Design

**Date:** 2026-07-22
**Status:** Approved (user, 2026-07-22)

## Problem

The overmind supervisor loads its worker specs from the fleet yaml once at `Run()` start (`pkg/overmind/supervisor/supervisor.go`) and respawns any killed worker from that in-memory list. Removing or adding a single agent therefore requires editing the roster and doing a full graceful drain + restart of the whole fleet (~7–10 min re-ramp for 40+ workers).

Live occurrences of this pain:
- 2026-06-27: trader-9 stuck in a non-filling sell loop; recovery deferred indefinitely because a 21-worker haul restart was too disruptive.
- 2026-07-22: pulling 10 engineers for manual cargo-clearing required a 41→31 drain + restart.
- 2026-07-22 (same evening): pulling craftsman-1 for manual triage required a 42→41 drain + restart, interrupting the freshly-started freight soak.

## Decisions (user-answered)

1. **Interfaces:** overmind-dashboard (:8091) per-agent "Remove from fleet" button **plus** yaml-edit + `SIGHUP` diff. Two front doors, one mechanism.
2. **Removal semantics:** per-worker drain, then stop (bounded wait, then force-TERM).
3. **Spec changes on SIGHUP:** a changed spec for a still-present agent rolling-restarts just that worker.
4. **Persistence of button-removes:** an overrides sidecar file (`data/overmind/<fleet>-overrides.json`); the yaml is never machine-rewritten. Effective roster = yaml specs − overrides.removed.

## Architecture

One **membership engine** inside `Supervisor`. All membership changes — from the dashboard socket path or the SIGHUP diff path — become queued *membership requests* applied at reap-tick start:

```
request := { op: add | remove | update, spec: WorkerSpec }
```

This reuses the existing `releases` queue-then-apply pattern (`releaseMu` / drained at the top of `reapAndRestart`), so `specs`, `restarts`, and proc bookkeeping stay owned by the single reap goroutine. No new locking model.

Effective roster invariant, everywhere (boot and SIGHUP):

```
effective = yamlSpecs − overrides.removed
```

## Components

### 1. Supervisor membership engine (`pkg/overmind/supervisor`)

- `specs []WorkerSpec` becomes mutable, but only from within the reap tick. A mutex-guarded pending-request queue (`Enqueue(req MembershipRequest)`) is safe from any goroutine; `reapAndRestart` drains it first, before the existing release/restart logic.
- **Remove:** mark the agent `leaving` (new fleet state, visible in `Snapshot()` for the dashboard) → `Server.Send(agentID, drain envelope)` (per-worker; `Send` already exists) → each subsequent tick, check `LastStatus.Drained` → on drained OR after a bounded window (`RemoveDrainTimeout`, default 4 min — mirrors the fleet drain poll bound) → `kill(proc)` (existing SIGTERM→SIGKILL path; the worker checkpoints on TERM) → delete from `specs`, `procs`, `restarts`, and drop the agent from the fleet (it leaves `Snapshot()` entirely). The dashboard's "Removed" list is derived from the overrides file at status-write time, not from fleet state (§4).
- **Add:** append the spec and launch **through the existing `RestartBatch` budget** (default 1/tick ≈ 12/min), so bulk re-adds pace under the per-IP /login limit automatically. Reset that agent's `restarts` counter.
- **Update (rolling restart):** remove-keeping-override-state-clean (drain → stop) then add with the new spec. Implemented as remove with a `relaunchSpec` attached, so the relaunch fires on the tick after the stop completes.
- **Quarantined agent removed:** record-keeping only (its proc is already dead); clear quarantine + spec, no drain.
- **Idempotence:** duplicate requests for an agent collapse (last-op-wins per agent per drain of the queue). Remove for an unknown agent is a no-op (acked as such on the admin path).
- **Re-add while draining:** queued; applies on the tick after the stop completes.

### 2. Control-plane admin protocol (`pkg/overmind/control`, `supervisor/server.go`)

New envelope types on the existing unix socket:

- `admin_remove` `{agent_id}` — enqueue remove; also used by the dashboard after writing the overrides file.
- `admin_readd` `{agent_id}` — enqueue add (spec resolved from the overmind's current yaml copy). Clearing the agent from the overrides file is the *caller's* job; the dashboard does it before sending.
- `admin_ack` `{agent_id, status, detail}` — reply; `status ∈ accepted | unknown_agent | already_pending`.

Admin connections are ordinary socket connections that send an `admin_*` envelope instead of `hello`; `handleConn` routes them to a supervisor callback and replies with the ack on the same connection, which then closes. They never register in `conns`, so they cannot collide with worker routing. No auth: local unix socket, same trust domain as today's SIGUSR1.

### 3. `cmd/overmind`: SIGHUP + overrides

- Add `syscall.SIGHUP` to the existing `signal.Notify` set (main.go:127). Handler: re-read the fleet yaml and the overrides file, compute `effective`, diff against the supervisor's current roster:
  - present in effective, not live → enqueue **add**
  - live, not in effective → enqueue **remove**
  - present in both with a different spec (field-wise compare of `WorkerSpec`) → enqueue **update**
- **Yaml parse failure → log loudly and keep the current roster** (never drop a fleet on a bad edit). Same for an unreadable/corrupt overrides file.
- Boot applies the same subtraction, so dashboard-removes survive overmind restarts.
- Overrides file (`data/overmind/<fleet>-overrides.json`, derived from the fleet yaml path or an explicit `--overrides-file` flag):

```json
{
  "removed": ["craftsman-1"],
  "updated_at": "2026-07-23T03:05:00Z",
  "by": "dashboard"
}
```

Written only by the dashboard backend (atomic write: temp file + rename). The overmind only reads it. Absent file = empty override set.

### 4. Dashboard (`cmd/tools/overmind-dashboard` backend + `frontend/`)

- **Backend endpoints:**
  - `POST /api/fleets/{fleet}/agents/{id}/remove` — append id to the fleet's overrides file, then dial the fleet's socket and send `admin_remove`; return the ack (or a "socket down — override recorded, applies at next overmind boot" result, which is still success).
  - `POST /api/fleets/{fleet}/agents/{id}/readd` — remove id from the overrides file, then send `admin_readd`; same degraded-mode semantics.
  - Fleet → socket mapping: `data/overmind/<fleet>.sock` convention, overridable by flag.
- **Frontend:** Remove button on each agent row (confirm dialog naming the agent and fleet); row chip states `draining` (from the fleet `leaving` state in the status file) and a per-fleet **Removed** section listing override-parked agents with a Re-add button. Status-file schema gains the `leaving` flag and the removed list (written by the overmind's status writer from `Snapshot()` + overrides).

## Error handling

| Failure | Behavior |
|---|---|
| Yaml parse error on SIGHUP | Keep current roster; loud log |
| Overrides file corrupt | Treat as empty on read; loud log; dashboard write path recreates it |
| Drain timeout on remove | Force TERM after `RemoveDrainTimeout` (bounded, matches today's practice) |
| Socket down when button clicked | Override recorded; UI reports "applies at next overmind start" |
| Remove unknown agent | `admin_ack` `unknown_agent`, no-op |
| Re-add with no yaml spec (agent no longer in yaml) | `admin_ack` `unknown_agent` — re-add only resurrects yaml-listed agents |
| Worker not connected when drain sent (crashed/booting) | Skip drain wait; stop/delete immediately |

## Out of scope (YAGNI)

- Adding brand-new agents from the dashboard (adds enter via yaml).
- Cross-fleet moves; editing specs in the UI; socket auth.
- Changing quarantine/rescue semantics (removal of a quarantined agent is bookkeeping only).

## Testing

- **Supervisor:** deterministic `Tick`-driven tests with a fake spawn: remove drains-then-stops (and the fleet state walks idle→leaving→gone); drain-timeout forces TERM; add respects the `RestartBatch` budget and resets the restart counter; update rolls exactly one worker with the new args; remove-while-quarantined is bookkeeping-only; duplicate requests collapse; re-add-while-draining lands after the stop. Tests must discriminate (neuter-and-observe-red per the repo's vacuous-test lesson).
- **Control:** `admin_*` envelope round-trips; server routes an admin conn to the callback and acks without registering it in `conns`.
- **cmd/overmind:** yaml+overrides diff unit tests (add/remove/change/parse-error-keeps-roster); boot subtraction.
- **Dashboard backend:** handler tests against a fake unix socket (ack, socket-down degraded mode, atomic overrides write).
- **Frontend:** build check; state-chip rendering from a fixture status file.

## Rollout note

Ships in `bin/overmind` + `bin/overmind-dashboard` + frontend. Because SIGHUP is additive and the overrides file is optional, deploying is: rebuild, restart each fleet overmind once (the last ever full-restart-for-membership), restart the dashboard. First real use: re-adding craftsman-1 after the 2026-07-22 manual triage, via the button.
