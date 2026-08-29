---
name: Thought Engine prompt improvements
description: Next iteration of ToT prompts — short-term memory, role-aware context, strategic identity, knowledge base integration
type: project
---

The current ToT assess/evaluate prompts are too generic and lack context. Three improvement areas identified:

**1. Short-term memory (action history)**
- Include last 3-5 actions and their outcomes in the prompt
- Prevents loops (get_cargo, get_cargo, get_cargo)
- Shows what worked/failed recently so LLM can adapt
- "I just mined. Cargo has space. Mine again." vs "I just checked status 3 times."

**2. Role-aware context filtering**
- NEARBY LOCATIONS shows everything — planets, stations, gas clouds
- Miners only need: resource locations (asteroid belts) + where to return (station)
- Explorers need: all POIs + which are unexplored
- Traders need: stations + market info
- CONNECTED SYSTEMS should show security level (Lawless/Medium/High Security)
- Include knowledge about neighboring systems (N+1 depth) — "System X has 40% higher Rare Ores"
- Draw from the existing knowledge base (SQLiteKB) for market/resource intelligence

**3. Strategic identity from personality**
- Current prompt says "You are Preston 'Pickaxe' Porter, a Miner" — too bland
- Should include personality biography excerpt, motivations, and a long-range goal
- Risk-taking miner chasing rare ores vs cautious miner grinding baseline ores
- The personality JSON already has biography, motivations, traits — wire them into the prompt
- Add a strategic goal context: "Your current goal is to fill cargo with ore and sell at Grand Exchange"

**Why:** The LLM makes poor choices (checking market when it should mine, scanning when it should undock) because it lacks context about what just happened, what matters for this role, and what the long-term plan is.

**How to apply:** Redesign BuildAssessPrompt and BuildEvaluatePrompt in pkg/tot/prompts.go. The existing prompts/context.go (TemplateContext) already has History, Knowledge, Goal, and Personality fields — they're just not used by the ToT prompts yet.
