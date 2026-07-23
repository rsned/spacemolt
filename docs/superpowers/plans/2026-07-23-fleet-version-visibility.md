# Fleet Binary Version Visibility — Implementation Plan

**For agentic workers:** REQUIRED SUB-SKILL: `superpowers:test-driven-development`. Every task below is written test-first — you MUST write the failing test, watch it fail for the stated reason, then write the minimal implementation. Do not skip the run-it-fails step.

**Goal:** Surface, per running binary, which build a fleet's overmind and each of its workers is on, and color-tier each as current / behind so an operator can watch a rolling binary upgrade convert one worker at a time.

**Architecture:** A new leaf package `pkg/buildinfo` reads Go's embedded VCS stamps plus two ldflags vars (`version`, `codeDirty`) into an `Info` value. Each worker reports its build in the existing `control.Hello`; the overmind stores it on `WorkerInfo`, folds it into each `balances.LiveRecord`, and writes its own build into the `StatusFile`. `pkg/ovdash` reads every status file, picks the newest build as "current", classifies every worker and overmind into a green/yellow/red SemVer-distance tier, and the React dashboard renders per-fleet badges and a per-version worker count summary.

**Tech Stack:** Go 1.24 (backend, `runtime/debug.ReadBuildInfo`, ldflags stamping), React 19 + TypeScript + Vite (frontend), SSE for the live snapshot stream. Reuses `pkg/version` SemVer parsing.

## Global Constraints

These are the spec's binding rules (docs/superpowers/specs/2026-07-23-fleet-version-visibility-design.md), copied verbatim as the authority for every decision below:

- **Color tiers (SemVer-distance from the current build):**
  - **Green** — current: identical to the newest build (same version string, code-clean).
  - **Yellow** — same `Major.Minor` as current but drifted: behind on Patch / commits-ahead, **or** code-dirty. "Missing fixes only," low risk.
  - **Red** — different `Minor`/`Major` from current, **or** `legacy`/unstamped. "Missing a whole feature, or unknown."
  - Build-time (`vcs.time`) still selects *which* build is current; SemVer distance selects the *color*.
- **`codeDirty` means code-dirty, not raw VCS dirty.** Go's `vcs.modified` is ~always true because tracked `data/*.json` churns. The build script computes a separate `codeDirty` flag over tracked files **excluding `data/`** (`git status --porcelain -- ':!data/'`) and stamps it. Coloring uses `codeDirty`; raw `vcs.modified` is a cosmetic `*` marker only.
- **"Current" reference** = the newest build seen across all fleets: the max `built_at` (vcs.time) observed. Ordering key is build-time, not SemVer — monotonic and robust even if a tag is applied out of order. Recomputed each render, zero config, self-adjusting through a roll.
- **Per-worker granularity.** Each worker reports its own build; the dashboard shows mixed versions within a fleet mid-roll.
- **Scheme: local git tags only** — not GitHub releases. Pushing to GitHub is an optional later step that changes nothing here.
- **ldflags targets** `github.com/rsned/spacemolt/pkg/buildinfo.version` (from `git describe --tags --always --dirty`) and `github.com/rsned/spacemolt/pkg/buildinfo.codeDirty` (`true|false`).
- **Version fallback ladder:** ldflags `version` var → module pseudo-version from `ReadBuildInfo()` → literal `"dev"`. Never panics.
- **Raw `vcs.modified` is cosmetic-only** (`*` marker); never affects the tier. Only `codeDirty` does.
- **Release policy:** Patch `0.0.X` = bug fixes/patches. Minor `0.X.0` = features that went through brainstorm→spec→plan. Major reserved, stays `0`.
- **SEPARATE axis** from the `pkg/version` game-API constants (`VersionID`, `BuiltForAPIVersion`), which track the game-server API the client targets. The two are deliberately not unified.

## Deviation from the suggested decomposition (read before Task 3)

The spec's Component 3 lists only `Version/Commit/BuiltAt` on `control.Hello`, but Component 6 requires per-worker `code_dirty` for tier classification, and there is no other transport for it. This plan therefore also adds `CodeDirty` and `Modified` (raw) to `Hello`, `WorkerInfo`, and `LiveRecord`, so the worker's color-relevant flag and cosmetic `*` marker reach the dashboard. All five fields flow as flat fields at each layer, matching the codebase's existing flat-copy style (System/POI/Credits are copied field-by-field at every hop, not via a shared struct).

---

## Task 1 — `pkg/buildinfo` package

**Files:**
- create `pkg/buildinfo/buildinfo.go`
- create `pkg/buildinfo/buildinfo_test.go`

**Interfaces:**
- Produces: `type Info struct { Version, Commit string; BuiltAt time.Time; Modified, CodeDirty bool }`
- Produces: `func Get() Info` (memoized; reads `runtime/debug.ReadBuildInfo()` once)
- Produces (unexported, for testability): `func resolve(version, codeDirty string, bi *debug.BuildInfo, ok bool) Info`
- Consumes: `runtime/debug` (`ReadBuildInfo`, `BuildInfo`, `BuildSetting`, `Module`), `time`, `sync`

### Steps

1. **Write the failing test** — `pkg/buildinfo/buildinfo_test.go`:

```go
package buildinfo

import (
	"runtime/debug"
	"testing"
	"time"
)

func settings(kv ...string) *debug.BuildInfo {
	bi := &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}
	for i := 0; i+1 < len(kv); i += 2 {
		bi.Settings = append(bi.Settings, debug.BuildSetting{Key: kv[i], Value: kv[i+1]})
	}
	return bi
}

func TestResolveLdflagsVersionWins(t *testing.T) {
	got := resolve("v0.3.0-2-g8016cd8", "true",
		settings("vcs.revision", "8016cd8abcdef0123456", "vcs.time", "2026-07-23T10:00:00Z", "vcs.modified", "true"), true)
	if got.Version != "v0.3.0-2-g8016cd8" {
		t.Fatalf("Version = %q, want stamped ldflags value", got.Version)
	}
	if !got.CodeDirty {
		t.Fatalf("CodeDirty = false, want true from codeDirty=\"true\"")
	}
	if got.Commit != "8016cd8abcde" {
		t.Fatalf("Commit = %q, want 12-char short revision", got.Commit)
	}
	if !got.BuiltAt.Equal(time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("BuiltAt = %v, want parsed vcs.time", got.BuiltAt)
	}
	if !got.Modified {
		t.Fatalf("Modified = false, want raw vcs.modified=true")
	}
}

func TestResolveFallsBackToPseudoVersion(t *testing.T) {
	bi := settings("vcs.revision", "deadbeef")
	bi.Main.Version = "v0.0.0-20260723100000-8016cd8abcde"
	got := resolve("", "", bi, true)
	if got.Version != "v0.0.0-20260723100000-8016cd8abcde" {
		t.Fatalf("Version = %q, want module pseudo-version fallback", got.Version)
	}
	if got.CodeDirty {
		t.Fatalf("CodeDirty = true, want false when codeDirty unstamped")
	}
}

func TestResolveFallsBackToDev(t *testing.T) {
	if got := resolve("", "", settings(), true); got.Version != "dev" {
		t.Fatalf("Version = %q, want \"dev\" when nothing stamped and Main is (devel)", got.Version)
	}
	if got := resolve("", "false", nil, false); got.Version != "dev" || got.Commit != "" || !got.BuiltAt.IsZero() {
		t.Fatalf("ReadBuildInfo ok=false must yield dev/empty/zero without panic, got %+v", got)
	}
}

func TestGetIsStableAcrossCalls(t *testing.T) {
	if Get() != Get() {
		t.Fatalf("Get() must be memoized and return a stable value")
	}
}
```

2. **Run it, watch it fail** — `go test ./pkg/buildinfo/`. Expected: compile failure `undefined: resolve`, `undefined: Get`, `undefined: Info` (package has no source yet).

3. **Minimal implementation** — `pkg/buildinfo/buildinfo.go`:

```go
// Package buildinfo is the single source of a running binary's build identity:
// a human-readable version label plus the embedded git commit, build time, and
// dirty flags. It is a leaf package (no spacemolt imports) so every binary can
// import it cheaply.
//
// This SemVer axis tracks fleet *binary* builds. It is deliberately separate
// from pkg/version (VersionID, BuiltForAPIVersion), which tracks the game-server
// API the client targets. The two are not unified.
package buildinfo

import (
	"runtime/debug"
	"sync"
	"time"
)

// version is set at build time via
//
//	-ldflags "-X github.com/rsned/spacemolt/pkg/buildinfo.version=$(git describe --tags --always --dirty)"
//
// A plain `go build` leaves it empty; resolve falls back to the module
// pseudo-version, then the literal "dev". See scripts/build.sh.
var version string

// codeDirty is set at build time via
//
//	-X github.com/rsned/spacemolt/pkg/buildinfo.codeDirty=true|false
//
// It reflects uncommitted tracked changes OUTSIDE data/ (data/*.json churns
// constantly, so raw vcs.modified is unusable for coloring). Unset ⇒ false.
var codeDirty string

// Info identifies the binary that is running.
type Info struct {
	Version   string    // git describe label, module pseudo-version, or "dev"
	Commit    string    // short vcs.revision, "" if unavailable
	BuiltAt   time.Time // vcs.time, zero if unavailable
	Modified  bool      // raw vcs.modified — cosmetic only (noisy from data/*.json)
	CodeDirty bool      // uncommitted tracked code outside data/ — color-relevant
}

var (
	once   sync.Once
	cached Info
)

// Get returns the running binary's build identity. It reads
// runtime/debug.ReadBuildInfo once, memoizes the result, and never panics.
func Get() Info {
	once.Do(func() {
		bi, ok := debug.ReadBuildInfo()
		cached = resolve(version, codeDirty, bi, ok)
	})
	return cached
}

// resolve builds an Info from the ldflags vars and a (possibly nil) build info.
// Split out from Get so tests can exercise the fallback ladder deterministically
// without depending on how the test binary itself was built.
func resolve(version, codeDirty string, bi *debug.BuildInfo, ok bool) Info {
	info := Info{Version: version, CodeDirty: codeDirty == "true"}
	if ok && bi != nil {
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				info.Commit = shortCommit(s.Value)
			case "vcs.time":
				if t, err := time.Parse(time.RFC3339, s.Value); err == nil {
					info.BuiltAt = t
				}
			case "vcs.modified":
				info.Modified = s.Value == "true"
			}
		}
		if info.Version == "" {
			if v := bi.Main.Version; v != "" && v != "(devel)" {
				info.Version = v
			}
		}
	}
	if info.Version == "" {
		info.Version = "dev"
	}
	return info
}

func shortCommit(rev string) string {
	if len(rev) > 12 {
		return rev[:12]
	}
	return rev
}
```

4. **Run it, watch it pass** — `go test ./pkg/buildinfo/`. Expected: `ok  github.com/rsned/spacemolt/pkg/buildinfo`.

5. **Lint** — `golangci-lint run ./pkg/buildinfo/`. Expected: no findings.

6. **Commit** — stage only this task's files:

```
git add pkg/buildinfo/buildinfo.go pkg/buildinfo/buildinfo_test.go
git commit --no-verify -m "feat(buildinfo): binary build identity via ldflags + embedded VCS"
```

(The repo's pre-commit race gate times out at its internal 300s under fleet load — `--no-verify` is user-approved; `golangci-lint run` in step 5 is the substitute gate. NEVER `git add -A`: `data/*.json` and other files are dirty in the working tree.)

---

## Task 2 — Build stamping (`scripts/build.sh` + stamp test)

**Files:**
- create `scripts/build.sh` (executable)
- create `scripts/buildinfo-probe/main.go` (tiny fixture the stamp test builds; not a shipped binary)
- create `pkg/buildinfo/stamp_test.go`

**Interfaces:**
- Consumes: `pkg/buildinfo.Get() Info` (from Task 1)
- Produces: `bin/overmind`, `bin/worker`, `bin/overmind-dashboard` stamped with the ldflags targets (runtime artifact, not a Go symbol)

### Steps

1. **Write the failing test** — `pkg/buildinfo/stamp_test.go`:

```go
package buildinfo_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// repoRoot walks up from the test's working directory (pkg/buildinfo) to the
// module root so `go build` and file reads use stable absolute paths.
func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}

// TestLdflagsStampWiresThrough proves the ldflags target names the real symbol:
// a build stamping buildinfo.version must surface verbatim via Get().Version.
func TestLdflagsStampWiresThrough(t *testing.T) {
	root := repoRoot(t)
	bin := filepath.Join(t.TempDir(), "probe")
	cmd := exec.Command("go", "build",
		"-ldflags", "-X github.com/rsned/spacemolt/pkg/buildinfo.version=v9.9.9-stamp-test",
		"-o", bin, "./scripts/buildinfo-probe")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("stamped build failed: %v\n%s", err, out)
	}
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("run probe: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != "v9.9.9-stamp-test" {
		t.Fatalf("stamped version = %q, want v9.9.9-stamp-test — ldflags target is wrong", got)
	}
}

// TestBuildScriptTargetsBuildinfo guards that the release script stamps the
// exact ldflags symbols and builds all three fleet binaries.
func TestBuildScriptTargetsBuildinfo(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRoot(t), "scripts", "build.sh"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		"github.com/rsned/spacemolt/pkg/buildinfo.version=",
		"github.com/rsned/spacemolt/pkg/buildinfo.codeDirty=",
		"git status --porcelain -- ':!data/'",
		"-o bin/overmind ",
		"-o bin/worker ",
		"-o bin/overmind-dashboard ",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("scripts/build.sh missing %q", want)
		}
	}
}
```

2. **Run it, watch it fail** — `go test -run 'Stamp|BuildScript' ./pkg/buildinfo/`. Expected: `TestLdflagsStampWiresThrough` fails building `./scripts/buildinfo-probe` (`no Go files` / path not found) and `TestBuildScriptTargetsBuildinfo` fails reading `scripts/build.sh` (file does not exist).

3. **Minimal implementation** — create `scripts/buildinfo-probe/main.go`:

```go
// Command buildinfo-probe prints only the build version string. It exists so
// build-stamp tests can verify the ldflags target wires through to
// buildinfo.Get(); it is not a shipped fleet binary.
package main

import (
	"fmt"

	"github.com/rsned/spacemolt/pkg/buildinfo"
)

func main() {
	fmt.Println(buildinfo.Get().Version)
}
```

Then create `scripts/build.sh`:

```bash
#!/usr/bin/env bash
# Build the fleet binaries into bin/ with build-identity stamping.
#
# Stamps two ldflags vars in pkg/buildinfo:
#   version   = git describe --tags --always --dirty (SemVer label + commit)
#   codeDirty = whether tracked files OUTSIDE data/ have uncommitted changes
#               (data/*.json churns constantly, so raw vcs.modified is unusable
#                for coloring).
#
# A plain `go build ./...` still works and yields version "dev" with codeDirty
# unset — this script is for release builds, not a hard requirement.
set -euo pipefail

cd "$(dirname "$0")/.."

DESC=$(git describe --tags --always --dirty)
if [ -z "$(git status --porcelain -- ':!data/')" ]; then
  CODEDIRTY=false
else
  CODEDIRTY=true
fi

LDFLAGS="-X github.com/rsned/spacemolt/pkg/buildinfo.version=$DESC \
-X github.com/rsned/spacemolt/pkg/buildinfo.codeDirty=$CODEDIRTY"

mkdir -p bin
go build -ldflags "$LDFLAGS" -o bin/overmind ./cmd/overmind
go build -ldflags "$LDFLAGS" -o bin/worker ./cmd/worker
go build -ldflags "$LDFLAGS" -o bin/overmind-dashboard ./cmd/overmind-dashboard

echo "built bin/overmind bin/worker bin/overmind-dashboard @ $DESC (codeDirty=$CODEDIRTY)"
```

Make it executable: `chmod +x scripts/build.sh`.

4. **Run it, watch it pass** — `go test -run 'Stamp|BuildScript' ./pkg/buildinfo/`. Expected: `ok`. Also sanity-run the script once: `./scripts/build.sh` should print a `built …` line and produce `bin/overmind`, `bin/worker`, `bin/overmind-dashboard` (per CLAUDE.md, built binaries belong in `bin/`).

5. **Lint** — `golangci-lint run ./pkg/buildinfo/ ./scripts/buildinfo-probe/`. Expected: no findings.

6. **Commit** — stage only this task's files (NOT the `bin/` artifacts, which are gitignored build output):

```
git add scripts/build.sh scripts/buildinfo-probe/main.go pkg/buildinfo/stamp_test.go
git commit --no-verify -m "build: scripts/build.sh stamps fleet binaries with buildinfo ldflags"
```

---

## Task 3 — `control.Hello` version fields + codec round-trip

**Files:**
- modify `pkg/overmind/control/messages.go`
- modify `pkg/overmind/control/messages_test.go`

**Interfaces:**
- Produces: `control.Hello` gains `Version, Commit, BuiltAt string` (BuiltAt RFC3339) plus `CodeDirty, Modified bool`, all `omitempty`.
- Consumes: existing `control.NewEnvelope(t Type, agentID string, payload any) (Envelope, error)` and `(Envelope).Into(v any) error` (unchanged).

### Steps

1. **Write the failing test** — append to `pkg/overmind/control/messages_test.go`:

```go
func TestHelloCarriesBuildIdentity(t *testing.T) {
	want := Hello{
		AgentID: "hauler-3", Role: "hauler", Station: "ST-1", PID: 7,
		Version: "v0.3.0-2-g8016cd8", Commit: "8016cd8abcde",
		BuiltAt: "2026-07-23T10:00:00Z", CodeDirty: true, Modified: true,
	}
	env, err := NewEnvelope(TypeHello, want.AgentID, want)
	if err != nil {
		t.Fatalf("NewEnvelope: %v", err)
	}
	var got Hello
	if err := env.Into(&got); err != nil {
		t.Fatalf("Into: %v", err)
	}
	if got != want {
		t.Fatalf("build identity lost in round trip: got %+v want %+v", got, want)
	}
}

func TestOldHelloWithoutBuildDecodesClean(t *testing.T) {
	// A pre-feature worker sends no version fields; they must decode to zero
	// values, never an error (backward compatibility during the rollout).
	env, err := NewEnvelope(TypeHello, "legacy-1", Hello{AgentID: "legacy-1", Role: "hauler", PID: 3})
	if err != nil {
		t.Fatalf("NewEnvelope: %v", err)
	}
	var got Hello
	if err := env.Into(&got); err != nil {
		t.Fatalf("Into: %v", err)
	}
	if got.Version != "" || got.CodeDirty || got.Modified || got.BuiltAt != "" {
		t.Fatalf("absent build fields must be zero, got %+v", got)
	}
}
```

2. **Run it, watch it fail** — `go test ./pkg/overmind/control/`. Expected: compile failure `unknown field Version in struct literal of type Hello` (and Commit/BuiltAt/CodeDirty/Modified).

3. **Minimal implementation** — in `pkg/overmind/control/messages.go`, replace the `Hello` struct:

```go
// Hello is the first message a worker sends after connecting.
type Hello struct {
	AgentID string `json:"agent_id"`
	Role    string `json:"role"`
	Station string `json:"station"`
	PID     int    `json:"pid"`
	// Build identity of the worker binary (buildinfo.Get). Omitted by a
	// pre-feature worker, which the overmind treats as "unknown"/legacy.
	Version   string `json:"version,omitempty"`
	Commit    string `json:"commit,omitempty"`
	BuiltAt   string `json:"built_at,omitempty"` // RFC3339
	CodeDirty bool   `json:"code_dirty,omitempty"`
	Modified  bool   `json:"modified,omitempty"` // raw vcs.modified — cosmetic
}
```

4. **Run it, watch it pass** — `go test ./pkg/overmind/control/`. Expected: `ok`, including the pre-existing `TestEnvelopeRoundTrip` (which constructs `Hello` without the new fields — they default to zero and are omitted).

5. **Lint** — `golangci-lint run ./pkg/overmind/control/`. Expected: no findings.

6. **Commit:**

```
git add pkg/overmind/control/messages.go pkg/overmind/control/messages_test.go
git commit --no-verify -m "feat(control): carry worker build identity on Hello"
```

---

## Task 4 — Worker sends buildinfo; supervisor stores it on `WorkerInfo`

**Files:**
- modify `cmd/worker/main.go`
- modify `pkg/overmind/supervisor/fleet.go`
- modify `pkg/overmind/supervisor/fleet_test.go` (create if absent)

**Interfaces:**
- Consumes: `buildinfo.Get() Info` (Task 1), `control.Hello{Version,Commit,BuiltAt,CodeDirty,Modified}` (Task 3)
- Produces: `supervisor.WorkerInfo` gains `Version, Commit, BuiltAt string; CodeDirty, Modified bool`
- Produces: `(*Fleet).ApplyHello(h control.Hello, pid int, now time.Time)` copies those five fields onto the worker (signature unchanged)

### Steps

1. **Write the failing test** — add to `pkg/overmind/supervisor/fleet_test.go` (create the file with this package clause if it does not exist):

```go
package supervisor

import (
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/overmind/control"
)

func TestApplyHelloStoresBuildIdentity(t *testing.T) {
	f := NewFleet()
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	f.ApplyHello(control.Hello{
		AgentID: "hauler-3", Role: "hauler", Station: "ST-1",
		Version: "v0.3.0", Commit: "8016cd8abcde", BuiltAt: "2026-07-23T10:00:00Z",
		CodeDirty: true, Modified: true,
	}, 7, now)
	snap := f.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("want 1 worker, got %d", len(snap))
	}
	w := snap[0]
	if w.Version != "v0.3.0" || w.Commit != "8016cd8abcde" || w.BuiltAt != "2026-07-23T10:00:00Z" {
		t.Fatalf("build identity not stored: %+v", w)
	}
	if !w.CodeDirty || !w.Modified {
		t.Fatalf("dirty flags not stored: CodeDirty=%v Modified=%v", w.CodeDirty, w.Modified)
	}
}
```

2. **Run it, watch it fail** — `go test ./pkg/overmind/supervisor/`. Expected: compile failure `w.Version undefined (type WorkerInfo has no field or method Version)`.

3. **Minimal implementation** — in `pkg/overmind/supervisor/fleet.go`, add fields to `WorkerInfo` (after `PID int`):

```go
	PID int
	// Version/Commit/BuiltAt/CodeDirty/Modified are the worker binary's build
	// identity, reported in its Hello (buildinfo.Get). Empty Version = a
	// pre-feature "legacy" worker. Modified is the raw vcs.modified cosmetic
	// flag; CodeDirty is the color-relevant one (uncommitted code outside data/).
	Version   string
	Commit    string
	BuiltAt   string
	CodeDirty bool
	Modified  bool
```

In the same file, extend `ApplyHello` to copy them (add after the existing `w.Role, w.Station, w.PID = ...` line):

```go
func (f *Fleet) ApplyHello(h control.Hello, pid int, now time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w := f.get(h.AgentID)
	w.Role, w.Station, w.PID = h.Role, h.Station, pid
	w.Version, w.Commit, w.BuiltAt = h.Version, h.Commit, h.BuiltAt
	w.CodeDirty, w.Modified = h.CodeDirty, h.Modified
	w.LastSeen, w.LastProgress, w.Healthy = now, now, true
}
```

Then wire the worker to send it — in `cmd/worker/main.go`, add the buildinfo import (`"github.com/rsned/spacemolt/pkg/buildinfo"`) and populate the `hello` literal (around line 179):

```go
			bi := buildinfo.Get()
			builtAt := ""
			if !bi.BuiltAt.IsZero() {
				builtAt = bi.BuiltAt.UTC().Format(time.RFC3339)
			}
			hello := control.Hello{
				AgentID:   *agentID,
				Role:      *role,
				Station:   *station,
				PID:       os.Getpid(),
				Version:   bi.Version,
				Commit:    bi.Commit,
				BuiltAt:   builtAt,
				CodeDirty: bi.CodeDirty,
				Modified:  bi.Modified,
			}
```

4. **Run it, watch it pass** — `go test ./pkg/overmind/supervisor/ ./cmd/worker/` and `go build ./cmd/worker/`. Expected: `ok` / clean build.

5. **Lint** — `golangci-lint run ./pkg/overmind/supervisor/ ./cmd/worker/`. Expected: no findings.

6. **Commit:**

```
git add cmd/worker/main.go pkg/overmind/supervisor/fleet.go pkg/overmind/supervisor/fleet_test.go
git commit --no-verify -m "feat(worker): report build identity in Hello; supervisor stores it"
```

---

## Task 5 — `balances`: per-worker version in `LiveRecord`, overmind version in `StatusFile`

**Files:**
- modify `pkg/overmind/balances/balances.go`
- modify `pkg/overmind/balances/balances_test.go`
- modify `cmd/overmind/main.go` (the `recordBalances` `LiveRecord` build + the `WriteStatus` call)

**Interfaces:**
- Produces: `balances.LiveRecord` gains `Version, Commit, BuiltAt string; CodeDirty, Modified bool` (all `omitempty`)
- Produces: `balances.StatusFile` gains `OvermindVersion, OvermindCommit, OvermindBuiltAt string; OvermindCodeDirty, OvermindModified bool` (all `omitempty`)
- Produces: `type OvermindBuild struct { Version, Commit, BuiltAt string; CodeDirty, Modified bool }`
- **Signature change:** `(*Recorder).WriteStatus(live []LiveRecord, removed []string, ov OvermindBuild, now time.Time) error` — gains the `ov` parameter. Two test callers and one production caller must be updated.
- Consumes: `buildinfo.Get() Info` (in the `cmd/overmind` caller only; `balances` stays buildinfo-free for deterministic tests)

### Steps

1. **Write the failing test** — append to `pkg/overmind/balances/balances_test.go`:

```go
func TestWriteStatusCarriesVersions(t *testing.T) {
	dir := t.TempDir()
	sp := filepath.Join(dir, "fleet-status.json")
	r, err := NewRecorder(sp, filepath.Join(dir, "fleet-history.jsonl"))
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}
	live := []LiveRecord{{
		AgentID: "hauler-3", Role: "hauler", System: "Sol", Seen: true,
		Version: "v0.2.9", Commit: "aaaa1111bbbb", BuiltAt: "2026-07-22T09:00:00Z",
		CodeDirty: true, Modified: true,
	}}
	ov := OvermindBuild{
		Version: "v0.3.0", Commit: "8016cd8abcde", BuiltAt: "2026-07-23T10:00:00Z",
		CodeDirty: false, Modified: true,
	}
	if err := r.WriteStatus(live, nil, ov, mustTime(t, "2026-07-23T12:00:00Z")); err != nil {
		t.Fatalf("WriteStatus: %v", err)
	}
	sf, err := ReadStatus(sp)
	if err != nil {
		t.Fatalf("ReadStatus: %v", err)
	}
	if sf.OvermindVersion != "v0.3.0" || sf.OvermindBuiltAt != "2026-07-23T10:00:00Z" || sf.OvermindModified != true {
		t.Fatalf("overmind version not written: %+v", sf)
	}
	w := sf.Workers[0]
	if w.Version != "v0.2.9" || w.Commit != "aaaa1111bbbb" || !w.CodeDirty || w.BuiltAt != "2026-07-22T09:00:00Z" {
		t.Fatalf("worker version not written: %+v", w)
	}
}
```

Also update the two existing `WriteStatus` callers in this file to the new signature (add `OvermindBuild{}` as the third argument):
- line ~31: `if err := r.WriteStatus(live, nil, OvermindBuild{}, mustTime(t, "2026-06-25T12:00:00Z")); err != nil {`
- line ~53: `if err := r.WriteStatus(nil, []string{"trader-9"}, OvermindBuild{}, mustTime(t, "2026-06-25T12:00:00Z")); err != nil {`

2. **Run it, watch it fail** — `go test ./pkg/overmind/balances/`. Expected: compile failure `unknown field Version in struct literal of type LiveRecord` and `undefined: OvermindBuild` / `too many arguments in call to r.WriteStatus`.

3. **Minimal implementation** — in `pkg/overmind/balances/balances.go`:

Add the build fields to `LiveRecord` (after `LastSeen string`):

```go
	Seen     bool   `json:"seen"`
	LastSeen string `json:"last_seen"`
	// Build identity of the worker binary, reported via its Hello. Empty
	// Version = a pre-feature "legacy" worker. Modified is the cosmetic raw
	// vcs.modified flag; CodeDirty is the color-relevant one.
	Version   string `json:"version,omitempty"`
	Commit    string `json:"commit,omitempty"`
	BuiltAt   string `json:"built_at,omitempty"`
	CodeDirty bool   `json:"code_dirty,omitempty"`
	Modified  bool   `json:"modified,omitempty"`
```

Add the `OvermindBuild` type and extend `StatusFile` (after the `Removed` field):

```go
// OvermindBuild identifies the overmind binary that wrote a status file.
type OvermindBuild struct {
	Version   string
	Commit    string
	BuiltAt   string
	CodeDirty bool
	Modified  bool
}

// StatusFile is the on-disk shape of the live status file.
type StatusFile struct {
	CapturedAt string       `json:"captured_at"`
	Workers    []LiveRecord `json:"workers"`
	Removed    []string     `json:"removed,omitempty"`
	// Build identity of the overmind process that wrote this file.
	OvermindVersion   string `json:"overmind_version,omitempty"`
	OvermindCommit    string `json:"overmind_commit,omitempty"`
	OvermindBuiltAt   string `json:"overmind_built_at,omitempty"`
	OvermindCodeDirty bool   `json:"overmind_code_dirty,omitempty"`
	OvermindModified  bool   `json:"overmind_modified,omitempty"`
}
```

(Keep the existing `Removed` doc comment; the literal above collapses it for brevity — retain the original comment block in the real edit.)

Update `WriteStatus`:

```go
// WriteStatus atomically overwrites the live status file with the given
// records, the current override-removed agent ids, and the overmind's own
// build identity.
func (r *Recorder) WriteStatus(live []LiveRecord, removed []string, ov OvermindBuild, now time.Time) error {
	sf := StatusFile{
		CapturedAt: now.UTC().Format(time.RFC3339), Workers: live, Removed: removed,
		OvermindVersion: ov.Version, OvermindCommit: ov.Commit, OvermindBuiltAt: ov.BuiltAt,
		OvermindCodeDirty: ov.CodeDirty, OvermindModified: ov.Modified,
	}
	data, err := json.MarshalIndent(sf, "", "  ")
	if err != nil {
		return fmt.Errorf("balances: marshal status: %w", err)
	}
	return atomicWrite(r.statusPath, append(data, '\n'))
}
```

In `cmd/overmind/main.go`, extend the `LiveRecord` literal inside `recordBalances` (the `for _, w := range snap` loop) to copy the build fields, and pass the overmind's own build to `WriteStatus`. Add the buildinfo import (`"github.com/rsned/spacemolt/pkg/buildinfo"`). Inside the loop, add to the `LiveRecord{...}` literal:

```go
			Seen: st.Timestamp != "", LastSeen: w.LastSeen.UTC().Format(time.RFC3339),
			Version: w.Version, Commit: w.Commit, BuiltAt: w.BuiltAt,
			CodeDirty: w.CodeDirty, Modified: w.Modified,
```

And replace the `WriteStatus` call:

```go
	bi := buildinfo.Get()
	ov := balances.OvermindBuild{
		Version: bi.Version, Commit: bi.Commit, CodeDirty: bi.CodeDirty, Modified: bi.Modified,
	}
	if !bi.BuiltAt.IsZero() {
		ov.BuiltAt = bi.BuiltAt.UTC().Format(time.RFC3339)
	}
	if err := recorder.WriteStatus(live, removedIDs, ov, now); err != nil {
		logger.Printf("balances: write status: %v", err)
	}
```

4. **Run it, watch it pass** — `go test ./pkg/overmind/balances/ ./cmd/overmind/` and `go build ./cmd/overmind/`. Expected: `ok` / clean build.

5. **Lint** — `golangci-lint run ./pkg/overmind/balances/ ./cmd/overmind/`. Expected: no findings.

6. **Commit:**

```
git add pkg/overmind/balances/balances.go pkg/overmind/balances/balances_test.go cmd/overmind/main.go
git commit --no-verify -m "feat(balances): per-worker + overmind build identity in status file"
```

---

## Task 6 — `ovdash`: snapshot version fields, current build, tier classification

**Files:**
- create `pkg/ovdash/version.go`
- create `pkg/ovdash/version_test.go`
- modify `pkg/ovdash/snapshot.go`
- modify `pkg/ovdash/snapshot_test.go`

**Interfaces:**
- Consumes: `pkg/version.ParseSemVer(string) (SemVer, error)`, `(SemVer).Major`, `(SemVer).MinorDiff(SemVer) int`
- Consumes: `balances.StatusFile.OvermindVersion/OvermindCommit/OvermindBuiltAt/OvermindCodeDirty/OvermindModified`, `balances.LiveRecord.Version/Commit/BuiltAt/CodeDirty/Modified`
- Produces: `type Tier string` with `TierGreen, TierYellow, TierRed`
- Produces: `func Classify(ver string, codeDirty bool, current string) Tier`
- Produces: `func currentVersion(samples []buildSample) string` and `type buildSample struct { Version, BuiltAt string }`
- Produces: `func worstTier(tiers ...Tier) Tier`
- Produces: `AgentState` gains `Version, Commit, BuiltAt string; CodeDirty, Modified bool; Tier Tier` (json `version,commit,built_at,code_dirty,modified,tier`, omitempty on the strings/bools)
- Produces: `type OvermindInfo struct { Version, Commit, BuiltAt string; CodeDirty, Modified bool; Tier, FleetTier Tier }`
- Produces: `Snapshot` gains `Overminds map[string]OvermindInfo` (`json:"overminds,omitempty"`, keyed by fleet label)

### Steps

1. **Write the failing tests.**

`pkg/ovdash/version_test.go`:

```go
package ovdash

import "testing"

func TestClassifyTiers(t *testing.T) {
	const cur = "v0.3.0"
	cases := []struct {
		name      string
		ver       string
		codeDirty bool
		want      Tier
	}{
		{"exact match clean is green", "v0.3.0", false, TierGreen},
		{"exact match code-dirty is yellow", "v0.3.0", true, TierYellow},
		{"same minor patch-behind is yellow", "v0.3.0-2-g8016cd8", false, TierYellow},
		{"minor behind is red", "v0.2.9", false, TierRed},
		{"minor ahead is red", "v0.4.0", false, TierRed},
		{"legacy empty is red", "", false, TierRed},
		{"dev unparseable is red", "dev", false, TierRed},
	}
	for _, c := range cases {
		if got := Classify(c.ver, c.codeDirty, cur); got != c.want {
			t.Errorf("%s: Classify(%q,%v,%q) = %q, want %q", c.name, c.ver, c.codeDirty, cur, got, c.want)
		}
	}
}

func TestCurrentVersionPicksNewestBuiltAt(t *testing.T) {
	got := currentVersion([]buildSample{
		{Version: "v0.2.9", BuiltAt: "2026-07-22T09:00:00Z"},
		{Version: "v0.3.0", BuiltAt: "2026-07-23T10:00:00Z"},
		{Version: "v0.1.0", BuiltAt: ""},          // no build time — ignored
		{Version: "bad", BuiltAt: "not-a-time"},   // unparseable — ignored
	})
	if got != "v0.3.0" {
		t.Fatalf("current = %q, want v0.3.0 (newest built_at)", got)
	}
	if currentVersion(nil) != "" {
		t.Fatalf("empty samples must yield empty current")
	}
}

func TestWorstTier(t *testing.T) {
	if worstTier(TierGreen, TierYellow, TierGreen) != TierYellow {
		t.Fatal("green+yellow → yellow")
	}
	if worstTier(TierGreen, TierRed, TierYellow) != TierRed {
		t.Fatal("any red → red")
	}
	if worstTier() != TierGreen {
		t.Fatal("no tiers → green")
	}
}
```

Append to `pkg/ovdash/snapshot_test.go` a mixed-version fixture test. First update the `writeStatus` helper to accept overmind build fields — change its signature and the two existing call sites (add `balances.OvermindBuild{}` — see note below), OR add a second helper. To avoid touching existing green tests, add a NEW helper and a new test:

```go
func writeStatusOv(t *testing.T, dir, fleetFile, capturedAt string, ov balances.OvermindBuild, ws []balances.LiveRecord) {
	t.Helper()
	sf := balances.StatusFile{
		CapturedAt: capturedAt, Workers: ws,
		OvermindVersion: ov.Version, OvermindCommit: ov.Commit, OvermindBuiltAt: ov.BuiltAt,
		OvermindCodeDirty: ov.CodeDirty, OvermindModified: ov.Modified,
	}
	b, err := json.Marshal(sf)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, fleetFile+"-status.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadSnapshotClassifiesVersionTiers(t *testing.T) {
	g, err := LoadGalaxy(context.Background(), fixtureKB(t))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	fresh := now.Add(-5 * time.Second).Format(time.RFC3339)
	newest := "2026-07-23T10:00:00Z" // the current build's built_at
	older := "2026-07-22T09:00:00Z"

	// haul overmind IS the current build (v0.3.0 @ newest).
	writeStatusOv(t, dir, "fleet", fresh,
		balances.OvermindBuild{Version: "v0.3.0", BuiltAt: newest},
		[]balances.LiveRecord{
			{AgentID: "hauler-green", System: "Sol", Seen: true, Healthy: true,
				Version: "v0.3.0", BuiltAt: newest},
			{AgentID: "hauler-yellow-patch", System: "Sol", Seen: true, Healthy: true,
				Version: "v0.3.0-2-g8016cd8", BuiltAt: "2026-07-23T09:00:00Z"},
			{AgentID: "hauler-yellow-dirty", System: "Sol", Seen: true, Healthy: true,
				Version: "v0.3.0", BuiltAt: newest, CodeDirty: true},
			{AgentID: "hauler-red-minor", System: "Sol", Seen: true, Healthy: true,
				Version: "v0.2.9", BuiltAt: older},
			{AgentID: "hauler-red-legacy", System: "Sol", Seen: true, Healthy: true},
		})

	s, err := ReadSnapshot(dir, g, now, time.Minute)
	if err != nil {
		t.Fatalf("ReadSnapshot: %v", err)
	}
	byID := map[string]AgentState{}
	for _, a := range s.Agents {
		byID[a.AgentID] = a
	}
	want := map[string]Tier{
		"hauler-green":        TierGreen,
		"hauler-yellow-patch": TierYellow,
		"hauler-yellow-dirty": TierYellow,
		"hauler-red-minor":    TierRed,
		"hauler-red-legacy":   TierRed,
	}
	for id, wt := range want {
		if got := byID[id].Tier; got != wt {
			t.Errorf("%s tier = %q, want %q", id, got, wt)
		}
	}
	ov, ok := s.Overminds["haul"]
	if !ok || ov.Version != "v0.3.0" || ov.Tier != TierGreen {
		t.Fatalf("haul overmind info wrong: %+v (ok=%v)", ov, ok)
	}
	// Rolled-up fleet tier = worst worker present = red (legacy + minor-behind).
	if ov.FleetTier != TierRed {
		t.Fatalf("haul FleetTier = %q, want red", ov.FleetTier)
	}
}
```

2. **Run it, watch it fail** — `go test ./pkg/ovdash/`. Expected: compile failure `undefined: Classify`, `undefined: Tier`, `undefined: buildSample`, `a.Tier undefined`, `s.Overminds undefined`, `undefined: writeStatusOv`.

3. **Minimal implementation.**

`pkg/ovdash/version.go`:

```go
package ovdash

import (
	"time"

	"github.com/rsned/spacemolt/pkg/version"
)

// Tier is a build-freshness color relative to the current (newest) fleet build.
type Tier string

const (
	TierGreen  Tier = "green"  // identical to current, code-clean
	TierYellow Tier = "yellow" // same major.minor but drifted, or code-dirty
	TierRed    Tier = "red"    // different minor/major, or legacy/unparseable
)

// buildSample is one (version, built_at) observation used to pick the current
// build. built_at is an RFC3339 string; unparseable/empty samples are ignored.
type buildSample struct {
	Version string
	BuiltAt string
}

// currentVersion returns the version string of the sample with the newest
// parseable built_at. Build-time — not SemVer — decides which build is current
// (monotonic, robust to out-of-order tags). Empty when nothing is datable.
func currentVersion(samples []buildSample) string {
	var best time.Time
	var bestVer string
	for _, s := range samples {
		if s.BuiltAt == "" {
			continue
		}
		t, err := time.Parse(time.RFC3339, s.BuiltAt)
		if err != nil {
			continue
		}
		if bestVer == "" || t.After(best) {
			best, bestVer = t, s.Version
		}
	}
	return bestVer
}

// Classify colors a build by SemVer distance from current. green = same version
// string and code-clean; yellow = same major.minor but patch/commit-behind or
// code-dirty; red = different minor/major, or an unparseable/legacy version.
func Classify(ver string, codeDirty bool, current string) Tier {
	v, errV := version.ParseSemVer(ver)
	cur, errCur := version.ParseSemVer(current)
	if errV != nil || errCur != nil {
		return TierRed
	}
	if v.Major != cur.Major || v.MinorDiff(cur) != 0 {
		return TierRed
	}
	if ver == current && !codeDirty {
		return TierGreen
	}
	return TierYellow
}

// worstTier returns the most severe tier present (red > yellow > green). No
// tiers ⇒ green.
func worstTier(tiers ...Tier) Tier {
	rank := map[Tier]int{TierGreen: 0, TierYellow: 1, TierRed: 2}
	worst := TierGreen
	for _, t := range tiers {
		if rank[t] > rank[worst] {
			worst = t
		}
	}
	return worst
}
```

In `pkg/ovdash/snapshot.go`, add the build fields to `AgentState` (after `Leaving`):

```go
	Leaving    bool    `json:"leaving,omitempty"`
	// Build identity (from the worker's Hello, via the status file). Tier is
	// this build's color vs the current (newest) fleet build. Modified is the
	// cosmetic raw vcs.modified flag; CodeDirty drives the tier.
	Version   string `json:"version,omitempty"`
	Commit    string `json:"commit,omitempty"`
	BuiltAt   string `json:"built_at,omitempty"`
	CodeDirty bool   `json:"code_dirty,omitempty"`
	Modified  bool   `json:"modified,omitempty"`
	Tier      Tier   `json:"tier,omitempty"`
```

Add `OvermindInfo` and the `Overminds` field on `Snapshot`:

```go
// OvermindInfo is one fleet's overmind build identity plus the rolled-up
// worst tier across that overmind and all its workers.
type OvermindInfo struct {
	Version   string `json:"version,omitempty"`
	Commit    string `json:"commit,omitempty"`
	BuiltAt   string `json:"built_at,omitempty"`
	CodeDirty bool   `json:"code_dirty,omitempty"`
	Modified  bool   `json:"modified,omitempty"`
	Tier      Tier   `json:"tier,omitempty"`       // the overmind's own tier
	FleetTier Tier   `json:"fleet_tier,omitempty"` // worst tier in the fleet
}

// Snapshot is the merged live view across every fleet.
type Snapshot struct {
	CapturedAt  map[string]string       `json:"captured_at"`
	Agents      []AgentState            `json:"agents"`
	OffMap      []AgentState            `json:"off_map"`
	StaleFleets []string                `json:"stale_fleets"`
	Removed     map[string][]string     `json:"removed,omitempty"`
	Overminds   map[string]OvermindInfo `json:"overminds,omitempty"` // fleet label -> overmind build
}
```

(Keep the existing per-field doc comments on `Snapshot` when editing.)

In `ReadSnapshot`, (a) copy the worker build fields when constructing each `AgentState`, (b) record each fleet's overmind build + build samples during the loop, then (c) after the loop compute the current build and backfill tiers.

Copy the worker fields into the `AgentState` literal (extend the existing literal):

```go
				Restarts: w.Restarts, LastSeen: w.LastSeen,
				Leaving:  w.Leaving,
				Version:  w.Version, Commit: w.Commit, BuiltAt: w.BuiltAt,
				CodeDirty: w.CodeDirty, Modified: w.Modified,
```

Initialize `s.Overminds` and a sample accumulator at the top of `ReadSnapshot` (next to `s := &Snapshot{...}`):

```go
	s := &Snapshot{CapturedAt: map[string]string{}, Overminds: map[string]OvermindInfo{}}
	var samples []buildSample
```

Inside the fleet loop, after `s.CapturedAt[f.Label] = sf.CapturedAt`, capture the overmind build and its sample:

```go
		s.Overminds[f.Label] = OvermindInfo{
			Version: sf.OvermindVersion, Commit: sf.OvermindCommit, BuiltAt: sf.OvermindBuiltAt,
			CodeDirty: sf.OvermindCodeDirty, Modified: sf.OvermindModified,
		}
		samples = append(samples, buildSample{Version: sf.OvermindVersion, BuiltAt: sf.OvermindBuiltAt})
```

Inside the `for _, w := range sf.Workers` loop, add a worker sample (before the resolve/append):

```go
			samples = append(samples, buildSample{Version: w.Version, BuiltAt: w.BuiltAt})
```

After the fleet loop (before the final `if len(s.Agents) == 0 ...` guard), classify everything:

```go
	current := currentVersion(samples)
	for i := range s.Agents {
		s.Agents[i].Tier = Classify(s.Agents[i].Version, s.Agents[i].CodeDirty, current)
	}
	for i := range s.OffMap {
		s.OffMap[i].Tier = Classify(s.OffMap[i].Version, s.OffMap[i].CodeDirty, current)
	}
	fleetTiers := map[string][]Tier{}
	for _, a := range s.Agents {
		fleetTiers[a.Fleet] = append(fleetTiers[a.Fleet], a.Tier)
	}
	for _, a := range s.OffMap {
		fleetTiers[a.Fleet] = append(fleetTiers[a.Fleet], a.Tier)
	}
	for label, oi := range s.Overminds {
		oi.Tier = Classify(oi.Version, oi.CodeDirty, current)
		oi.FleetTier = worstTier(append(fleetTiers[label], oi.Tier)...)
		s.Overminds[label] = oi
	}
```

Note: `s.Overminds` is keyed by fleet **label** (e.g. `"haul"`), matching `f.Label`, `s.CapturedAt`, and `s.Removed`. The `fleetTiers` map is likewise keyed by `AgentState.Fleet`, which is set to `f.Label`.

4. **Run it, watch it pass** — `go test ./pkg/ovdash/`. Expected: `ok`, including the pre-existing snapshot tests (they use the old `writeStatus` helper with no overmind build → `Overminds["…"]` has empty version, tier red, but those tests never assert on tiers).

5. **Lint** — `golangci-lint run ./pkg/ovdash/`. Expected: no findings.

6. **Commit:**

```
git add pkg/ovdash/version.go pkg/ovdash/version_test.go pkg/ovdash/snapshot.go pkg/ovdash/snapshot_test.go
git commit --no-verify -m "feat(ovdash): current-build detection + per-worker/fleet version tiers"
```

---

## Task 7 — Frontend: version types, per-fleet badge, worker-version summary

**Files:**
- modify `frontend/src/lib/useFleetStream.ts`
- modify `frontend/src/components/overmind/FleetRail.tsx`

**Interfaces:**
- Consumes (JSON from `pkg/ovdash`): `AgentState.version/commit/built_at/code_dirty/modified/tier`, `Snapshot.overminds` = `Record<string, OvermindInfo>` where `OvermindInfo = { version, commit, built_at, code_dirty, modified, tier, fleet_tier }`.
- Produces: `useFleetStream` returns `overminds: Record<string, OvermindInfo>` on `FleetStream`; `FleetRail` renders a per-fleet overmind version badge (colored by `fleet_tier`) and a worker-version summary line grouping counts by version, each colored by its own tier, with a `*` suffix when any grouped worker is raw-`modified`.

### Steps

1. **Write the failing check** — extend the frontend types and consume them so the production build fails until wired. (This repo's frontend has no unit-test harness for these components; the guard is `npm run build` type-checking. The "failing test" is a type error you introduce by referencing not-yet-added fields.)

In `frontend/src/lib/useFleetStream.ts`, the `AgentState` interface gains the version fields, a new `Tier` type + `TIER_COLORS`, an `OvermindInfo` interface, `overminds` on `Snapshot` and `FleetStream`. In `FleetRail.tsx`, reference `overminds[fleet]` and `a.tier` — before adding the types, `tsc` reports `Property 'tier' does not exist on type 'AgentState'`.

Run the guard first to see the current baseline pass, then after editing `FleetRail.tsx` (step below) but before editing the types, `cd frontend && npm run build` fails with the missing-property type errors. That is the red state.

2. **Run it, watch it fail** — `cd frontend && npm run build`. Expected (after the FleetRail edit, before the type edit): `error TS2339: Property 'tier' does not exist on type 'AgentState'` and `Property 'overminds' does not exist on type 'FleetStream'`.

3. **Minimal implementation.**

In `frontend/src/lib/useFleetStream.ts`:

Add the tier type, color map, and extend `AgentState` (add fields at the end of the interface, before the closing brace):

```ts
export type Tier = 'green' | 'yellow' | 'red';

/** Tier → badge color. Kept distinct from FLEETS accent colors. */
export const TIER_COLORS: Record<Tier, string> = {
  green: '#34d399',
  yellow: '#fbbf24',
  red: '#f87171',
};

export interface OvermindInfo {
  version?: string;
  commit?: string;
  built_at?: string;
  code_dirty?: boolean;
  modified?: boolean;
  tier?: Tier;
  fleet_tier?: Tier;
}
```

Extend `AgentState` with:

```ts
  leaving?: boolean;
  version?: string;
  commit?: string;
  built_at?: string;
  code_dirty?: boolean;
  modified?: boolean;
  tier?: Tier;
```

Add `overminds` to the `Snapshot` interface and to `FleetStream`:

```ts
interface Snapshot {
  agents: AgentState[] | null;
  off_map: AgentState[] | null;
  stale_fleets: string[] | null;
  removed?: Record<string, string[]>;
  overminds?: Record<string, OvermindInfo>;
}
```

```ts
export interface FleetStream {
  agents: Map<string, AgentState>;
  offMap: AgentState[];
  accounting: Accounting | null;
  staleFleets: string[];
  removed: Record<string, string[]>;
  /** Fleet label -> overmind build identity. Carried only on the "snapshot"
   * keyframe (like `removed`); persists across deltas until the next snapshot. */
  overminds: Record<string, OvermindInfo>;
  moves: AgentMove[];
  connected: boolean;
}
```

Initialize it in `useState` and set it in the `snapshot` handler:

```ts
  const [state, setState] = useState<FleetStream>({
    agents: new Map(), offMap: [], accounting: null,
    staleFleets: [], removed: {}, overminds: {}, moves: [], connected: false,
  });
```

In the `snapshot` listener's `setState`, add `overminds: snap.overminds ?? {},` alongside `removed: snap.removed ?? {},`.

In `frontend/src/components/overmind/FleetRail.tsx`, accept `overminds` as a prop and render the badge + summary. Update the prop signature:

```tsx
import { useMemo, useState } from 'react';
import { FLEETS, TIER_COLORS, type AgentState, type OvermindInfo, type Tier } from '../../lib/useFleetStream';
import { AgentCard } from './AgentCard';

export function FleetRail({
  agents, offMap, staleFleets, selectedId, onSelect, highlightedIds,
  removed, onRemove, onReadd, overminds,
}: {
  agents: AgentState[]; offMap: AgentState[]; staleFleets: string[];
  selectedId: string | null; onSelect: (id: string) => void;
  highlightedIds?: ReadonlySet<string>;
  removed?: Record<string, string[]>;
  onRemove?: (agent: AgentState) => void;
  onReadd?: (fleet: string, agentId: string) => void;
  overminds?: Record<string, OvermindInfo>;
}) {
```

Add a version-summary helper above the `return` (after the `groups` useMemo):

```tsx
  // Group a fleet's workers by version string, worst-tier and any raw-modified
  // marker per group, for the "v0.3.0 ×35  v0.2.9 ×6" summary line.
  const versionGroups = (list: AgentState[]): { version: string; count: number; tier: Tier; modified: boolean }[] => {
    const m = new Map<string, { count: number; tier: Tier; modified: boolean }>();
    const rank: Record<Tier, number> = { green: 0, yellow: 1, red: 2 };
    for (const a of list) {
      const version = a.version || 'legacy';
      const tier = a.tier ?? 'red';
      const cur = m.get(version);
      if (cur) {
        cur.count += 1;
        if (rank[tier] > rank[cur.tier]) cur.tier = tier;
        cur.modified = cur.modified || !!a.modified;
      } else {
        m.set(version, { count: 1, tier, modified: !!a.modified });
      }
    }
    return [...m.entries()]
      .map(([version, v]) => ({ version, ...v }))
      .sort((x, y) => y.count - x.count);
  };
```

In the fleet header block (inside the `.map(([fleet, list]) => {` body), compute the overmind info and render the badge + summary. Add just after `const worstUnhealthy = …`:

```tsx
          const ov = overminds?.[fleet];
          const fleetTier: Tier = ov?.fleet_tier ?? 'red';
          const vGroups = versionGroups(list);
```

Then inside the header `<button>`, after the fleet name span and before the credits span, add the overmind badge; and below the button (still inside the fleet `<div>`, before the `{!isCollapsed && list.map(...)}`) add the worker-version summary:

```tsx
              <span className="flex items-center gap-1">
                {ov?.version && (
                  <span
                    className="px-1 text-[9px] font-mono rounded-sm border"
                    style={{ color: TIER_COLORS[fleetTier], borderColor: TIER_COLORS[fleetTier] }}
                    title={`overmind ${ov.version}${ov.commit ? ` (${ov.commit})` : ''}${ov.modified ? ' *' : ''}`}
                  >
                    {ov.version}{ov.modified ? ' *' : ''}
                  </span>
                )}
                <span className="font-mono text-[#8a8570]">₡ {Math.round(credits).toLocaleString()}</span>
              </span>
```

(Replace the existing bare `<span className="font-mono …">₡ …</span>` with the wrapped version above so the badge sits beside the credits.)

And the summary line, rendered when the fleet is expanded and has workers:

```tsx
            {!isCollapsed && vGroups.length > 0 && (
              <div className="flex flex-wrap gap-x-2 gap-y-0.5 px-1 pb-1 text-[9px] font-mono">
                {vGroups.map((g) => (
                  <span key={g.version} style={{ color: TIER_COLORS[g.tier] }}>
                    {g.version}{g.modified ? '*' : ''} ×{g.count}
                  </span>
                ))}
              </div>
            )}
```

Finally, pass `overminds` from `OvermindPage.tsx` into `FleetRail` (add the prop to the existing `<FleetRail … />`):

```tsx
          <FleetRail
            agents={agents}
            offMap={stream.offMap}
            staleFleets={stream.staleFleets}
            selectedId={selectedAgent}
            onSelect={setSelectedAgent}
            highlightedIds={highlightedIds}
            removed={stream.removed}
            onRemove={handleRemove}
            onReadd={handleReadd}
            overminds={stream.overminds}
          />
```

4. **Run it, watch it pass** — `cd frontend && npm run build`. Expected: build completes with no type errors and emits the production bundle.

5. **Commit:**

```
git add frontend/src/lib/useFleetStream.ts frontend/src/components/overmind/FleetRail.tsx frontend/src/components/overmind/OvermindPage.tsx
git commit --no-verify -m "feat(ovdash-ui): per-fleet overmind version badge + worker-version summary"
```

---

## Final verification

After all seven tasks:

```
go build ./...
go test ./pkg/buildinfo/ ./pkg/overmind/control/ ./pkg/overmind/supervisor/ ./pkg/overmind/balances/ ./pkg/ovdash/ ./cmd/worker/ ./cmd/overmind/
golangci-lint run ./pkg/buildinfo/ ./pkg/overmind/... ./pkg/ovdash/ ./cmd/worker/ ./cmd/overmind/ ./scripts/buildinfo-probe/
cd frontend && npm run build
```

All must pass with no new lint findings. Then, per the rollout note, cut the first tag (`git tag -a v0.X.0` per the release policy) and build the fleet via `./scripts/build.sh` before deploying. Because reporting is additive and unstamped/legacy binaries degrade to `dev`/`legacy` (classified red), deploying mid-roll is safe — not-yet-updated fleets simply show as behind, which is the intended signal.
