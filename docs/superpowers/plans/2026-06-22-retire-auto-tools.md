# Retire Redundant `auto-*` Tools (Phase 1) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Delete six redundant/stub `cmd/auto-*` binaries and remove all the dead code, scripts, and documentation they leave behind, leaving the tree building/testing clean and the docs pointing at overmind.

**Architecture:** Pure deletion + cleanup. No new behavior is added. Because these are `package main` binaries imported by nothing, removal cannot break other packages' compilation. The only follow-on work is excising code/scripts/docs whose *sole* purpose was serving the deleted binaries — each removal verified by grep + `go build`/`go test`, never assumed.

**Tech Stack:** Go 1.24+, Make, Bash scripts, Markdown docs.

**Spec:** `docs/superpowers/specs/2026-06-22-retire-auto-tools-design.md` (plus two scope decisions made during planning: delete obsolete auto-miner scripts & fix shared launchers; clean all vestigial refs).

## Global Constraints

- Retire (delete) exactly these six: `cmd/auto-miner`, `cmd/auto-craftsman`, `cmd/auto-pirate`, `cmd/auto-salvager`, `cmd/auto-recall`, `cmd/auto-llm-miner`.
- Keep (do NOT touch the binaries of): `cmd/auto-explorer`, `cmd/auto-trader`, `cmd/auto-prophet`, `cmd/auto-fighter`, `cmd/auto-random`.
- Keep `game.MiningLoop` and `MiningStrategy` — still used by `pkg/strategy/mining.go`. Only `game.CraftingLoop` is dead.
- After every task: `go build ./...` clean, `go test ./...` green, `golangci-lint` introduces no new findings.
- These are deletions, not feature work, so the TDD "failing test first" cycle does not apply. The verification step in each task IS the test: build + test + lint + a grep proving no dangling references remain.
- Frequent commits — one per task.

---

## File Structure (what changes)

- **Deleted dirs:** `cmd/auto-miner/`, `cmd/auto-craftsman/`, `cmd/auto-pirate/`, `cmd/auto-salvager/`, `cmd/auto-recall/`, `cmd/auto-llm-miner/`
- **Deleted files:** `pkg/game/crafting_loop.go`, `scripts/deploy-upgrades.sh`, `scripts/monitor-upgrades.sh`, `scripts/pirates.sh`, `scripts/start-missing-agents.sh`
- **Modified files:** `Makefile`, `.gitignore`, `pkg/game/crafting_test.go`, `pkg/registry/server.go`, `pkg/game/mining.go`, `scripts/launch-agents.sh`, `scripts/start-agents-staggered.sh`, `scripts/add-captains-log-to-agents.sh`, `README.md`, `CLAUDE.md` (in-repo), and `/home/robert/spacemolt/CLAUDE.md` (parent dir, outside repo — edited but not committed here)

---

### Task 1: Delete the six binary directories, Makefile target, and .gitignore lines

**Files:**
- Delete: `cmd/auto-miner/`, `cmd/auto-craftsman/`, `cmd/auto-pirate/`, `cmd/auto-salvager/`, `cmd/auto-recall/`, `cmd/auto-llm-miner/`
- Modify: `Makefile` (remove the `bin/auto-miner` build line)
- Modify: `.gitignore` (remove the five retired root-binary ignore lines)

**Interfaces:**
- Consumes: nothing.
- Produces: a tree with only the five kept `auto-*` dirs. Later tasks (2, 5) rely on these dirs being gone so the code they reference (`game.CraftingLoop`, `ToolTypeAutoMiner`) is provably dead.

- [ ] **Step 1: Delete the six directories**

```bash
cd /home/robert/spacemolt/spacemolt
git rm -r cmd/auto-miner cmd/auto-craftsman cmd/auto-pirate cmd/auto-salvager cmd/auto-recall cmd/auto-llm-miner
```

- [ ] **Step 2: Remove the auto-miner Makefile target**

In `Makefile`, delete this single line (currently line 57):

```make
	go build -o bin/auto-miner ./cmd/auto-miner
```

Leave the `auto-explorer`, `auto-prophet`, `overmind`, and `worker` build lines untouched.

- [ ] **Step 3: Remove retired entries from `.gitignore`**

In `.gitignore`, delete exactly these five lines (keep `auto-explorer`, `auto-trader`, `auto-fighter`, `auto-prophet`, `auto-random`):

```
/auto-miner
/auto-pirate
/auto-craftsman
/auto-salvager
/auto-recall
/auto-llm-miner
```

(Note: that is six lines — `auto-miner`, `auto-pirate`, `auto-craftsman`, `auto-salvager`, `auto-recall`, `auto-llm-miner`. The kept five remain.)

- [ ] **Step 4: Verify build, tests, and Makefile**

```bash
go build ./...
go test ./...
make build
```

Expected: all succeed. `make build` no longer references `auto-miner`. `go build ./...` is clean because the deleted dirs are gone and nothing imported them.

- [ ] **Step 5: Verify no source references the deleted binaries' dirs**

```bash
grep -rn 'cmd/auto-miner\|cmd/auto-craftsman\|cmd/auto-pirate\|cmd/auto-salvager\|cmd/auto-recall\|cmd/auto-llm-miner' --include='*.go' --include='Makefile' .
```

Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "chore: delete redundant auto-* binaries (miner, craftsman, pirate, salvager, recall, llm-miner)"
```

---

### Task 2: Remove dead `game.CraftingLoop` and its loop tests

**Files:**
- Delete: `pkg/game/crafting_loop.go`
- Modify: `pkg/game/crafting_test.go` (remove the two `CraftingLoop`-specific tests)

**Interfaces:**
- Consumes: Task 1 complete (`cmd/auto-craftsman` gone, so `CraftingLoop` has no non-test consumer).
- Produces: nothing downstream depends on this.

Background: `crafting_loop.go` defines `CraftingLoopConfig`, `CraftingLoopResult`, `RecipeSelector`, `StorageManager`, `DefaultRecipeSelector`, and `CraftingLoop`. Grep confirms **none** of these have any consumer outside `crafting_loop.go` itself and two tests, now that `auto-craftsman` is deleted. Crafting will be rebuilt from scratch later (see spec Roadmap), so the whole file goes.

- [ ] **Step 1: Confirm the file is fully dead**

```bash
cd /home/robert/spacemolt/spacemolt
for sym in CraftingLoop CraftingLoopConfig CraftingLoopResult RecipeSelector StorageManager DefaultRecipeSelector; do
  echo "--- $sym ---"
  grep -rn "\b$sym\b" --include='*.go' | grep -v 'pkg/game/crafting_loop.go'
done
```

Expected: the only matches are inside `pkg/game/crafting_test.go` (the two loop tests). If anything ELSE appears, STOP — the file is not dead; report it instead of deleting.

- [ ] **Step 2: Delete the loop file**

```bash
git rm pkg/game/crafting_loop.go
```

- [ ] **Step 3: Remove the two loop tests from `crafting_test.go`**

Delete these two complete test functions from `pkg/game/crafting_test.go`:
- `TestCraftingLoopConfig_Defaults` (currently starts at line 136)
- `TestCraftingLoopConfig_InvalidStrategy` (currently starts at line 148)

Leave the other seven tests intact (`TestCraftRecipeQueuesOnce`, `TestXpToLevel`, `TestCraftWithQuantity_Validation`, `TestCraftQueryResult_EmptyInit`, `TestCraftWithOptionsPayload`, `TestCraftWithOptionsRejectsBadQuantity`, `TestCraftableRecipe_Fields`). After editing, verify only those two are gone:

```bash
grep -nE '^func Test' pkg/game/crafting_test.go
```

Expected: lists seven test functions, none containing `CraftingLoop`.

- [ ] **Step 4: Verify build, tests, lint**

```bash
go build ./...
go test ./pkg/game/...
golangci-lint run ./pkg/game/...
```

Expected: build clean, `pkg/game` tests pass, no new lint findings (in particular no "declared and not used" / unused-symbol errors — proving nothing dangled).

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "chore(game): remove dead CraftingLoop (crafting to be rebuilt from scratch)"
```

---

### Task 3: Delete obsolete auto-miner-only scripts

**Files:**
- Delete: `scripts/deploy-upgrades.sh`, `scripts/monitor-upgrades.sh`, `scripts/pirates.sh`, `scripts/start-missing-agents.sh`

**Interfaces:**
- Consumes: Task 1 (the `auto-miner` binary these scripts drive no longer exists).
- Produces: nothing.

These four scripts exist solely to build/deploy/launch/monitor `auto-miner` (`pirates.sh` runs `auto-miner` instances and labels them "pirates"). With the binary gone they are dead operational tooling; overmind replaces this workflow.

- [ ] **Step 1: Confirm each script is auto-miner-centric (sanity check before deleting)**

```bash
cd /home/robert/spacemolt/spacemolt
grep -l 'auto-miner' scripts/deploy-upgrades.sh scripts/monitor-upgrades.sh scripts/pirates.sh scripts/start-missing-agents.sh
```

Expected: all four paths listed.

- [ ] **Step 2: Delete them**

```bash
git rm scripts/deploy-upgrades.sh scripts/monitor-upgrades.sh scripts/pirates.sh scripts/start-missing-agents.sh
```

- [ ] **Step 3: Verify no other script sources the deleted ones**

```bash
grep -rn 'deploy-upgrades\|monitor-upgrades\|start-missing-agents\|pirates\.sh' scripts/ Makefile 2>/dev/null
```

Expected: no output (no remaining script invokes them).

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "chore(scripts): remove obsolete auto-miner deploy/launch scripts"
```

---

### Task 4: Fix shared launcher scripts to drop retired-bot references

**Files:**
- Modify: `scripts/launch-agents.sh`
- Modify: `scripts/start-agents-staggered.sh`
- Modify: `scripts/add-captains-log-to-agents.sh`

**Interfaces:**
- Consumes: Task 1 (retired binaries gone).
- Produces: launcher scripts that reference only kept binaries.

- [ ] **Step 1: Update `scripts/add-captains-log-to-agents.sh`**

Change the `AGENTS` array (currently line 6) to drop the three deleted agents (`auto-craftsman`, `auto-salvager`, `auto-pirate`), keeping the two that still exist:

```bash
AGENTS=("auto-random" "auto-fighter")
```

- [ ] **Step 2: Update `scripts/launch-agents.sh` help examples**

In the `Examples:` block (currently lines ~449–453), remove the two lines that reference deleted binaries and reword the pirate example to use a kept binary. Replace:

```bash
    echo "  $0 start pirate                    # Start only pirate agents with auto-pirate"
    echo "  $0 start --binary miner            # Start ALL agents with auto-miner"
    echo "  $0 start pirate --binary miner     # Start pirate agents with auto-miner"
    echo "  $0 restart --binary explorer       # Restart all with auto-explorer"
```

with:

```bash
    echo "  $0 start --binary explorer         # Start ALL agents with auto-explorer"
    echo "  $0 restart --binary trader         # Restart all with auto-trader"
```

Then verify no retired binary name remains in the file:

```bash
grep -nE 'auto-(miner|craftsman|pirate|salvager|recall|llm-miner)' scripts/launch-agents.sh
```

Expected: no output. (If other matches surface, reword them to a kept binary the same way.)

- [ ] **Step 3: Update `scripts/start-agents-staggered.sh`**

This script has both documentation references and one live conditional tied to `auto-miner`. Handle both:

1. In the header/usage comments and the `--strategy` help text (currently lines ~7, ~25, ~40), replace `auto-miner` with `auto-explorer` (a kept binary) so the examples stay valid.
2. The strategy-application conditional (currently line ~169):

   ```bash
   if [ -n "$STRATEGY_ARG" ] && [[ "$binary" == "auto-miner" ]]; then
   ```

   Since `auto-miner` no longer exists, this branch can never fire. Remove the `auto-miner`-specific guard so the strategy flag applies to whatever binary is selected (matching the comment at line ~40 that says it applies to ANY agent). Change it to:

   ```bash
   if [ -n "$STRATEGY_ARG" ]; then
   ```

Then verify:

```bash
grep -nE 'auto-(miner|craftsman|pirate|salvager|recall|llm-miner)' scripts/start-agents-staggered.sh
```

Expected: no output.

- [ ] **Step 4: Shellcheck-style sanity (syntax) on the edited scripts**

```bash
bash -n scripts/launch-agents.sh && bash -n scripts/start-agents-staggered.sh && bash -n scripts/add-captains-log-to-agents.sh && echo "syntax OK"
```

Expected: `syntax OK`.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "chore(scripts): drop retired auto-* references from shared launchers"
```

---

### Task 5: Remove vestigial Go references

**Files:**
- Modify: `pkg/registry/server.go` (remove `ToolTypeAutoMiner`)
- Modify: `pkg/game/mining.go` (fix stale comment at line 226)

**Interfaces:**
- Consumes: Task 1.
- Produces: nothing downstream.

- [ ] **Step 1: Remove the `ToolTypeAutoMiner` constant**

In `pkg/registry/server.go`, delete this line from the `const` block (currently line 18):

```go
	ToolTypeAutoMiner    ToolType = "auto-miner"
```

Keep `ToolTypeAutoExplorer` (auto-explorer is a kept binary), `ToolTypeAgentServer`, and `ToolTypePlayAs`. Grep confirms `ToolTypeAutoMiner` has zero usages elsewhere, so this is safe.

- [ ] **Step 2: Fix the stale comment in `pkg/game/mining.go`**

The comment at line 226 currently reads:

```go
// This is the shared mining loop used by auto-miner, auto-explorer, and other autonomous agents.
```

`auto-miner` no longer exists. Replace with:

```go
// This is the shared mining loop used by auto-explorer, MiningStrategy, and other autonomous agents.
```

- [ ] **Step 3: Verify build, tests, lint**

```bash
go build ./...
go test ./...
golangci-lint run ./pkg/registry/... ./pkg/game/...
```

Expected: all clean (`ToolTypeAutoMiner` removal compiles because nothing referenced it).

- [ ] **Step 4: Verify no Go file still names a retired binary (except the harmless calllog test fixture)**

```bash
grep -rn 'auto-miner\|auto-craftsman\|auto-pirate\|auto-salvager\|auto-recall\|auto-llm-miner' --include='*.go' .
```

Expected: the ONLY remaining match is `pkg/calllog/calllog_test.go` (uses `"auto-miner"` as an arbitrary logger-name fixture — intentionally left, not load-bearing). If anything else appears, address it.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "chore: remove vestigial auto-miner references in registry and mining comment"
```

---

### Task 6: Reframe documentation toward overmind

**Files:**
- Modify: `README.md` (four reference sites + run instructions)
- Modify: `CLAUDE.md` (in-repo `cmd/` directory listing)
- Modify: `/home/robert/spacemolt/CLAUDE.md` (parent-dir copy — edit for consistency; it is OUTSIDE this git repo and will NOT be part of this commit)

**Interfaces:**
- Consumes: Task 1.
- Produces: docs that describe the actual tree and point readers at overmind.

- [ ] **Step 1: README — "Specialized Automated Agents" bin list**

Replace the block (currently lines ~197–208) with the kept-five list, relabeled:

```markdown
**Specialized Automated Agents (legacy — migrating to overmind roles):**
- `bin/auto-explorer` - Automated exploration bot
- `bin/auto-trader` - Automated trading bot
- `bin/auto-fighter` - Automated combat bot
- `bin/auto-prophet` - Automated prophet bot
- `bin/auto-random` - Automated random-action bot
```

- [ ] **Step 2: README — ASCII architecture diagram box**

In the "Automated Agents" box (currently lines ~374–383), keep only the five surviving entries, preserving the `│• ...│` fixed-width format. Replace the listed agent lines with:

```
│• auto-explorer │
│• auto-trader   │
│• auto-fighter  │
│• auto-prophet  │
│• auto-random   │
```

(i.e. remove the `auto-miner`, `auto-pirate`, `auto-salvager`, `auto-craftsman`, and `auto-llm-miner` lines from inside the box.)

- [ ] **Step 3: README — "Available Automated Agents" list**

Replace the list (currently lines ~891–903) with:

```markdown
- **auto-explorer** - Systematically explores and maps systems
- **auto-trader** - Trades goods between stations
- **auto-fighter** - Engages in combat and bounty hunting
- **auto-prophet** - Prophet-style agent with strategic foresight
- **auto-random** - Random-action agent for testing and exploration
```

- [ ] **Step 4: README — "Running Automated Agents" section → overmind**

Replace the "Running Automated Agents" code block and its trailing line (currently lines ~905–919) with overmind-centric guidance:

````markdown
### Running Agents

Standing/recurring agent behaviors are now run under **overmind**, which supervises
a fleet of `worker` processes driven by per-role config in `data/overmind/roles.yaml`
(scheduled commands + an idle script per role). This replaces the old one-binary-per-bot
model (e.g. mining now runs as the resident role's `idle: idle_mine`).

```bash
# Build the supervisor and worker runtime
make build   # produces bin/overmind and bin/worker

# Edit the fleet roster (data/overmind/fleet.yaml) and roles (data/overmind/roles.yaml),
# then launch the supervised fleet
bin/overmind
```

The remaining `auto-*` binaries above are legacy specialized bots not yet migrated to
roles; see `docs/superpowers/specs/2026-06-22-retire-auto-tools-design.md` for the
migration roadmap.
````

- [ ] **Step 5: README — directory tree**

In the `cmd/` directory tree (currently lines ~1009–1020), remove the six retired entries (`auto-miner`, `auto-pirate`, `auto-salvager`, `auto-craftsman`, `auto-recall`, `auto-llm-miner`), keeping `auto-explorer`, `auto-trader`, `auto-fighter`, `auto-prophet`, `auto-random`.

- [ ] **Step 6: README — verify no retired names remain**

```bash
grep -nE 'auto-(miner|craftsman|pirate|salvager|recall|llm-miner)' README.md
```

Expected: no output.

- [ ] **Step 7: CLAUDE.md (in-repo) — `cmd/` listing**

In `CLAUDE.md` (the in-repo one at `/home/robert/spacemolt/spacemolt/CLAUDE.md`), remove the six retired lines from the `cmd/` tree (currently lines ~30, 33, 34, 35, 38, 39 — `auto-miner`, `auto-pirate`, `auto-craftsman`, `auto-salvager`, `auto-llm-miner`, `auto-recall`), keeping `auto-explorer`, `auto-trader`, `auto-fighter`, `auto-prophet`, `auto-random`. Verify:

```bash
grep -nE '│.*auto-(miner|craftsman|pirate|salvager|recall|llm-miner)' CLAUDE.md
```

Expected: no output.

- [ ] **Step 8: Parent-dir CLAUDE.md (outside repo) — same edit**

Apply the identical six-line removal to `/home/robert/spacemolt/CLAUDE.md` for consistency. This file is OUTSIDE this git repository, so it will not appear in `git status` and is not part of the commit below — just leave it edited on disk.

- [ ] **Step 9: Commit (in-repo docs only)**

```bash
git add README.md CLAUDE.md
git commit -m "docs: reframe agent docs around overmind; drop retired auto-* tools"
```

---

### Task 7: Final full-tree verification

**Files:** none (verification only).

- [ ] **Step 1: Full build + test + lint**

```bash
cd /home/robert/spacemolt/spacemolt
go build ./...
go test ./...
golangci-lint run ./...
```

Expected: build clean, all tests green, no new lint findings.

- [ ] **Step 2: Final reference sweep across tracked files**

```bash
git grep -nE 'auto-(miner|craftsman|pirate|salvager|recall|llm-miner)' -- . ':!docs/superpowers/' ':!docs/plans/' ':!docs/*.md' ':!playbook/' ':!data/skills/'
```

Expected: the only remaining match is `pkg/calllog/calllog_test.go` (intentional fixture). Historical docs under `docs/` and `playbook/`, and `data/skills/mine.*`, are deliberately left as-is (historical record / unrelated skill definitions). If any NEW unexpected match appears in active code/config, resolve it before declaring done.

- [ ] **Step 3: Confirm kept binaries still build individually**

```bash
go build -o /tmp/_chk_explorer ./cmd/auto-explorer && go build -o /tmp/_chk_trader ./cmd/auto-trader && go build -o /tmp/_chk_prophet ./cmd/auto-prophet && go build -o /tmp/_chk_fighter ./cmd/auto-fighter && go build -o /tmp/_chk_random ./cmd/auto-random && echo "kept binaries OK" && rm -f /tmp/_chk_*
```

Expected: `kept binaries OK`.

---

## Self-Review

**Spec coverage:**
- Delete six dirs → Task 1. ✓
- Remove dead `crafting_loop.go` + loop tests, keep `MiningLoop` → Task 2. ✓
- Makefile auto-miner target → Task 1. ✓
- README reframe (4 sites + run instructions → overmind) → Task 6. ✓
- Roadmap note → already in the committed spec; nothing to implement. ✓
- Planning-time scope additions: delete obsolete auto-miner scripts (Task 3), fix shared launchers (Task 4), clean vestigial refs — registry constant, .gitignore, CLAUDE.md, mining.go comment (Tasks 1, 5, 6). ✓
- Verification gate (build/test/lint/grep) → every task + Task 7. ✓

**Placeholder scan:** No TBD/TODO/"handle edge cases"/"similar to Task N". Every edit shows exact text. ✓

**Type/name consistency:** Symbol names (`CraftingLoop`, `ToolTypeAutoMiner`, `MiningLoop`, `idle_mine`) match the codebase grep results; test function names match `crafting_test.go`; script/line references match current file contents. ✓
