# Explorer Weekly Sweep Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a fleet yaml set the `explorer` role's staleness age (`explore_stale_days`) so one ship sweeps the whole galaxy about weekly.

**Architecture:** One new field threaded the way `frontier_wedge` is: supervisor `WorkerSpec` yaml field → `--explore-stale-days` worker flag → `WorkerDispatch.ExploreStaleDays` → `ExploreDeps.StaleTicks` (days × 8640). Unset (0) keeps today's 1-day default.

**Tech Stack:** Go 1.24, existing worker/supervisor packages.

**Spec:** docs/superpowers/specs/2026-11-26-explorer-weekly-sweep-design.md

## Global Constraints

- Default behaviour unchanged when the field is absent (1 day = `DefaultExploreStaleTicks` 8640).
- golangci-lint clean; no `time.Sleep` in pkg/worker; binaries in bin/.

## Review Focus

- Negative `explore_stale_days`: treated as unset (default), never a negative tick window.
- A worker spec without the field gets no `--explore-stale-days` arg.

---

### Task 1: explore_stale_days end to end

**Files:**
- Modify: `pkg/overmind/supervisor/config.go` (WorkerSpec, after FrontierRun)
- Modify: `pkg/overmind/supervisor/supervisor.go:~106` (arg forwarding)
- Modify: `cmd/worker/main.go:~96,~546` (flag + assignment)
- Modify: `pkg/worker/dispatch.go:~92` (field), `:534` (explore case)
- Test: `pkg/overmind/supervisor/sweep_args_test.go`, `pkg/worker/dispatch_test.go`

**Interfaces:**
- Produces: `WorkerSpec.ExploreStaleDays int` (`yaml:"explore_stale_days,omitempty"`); worker flag `--explore-stale-days int`; `WorkerDispatch.ExploreStaleDays int`.

- [ ] **Step 1: Write failing tests**
  - `TestExploreStaleDaysPassedThrough` (supervisor): spec `{AgentID: "explorer-7", ExploreStaleDays: 7}` → args contain `--explore-stale-days 7`; spec without it → no `--explore-stale-days`.
  - `TestDispatchExploreStaleDaysWidensFreshWindow` (worker): systems a (current) and b (LastVisitedTick 5, LastUpdatedTick = now−2×8640), connected; state CurrentTick = now. With `ExploreStaleDays: 7` explore does NOT call `find_route:`; with 0 it calls `find_route:b`.
- [ ] **Step 2: Run, expect FAIL** — `go test ./pkg/overmind/supervisor/ ./pkg/worker/ -run 'ExploreStaleDays'` → compile errors (unknown field).
- [ ] **Step 3: Implement** — field, forwarding (only when > 0), flag, dispatch field; explore case passes `StaleTicks: int64(max(d.ExploreStaleDays,0)) * DefaultExploreStaleTicks`.
- [ ] **Step 4: Run, expect PASS**; then `go build ./... && go test ./pkg/worker/ ./pkg/overmind/... ./cmd/worker/` and `golangci-lint run ./pkg/worker/... ./pkg/overmind/... ./cmd/worker/...`.
- [ ] **Step 5: Commit** (explicit paths) — `explorer: explore_stale_days fleet setting for weekly sweeps`.

### Task 2: Roll out explorer-7

- [ ] Check explorer-7's fit for a survey scanner (`get_ship`); report if missing.
- [ ] Rebuild `bin/worker`; add explorer-7 to a fleet yaml with `role: explorer, explore_stale_days: 7` (remove it from any `removed` set that would block it); SIGHUP that overmind; confirm its log shows `explore: heading to …`.
- [ ] Commit the yaml (explicit path).
