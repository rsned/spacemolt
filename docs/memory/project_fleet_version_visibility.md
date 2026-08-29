---
name: project_fleet_version_visibility
description: "Surface each overmind/worker build version in the overmind-dashboard (current/behind), lightweight local git-tag SemVer scheme — spec APPROVED 2026-07-23, writing implementation plan"
metadata:
  node_type: memory
  type: project
  originSessionId: 86df3835-f10f-4c25-928d-7948d122ecd7
  modified: 2026-07-23T23:24:31.422Z
---

**BUILT + MERGED to main `169d5e0` 2026-07-23 (fast-forward, branch feat/fleet-version-visibility deleted; NOT pushed — main now 23 commits ahead of origin; NOT yet deployed).** Full SDD run: 7 tasks (83e6e51 buildinfo, 438e238 build.sh stamping, 29aae08 control.Hello, 3b00224 worker+supervisor, 0dcc7ef balances, 7278c76 ovdash tiering, 8009442 frontend), all task-reviewed+Approved [sonnet], + Opus whole-branch review READY-TO-MERGE, + 1 hardening fix (169d5e0: currentVersion skips unparseable-SemVer samples so a stray plain-`go build` binary can't skew the "current" reference and redden the fleet). Gates: build/test/lint-0/frontend all green.

**✅ v0.1.0 TAGGED + BASELINE BINARIES BUILT 2026-07-23 (main `7d73cd4`, tag v0.1.0, NOT pushed).** Annotated tag v0.1.0 at 7d73cd4. `bin/overmind`, `bin/worker`, `bin/overmind-dashboard` built via `./scripts/build.sh`, all stamped `version=v0.1.0 codeDirty=false` → GREEN. Clean-baseline prep (user chose "full clean"): removed stray root `./overmind` artifact; gitignored `/overmind` `/overmind-dashboard` (match existing `/worker`) + `cmd/debug/enqueue-rescue/`; **FIXED build.sh: dropped `--dirty` from `git describe`** (data/*.json churn was polluting the label → "-dirty"; codeDirty carries the real signal, data-excluded) — commit 7d73cd4. **ROLLOUT PENDING: running fleets still on OLD unstamped "(deleted)" binaries → dashboard shows them legacy/red until restarted; the stamped v0.1.0 lands naturally on the next fleet redeploy (e.g. the probation feature deploy). No mass-restart done (disruptive; login rate limits).**

**⚠️ ORIGINAL DEPLOY NOTE (Opus) — now partly satisfied (tag+build done):** (1) cut initial tag `git tag -a v0.1.0` FIRST — without a tag `git describe --tags --always` = bare hash = unparseable = EVERYTHING red; (2) build via `./scripts/build.sh` (stamps ldflags version=git-describe + codeDirty); (3) deploy build.sh outputs ONLY (a plain `go build` binary in the fleet skews "current"); (4) roll fleet-by-fleet — dashboard greens as each stamped binary lands. First tag number: dynamic-fleet-membership + this = candidate v0.1.0 or v0.2.0 per Minor-per-feature policy. NOTE: `scripts/build.sh` is now the 3-binary STAMPING release builder; old recursive `-race` dev builder moved to `scripts/build-all.sh`.

**Original spec APPROVED + committed `ea4927a` (`docs/superpowers/specs/2026-07-23-fleet-version-visibility-design.md`).**

Goal: show each overmind's + worker's build version in overmind-dashboard so operators see who's current vs behind during a rolling binary upgrade.

**Locked decisions:**
- New `pkg/buildinfo`: `Get() Info{Version,Commit,BuiltAt,Modified,CodeDirty}`. `Version` = ldflags `git describe --tags --always --dirty`; Commit/BuiltAt/Modified from `runtime/debug.ReadBuildInfo()` (`vcs.revision/time/modified`, auto-embedded, zero build change). Fallback → module pseudo-version → `"dev"`, never panics.
- **Per-worker** granularity (not fleet-level): worker reports version in `control.Hello` → `ApplyHello` → `balances.LiveRecord.Version`. Overmind writes its own into `balances.StatusFile.OvermindVersion/OvermindBuiltAt`. ovdash reads status files.
- **Current = newest `built_at`** seen across all fleets (self-adjusting). Color by SemVer DISTANCE (reuse `pkg/version.ParseSemVer`+`MinorDiff` on git-describe base tag): **green**=current, **yellow**=same Major.Minor but patch/commits-behind OR code-dirty, **red**=different Minor/Major or legacy/unparseable.
- **codeDirty** = tracked changes EXCLUDING `data/` (`git status --porcelain -- ':!data/'`), stamped via ldflags `buildinfo.codeDirty`. Raw `vcs.modified` is ~always true (data/*.json churn) so it's COSMETIC `*` marker only, never gates color.
- **Local git tags only, NOT GitHub releases.** Release policy: **Patch 0.0.X = fixes, Minor 0.X.0 = brainstormed features, Major reserved (stays 0).** SEPARATE axis from `pkg/version` VersionID/BuiltForAPIVersion (those = game-server API). First tag ~v0.1.0/v0.2.0 (dynamic-fleet-membership + this feature are each minor bumps).
- Build: `scripts/build.sh` stamps ldflags for all 3 binaries; plain `go build` still works (shows `dev`).

Frontend: per-fleet header badge (overmind version, tier color) + worker-version summary grouping counts (`v0.3.0 ×35 v0.2.9 ×6`). Relates to [[project_fleet_pool_dynamic_membership]] (built the rolling-upgrade capability this makes visible) [[project_overmind_dashboard_v1]].
