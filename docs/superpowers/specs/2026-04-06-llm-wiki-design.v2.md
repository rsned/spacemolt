<!-- v2 - 2026-04-06 - Generated via design-review swarm from docs/superpowers/specs/2026-04-06-llm-wiki-design.md -->

# LLM Wiki Design Specification — v2

**Date:** 2026-04-06
**Version:** 2 (revised from review feedback)
**Status:** Approved

## Overview

A persistent, LLM-maintained knowledge base that accumulates strategies, techniques, and insights about Spacemolt gameplay. The wiki serves as shared memory across ~100 agents, compounding knowledge over time.

**Core Problem Solved:**
- Agents cannot share learned strategies efficiently across sessions
- No persistent memory for "what works" — effective techniques discovered by one agent are invisible to others
- Agent prompts contain repetitive experience data instead of distilled, actionable knowledge

**Solution:**
- Markdown wiki with domain pages (mining, trading, combat) and technique pages (can-mining, marauder tactics)
- Agents read distilled wiki context during decisions via existing enrichment channels
- A scheduled ingestion service processes agent experiences through an LLM to extract and maintain wiki content

**Non-Goal:**
- Token count reduction. Wiki context adds tokens, not removes them. The goal is better decisions-per-token by replacing verbose, repetitive experience dumps with distilled strategies.

## Architecture

### Two-Package System

The wiki is split into two packages to keep the read path dependency-free:

- **`pkg/wiki/`** — Read-only interface. Dependencies: only `os` and stdlib. Every agent binary links this.
- **`pkg/wikiingest/`** — Write path with LLM and KB dependencies. Only linked by `cmd/wiki-ingest/`.
- **`cmd/wiki-ingest/`** — Standalone binary for scheduled ingestion runs.

This separation ensures that adding wiki reads to agent binaries does not pull in `pkg/knowledge`, `pkg/llm`, or their transitive dependencies (SQLite, HTTP client, etc.).

```
┌─────────────────────────────────────────────────────────────┐
│                     Agent Decision Cycle                    │
│  ┌──────────────┐         ┌──────────────┐                 │
│  │ Game Client  │────────▶│  pkg/wiki/   │◀──── wiki/      │
│  │              │         │  (read-only)  │    (markdown)   │
│  └──────────────┘         └──────────────┘                 │
│         ▲                        ▲                          │
│         │                        │                          │
│    Current State          Wiki Context                      │
│                     (via EnrichedState /                     │
│                      PromptContext)                          │
└─────────┴────────────────────────┴──────────────────────────┘
         │
         ▼
┌─────────────────────────────────────────────────────────────┐
│              Scheduled Ingestion Service                    │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐      │
│  │ SQLite KB    │──│pkg/wikiingest│──│  pkg/llm/    │      │
│  │ (experiences)│  │  (write ops)  │  │  (Ollama)    │      │
│  └──────────────┘  └──────────────┘  └──────────────┘      │
│                           │                                 │
│                    wiki/ updates                            │
└─────────────────────────────────────────────────────────────┘
```

## Wiki Read Package (`pkg/wiki/`)

### Interface

```go
// Wiki provides read access to the wiki knowledge base.
// Thread-safe. Zero external dependencies beyond os/stdlib.
type Wiki interface {
    // GetContext returns pre-formatted wiki context for an agent's current situation.
    // This is the primary read path — called once per decision cycle.
    // role: agent role (e.g., "miner", "trader", "explorer")
    // poiType: current POI type (e.g., "asteroid_belt", "station"), empty if in space
    // limit: maximum number of pages to include
    GetContext(ctx context.Context, role string, poiType string, limit int) (string, error)

    // GetPage returns a single wiki page by path (e.g., "domains/mining.md").
    GetPage(ctx context.Context, path string) (*Page, error)

    // GetIndex returns the wiki index with all page metadata.
    GetIndex(ctx context.Context) (*Index, error)

    // QueryPages performs keyword search across wiki content.
    // Secondary read path for ad-hoc queries (MCP tools, debugging).
    QueryPages(ctx context.Context, query string) ([]Page, error)
}
```

### Implementation

**`client.go`** — `WikiClient` implements `Wiki`. At startup, loads all markdown files from `wiki/` into an in-memory `map[string]*Page`. Pages are parsed for YAML frontmatter. Reloads on a configurable interval (default: 5 minutes) or on SIGHUP.

`GetContext()` is the hot path: it performs a map lookup by role to find relevant domain pages, optionally filtered by `poiType`. Returns pre-joined page content as a formatted string. No file I/O per call.

```go
type WikiClient struct {
    wikiDir        string
    pages          map[string]*Page    // path -> page
    roleIndex      map[string][]string // role -> relevant page paths
    mu             sync.RWMutex        // protects pages/roleIndex during reload
    reloadInterval time.Duration
}
```

**`types.go`** — Data structures:

```go
type Page struct {
    Path        string            // e.g., "domains/mining.md"
    Content     string            // Markdown content (without frontmatter)
    FrontMatter PageMeta          // Parsed YAML metadata
    ModTime     time.Time         // File modification time
}

type PageMeta struct {
    Title    string   `yaml:"title"`
    Category string   `yaml:"category"` // "domain", "technique"
    Updated  string   `yaml:"updated"`
    Related  []string `yaml:"related"`
}

type Index struct {
    Pages       []IndexEntry
    LastUpdated time.Time
}

type IndexEntry struct {
    Path     string
    Title    string
    Category string
    Updated  string
}
```

**`search.go`** — `QueryPages()` implementation. For MVP, iterates over in-memory pages and does case-insensitive substring matching. Upgradable to more sophisticated search later.

**`roleindex.go`** — Builds the `roleIndex` mapping at load time. Rules:
- Role "miner" -> `domains/mining.md`, `domains/crafting.md`
- Role "trader" -> `domains/trading.md`
- Role "explorer" -> `domains/exploration.md`
- Role "fighter" / "pirate" -> `domains/combat.md`
- All roles -> any technique pages with matching `category` in frontmatter
- Fallback: if role not recognized, return all domain pages up to `limit`

### Unit Tests

- `TestGetContext()` — Returns relevant pages for role, respects limit
- `TestGetContextUnknownRole()` — Falls back to all domains
- `TestGetPage()` — Loads page with frontmatter parsing
- `TestGetPageNotFound()` — Returns appropriate error
- `TestQueryPages()` — Keyword search returns matches
- `TestReload()` — File changes picked up after reload interval
- `TestConcurrentReads()` — 100 goroutines reading simultaneously
- `TestRoleIndex()` — Correct pages mapped to each role

All tests use `t.TempDir()` with in-test markdown files. No external dependencies.

## Wiki Ingest Package (`pkg/wikiingest/`)

### Interface

```go
// Ingestor handles wiki write operations. Only used by cmd/wiki-ingest.
type Ingestor interface {
    // IngestExperiences processes recent agent experiences and updates wiki pages.
    IngestExperiences(ctx context.Context, experiences []knowledge.Experience) error

    // UpdatePage writes content to a wiki page (atomic file replace).
    UpdatePage(ctx context.Context, path string, content string) error

    // CreatePage creates a new wiki page.
    CreatePage(ctx context.Context, path string, content string) error

    // UpdateIndex regenerates wiki/index.md from current page files.
    UpdateIndex(ctx context.Context) error

    // AddLogEntry appends an entry to the daily log file.
    AddLogEntry(ctx context.Context, entry LogEntry) error

    // RunLintPass checks wiki health and returns issues.
    RunLintPass(ctx context.Context) ([]LintIssue, error)
}
```

### Implementation

**`ingestor.go`** — Core ingestion logic:
1. Groups experiences by domain/topic (mining, trading, combat, exploration, crafting)
2. For each group, calls LLM to extract insights
3. LLM prompt includes: current page content + new experiences + instructions to update
4. Validates LLM output (must be valid markdown, must preserve existing sections)
5. Writes updated page via atomic file replace (write to temp, `os.Rename()`)
6. Updates index, appends to daily log

**`lint.go`** — Health checks:
- Pages with no `related:` links
- Pages not updated in 30+ days
- `**CONTRADICTION:**` markers not resolved
- Index out of sync with actual files

### Error Handling

The ingest pipeline must be resilient:
- **LLM unavailable:** Skip ingestion, log warning, exit cleanly
- **Invalid LLM output:** Reject update, log the bad output, continue with next group
- **Page write failure:** Log error, continue with remaining pages
- **`--dry-run` flag:** Show proposed changes without writing
- **`--interactive` flag:** Prompt for confirmation before each page update

### Data Types

```go
type LogEntry struct {
    Type          string   // "ingest", "lint", "manual"
    Summary       string
    PagesAffected []string
    Timestamp     time.Time
}

type LintIssue struct {
    Path     string
    Severity string // "warning", "error"
    Message  string
}
```

## New KB Method Required

The existing `GetRecentExperiences(ctx, agentID, limit)` returns experiences for a single agent with a row-count limit. The ingest service needs cross-agent, time-based queries.

**Add to `knowledge.Base` interface:**
```go
// GetExperiencesSince returns all experiences across all agents since the given time.
GetExperiencesSince(ctx context.Context, since time.Time) ([]Experience, error)
```

**SQLite implementation** requires a new index:
```sql
CREATE INDEX IF NOT EXISTS idx_experiences_time ON experiences(time DESC);
```

**Additional changes to experience retention:**
- Increase the per-agent experience cap from 100 to 500 in `AddExperience()` cleanup query (`pkg/knowledge/sqlite.go` line ~753)
- Add an `ingested_at` timestamp column to the experiences table (nullable) so the ingest service can track its high-water mark and avoid reprocessing
- The migration should **delete all existing experience rows** — they are stale data from early development and were never consumed by any agent. Clean slate for the wiki ingest pipeline.

## Agent Integration

### No Interface Changes Required

Wiki context flows through existing enrichment channels. The `Agent.Decide(ctx, EnrichedState)` signature is **unchanged**.

**For ToT agents (`decision_mode: "tot"`):**
Add a `WikiContext string` field to `PromptContext` in `pkg/tot/prompt_context.go`:

```go
type PromptContext struct {
    // ... existing fields ...

    // WikiContext is pre-formatted wiki content relevant to the agent's
    // current situation. Populated by the runner from pkg/wiki.
    WikiContext string
}
```

The ToT evaluator prompt templates include `{{.WikiContext}}` when non-empty.

**For template-based agents (`BaseAgent`):**
Add wiki context to `buildKnowledgeContext()` in `pkg/agent/base.go`. The `BaseAgent` gains an optional `wiki.Wiki` field set via a setter method:

```go
func (a *BaseAgent) SetWiki(w wiki.Wiki) {
    a.wiki = w
}
```

In `buildKnowledgeContext()`, if `a.wiki != nil`, call `GetContext(role, poiType, limit)` and include the result in the knowledge context passed to prompt templates.

**For strategy-based agents (non-LLM):**
Add an optional `Wiki wiki.Wiki` field to `strategy.Config`. Strategy implementations can read wiki pages for tunable parameters (e.g., mining richness thresholds, trade route preferences). This is a Phase 3+ concern — strategy bots benefit differently from LLM agents.

### Runner Integration

In `pkg/agent/runner.go`, the `Runner` gains an optional `wiki wiki.Wiki` field:

```go
func (r *Runner) SetWiki(w wiki.Wiki) {
    r.wiki = w
}
```

During `executeCycle()`, after refreshing the enriched state, the runner calls `wiki.GetContext()` and passes the result through to the agent's decision method via existing enrichment channels. The wiki client is shared across all runners (it is read-only and thread-safe).

### Manager Integration

In `pkg/agent/manager.go`, the `Manager` accepts an optional `wiki.Wiki` in its config and passes it to each `Runner` via `SetWiki()`:

```go
type ManagerConfig struct {
    // ... existing fields ...
    Wiki wiki.Wiki // optional, nil disables wiki context
}
```

### Prompt Integration

Wiki context appears in prompts as a clearly delineated section:

```
=== Wiki Knowledge ===
[Mining Strategies]
- Use basic scanner at asteroids with >50% richness for best yield
- Iron and copper deposits near Achernar are high-value targets
- Avoid mining in systems with danger level >5 without escort

[Relevant Technique: Can Mining]
- Coordinate with hauler for 3x throughput
- Requires: mining ship + cargo hauler in same system
=== End Wiki ===
```

The wiki section supplements game state in the prompt. Game state remains unchanged.

## Directory Structure

```
wiki/
├── CLAUDE.md                  # Wiki conventions for Claude (auto-discovered at root)
├── index.md                   # Page catalog (auto-generated)
├── domains/
│   ├── mining.md              # Mining strategies, POI selection, ship configs
│   ├── trading.md             # Trade routes, market patterns, arbitrage
│   ├── combat.md              # Combat tactics, ship builds, threat assessment
│   ├── exploration.md         # System discovery, POI mapping, efficient travel
│   ├── crafting.md            # Recipes, production chains, profitability
│   └── factions.md            # Faction mechanics, standing strategies
├── techniques/                # Starts empty, populated by ingest as agents discover tactics
└── log/
    └── YYYY-MM-DD.md          # Daily log files, rotated after 30 days
```

**Omitted from MVP:**
- `agents/` — Per-agent findings duplicate KB experiences. Use `sources:` frontmatter for attribution instead.
- `analysis/` — Generated analyses belong in KB `market_analyses` table, not wiki.
- `schema/WIKI.md` — Premature until write path is validated.

### Initial Seed Content

Each `domains/*.md` gets hand-authored content with real strategies from:

1. **Core Mechanics:** `https://www.spacemolt.com/skill.md`
2. **Strategy Guides:** `https://github.com/SpaceMolt/www/tree/main/public/guides`
3. **KB Intelligence:** Synthesized from existing market analyses, resource distributions, and system data in SQLite

**The seed content must be authored before agent integration.** Integrating agents against empty stubs wastes effort and produces no signal.

## Schema & Conventions

### Page FrontMatter (Phase 1 — Minimal)

```yaml
---
title: "Mining Strategies"
category: "domain"
updated: "2026-04-06"
related: ["techniques/can-mining.md", "domains/trading.md"]
---
```

Phase 2+ adds: `tags`, `sources` (for attribution), `last_reviewed`.

### Domain Page Template

```markdown
## Overview
[High-level summary of the domain]

## Ship Configurations
### Best Ships for [Activity]
- [Ship class]: [Why it's good, when to use]

### Module Loadouts
- [Loadout name]: [Modules, reasoning]

## Strategy
### High-Value Targets
- [Target]: [Where to find, market price, difficulty]

### Location Selection
- [Location type]: [Characteristics, when to choose]

## Profitability Analysis
[Estimated rewards, costs, risks]

## Known Issues & Risks
- [Issue]: [How to mitigate]

## Insights from Agents
- [Date] [Agent]: [What they learned, outcome]
```

### Technique Page Template

```markdown
## Overview
[Brief description of the technique]

## Required Roles
- [Role 1]: [Responsibilities, skills needed]

## Coordination Protocol
1. [Step 1]
2. [Step 2]

## Efficiency Gains
[Why this is better than solo]

## Prerequisites
[Ships, skills, equipment needed]

## Known Variations
- [Variation]: [When to use it]

## Insights from Agents
[Who used it successfully, lessons learned]
```

### Wiki Conventions (`wiki/CLAUDE.md`)

**When updating pages:**
- Always update `updated:` in frontmatter
- Flag contradictions with `**CONTRADICTION:**` prefix — do not silently overwrite
- Append to sections rather than rewriting (preserves evolution of knowledge)
- Update `related:` if adding cross-references

**When creating pages:**
- Use kebab-case filenames (`can-mining.md`, not `can_mining.md`)
- Always include frontmatter
- Link to related pages
- Add to `index.md` under appropriate category

**Contradiction handling:**
- Mark contradictions explicitly when new data conflicts with existing content
- Include date and source of both the original claim and the new data
- Resolution is manual until Phase 5 (lint pass identifies unresolved contradictions)

## Ingestion Service (`cmd/wiki-ingest/`)

### Workflow

```
1. cmd/wiki-ingest runs (scheduled, default every 6 hours)
2. Fetch experiences: kb.GetExperiencesSince(ctx, lastIngestTime)
3. Group by domain/topic using keyword matching on experience type/description
4. For each group with >= 3 experiences:
   a. Load current wiki page content
   b. LLM analyzes experiences against current page
   c. LLM extracts insights, flags contradictions
   d. Validate LLM output (valid markdown, sections preserved)
   e. Write updated page (atomic rename)
5. Update wiki/index.md
6. Append to wiki/log/YYYY-MM-DD.md
7. Update high-water mark (ingested_at on processed experiences)
8. Optional: Run lint pass (weekly, not every run)
```

### Scheduling

- Default: every 6 hours (configurable via flag)
- Can be run via cron, systemd timer, or manually
- `--once` flag for single-run mode
- `--dry-run` flag to preview changes without writing
- `--interactive` flag to confirm each update

### Experience Grouping

Experiences are grouped by domain using keyword matching on the `type` and `description` fields:

| Domain | Experience Types / Keywords |
|--------|---------------------------|
| mining | `mining`, `mine`, `resource`, `asteroid`, `refine` |
| trading | `trade`, `market`, `buy`, `sell`, `profit`, `cargo` |
| combat | `combat`, `battle`, `pirate`, `fight`, `weapon`, `attack` |
| exploration | `travel`, `jump`, `explore`, `discover`, `system`, `scan` |
| crafting | `craft`, `recipe`, `produce`, `manufacture` |
| factions | `faction`, `standing`, `reputation` |

Experiences that do not match any domain are logged but skipped.

### LLM Prompt for Insight Extraction

```
You are analyzing agent gameplay experiences to update a wiki page.

Current page content:
---
{current_page_content}
---

New experiences to analyze:
{experiences_json}

Instructions:
1. Identify actionable insights from these experiences
2. Update the relevant sections of the page
3. If new data contradicts existing content, prefix with **CONTRADICTION:** and include both claims
4. Append new insights to existing sections — do not rewrite or remove existing content
5. Return the complete updated page content as valid markdown

Return ONLY the updated markdown content, no explanations.
```

## Data Flow

### Agent Decision Cycle (Read Path)
```
1. Runner.executeCycle() called
2. enrichedState.Refresh(ctx) rebuilds KB enrichment
3. runner.wiki.GetContext(role, poiType, 3) returns formatted wiki text
4. Wiki context injected into PromptContext.WikiContext (ToT)
   or into buildKnowledgeContext() result (template agents)
5. LLM makes decision with wiki-enhanced knowledge
6. No wiki writes during gameplay — agents are read-only consumers
```

### Ingestion Cycle (Write Path)
```
1. cmd/wiki-ingest runs on schedule (every 6 hours)
2. Fetches unprocessed experiences via GetExperiencesSince()
3. Groups by domain, filters groups with < 3 experiences
4. LLM processes each group against current page content
5. Validated updates written via atomic file replace
6. Index and daily log updated
7. High-water mark updated on processed experiences
```

### Concurrency Model

- **Reads:** No locking required for individual reads. The `WikiClient` holds an in-memory copy of all pages. Periodic reload acquires a write lock briefly to swap the page map. Readers see a consistent snapshot via `sync.RWMutex`.
- **Writes:** Only the ingest service writes. It uses atomic file replace (write to temp file, `os.Rename()`). No concurrent writers exist by design.
- **Scale:** With ~20 wiki pages at ~2KB each (40KB total), 100 agents reading from the in-memory map at ~9 reads/second is trivially fast. No additional caching layer needed beyond the in-memory page map.

## Agent Configuration

Add optional wiki fields to agent personality YAML:

```yaml
wiki_enabled: true          # default: true if wiki package is configured
wiki_context_limit: 3       # max pages to include in prompt (default: 3)
```

The agent's `Role` field determines which domain pages are relevant. No `wiki_focus_domains` field is needed — role-based inference handles this. For agents with unusual domain needs, the `Parameters` map in `strategy.Config` or personality YAML can override.

## Testing & Validation

### Unit Tests (`pkg/wiki/`)

- `TestGetContext()` — Returns relevant pages for role, respects limit
- `TestGetContextUnknownRole()` — Falls back to all domains
- `TestGetPage()` — Loads page with frontmatter parsing
- `TestGetPageNotFound()` — Returns appropriate error
- `TestQueryPages()` — Keyword search returns matches
- `TestReload()` — File changes picked up after reload interval
- `TestConcurrentReads()` — 100 goroutines reading simultaneously
- `TestRoleIndex()` — Correct pages mapped to each role

All tests use `t.TempDir()` with in-test markdown files. No external dependencies.

### Unit Tests (`pkg/wikiingest/`)

- `TestIngestExperiences()` — Mock LLM, verify page updates
- `TestIngestExperiencesLLMDown()` — Graceful skip when LLM unavailable
- `TestIngestExperiencesInvalidOutput()` — Reject bad LLM output, continue
- `TestUpdateIndex()` — Index reflects current page files
- `TestAtomicWrite()` — Verify temp file + rename pattern
- `TestLogRotation()` — Daily log files created correctly

### Integration Tests

```go
func TestAgentDecisionWithWiki(t *testing.T) {
    // Setup: Create test wiki with mining strategy page
    // Setup: Create agent with wiki configured
    // Act: Run one decision cycle at an asteroid belt
    // Assert: PromptContext.WikiContext contains mining content
}

func TestIngestPipeline(t *testing.T) {
    // Setup: SQLite KB with test experiences, mock LLM
    // Act: Run full ingest pipeline
    // Assert: Wiki pages updated, index current, log entry created
}
```

### Manual Validation

**After Phase 0 (quick validation):**
- Does the mining agent's decision quality visibly improve with wiki context?
- Is the wiki content showing up in prompt logs?

**After Phase 3 (ingestion):**
- Review LLM-generated page updates for accuracy
- Check for hallucinations or incorrect conclusions
- Verify contradictions are flagged, not silently overwritten

**After Phase 4 (experience loop):**
- Are agent-discovered insights appearing in wiki pages?
- Is cross-agent knowledge sharing working? (miner learns from explorer findings)
- Run lint pass, review issues

## Implementation Phases

### Phase 0: Quick Validation (2 hours)
**Goal:** Validate that wiki context improves agent decisions before investing in the full system.

**Tasks:**
- [ ] Hand-write `wiki/domains/mining.md` with real mining strategies from game guides
- [ ] Add `os.ReadFile()` call in `buildKnowledgeContext()` to inject mining.md content
- [ ] Run one mining agent (tot mode), observe decision quality in logs
- [ ] If no visible improvement, reassess approach before proceeding

**Deliverable:** Go/no-go signal for the full implementation.

**Exit criteria:** If wiki context does not produce visibly different/better decisions, investigate why before proceeding. Possible causes: page content too generic, prompt template needs tuning, agent not in situations where wiki helps.

---

### Phase 1: Foundation (2-3 days)
**Goal:** Working `pkg/wiki/` read package with in-memory page cache.

**Tasks:**
- [ ] Create `pkg/wiki/` package with `Wiki` interface
- [ ] Implement `WikiClient` with file-based storage and in-memory cache
- [ ] Implement `GetContext()`, `GetPage()`, `GetIndex()`, `QueryPages()`
- [ ] Implement role-based page index (`roleindex.go`)
- [ ] Create `wiki/` directory structure
- [ ] Hand-author all 6 domain stubs with real content from game guides
- [ ] Create `wiki/CLAUDE.md` with conventions
- [ ] Unit tests for all read operations
- [ ] Run `golangci-lint`, fix any findings

**Deliverable:** Can read and query wiki pages via Go API. Wiki contains useful seed content.

---

### Phase 2: Agent Integration (2 days)
**Goal:** Agents use wiki during decisions via existing enrichment channels.

**Tasks:**
- [ ] Add `WikiContext string` field to `PromptContext` in `pkg/tot/prompt_context.go`
- [ ] Add optional `wiki.Wiki` field to `BaseAgent` with setter
- [ ] Modify `buildKnowledgeContext()` to include wiki content when available
- [ ] Add optional `Wiki` field to `strategy.Config`
- [ ] Add optional `Wiki` to `ManagerConfig`, wire through to runners
- [ ] Update prompt templates to include `{{.WikiContext}}` section
- [ ] Add `wiki_enabled` and `wiki_context_limit` to personality YAML parsing
- [ ] Integration tests for wiki-enhanced decisions
- [ ] Run `golangci-lint`, fix any findings

**Deliverable:** Agents make decisions informed by wiki knowledge. No interface changes to `Agent.Decide()`.

---

### Phase 3: Ingestion Pipeline (2-3 days)
**Goal:** Automated processing of agent experiences into wiki updates.

**Tasks:**
- [ ] Add `GetExperiencesSince(ctx, time.Time)` to `knowledge.Base` interface
- [ ] Implement in `SQLiteKB` and `MemoryKB`
- [ ] Add `idx_experiences_time` index to experiences table
- [ ] Increase experience cap from 100 to 500 per agent
- [ ] Add `ingested_at` column to experiences table (new migration)
- [ ] Create `pkg/wikiingest/` package with `Ingestor` interface
- [ ] Implement LLM-based insight extraction with experience grouping
- [ ] Implement atomic file writes (temp + `os.Rename()`)
- [ ] Implement `UpdateIndex()` and `AddLogEntry()` with daily log rotation
- [ ] Create `cmd/wiki-ingest/` binary with `--dry-run`, `--interactive`, `--once` flags
- [ ] Seed wiki from game guides using interactive mode
- [ ] Unit tests for ingest operations
- [ ] Run `golangci-lint`, fix any findings

**Deliverable:** Wiki can be updated from agent experiences. Ingest service runs standalone.

---

### Phase 4: Experience Loop (1-2 days)
**Goal:** Scheduled ingestion processes agent experiences on a regular cadence.

**Tasks:**
- [ ] Set up scheduled ingest job (cron/systemd timer, default every 6 hours)
- [ ] Implement high-water mark tracking via `ingested_at` column
- [ ] Monitor and tune LLM extraction quality
- [ ] Validate cross-agent knowledge sharing (miner learns from explorer)
- [ ] Log rotation for `wiki/log/` (delete files older than 30 days)

**Deliverable:** Wiki grows from agent experiences on autopilot.

---

### Phase 5: Refinement (1 week)
**Goal:** Production-ready system with health monitoring.

**Tasks:**
- [ ] Implement `RunLintPass()` with full health checks
- [ ] Add contradiction detection to ingest LLM prompts
- [ ] Performance testing with 100 agents reading concurrently
- [ ] Tune wiki context selection per agent role
- [ ] Add metrics: pages served, ingest duration, LLM calls, lint issues
- [ ] Strategy bot integration: read wiki for tunable parameters

**Deliverable:** Stable, monitored wiki serving 100 agents.

---

### Optional Future Enhancements
- [ ] MCP server wrapper for wiki access from Claude tools
- [ ] Upgrade search to full-text index
- [ ] Web UI for browsing wiki (integrated into frontend)
- [ ] Agent contribution metrics (who contributed which insights)
- [ ] Automatic contradiction resolution via LLM

**Total Timeline:** ~10-14 days to production-ready (including Phase 0 validation gate)

## Success Criteria

### Technical
- [ ] Wiki read package passes all unit and integration tests
- [ ] 100 agents can query wiki concurrently without performance issues (in-memory map, no I/O)
- [ ] Scheduled ingest processes 6 hours of experiences in <5 minutes
- [ ] Ingest is resilient to LLM failures (skip and retry next cycle)
- [ ] `golangci-lint` passes cleanly on all new code

### Knowledge Quality
- [ ] Wiki contains actionable strategies for mining, trading, combat, exploration, crafting, factions
- [ ] Technique pages document multi-agent coordination as agents discover tactics
- [ ] Lint passes identify stale content, missing links, and unresolved contradictions
- [ ] Manual review confirms insights are accurate (not LLM hallucinations)

### Agent Performance
- [ ] Agents make fewer repeat mistakes (measurable via experience outcome tracking)
- [ ] Agents discover and adopt successful strategies faster
- [ ] Cross-agent knowledge sharing works (e.g., miner benefits from explorer's system discoveries)
- [ ] Decision quality improves with wiki context vs. without (A/B comparison)

## Open Questions

1. ~~**Experience retention policy:**~~ **RESOLVED:** Delete existing entries (unused/stale from early development). Going forward, simple cap at 500 per agent — oldest entries deleted on insert, no archive table. Revisit if preservation becomes valuable.
2. ~~**Contradiction resolution:**~~ **RESOLVED:** Two signals for automatic resolution in Phase 5:
   - **Recency** (primary): Newer data wins. The game is constantly updated, so older strategies may no longer be valid.
   - **Role relevance** (secondary): Weight agent contributions by role affinity to the page topic. A fighter's combat experiences carry more weight on `domains/combat.md` than a trader's. A craftsman's insights are more authoritative on `domains/crafting.md`. All agents are equally reliable — no per-agent trust scores needed.
   Manual review in Phase 1-4. Automatic resolution applies these rules starting Phase 5.
3. ~~**Wiki size limits:**~~ **RESOLVED:** Stay with in-memory approach. When page count reaches ~100, run benchmarks on memory usage and lookup latency to decide if indexed search is warranted. No preemptive optimization.
4. ~~**Ingest frequency tuning:**~~ **RESOLVED:** Fixed at every 6 hours. No auto-adaptation. Revisit if experience volume changes significantly.
5. ~~**Strategy bot parameter extraction:**~~ **RESOLVED:** Use structured `params` block in frontmatter:
   ```yaml
   params:
     min_richness: 50
     max_danger_level: 5
     preferred_resources: ["iron", "copper", "titanium"]
   ```
   Strategy bots parse `params` directly into typed values — no markdown parsing needed. If LLM ingest struggles to preserve the structured YAML cleanly, fall back to sidecar files (`mining.params.yaml` alongside `mining.md`) with a lint rule to detect drift between prose and params.
