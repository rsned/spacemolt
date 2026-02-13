# SpaceMolt Agent Playbooks

This directory contains comprehensive LLM prompt playbooks for each career-focused agent in SpaceMolt. These playbooks provide strategic guidance for autonomous gameplay across different playstyles.

## Overview

Each playbook is a complete strategy guide that an LLM can use to understand:
- Core gameplay loops and mechanics
- Upgrade paths and priorities
- Advanced tactics and optimization strategies
- Profitability analysis and decision matrices
- Safety protocols and emergency procedures
- Empire synergies and career-specific considerations

## Available Playbooks

| Career | File | Playstyle | Risk Level | Income Potential |
|--------|------|-----------|-------------|------------------|
| Miner | [miner.md](miner.md) | Resource extraction | Low | Steady, reliable |
| Explorer | [explorer.md](explorer.md) | Galaxy mapping | Medium | Variable, knowledge-based |
| Fighter | [fighter.md](fighter.md) | Combat specialist | High | High-risk, high-reward |
| Trader | [trader.md](trader.md) | Arbitrage | Low-Medium | Consistent, scalable |
| Pirate | [pirate.md](pirate.md) | Predation | Very High | High-risk, high-reward |
| Salvager | [salvager.md](salvager.md) | Scavenging | Low-Medium | Consistent, opportunistic |
| Craftsman | [craftsman.md](craftsman.md) | Manufacturing | Low | Scalable, skill-based |

## Playbook Structure

Each playbook follows a consistent structure:

1. **Overview** - Career summary and core philosophy
2. **Core Strategy Loop** - Step-by-step gameplay instructions
3. **Advanced Tactics** - Optimization strategies and expert techniques
4. **Profitability Analysis** - Economic decision frameworks
5. **Progression Goals** - Short/medium/long-term targets
6. **Common Pitfalls** - Mistakes to avoid
7. **Safety Protocols** - Emergency procedures
8. **Empire Synergies** - Best empire choices
9. **Commands Reference** - Relevant API commands
10. **Metrics** - Performance benchmarks

## Usage for LLM Agents

These playbooks can be used as system prompts or context for LLM-controlled agents:

```python
# Example: Load playbook content
with open('playbook/miner.md', 'r') as f:
    miner_playbook = f.read()

# Include in agent system prompt
system_prompt = f"""
You are a SpaceMolt autonomous agent following the Miner career path.

{miner_playbook}

Current game state:
{game_state}

Make decisions based on the playbook strategies and your current situation.
"""
```

## Integration with Agent Code

The playbooks are designed to align with the agent implementations in `cmd/auto-*`:

- **auto-miner** → miner.md
- **auto-explorer** → explorer.md
- **auto-fighter** → fighter.md
- **auto-trader** → trader.md
- **auto-pirate** → pirate.md
- **auto-salvager** → salvager.md
- **auto-craftsman** → craftsman.md

## Key Concepts Across All Careers

### Credit Thresholds
All careers use tiered credit thresholds for progression:
- Tier 1: 300-1000 credits (basic upgrades)
- Tier 2: 1000-5000 credits (intermediate upgrades)
- Tier 3: 5000-10000 credits (major upgrades)
- Tier 4: 10000+ credits (advanced equipment)

### Reserve Funds
Always maintain reserve credits (never spend below):
- Miners: 50 credits
- Explorers: 100 credits
- Fighters: 100 credits
- Traders: 1000+ credits
- Pirates: 500+ credits
- Salvagers: 200 credits
- Craftsmen: 30% of total capital

### Safety Protocols
Every career includes emergency procedures for:
- Low fuel situations
- Hull damage
- Combat encounters
- Capital depletion
- Getting stranded

### Empire Selection
Each playbook includes empire synergy analysis:
- **Solarian**: Mining/trade bonuses (miners, traders, craftsmen)
- **Nebula**: Exploration/scanning bonuses (explorers, salvagers)
- **Crimson**: Combat bonuses (fighters, pirates)
- **Voidborn**: Stealth/shield bonuses (explorers, pirates, salvagers)
- **Outer Rim**: Cargo/crafting bonuses (traders, craftsmen, salvagers)

## Contributing

When adding new careers or updating strategies:

1. Maintain the consistent structure
2. Include concrete credit thresholds and progression paths
3. Provide specific command examples
4. Document common mistakes explicitly
5. Include safety/emergency procedures
6. Align with actual agent code in `cmd/auto-*`

## Performance Metrics

Each playbook defines "Good" and "Excellent" performance benchmarks:
- Credits earned per run/hour
- Survivability percentages
- Efficiency measures
- Progression milestones

These metrics can be used to evaluate agent performance and identify areas for improvement.

## Future Enhancements

Potential additions to the playbooks:
- [ ] Faction-specific strategies
- [ ] Multi-career hybrid approaches
- [ ] Player interaction protocols
- [ ] Market manipulation tactics
- [ ] PvP combat matrices
- [ ] Station/facility building guides
- [ ] Territory control strategies
