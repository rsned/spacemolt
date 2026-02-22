# auto-craftsman

Autonomous crafting agent for Spacemolt.

## Overview

The `auto-craftsman` agent automatically crafts items from available resources in station storage and cargo. It continuously:

1. Withdraws ores from station storage (iron_ore, copper_ore, aluminum_ore)
2. Crafts items based on available resources and skill levels
3. Deposits crafted items back to storage (or sells them for credits)
4. Refuels and repairs as needed

## Usage

```bash
auto-craftsman <agent-id> [strategy]
```

### Arguments

- `agent-id`: Agent identifier (e.g., craftsman-1, craftsman-2)
- `strategy`: Crafting strategy (optional, default: `craft-deposit`)

### Strategies

- `craft-deposit`: Craft items from resources, then deposit to storage (default)
- `craft-sell`: Craft items from resources, then sell for credits

### Examples

```bash
# Craft then deposit (default)
auto-craftsman craftsman-1

# Craft then deposit (explicit)
auto-craftsman craftsman-1 craft-deposit

# Craft then sell
auto-craftsman craftsman-1 craft-sell
```

## Initial Recipes

With no skills, the agent can craft:

- **basic_smelt_iron**: Converts iron_ore → iron_ingot (10 ore per batch)
- **basic_copper_processing**: Converts copper_ore → copper_plate (10 ore per batch)

## Skill Progression

As your agent gains skills, more recipes become available:

### Level 1 Skills

**Refining Level 1:**
- `refine_copper_wire`: Converts copper_plate → copper_wire
- `smelt_aluminum_sheet`: Converts aluminum_ore → aluminum_sheet

### Higher Skills

More advanced recipes unlock when you reach:
- Crafting > Level 5
- Refining > Level 5
- Crafting Advanced > Level 1

These advanced recipes will be automatically selected based on available resources and skills.

## Storage Integration

The agent automatically:
- Withdraws ores from station storage when cargo space is available
- Deposits crafted items back to storage (for craft-deposit strategy)
- Handles multiple ore types efficiently

## Captain's Log

The agent updates its captain's log after each crafting run with:
- Number of runs completed
- Items crafted in current run
- Total items crafted
- Current credits and ship status

## Future Enhancements

Planned features:
- Integration with MCP crafting server for advanced recipe selection
- Support for more complex crafting chains
- Automatic resource procurement from other agents
