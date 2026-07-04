# Stranded-Worker Quarantine + Assist Rescue

**Date:** 2026-07-03
**Status:** Approved design, pre-implementation
**Motivating incident:** After the 2026-07-02 fleet relaunch, 7 haulers (salvager-3/9/10, trader-1/5/8/9) sat fuel-dead (0–12 fuel) undocked at non-station POIs. The stall watchdog restarted each of them every 15 minutes indefinitely (~490 stall events on 2026-07-03 alone, restart counters up to 105) while their status still reported `healthy: true`. A process restart cannot refill a fuel tank; these workers need outside intervention.

## Goals

1. **Detect** workers that are stranded — stuck in a way a restart provably cannot fix.
2. **Quarantine** them: flag them visibly, stop the worker process, stop restarting it, and free the game session for manual intervention.
3. **Rescue** them automatically where possible: the five `assist-<capital>` agents fly fuel to the strandee.
4. **Rejoin** rescued workers to their fleet automatically.

Non-goals: preventing strands in the first place (fuel-guard / lawless routing is separate, queued work); towing, credit gifts, or any rescue beyond fuel transfer; retry logic for failed rescues (a failed rescue is an operator problem).

## Background / current behavior

- Each fleet (haul, mb, shuttle) runs under its own overmind process with a unix control socket. Workers heartbeat `control.Status` (system, POI, docked, fuel, max_fuel, credits, …) into `supervisor.Fleet`.
- `Fleet.ApplyStatus` advances `WorkerInfo.LastProgress` when system/POI/credits/docked change. `Stalled()` (pkg/overmind/supervisor/fleet.go) fires when a worker is undocked with no progress past `StallTimeout` (15 min); `reapAndRestart` (supervisor.go) then kills and relaunches it.
- Known defect (fix in scope): `MarkRestart` does not reset `LastProgress`, so the watchdog re-fires on the freshly restarted process before it reports in — restarts double-fire (log shows "restart #1" and "#2" in the same second).
- Known defect (fix in scope): a thrashing worker still reports `healthy: true` in the status JSON.
- The server API supports ship-to-ship fuel transfer: `refuel(item_id?, quantity?, target?)`. The Go client's `Refuel()` sends no payload; it must be extended.
- The five assist agents (`data/agents/assist-{frontier,haven,krynn,nexus,sol}/`) exist as credentials only. They fly starter ships fitted with a `refuel_rig`. They are not referenced by any worker/overmind code yet.
- **In-game usernames differ from disk aliases** (salvager-2 = "Jaxon 'JunkKing' Jarvis"). The in-game name is the `username` field of `data/agents/<alias>/credentials.json`. `refuel --target` takes the in-game username.

## Design

### 1. Detection (pkg/overmind/supervisor)

New predicate `Stranded(info WorkerInfo, now time.Time, cfg …) (bool, reason string)`, evaluated in `reapAndRestart` before the existing stall-restart case. True when either:

- **Fuel-dead signature (immediate):** `Stalled()` is true AND undocked AND `Fuel < max(FuelStrandFraction × MaxFuel, FuelStrandFloor)` with `FuelStrandFraction = 0.10`, `FuelStrandFloor = 10`. The worker cannot move; restarting is futile. All 7 observed strands match; healthy workers (lowest observed non-stranded: 57/90 = 63%) do not.
- **Futile-restart escalation (general):** the stall watchdog has restarted this worker `StallRestartLimit = 3` consecutive times with no progress between restarts. New `WorkerInfo.StallRestarts` counter: incremented when a stall-restart fires, reset to 0 whenever `LastProgress` advances. Catches unknown strand modes at ~60 min (the 3 futile restarts each consume a 15-min stall window, and detection happens in the following window) instead of thrashing forever.

Constants live with the existing supervisor config fields (StallTimeout et al.) and are overridable the same way.

**Double-fire fix (prerequisite, same change set):** `MarkRestart` zeroes `LastProgress`. `Stalled()` already returns false on a zero `LastProgress`; the clock restarts when the new process sends Hello (`ApplyHello` sets it). This also makes `StallRestarts` meaningful — without it the counter would hit the limit in seconds.

### 2. Quarantine

When `Stranded` fires, the supervisor:

1. Kills the worker process (existing `kill` path — frees the game session; no session_replaced tug-of-war during manual play_as).
2. Appends a rescue request to the queue file (§3). This happens BEFORE the flag is set: the rejoin poll treats quarantined-with-no-record as "operator resolved → release", so the record must land first or a slow enrichment races into a spurious relaunch.
3. Sets `WorkerInfo.Quarantined = true`, `QuarantineReason = <reason>`, `Healthy = false`.
4. Logs one loud line: `QUARANTINED <agent>: <reason>; rescue queued — no further restarts`.
5. Skips the agent in every subsequent `reapAndRestart` pass (checked before all restart cases) until rejoin (§5).

Status surfacing: the balances Recorder adds `"quarantined": bool` and `"quarantine_reason": string` to the per-worker entries in the status JSON, so `fleet-report` and the `:8087` dashboard show it. A quarantined worker is never `healthy: true`.

**Overmind restart persistence:** on boot, before first launch, the supervisor reads the queue file; any of its agents with an open (`pending`/`claimed`/`failed`) record starts quarantined instead of being launched stranded. A `done` record rejoins normally via §5.

### 3. Rescue queue — `data/overmind/rescue-queue.json`

Shared, flock-guarded (exclusive `syscall.Flock` on a sidecar `.lock` file — never the queue file itself, since the atomic temp+rename swaps the queue's inode and would let a blocked locker read stale content through its pre-rename fd; writers rewrite atomically: temp file + rename while holding the lock). Writers: the 3 fleet overminds (enqueue, rejoin-archive), the assist overmind (claim/done/failed), the operator (manual edits). A JSON array of records:

```json
{
  "agent_id": "trader-8",
  "target_username": "<in-game username from credentials.json>",
  "fleet": "haul",
  "system": "BD+20 2457",
  "poi": "bd20_2457_star",
  "fuel": 12,
  "max_fuel": 420,
  "rescue_fuel": 15,
  "reason": "fuel-dead: stalled >15m undocked, fuel 12/420",
  "status": "pending",
  "claimed_by": "",
  "requested_at": "2026-07-03T19:00:00Z",
  "updated_at": "2026-07-03T19:00:00Z"
}
```

- `target_username` is resolved **at enqueue time** from `data/agents/<agent_id>/credentials.json` (`username` field). The assist side never touches other fleets' credential files.
- `rescue_fuel = RescueFuelPerJump × jumps + RescueFuelBuffer`, floor `RescueFuelMin`, where `jumps` = KB BFS distance from the strandee's system to the nearest station-bearing system (the `FindNearestByPOIType` query the shuttle escape hatch uses). Constants: `RescueFuelPerJump = 5`, `RescueFuelBuffer = 5` (covers the intra-system leg to the station POI), `RescueFuelMin = 10`. If no reachable station is found, fall back to `RescueFuelFallback = 25`.
- Lifecycle: `pending → claimed → done | failed`. `failed` and manual-rescue completion are operator-driven: mark the record `done` by hand (documented `jq` one-liner). Records for rejoined workers are archived to `data/overmind/rescue-history.jsonl` and removed from the queue.
- Enqueue is idempotent: one open record per agent (re-detection while a record is open is a no-op).

### 4. Assist overmind + `assist` role (phase 2)

A fourth overmind instance: `assist.sock`, `data/overmind/assist-fleet.yaml` listing the 5 assist agents with role `assist` and a per-agent `home_base` param (their capital station: assist-haven → Haven, assist-sol → Sol, assist-krynn → Krynn, assist-frontier → Frontier, assist-nexus → Nexus Prime).

Standing behavior (`pkg/worker`, same engine pattern as haul/shuttle):

1. Idle docked at `home_base`, tank full.
2. Poll the rescue queue each cycle for `pending` records.
3. Claim the record if this agent is the **nearest** assist agent: the five home capitals are a fixed set, so each worker computes BFS jumps from **all five** capitals to the strandee's system and claims only if its own distance is minimal (ties broken by agent ID). No inter-agent communication needed; the flock on the queue resolves any race (first to set `status: claimed` + `claimed_by` wins; a record already `claimed` is skipped). If the elected agent is down, the record simply stays `pending` until it returns — acceptable; `failed`/stuck-`pending` records are visible to the operator.
4. Autopilot to the strandee's system, travel to its POI.
5. `refuel --target=<target_username> --quantity=<rescue_fuel>`.
6. Mark the record `done`; autopilot home, dock, refuel own tank.
7. Any step failing (route not found, refuel rejected, target absent): mark `failed`, return home. No retries — `failed` means operator.

Client extension: `RefuelShip(ctx, targetUsername string, quantity int) error` sending `{type: "refuel", payload: {target, quantity}}` — added to `client.go`, `GameClient` interface, runner dispatch, `isActionCommand`, and the mocks in pkg/agent + pkg/skills (the known interface-change gotcha; `go test ./...` catches it).

Assist workers are exempt from the fuel-dead quarantine check by role (they legitimately run their tank down mid-rescue; the escalation signal still covers them).

### 5. Rejoin

Each reap tick, an overmind holding quarantined agents reads the queue:

- Record `done` → relaunch the worker (normal spec launch; fresh login runs the role's own recovery — first move is autopilot-to-station + refuel), clear `Quarantined`, reset `StallRestarts`, archive the record to `rescue-history.jsonl`.
- Record `pending`/`claimed`/`failed` → still quarantined; keep showing in status.

Manual rescues follow the identical path: operator refuels by hand, marks the record `done`, worker rejoins on the next tick.

### 6. Testing

Table-driven unit tests in the existing styles:

- `Stranded()`: fuel signature thresholds (fraction vs floor, small/large tanks), docked/drained exemptions, escalation counter trip + reset-on-progress.
- `MarkRestart` zeroes `LastProgress` (double-fire regression test: two immediate reap ticks produce one restart).
- Queue file: enqueue idempotency, claim/done/failed transitions, concurrent flock writers, atomic rewrite, corrupt-file handling (log + treat as empty rather than crash).
- Rejoin: relaunch on `done`, hold on `pending`/`claimed`/`failed`, boot-time quarantine restore.
- Assist engine: nearest-agent claim election, happy path, each failure path marks `failed` and returns home — against the worker fake client.
- `RefuelShip`: payload shape; mock updates compile via `go test ./...`.

Live validation: phase 1 deploys against the 7 currently stranded haulers (they should quarantine within one stall window and the thrash stops). Phase 2's first live rescue targets whichever strandee is closest to a capital.

## Build order

- **Phase 1 — quarantine:** §1 detection + double-fire fix, §2 quarantine, §3 queue (enqueue + operator-manual `done`), §5 rejoin. Immediately stops the watchdog thrash and unblocks manual rescue through the queue.
- **Phase 2 — rescue:** §4 client `RefuelShip`, assist role, assist overmind deployment.

## Decisions log (from brainstorm)

- Quarantine **stops** the worker process (frees the session) rather than leaving it running flagged.
- Detection uses **both** the fuel-dead signature and futile-restart escalation.
- Assist agents live in a **dedicated 4th overmind**; cross-overmind communication is the **shared queue file**, not sockets.
- Rejoin is **automatic on `done`**, one path for auto and manual rescues.
- Rescue fuel is **distance-sized** (5/jump + buffer), not a full tank; user-specified.

## Deployment

Station picks for `data/overmind/assist-fleet.yaml` were confirmed against
`data/spacemolt-knowledge.db` (`pois` table, `type='station'`) for Haven, Sol,
Krynn, and Nexus Prime — all matched the known-good marketbot/fleet-status
docks (`grand_exchange`, `sol_central`, `war_citadel`, `the_core`).

### assist-frontier: mobile home (amended 2026-07-04)

assist-frontier does **not** work out of a fixed Frontier station: its home is
`mobile_capital`, the Outerrim empire's capital base, which hyperspace-jumps to
another of its empire's systems once a day (fixed, learnable rotation; the
server sends a push announcement before each jump). The original
`expedition_launch` pick was wrong and has been replaced.

Mechanism (user-directed): the in-game routing tool always knows where
`mobile_capital` is, so the worker resolves its current system with
`find_route mobile_capital` (a no-tick-cost query) instead of learning the
schedule or parsing announcements.

- `pkg/worker/assist.go` splits homes into the static `assistHomes` map (four
  fixed capitals) and `assistMobileHomes` (`assist-frontier` →
  `mobile_capital`). `resolveAssistHomes` overlays the mobile entries via
  `FindRoute`, taking the last route step's system (empty route = we are
  already in that system).
- **Election:** all five agents resolve the mobile home the same way, so the
  deterministic nearest-home election still agrees. A transient `find_route`
  failure drops the mobile home from that agent's candidate set for the pass
  (logged); the queue's CAS claim backstops the brief disagreement.
- **Ensure-home:** assist-frontier re-resolves on every return, so a capital
  jump mid-rescue retargets the return leg to the new system. Resolution
  failure logs and stays put; the standing loop retries next pass.
- Future options (not built): subscribe to the jump-announcement push event;
  learn the fixed daily rotation. Per-pass `find_route` makes both unnecessary
  for correctness.

All five `data/agents/assist-*/credentials.json` files are present. Fitted
`refuel_rig` per agent is a live-game check this task cannot perform —
**operator TODO:** verify each assist agent's ship actually carries a
`refuel_rig` before the first live rescue.

Rebuild: `go build -o bin/overmind ./cmd/overmind && go build -o bin/worker ./cmd/worker`

Assist overmind (4th fleet):
setsid nohup ./bin/overmind --socket data/overmind/assist.sock \
  --fleet data/overmind/assist-fleet.yaml --worker-bin bin/worker \
  --status-file data/overmind/assist-status.json \
  --history-file data/overmind/assist-history.jsonl \
  --stagger 10s >> data/overmind/assist-overmind.log 2>&1 &

Fleet overminds pick up quarantine on their next binary restart (drain first:
kill -USR1 <pid>). Manual rescue: refuel by hand, then mark the record done —
flock data/overmind/rescue-queue.json.lock -c \
  'jq '\''(.[] | select(.agent_id=="X") | .status) = "done"'\'' data/overmind/rescue-queue.json > /tmp/rq && mv /tmp/rq data/overmind/rescue-queue.json'
(the flock on the .lock sidecar is the same lock the overminds take — without it a manual edit can race a concurrent claim/enqueue and silently drop it)
