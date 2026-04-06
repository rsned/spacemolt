# LLM Wiki Design Specification

**Date:** 2026-04-06
**Status:** Approved
**Author:** Generated from collaborative design session

## Overview

A persistent, LLM-maintained knowledge base that accumulates strategies, techniques, and insights about Spacemolt gameplay. The wiki serves as shared memory across ~100 agents, reducing prompt token bloat while compounding knowledge over time.

**Core Problem Solved:**
- Agent prompts grow in token count without increasing effectiveness
- No persistent memory for "what works" across agent sessions
- Agents cannot share learned strategies efficiently

**Solution:**
- Markdown wiki with domain pages (mining, trading, combat) and technique pages (can-mining, marauder tactics)
- Agents read from wiki during decisions, write to it via daily experience processing
- LLM maintains wiki structure, extracts insights from experiences, and keeps knowledge current

## Architecture

### Three-Layer System

```
┌─────────────────────────────────────────────────────────────┐
│                     Agent Decision Cycle                    │
│  ┌──────────────┐         ┌──────────────┐                 │
│  │ Game Client  │────────▶│ Wiki Package │◀──── wiki/      │
│  │              │         │  (pkg/wiki/)  │    (markdown)   │
│  └──────────────┘         └──────────────┘                 │
│         ▲                        ▲                          │
│         │                        │                          │
│    Current State          Wiki Context                      │
└─────────┴────────────────────────┴──────────────────────────┘
         │
         ▼
┌─────────────────────────────────────────────────────────────┐
│                 Daily Ingestion Service                     │
│  ┌──────────────┐         ┌──────────────┐                 │
│  │ SQLite KB    │────────▶│ Wiki Ingest  │─────────▶       │
│  │ (experiences)│         │   (LLM)       │   Wiki Updates  │
│  └──────────────┘         └──────────────┘                 │
└─────────────────────────────────────────────────────────────┘
```

### Components

1. **Wiki Package** (`pkg/wiki/`) - Clean interface for read/write operations
2. **Wiki Data** (`wiki/`) - Markdown files with hierarchical organization
3. **Ingestion Service** (`cmd/wiki-ingest/`) - Daily job processing experiences

## Components

### Wiki Package (`pkg/wiki/`)

**Main Interface:**
```go
type Wiki interface {
    // Read operations - thread-safe, called during agent decisions
    // QueryPages performs simple grep-based search over wiki content
    QueryPages(ctx context.Context, query string) ([]Page, error)
    GetPage(ctx context.Context, path string) (*Page, error)
    GetIndex(ctx context.Context) (*Index, error)

    // Write operations - called by daily ingest job
    UpdatePage(ctx context.Context, path string, content string) error
    CreatePage(ctx context.Context, path string, content string) error
    AddToLog(ctx context.Context, entry LogEntry) error
    UpdateIndex(ctx context.Context) error

    // Maintenance
    RunLintPass(ctx context.Context) ([]LintIssue, error)
}
```

**Implementation Files:**

- **client.go** - `WikiClient` implements `Wiki` interface, file-based markdown storage, mutex-protected writes
- **types.go** - `Page`, `Index`, `LogEntry`, `LintIssue` structs
- **search.go** - Simple grep-based search (upgradable to qmd/ripgrep)
- **ingest.go** - Pulls experiences from KB, uses LLM for insight extraction
- **lint.go** - Health checks for contradictions, stale info, orphan pages

**Data Types:**
```go
type Page struct {
    Path       string                 // e.g., "domains/mining.md"
    Content    string                 // Markdown content
    FrontMatter map[string]any        // Tags, date, sources
    CreatedAt  time.Time
    UpdatedAt  time.Time
}

type Index struct {
    ByCategory  map[string][]IndexEntry // domains, techniques, agents
    LastUpdated time.Time
}

type LogEntry struct {
    Type           string   // "ingest", "query", "lint", "update"
    Source         string
    Summary        string
    PagesAffected []string
    Timestamp      time.Time
}
```

### Ingestion Service (`cmd/wiki-ingest/`)

**Workflow:**
1. Fetch recent experiences: `kb.GetRecentExperiences("*", 24h)`
2. Group by domain/topic (mining, trading, combat)
3. For each group:
   - LLM analyzes experiences
   - Extracts insights ("mining in X with Y ship is 20% more profitable")
   - Updates relevant wiki pages
   - Adds contradictions to existing pages if found
4. Update `wiki/index.md` with new/changed pages
5. Append entry to `wiki/log.md`
6. Optional: Run lint pass (weekly rather than daily)

## Data Flow

### Agent Decision Cycle (Read Path)
```
1. Agent.Decide() called
2. EnrichedState enriched with wiki context
3. Wiki.QueryPages("mining strategies") called
4. Returns relevant Page structs (content + metadata)
5. Pages formatted into prompt as context
6. LLM makes decision with wiki-enhanced knowledge
```

### Daily Ingestion Cycle (Write Path)
```
1. cmd/wiki-ingest runs (scheduled job)
2. Fetch recent experiences: kb.GetRecentExperiences("*", 24h)
3. Group by domain/topic
4. LLM analyzes each group, extracts insights
5. Updates wiki pages with new findings
6. Update wiki/index.md
7. Append to wiki/log.md
8. Optional lint pass
```

### Query to Wiki Update (Optional)
```
1. User/agent asks: "Compare mining profits across ship classes"
2. Wiki.QueryPages() finds relevant pages
3. LLM synthesizes answer with citations
4. If valuable: Wiki.CreatePage("analysis/mining-ship-comparison.md", answer)
5. Index and log updated
```

## Directory Structure

```
wiki/
├── domains/
│   ├── mining.md              # Mining strategies, POI selection, ship configs
│   ├── trading.md             # Trade routes, market patterns, arbitrage
│   ├── combat.md              # Combat tactics, ship builds, threat assessment
│   ├── exploration.md         # System discovery, POI mapping, efficient travel
│   ├── crafting.md            # Recipes, production chains, profitability
│   └── factions.md            # Faction mechanics, standing strategies
│
├── techniques/
│   ├── can-mining.md          # Coordinated mining operations
│   ├── marauder-squad.md      # Ambush tactics, target isolation
│   ├── station-camping.md     # Gate/station hunting strategies
│   └── efficient-refueling.md # Fuel optimization techniques
│
├── agents/                    # Optional: agent-specific findings
│   ├── explorer-1-findings.md
│   └── miner-3-discoveries.md
│
├── analysis/                  # Generated from queries
│   ├── mining-ship-comparison.md
│   └── trade-route-profitability.md
│
├── schema/
│   ├── CLAUDE.md              # Wiki conventions for Claude
│   └── WIKI.md                # General schema documentation
│
├── index.md                   # Page catalog
└── log.md                     # Change history
```

**Initial seed:** Each `domains/*.md` gets a stub with section headers. `techniques/` starts empty (populated as agents discover tactics).

## Schema & Conventions

### Page FrontMatter Standard
```yaml
---
title: "Mining Strategies"
category: "domain"
tags: ["mining", "resources", "profitability"]
created: "2026-04-06"
updated: "2026-04-06"
sources: ["agent:explorer-1", "agent:miner-3", "guide:starter-mining"]
related: ["techniques/can-mining.md", "domains/trading.md"]
last_reviewed: "2026-04-06"
---
```

### Domain Page Template
```markdown
## Overview
[High-level summary]

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

## Agent Experiences
- [Agent]: [What they learned, outcome]
```

### Technique Page Template
```markdown
## Overview
[Brief description of the technique]

## Required Roles
- [Role 1]: [Responsibilities, skills needed]
- [Role 2]: [Responsibilities, skills needed]

## Coordination Protocol
1. [Step 1]
2. [Step 2]
...

## Efficiency Gains
[Why this is better than solo]

## Prerequisites
[Ships, skills, equipment needed]

## Known Variations
- [Variation]: [When to use it]

## Agent Experiences
[Who used it successfully, lessons learned]
```

### Wiki Conventions (`schema/CLAUDE.md`)

**When updating pages:**
- Always update `updated:` in frontmatter
- Add new `sources:` when incorporating experiences
- Update `related:` if adding cross-references
- Flag contradictions with `**CONTRADICTION:**` prefix
- Append to sections rather than rewriting (preserves evolution)

**When creating pages:**
- Use kebab-case filenames (`can-mining.md`, not `can_mining.md`)
- Always include frontmatter
- Link to related pages
- Add to `index.md` under appropriate category

**Lint rules:**
- Pages with no `related:` links (unless orphan pages are intentional)
- Pages with `last_reviewed:` > 30 days ago
- Contradictions not resolved
- Important concepts mentioned but not linked to their own page

## Agent Integration

### Enhanced Decision Flow

**Before:**
```go
func (r *Runner) decide() {
    es := r.getEnrichedState()
    decision, _ := r.agent.Decide(ctx, es)
    // execute decision
}
```

**After:**
```go
func (r *Runner) decide() {
    es := r.getEnrichedState()

    // NEW: Query wiki for relevant context
    wikiContext := r.getWikiContext(es)

    decision, _ := r.agent.Decide(ctx, es, wikiContext)
    // execute decision
}
```

### Wiki Context Selection

The `getWikiContext()` function:
1. Determines context from agent's current state (location, action, ship)
2. Queries wiki: `wiki.QueryPages("mining AND asteroid")`
3. Returns formatted summaries (not full pages to save tokens)
4. Caches results per tick

### Prompt Template Change

**Before (verbose):**
```
Current State: [500 tokens]
Recent Experiences: [300 tokens]
Game Knowledge: [200 tokens]
```

**After (wiki-enhanced):**
```
Current State: [500 tokens]
Wiki Context:
- Mining: Use basic scanner at asteroids >50% richness (ref: domains/mining.md)
- This POI: Iron deposit, 65% richness (ref: systems/achernar/asteroids.md)
Recent Experiences: [100 tokens - notable only]
```

### Agent Configuration

Add to agent personality YAML:
```yaml
wiki_enabled: true
wiki_context_limit: 3  # Maximum number of wiki pages to include in prompt
wiki_focus_domains:    # Optional preference
  - mining
  - trading
```

## Testing & Validation

### Unit Tests

**Package tests (`pkg/wiki/*.go`):**
- `TestQueryPages()` - Search returns relevant pages
- `TestGetPage()` - Page loading with frontmatter parsing
- `TestUpdatePage()` - Atomic writes, concurrency safety
- `TestUpdateIndex()` - Index reflects page changes
- `TestAddToLog()` - Log entries append correctly

**File-based tests:**
- Use temporary directory for test wiki
- Mock markdown files
- Verify file I/O, locking, error handling

### Integration Tests

**Agent decision enhancement:**
```go
func TestAgentDecisionWithWiki(t *testing.T) {
    // Setup: Create test wiki with mining strategy
    // Act: Run agent decision at asteroid
    // Assert: Agent uses wiki-recommended action
}
```

**Ingestion pipeline:**
```go
func TestIngestExperiences(t *testing.T) {
    // Setup: Mock KB with experiences
    // Act: Run ingest job
    // Assert: Wiki pages updated, index current
}
```

### Manual Validation

**Week 1:**
- Monitor wiki page quality after each ingest
- Check for hallucinations or incorrect conclusions
- Verify agent decisions improve with wiki context
- Measure prompt token reduction

**Week 2-4:**
- Run lint pass weekly, review issues
- Check for stale information
- Verify contradictions are flagged
- Validate cross-references work

**Metrics:**
- Prompt token count (before vs after wiki)
- Agent success rate (mining yield, trade profits)
- Wiki page quality (manual review)
- Lint issue count (should decrease)

## Implementation Phases

### Phase 1: Foundation (2-3 days)
**Goal:** Working wiki package with basic file operations

**Tasks:**
- [ ] Create `pkg/wiki/` package with interface
- [ ] Implement `WikiClient` with file-based storage
- [ ] Create `wiki/` directory structure with stubs
- [ ] Implement `QueryPages()`, `GetPage()`, `UpdatePage()`, `CreatePage()`
- [ ] Create `wiki/schema/CLAUDE.md` with conventions
- [ ] Unit tests for file operations

**Deliverable:** Can create/read/update markdown wiki pages via Go API

---

### Phase 2: Ingestion Pipeline (2-3 days)
**Goal:** Convert guides and KB data into wiki content

**Tasks:**
- [ ] Create `cmd/wiki-ingest/` binary
- [ ] Implement LLM-based insight extraction
- [ ] Ingest skill.md and starter guides
- [ ] Pull valuable insights from `/kb/kb/`
- [ ] Implement `UpdateIndex()` and `AddToLog()`
- [ ] Interactive mode for reviewing changes

**Deliverable:** Wiki seeded with initial game knowledge

---

### Phase 3: Agent Integration (2 days)
**Goal:** Agents use wiki during decisions

**Tasks:**
- [ ] Modify `pkg/agent/runner.go` to query wiki
- [ ] Implement `getWikiContext()` with smart page selection
- [ ] Update prompt templates to include wiki context
- [ ] Add wiki configuration to agent personalities
- [ ] Integration tests for wiki-enhanced decisions

**Deliverable:** Agents make decisions informed by wiki knowledge

---

### Phase 4: Experience Loop (1-2 days)
**Goal:** Daily job processes agent experiences

**Tasks:**
- [ ] Scheduled ingest job (cron/systemd)
- [ ] Batch experience processing by domain
- [ ] LLM extracts insights from recent agent actions
- [ ] Update wiki pages with new learnings
- [ ] Monitor and tune extraction quality

**Deliverable:** Wiki grows smarter from agent experiences daily

---

### Phase 5: Refinement (1 week)
**Goal:** Production-ready system

**Tasks:**
- [ ] Implement `RunLintPass()` with health checks
- [ ] Add caching for frequently-accessed pages
- [ ] Performance testing with 100 agents
- [ ] Tune wiki context selection per agent
- [ ] Document best practices in `schema/WIKI.md`

**Deliverable:** Stable, scalable wiki serving 100 agents

---

### Optional Future Enhancements
- [ ] MCP server wrapper for wiki access
- [ ] Upgrade search to qmd/ripgrep
- [ ] Web UI for browsing wiki (Obsidian Publish-style)
- [ ] Agent contribution metrics (who added what)

**Total Timeline:** ~10-14 days to production-ready

## Initial Seed Content

### Sources to Ingest

1. **Core Mechanics:**
   - `https://www.spacemolt.com/skill.md`

2. **Strategy Guides:**
   - `https://github.com/SpaceMolt/www/tree/main/public/guides`

3. **KB Insights:**
   - System POI patterns from `/kb/kb/systems/`
   - Resource distributions from `/kb/kb/resources/`
   - Market patterns from KB snapshots

### Ingestion Process

**Step 1:** Create manual markdown files in `wiki/raw/`
```bash
wiki/raw/skill.md
wiki/raw/starter-mining-guide.md
wiki/raw/starter-trading-guide.md
```

**Step 2:** Run ingest interactively to validate
```bash
go run cmd/wiki-ingest/main.go --source wiki/raw/skill.md --interactive
```

**Step 3:** Review changes, iterate on prompts and structure

**Step 4:** Enable automatic daily ingest after validation

## Evolution Path

The system evolves through three stages:

1. **File-based wrapper** (Phase 1-2) - Simple, manual, easy to debug
2. **Wiki package** (Phase 3-4) - Clean interface, agent integration
3. **MCP service** (Future) - Standalone service, accessible to multiple tools

Each stage preserves the markdown files in `wiki/` for version control and sharing.

## Success Criteria

**Technical:**
- [ ] Wiki package passes all unit and integration tests
- [ ] 100 agents can query wiki concurrently without performance degradation
- [ ] Prompt token count reduced by 30-50%
- [ ] Daily ingest processes 24h of experiences in <5 minutes

**Knowledge Quality:**
- [ ] Wiki contains useful strategies for mining, trading, combat
- [ ] Technique pages document multi-agent coordination (can-mining, marauder squad)
- [ ] Lint passes identify issues and contradictions accurately
- [ ] Manual review confirms insights are accurate (not hallucinations)

**Agent Performance:**
- [ ] Agents make fewer repeat mistakes
- [ ] Agents discover and adopt successful strategies faster
- [ ] Cross-agent knowledge sharing works (miner learns from explorer findings)

## Open Questions

1. **Experience retention:** How long to keep experiences in SQLite KB before archiving?
2. **Wiki size limits:** At what scale does index.md become unmanageable? (Plan: upgrade to search engine)
3. **Contradiction resolution:** Manual review or automatic merging? (Start manual, add automation later)
4. **Agent permissions:** Can all agents write to any page, or role-based access? (Start open, add restrictions if needed)
