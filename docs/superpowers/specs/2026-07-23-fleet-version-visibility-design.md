# Fleet Binary Version Visibility — Design

**Date:** 2026-07-23
**Status:** Draft (awaiting user review)

## Problem

During a rolling binary upgrade (the dynamic-fleet-membership rollout, 2026-07-23), fleets run mixed builds: some overminds/workers are on the new binary, some on the old one. Today nothing surfaces which build a running process is on — the operator reconstructs it from `/proc/<pid>/exe` timestamps and memory. There is no in-product answer to "which fleets are current, which are behind?"

The binaries already carry the raw data: Go's build system auto-embeds `vcs.revision` (git commit), `vcs.time` (commit time), and `vcs.modified` (dirty flag), readable at runtime via `runtime/debug.ReadBuildInfo()`. What's missing is (a) a human-readable SemVer label, (b) reporting each process's version up to the dashboard, and (c) a current-vs-behind display.

## Decisions (user-answered)

1. **Worker granularity:** per-worker version. Each worker reports its own build; the dashboard shows mixed versions within a fleet mid-roll (e.g. `v0.3.0 ×35  v0.2.9 ×6`). This is the point — you watch a fleet convert one worker at a time.
2. **"Current" reference:** the newest build seen across all fleets. The dashboard computes the max build-time observed; anything older is "behind." Zero config, self-adjusting as you deploy.
3. **Version label:** `git describe --tags --always --dirty` SemVer + commit (e.g. `v0.3.0-2-g8016cd8`), stamped via ldflags, layered on the auto-embedded commit/time.
3a. **Color tiers (SemVer-distance from the current build):**
   - **Green** — current: identical to the newest build (same version string, code-clean).
   - **Yellow** — same `Major.Minor` as current but drifted: behind on Patch / commits-ahead, **or** code-dirty. "Missing fixes only," low risk.
   - **Red** — different `Minor`/`Major` from current, **or** `legacy`/unstamped. "Missing a whole feature, or unknown."
   Build-time (`vcs.time`) still selects *which* build is current; SemVer distance selects the *color*. Parsing reuses `pkg/version.ParseSemVer` + `MinorDiff` on the base tag from `git describe`.
3b. **"Dirty" means code-dirty, not raw VCS dirty.** Go's `vcs.modified` is ~always true because tracked `data/*.json` churns, which would make green unreachable. The build script computes a separate `codeDirty` flag over tracked files **excluding `data/`** (`git status --porcelain -- ':!data/'`) and stamps it. Coloring uses `codeDirty`; raw `vcs.modified` is a cosmetic `*` marker only.
4. **Scheme:** local git tags only — **not** GitHub releases. Tags are local git objects; pushing to GitHub is an optional later step that changes nothing here.
5. **Release policy (when to bump):**
   - **Patch `0.0.X`** — bug fixes and patches.
   - **Minor `0.X.0`** — features that went through the brainstorm→spec→plan flow.
   - **Major** — reserved; stays `0` for now.

This SemVer axis tracks **our fleet binary builds**. It is deliberately separate from the existing `pkg/version` constants (`VersionID`, `BuiltForAPIVersion`), which track the **game server API** the client targets. The two are not unified.

## Architecture

One new package, `pkg/buildinfo`, is the single source of a binary's identity:

```
buildinfo.Get() → Info{ Version, Commit, BuiltAt, Modified }
```

- `Version` — a package-level `var version string` set at build via ldflags (`git describe --tags --always --dirty`). When unset (plain `go build`), falls back to the module pseudo-version from `ReadBuildInfo()`, else the literal `"dev"`. Never panics.
- `Commit`, `BuiltAt`, `Modified` — read from `runtime/debug.ReadBuildInfo().Settings` (`vcs.revision` short-form, `vcs.time` parsed to `time.Time`, `vcs.modified`). Zero build change required for these.

Every binary (`cmd/overmind`, `cmd/worker`, `cmd/overmind-dashboard`) imports `buildinfo`. The version rides the **existing** worker→overmind→status-file→dashboard path — no new transport:

```
worker buildinfo.Get()
   → control.Hello{Version,Commit,BuiltAt}     (worker → overmind, on connect)
   → Fleet.ApplyHello stores on WorkerInfo
   → balances.LiveRecord.Version               (per-worker, in status file)
overmind buildinfo.Get()
   → balances.StatusFile.Overmind{Version,BuiltAt}
dashboard reads all *-status.json
   → compute newest BuiltAt across every overmind + worker
   → flag each as current / behind
   → render
```

## Components

### 1. `pkg/buildinfo` (new)
- `type Info struct { Version, Commit string; BuiltAt time.Time; Modified bool; CodeDirty bool }`
- `func Get() Info` — memoized; reads `ReadBuildInfo()` once.
- `var version string` — ldflags target `-X github.com/rsned/spacemolt/pkg/buildinfo.version=...`.
- `var codeDirty string` — ldflags target `...buildinfo.codeDirty=true|false`; parsed to `Info.CodeDirty` (unset ⇒ false).
- `Modified` = raw `vcs.modified` (cosmetic); `CodeDirty` = the color-relevant flag.
- Unit-tested: fallback ladder (ldflags var → pseudo-version → `"dev"`), settings parse, missing-settings safety, `codeDirty` parse.

### 2. Build stamping (`scripts/build.sh` + optional `Makefile`)
- A script that builds the fleet binaries into `bin/` with the ldflags stamp:
  ```
  DESC=$(git describe --tags --always --dirty)
  # code-dirty = tracked changes OUTSIDE data/ (data/*.json churns constantly)
  test -z "$(git status --porcelain -- ':!data/')" && CODEDIRTY=false || CODEDIRTY=true
  LDFLAGS="-X github.com/rsned/spacemolt/pkg/buildinfo.version=$DESC \
           -X github.com/rsned/spacemolt/pkg/buildinfo.codeDirty=$CODEDIRTY"
  go build -ldflags "$LDFLAGS" -o bin/overmind ./cmd/overmind
  go build -ldflags "$LDFLAGS" -o bin/worker ./cmd/worker
  go build -ldflags "$LDFLAGS" -o bin/overmind-dashboard ./cmd/overmind-dashboard
  ```
- One ldflags target covers all binaries (it names the shared `buildinfo` package). A plain `go build ./...` still works and just yields `dev` + the embedded commit (`codeDirty` unset → treated as unknown/false) — the script is for release builds, not a hard requirement.

### 3. `pkg/overmind/control` — `Hello`
- Add `Version string`, `Commit string`, `BuiltAt string` (RFC3339) to `control.Hello`. Backward compatible: an old worker omits them; the overmind treats empty as "unknown."

### 4. `pkg/overmind/supervisor` — `ApplyHello` / `WorkerInfo`
- Store the hello's version fields on `WorkerInfo`; surface into the per-worker `LiveRecord`.

### 5. `pkg/overmind/balances`
- `LiveRecord.Version string` (+ `Commit`, `BuiltAt` as needed), `omitempty`.
- `StatusFile.OvermindVersion string`, `StatusFile.OvermindBuiltAt string` — the overmind writes its own `buildinfo.Get()` when it writes the status file.

### 6. `pkg/ovdash`
- Snapshot gains per-worker `{version, commit, built_at, code_dirty}` + per-fleet overmind version.
- Determine the **current** build = the one with the newest `built_at` across all overminds and workers.
- Classify each process into a color tier vs current (parse base SemVer via `pkg/version.ParseSemVer` on the `git describe` base tag):
  - **green** — same version string as current AND not code-dirty.
  - **yellow** — same `Major.Minor` as current, but behind on patch/commits **or** code-dirty.
  - **red** — `MinorDiff != 0` (or major differs), or unparseable/`legacy`/no version.
- Expose the tier per worker and a rolled-up fleet tier (worst tier present) on the snapshot.

### 7. `frontend/`
- Per-fleet header badge: overmind version + tier color (green/yellow/red).
- Worker-version summary line grouping counts by version (`v0.3.0 ×35  v0.2.9 ×6`), each group colored by its tier.
- Raw `vcs.modified` shown as a small `*` marker; it is cosmetic and never changes the tier (only `code_dirty` does).

## Data flow / ordering

- **Ordering key is build-time (`vcs.time`)**, not SemVer — monotonic and robust even if a tag is applied out of order. SemVer is the display label; build-time decides current-vs-behind.
- "Newest seen" is recomputed each dashboard render from the live status files, so it self-adjusts through a roll.

## Error handling

| Case | Behavior |
|---|---|
| Plain `go build` (no ldflags) | `Version` = module pseudo-version, else `"dev"`; commit/time still from embedded VCS |
| Binary predates this feature (no version in hello / status) | Shown as `legacy`, classified **red** (correct during this rollout) |
| `vcs.modified=true` (dirty tree) | Cosmetic `*` marker only; never affects tier (noisy from `data/*.json` churn) |
| `codeDirty=true` (uncommitted code outside data/) | Pushes an otherwise-green build to **yellow** |
| `codeDirty` unstamped (plain `go build`) | Treated as false; the `dev` version already classifies red via unparseable SemVer |
| `ReadBuildInfo()` returns `ok=false` (unusual) | `Commit`/`BuiltAt` empty; `Version` = `"dev"`; no panic |

## Out of scope (YAGNI)

- Version history / changelog per release.
- Auto-upgrade or "click to roll" from the version display (membership buttons already exist separately).
- Publishing tags to GitHub Releases (optional manual step, unchanged by this design).
- Unifying with `pkg/version` game-API constants.

## Testing

- **buildinfo:** fallback ladder, settings parse, missing-settings safety.
- **control:** `Hello` round-trip carrying `Version`/`Commit`/`BuiltAt`; old-hello (empty) decodes clean.
- **balances:** status file carries per-worker `Version` and overmind version; existing callers updated.
- **ovdash:** current = newest built_at; tier classification on a fixture covering green (==current), yellow (same minor, patch-behind), yellow (code-dirty at current version), red (minor behind), red (legacy/unparseable); rolled-up fleet tier = worst present.
- **frontend:** production build clean; badge + summary render from a fixture status file across all tiers, plus the cosmetic `*` for raw dirty.

## Rollout note

Ships in `pkg/buildinfo`, the three binaries, and the frontend. First tag: apply `git tag -a v0.X.0` per the release policy, then build via `scripts/build.sh`. Because reporting is additive and unstamped/legacy binaries degrade to `dev`/`legacy`, deploying is safe mid-roll — the not-yet-updated fleets simply show as behind, which is the intended signal.
