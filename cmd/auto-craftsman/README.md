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

## Profit-Based Strategy (craft-profit)

The `craft-profit` strategy uses market data to determine which items to craft:

### Features
- **Automatic Market Analysis**: Fetches current market listings from the station
- **Profit Calculation**: For each recipe:
  - Calculates material cost (buying from lowest sell listings)
  - Calculates revenue (selling to highest buy listings)
  - Computes profit margin percentage
- **Smart Filtering**: Only crafts recipes with:
  - Positive profit (> 0 credits)
  - Profit margin > 5%
  - Required skill levels met
- **Optimization**: Sorts recipes by total profit, then by margin percentage
- **Caching**: Market data cached for 1 hour to reduce API calls

### Example Output

```
[craftsman-1] 📊 Initialized knowledge base for market analysis
[craftsman-1]    Market data will be cached for 1 hour
[craftsman-1]    Market analysis will be cached for 2 hours
[craftsman-1] 💰 Found 3 profitable recipes:
[craftsman-1]    1. craft_steel_plate: profit 150 (margin 75.0%, cost 50, sell 200)
[craftsman-1]    2. refine_copper_wire: profit 75 (margin 50.0%, cost 50, sell 125)
[craftsman-1]    3. basic_smelt_iron: profit 40 (margin 40.0%, cost 60, sell 100)
```

### Requirements
- Agent must be docked at a station with a market
- Market must have active buy/sell listings
- Knowledge base file (`spacemolt-knowledge.db`) will be created automatically

## Future Enhancements

Planned features:
- Integration with MCP crafting server for advanced recipe selection
- Support for more complex crafting chains
- Automatic resource procurement from other agents

