# Design Review Synthesis: LLM Wiki v1 -> v2

**Date:** 2026-04-06
**Original:** `docs/superpowers/specs/2026-04-06-llm-wiki-design.md`
**Updated:** `docs/superpowers/specs/2026-04-06-llm-wiki-design.v2.md`
**Reviewers:** Opus (deep architecture), Gemini (practical implementation), GLM (simplification)

## 1. Strongly Agree (Must Fix)

### All three reviewers caught:

**`GetRecentExperiences("*", 24h)` does not exist** (Opus, Gemini, GLM)
The plan references a non-existent KB API. The actual signature is `GetRecentExperiences(ctx, agentID, limit)`. A new `GetExperiencesSince(ctx, time.Time)` method is needed. Added to v2 with SQL index requirement.

**`Agent.Decide()` signature change is breaking** (Opus, Gemini, GLM)
Adding `wikiContext` as a third parameter breaks every `Agent` implementation. All three reviewers independently concluded wiki context should flow through existing enrichment channels (`EnrichedState`, `PromptContext`, `buildKnowledgeContext()`). Fixed in v2 — no interface changes.

**"30-50% prompt token reduction" is contradicted by the design** (Opus, Gemini, GLM)
Wiki context *adds* tokens. The goal is better decisions-per-token, not fewer tokens. Corrected in v2 with explicit "Non-Goal" section.

**`wiki/schema/CLAUDE.md` should be `wiki/CLAUDE.md`** (Opus, Gemini)
CLAUDE.md auto-discovery requires root placement. Moved in v2.

**Experience cap (100/agent) blocks the ingestion model** (Opus, GLM)
At one experience per tick (11s), 100 entries covers ~18 minutes. Daily ingest finds nothing. Cap increased to 500, ingest frequency changed to every 6 hours, high-water mark tracking added.

### Critical design gap:

**Code examples don't match actual codebase** (Opus, GLM)
The plan shows `r.getEnrichedState()` and `r.decide()` — neither exists. Decisions happen in `executeCycle()`. v2 references actual code locations.

## 2. Agree With Caveats

**Split pkg/wiki into read and write packages** (Gemini)
Agreed — `pkg/wiki/` (read, stdlib-only) and `pkg/wikiingest/` (write, depends on KB+LLM). Prevents dependency bloat in agent binaries. Adopted in v2.

**Remove `agents/` and `analysis/` directories** (GLM, Gemini)
Agreed for MVP. Per-agent pages duplicate KB. Attribution via `sources:` frontmatter. Can add back later if needed.

**Add Phase 0 quick validation** (Gemini, GLM)
Agreed — 2 hours to validate hypothesis before 14-day investment. Added as Phase 0 with go/no-go gate. Caveat: even if Phase 0 shows modest results, don't abandon — page content quality matters enormously.

**Seed content before agent integration** (Gemini)
Agreed — integrating against empty wiki is pointless. Phase ordering changed: seed content in Phase 1, agent integration in Phase 2 (swapped from original).

**Strategy bots need wiki access too** (Opus)
Agreed — only miner-1 uses ToT/LLM. Added optional `Wiki` field to `strategy.Config`. But this is Phase 3+ — strategy bots use wiki differently than LLM agents.

**Simplify frontmatter** (Opus)
Agreed — Phase 1 needs only `title`, `category`, `updated`, `related`. Other fields deferred.

## 3. Shouldn't Do

**Collapse Wiki interface to 1 method** (GLM)
Too aggressive. A single `GetContext(role, limit)` is the primary hot path, but `GetPage()` and `QueryPages()` are needed for MCP tools, debugging, and the ingest pipeline's page reads. Compromise: 4 read methods (not 8 total), write methods in separate package.

**Validate the repeat-mistake hypothesis before building** (GLM)
Directionally correct but over-cautious. The value of curated strategies is clear even without formal measurement. Phase 0 provides the validation gate without requiring a formal study.

**Drop `wiki_focus_domains` entirely** (GLM)
Agreed to drop from per-agent YAML config. But the role-to-page mapping needs to exist somewhere — moved to `roleindex.go` as code rather than per-agent config.

## 4. Overlap Analysis

### What multiple reviewers caught:
| Issue | Opus | Gemini | GLM |
|-------|------|--------|-----|
| KB API mismatch | X | X | X |
| Breaking Decide() signature | X | X | X |
| Token reduction claim wrong | X | X | X |
| CLAUDE.md placement | X | X | |
| Experience cap conflict | X | | X |
| Seed content before integration | | X | X |
| Remove agents/ directory | | X | X |

### Unique insights by reviewer:

**Opus** (most thorough codebase exploration):
- Strategy agents are 99% of the fleet — wiki must serve them too
- Log file grows unbounded — daily rotation needed
- Write access model needs explicit definition (read-only agents, single writer)
- Experience high-water mark tracking needed

**Gemini** (best practical implementation focus):
- Package split for dependency isolation (pkg/wiki vs pkg/wikiingest)
- Caching strategy is first-class, not afterthought
- Phase 0 validation concept
- Contradiction handling promoted from "open question" to design requirement

**GLM** (most radical simplification perspective):
- Role-keyed static lookup replaces grep search
- In-memory map at startup eliminates caching concerns entirely
- Existing personality biographies already contain strategy text
- PromptContext struct is the exact right integration point
- Experience table is barely used today — only for system filtering

### Which reviewer had the most valuable insights?
**Opus** had the deepest codebase understanding and caught the most implementation-blocking issues (experience cap, write access model, strategy agent exclusion). **GLM** had the best simplification ideas (in-memory map, role-keyed lookup). **Gemini** bridged the gap with practical concerns (package split, Phase 0, contradiction handling).

## 5. Priority Order

1. **Phase 0 validation** — 2 hours, gates everything else
2. **Fix KB API** — `GetExperiencesSince()`, increase cap, add index
3. **Build read package** — `pkg/wiki/` with in-memory cache, role index
4. **Hand-author seed content** — 6 domain pages with real strategies
5. **Agent integration** — via existing `PromptContext`/`buildKnowledgeContext()`
6. **Ingest pipeline** — `pkg/wikiingest/` + `cmd/wiki-ingest/`
7. **Scheduling & experience loop** — cron, high-water mark
8. **Lint pass & refinement** — health checks, metrics

## Major Changes Applied (v1 -> v2)

| # | Change | Source |
|---|--------|--------|
| 1 | Added Non-Goal section (wiki adds tokens, goal is better decisions) | All 3 |
| 2 | Split into `pkg/wiki/` (read) + `pkg/wikiingest/` (write) | Gemini |
| 3 | Primary read path: `GetContext(role, poiType, limit)` with in-memory map | GLM, Opus |
| 4 | No `Agent.Decide()` signature changes — flows through PromptContext/buildKnowledgeContext | All 3 |
| 5 | Added `GetExperiencesSince()` KB method requirement | Opus, Gemini |
| 6 | Experience cap 100->500, ingest every 6h not daily | Opus |
| 7 | Added Phase 0 quick validation gate | Gemini, GLM |
| 8 | Reordered phases: seed content (Phase 1) before agent integration (Phase 2) | Gemini |
| 9 | Removed `agents/`, `analysis/`, `schema/` directories | Gemini, GLM |
| 10 | Moved CLAUDE.md to wiki root | Opus, Gemini |
| 11 | Simplified frontmatter to 4 fields | Opus |
| 12 | Daily log rotation instead of unbounded log.md | Opus |
| 13 | Explicit concurrency model (in-memory + RWMutex + atomic writes) | Opus, GLM |
| 14 | Explicit error handling for ingest pipeline | Opus |
| 15 | Strategy bot integration path via strategy.Config | Opus |
| 16 | Removed `wiki_focus_domains` from agent config | GLM |
| 17 | Added experience grouping table for domain classification | Synthesis |
| 18 | Added LLM prompt template for insight extraction | Synthesis |
| 19 | Corrected success criteria to be measurable per-phase | GLM |
| 20 | Runner/Manager/BaseAgent integration points specified | Opus |

## Unresolved Questions (Require Human Input)

1. **Experience retention policy:** 500 cap set, but should old experiences archive to a separate table or just be deleted?
2. **Contradiction resolution criteria:** Manual for Phase 1-4. What rules for automatic resolution in Phase 5? (recency wins? majority of sources? source reliability weighting?)
3. **Wiki size limits:** In-memory works for ~100 pages. When to switch to indexed search?
4. **Ingest frequency adaptation:** Should frequency auto-adjust based on experience volume?
5. **Strategy bot parameter parsing:** How should non-LLM bots extract numeric thresholds from wiki markdown? Structured frontmatter vs. inline markup vs. explicit key-value sections?

## Stats

- **Changes incorporated from Opus:** 10
- **Changes incorporated from Gemini:** 8
- **Changes incorporated from GLM:** 7
- **Changes rejected:** 2 (collapse to 1-method interface, formal hypothesis testing before building)
- **Net document size:** v1 ~485 lines -> v2 ~747 lines (more detail where it matters, less where it was verbose)
