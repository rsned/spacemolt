---
name: LLM agent rollout plan
description: All agents will eventually use LLM/ToT decisions — currently only miner-1, but rollout is planned
type: project
---

All ~100 agents are planned to use LLM-driven decisions (ToT) eventually. Currently only miner-1 has `decision_mode: "tot"` — the rest use strategy-based bots. Integration and testing has begun but full rollout is pending.

**Why:** The strategy bots are the baseline; LLM decisions are the goal for richer, adaptive gameplay.

**How to apply:** Don't treat the current 1-agent LLM state as permanent when designing systems. Wiki, prompts, and enrichment infrastructure should be built for the full fleet, not just miner-1.
