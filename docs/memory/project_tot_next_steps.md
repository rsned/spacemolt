---
name: Thought Engine next steps
description: Outstanding improvements for the ToT system after initial implementation — prompt tuning, async planning, UI polish
type: project
---

The Thought Engine is merged to main and functional. Outstanding work:

**Prompt Quality:**
- Query actions (get_status, get_cargo) still clutter options — consider removing them from the filter entirely for action-oriented roles, or deprioritizing in prompt instructions
- The miner sometimes picks suboptimal actions (checking market when should mine) — needs more role-specific prompt guidance
- Connected systems show "Unknown" security — wire KB queries for visited system data

**Async Planning / Idle Time:**
- When tick hasn't advanced, agent currently skips cycle entirely (saves LLM calls)
- Future: use idle time for "planning ahead" — run a lightweight forecast for the next likely situation
- The multi-step plan (3-4 queued actions) helps but could be deeper
- Pre-planning results could feed into the next ToT cycle as context

**Frontend Polish:**
- Tree nodes rebuild on every update (React Flow re-renders) — could be smoother with in-place updates
- Child plan nodes are small and could show more detail
- Consider showing the action queue status (executing step 2/5 of plan)

**Performance:**
- dolphin3 (~7s/call) works well, qwen3:14b too slow (~30s/call)
- Test qwen3.5:9b and other models on different GPUs (2060, 3080, 4080)
- Consider caching assess results if game state hasn't changed

**How to apply:** These are incremental improvements. Pick one area at a time. The code is in pkg/tot/ (backend) and frontend/src/components/ThoughtTreeView.tsx + useThoughtEngine.ts (frontend).
