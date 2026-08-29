---
name: project_fleet_pool_dynamic_membership
description: "Dynamic overmind worker exit/join without a full fleet restart — BUILT + MERGED to main 2026-07-23 (NOT pushed, NOT yet rolled out)"
metadata: 
  node_type: memory
  type: project
  originSessionId: 1e4c8a31-2e69-4e33-8b4b-1c2956839940
  modified: 2026-07-23T19:51:32.013Z
---

**BUILT + MERGED to main `8016cd8` 2026-07-23 (branch feat/dynamic-fleet-membership, deleted; NOT pushed to origin; NOT yet rolled out).** Full SDD run, 8 tasks + final-review fix wave, every task/review Approved. Add/remove/update a single overmind worker in a live fleet without draining+restarting the whole thing.

**What shipped:**
- Supervisor membership engine (`pkg/overmind/supervisor/membership.go` + supervisor.go): MembershipOp add/remove/update queued via `EnqueueMembership`, applied at reap-tick start (mirrors the `releases` pattern; specs/restarts/leaving owned solely by the reap goroutine). Remove = per-worker drain (`RemoveDrainTimeout` 4min) then stop; add/rolling-update paced through `RestartBatch` budget (1/tick). `Roster()` snapshot behind specsMu.
- Control-plane admin envelopes (`pkg/overmind/control/messages.go`): `admin_remove`/`admin_readd`/`admin_ack` (status accepted|unknown_agent|already_pending); admin conns hit `SetAdminHook`, get one ack, never register in `conns`.
- Overrides sidecar (`pkg/overmind/supervisor/overrides.go`): `data/overmind/<fleet>-overrides.json`, atomic temp+rename, dashboard is SOLE writer, overmind read-only. Effective roster = yamlSpecs − overrides.removed. YAML NEVER machine-rewritten.
- `cmd/overmind`: SIGHUP re-reads yaml+overrides, `diffSpecs` → enqueue add/remove/update; boot applies same subtraction (fatal on bad boot yaml); `--overrides-file` flag.
- Dashboard (`pkg/ovdash/admin.go` + `cmd/overmind-dashboard`): POST `/api/overmind/fleets/{fleet}/agents/{id}/remove|readd`, writes sidecar THEN sends envelope, `recorded_offline` degraded mode when socket down. Snapshot gains `leaving`/`removed`.
- Frontend: per-agent ✕ Remove button (window.confirm names agent+fleet), draining chip, per-fleet Removed section + Re-add.

**Fix wave (F1 fleet-killer, must-fix):** empty/truncated-but-VALID yaml on SIGHUP would emit REMOVE for every live worker → `safeDiff` guard refuses a reload that empties or drops >half a running fleet (strict `removes*2>len(live)`; boot ungated). F2: remove→quick-readd within drain window vanished worker → memberAdd cancels pending removal + sends TypeResume to un-park. F3: quarantine-release+remove ghost → drainReleases before applyMembership. F6: ovdash SetWriteDeadline(5s).

**Accepted-not-fixed (fast-follow candidates, none fleet-endangering):** F4 disconnected worker waits full drain timeout; F5 dead code (unread `StatusFile.Removed`, unreturned `AckAlreadyPending`, unused `makeAdminHook` overridesPath param — drop on next touch); 0600 sidecar perms (dashboard+overmind both run as robert); SaveOverrides-failure→HTTP 400 (arguably 500); UI: no in-flight button disable (double-POST, supervisor idempotent), 60s Removed-section lag (Delta carries no removed field — only keyframe does; draining chip IS prompt ~2s), undifferentiated alert().

**ROLLOUT IN PROGRESS (2026-07-23), feature PROVEN LIVE via BOTH front doors:**
- ✅ Rebuilt bin/overmind + bin/overmind-dashboard + bin/worker + frontend from main 8016cd8.
- ✅ SHUTTLE bootstrapped to new binary (canary); johnny_cab healthy. Then rolling-cycle proof: dashboard remove→readd of johnny_cab end-to-end (sidecar write/clear, drain→4min force-stop since busy, respawn). DASHBOARD BUTTON PATH VALIDATED.
- ✅ DASHBOARD (:8091) restarted on new binary + new frontend/dist.
- ✅ MISSION-LEARN bootstrapped to new binary (graceful drain of 41 → relaunch --stagger 10s → 41 healthy in ~6.5min). Then **craftsman-1 RE-ADDED via SIGHUP** (un-comment yaml line 22 + `kill -HUP`): "received SIGHUP → 1 membership change enqueued → membership: added craftsman-1". Fleet now 42/42 healthy, craftsman-1 docked Haven routing freight (max_packages 11). SIGHUP YAML-DIFF PATH VALIDATED. First real production use. yaml line now UN-commented (uncommitted working-tree edit).
- ✅ ROLLOUT COMPLETE 2026-07-23: ALL fleets now on new membership binary. marketbots (35/35), craft (9/9, plan runner enabled), assist (5/5) = easy full down+restart+stagger. HAUL (21/21) = graceful bootstrap: SIGUSR1 drain → 11/21 idled cleanly, drain-poll-ended bound reached ~3.5min, SIGTERM force-stopped the 10 stragglers (0 orphans), relaunched --stagger 10s → 21/21 healthy; scanner confirmed alive throughout. Ramped fleets SERIALLY (never 2 at once — >12 logins/min trips per-IP /login limit).
- Gotcha confirmed live: BUSY worker takes full 4min force-stop on remove; idle/docked workers drain instantly (the fast rolling-cycle target). Enhancement idea → [[project_haul_rolling_drain_on_completion]].

Relates to [[project_overmind_fleet_manager]] [[project_shipping_carrier]] [[reference_craftsman1_vacuum_bid_economics]] [[project_overmind_stall_kill_connect_loop]] (F2/D2 partially mitigate the pre-Hello kill-loop). Original trigger: trader-9 stuck-worker recovery deferred 2026-06-27 + the 10-engineer 41→31 restart 2026-07-22.
