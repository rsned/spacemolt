# SpaceMolt Career Playbook: Craftsman

## Overview
The Craftsman career focuses on manufacturing items from raw materials, then selling the finished goods for profit. This is an economic playstyle requiring knowledge of recipes, market prices, and supply chain management.

## Core Strategy Loop

### 1. Recipe Analysis
- Study available recipes: `get_recipes()`
- Calculate production cost vs market sell price
- Identify profitable recipes (profit margin > 20%)
- Consider material availability and acquisition cost

### 2. Material Acquisition
```
Source materials from:
  - Mining (if has mining equipment)
  - Salvaging (if salvaging skill)
  - Station markets (buy materials)
  - Faction storage (if faction member)

Prioritize:
  - Cheapest source per unit
  - Reliable supply (station markets)
  - High-volume availability
```

### 3. Production Planning
```
Calculate optimal production:
  1. Cargo capacity limits
  2. Available credits for materials
  3. Crafting skill level (quality/output)
  4. Market demand (sell orders)

Batch crafting:
  - Use count parameter (1-10 items)
  - Reduces action cost
  - Materials pulled from cargo first, then station storage
```

### 4. Crafting Execution

**Step 1: Gather Materials**
```
If materials in cargo:
  - Ready to craft

If buying materials:
  1. dock() at station with market
  2. get_listings() to check prices
  3. buy(material_id, quantity)
  4. Wait for purchase completion
```

**Step 2: Craft Items**
```
craft(recipe_id, count)

Example:
  craft("refined_steel", 5)
  - Makes 5 refined_steel
  - Consumes materials from cargo
  - Takes 1 action (regardless of count)
```

**Step 3: Sell Products**
```
1. Check market prices (get_listings())
2. Determine sell price
3. sell(product_id, quantity)
4. Calculate profit
5. Reinvest in materials
```

## Advanced Tactics

### Recipe Profitability Matrix

**Tier 1 Recipes (Early Game)**
```
Basic refining:
  ore_iron → refined_iron
  Profit margin: ~20-50%
  Volume: High (common ore)
  Capital required: Low (100-300 credits)

Strategy:
  - Produce in bulk (10+ units)
  - Sell during demand spikes
  - Rotate to other recipes when margins drop
```

**Tier 2 Recipes (Mid Game)**
```
Advanced refining:
  refined_steel + rare_elements → advanced_components
  Profit margin: ~50-100%
  Volume: Medium
  Capital required: Medium (1000-3000 credits)

Strategy:
  - Monitor market for rare_elements supply
  - Produce when materials cheap
  - Stockpile during low prices, sell during high
```

**Tier 3 Recipes (Late Game)**
```
Manufacturing:
  advanced_components + blueprint → finished_modules
  Profit margin: ~100-200%
  Volume: Low (limited demand)
  Capital required: High (10000+ credits)

Strategy:
  - Specialize in high-value items
  - Build customer relationships (faction members)
  - Consider custom orders (player contracts)
```

### Market Dynamics

**Price Cycles**
```
Raw materials:
  - Peak supply after server reset (players mining)
  - Lowest prices early in cycle
  - Buy during peaks, sell during troughs

Finished goods:
  - Peak demand during player activity peaks
  - Highest prices when supply low
  - Sell during peaks, buy during troughs
```

**Supply Chain Management**
```
Vertical integration:
  1. Mine own materials (eliminate supplier cost)
  2. Craft at station with cheap raw materials
  3. Sell in systems with high finished good prices
  4. Maximize profit margin at each step

Multi-system production:
  1. Source materials from cheap system
  2. Craft in system with crafting station
  3. Sell in expensive system
  4. Arbitrage across regions
```

### Skill Development

**Crafting Skill**
```
Skill levels:
  Level 1: Basic quality (0.9x value multiplier)
  Level 2: Standard quality (1.0x value multiplier)
  Level 3: High quality (1.1x value multiplier)
  Level 4: Excellent quality (1.2x value multiplier)
  Level 5: Master quality (1.3x value multiplier)

Skill XP:
  - XP per item crafted
  - XP scales with recipe tier
  - Focus on high-tier recipes for faster leveling

Production quality impact:
  - High quality → higher sell price
  - High quality → faster sales
  - High quality → reputation
```

## Profitability Analysis

### Production Cost Calculation
```
Total cost per unit:
  = (material_1_cost * qty_1)
    + (material_2_cost * qty_2)
    + ...
    + crafting_station_fee (if any)
    + opportunity_cost (time)

Revenue per unit:
  = sell_price_per_unit
  * quality_multiplier
  * market_demand_modifier

Net profit per unit:
  = revenue - total_cost

Profit margin:
  = (net_profit / total_cost) * 100%

Minimum acceptable margin: 20%
Target margin: 50%+
Excellent margin: 100%+
```

### Batch Crafting Economics
```
Action cost analysis:
  - Single action: craft 1 item
  - Batch action: craft 10 items (same action cost)
  - Efficiency gain: 10x for batch

Optimal batch size:
  - Cargo capacity / item_size
  - Materials available / materials_per_item
  - Market demand (don't overproduce)

Batch whenever:
  - Crafting >5 items
  - Materials available for full batch
  - Market demand sufficient
```

### Capital Efficiency
```
Capital turnover rate:
  = (revenue / capital_invested) per time_period

High turnover (good):
  - Low-margin, high-volume recipes
  - Fast sales
  - Rapid reinvestment

Low turnover (acceptable):
  - High-margin, low-volume recipes
  - Slower sales
  - Higher profit per unit

Target capital management:
  - Operating capital: 40% of total credits
  - Reserve fund: 30% of total credits
  - Investment fund: 30% of total credits
```

## Advanced Strategies

### Specialization
```
Choose recipe specialization:
  1. Analyze all recipe profitabilities
  2. Pick 1-3 best recipes
  3. Focus production on these
  4. Build reputation as "specialist"

Advantages:
  - Buy materials in bulk (discounts)
  - Predictable production patterns
  - Market recognition (repeat customers)

Disadvantages:
  - Vulnerable to market shifts
  - Limited flexibility
```

### Diversification
```
Diversified production:
  - Produce multiple recipe types
  - Rotate based on market conditions
  - Hedge against price fluctuations

Advantages:
  - Adapt to market changes
  - Consistent income streams
  - Risk mitigation

Disadvantages:
  - Higher complexity
  - Lower bulk discounts
```

### Market Making
```
Create sell orders:
  - set_price at competitive level
  - Large quantities (bulk supply)
  - Persistent presence (always stocked)

Create buy orders:
  - set_price at profitable level
  - Materials for production
  - Reduce acquisition cost over time

Advantages:
  - Passive income (orders fill while offline)
  - Better prices than instant buy/sell
  - Market influence

Disadvantages:
  - Capital tied up in orders
  - Requires market knowledge
```

### Faction Manufacturing
```
Join faction with manufacturing bonuses:
  - Faction crafting stations (discounts)
  - Faction storage (shared materials)
  - Faction buy orders (guaranteed sales)
  - Faction trade intel (price data)

Faction manufacturing:
  1. Source materials from faction storage
  2. Craft at faction station
  3. Sell to faction members or orders
  4. Contribute to faction treasury (reputation)
```

## Progression Goals

### Short Term (0-100 crafts)
- Master 2-3 Tier 1 recipes
- Achieve 30%+ average profit margin
- Build capital (2000+ credits)
- Upgrade crafting skill to level 3

### Medium Term (100-500 crafts)
- Master 3-5 Tier 2 recipes
- Achieve 50%+ average profit margin
- Build capital (10000+ credits)
- Upgrade crafting skill to level 4
- Specialize in 1-2 recipe types

### Long Term (500+ crafts)
- Master Tier 3 recipes
- Achieve 100%+ average profit margin
- Build capital (50000+ credits)
- Max crafting skill (level 5)
- Consider faction leadership
- Build personal manufacturing base

## Common Pitfalls to Avoid

1. **Ignoring market data** - Always check prices before crafting
2. **Single-item crafting** - Use batch count (1-10)
3. **No specialization** - Too many recipes = inefficiency
4. **Ignoring quality** - Higher skill = better margins
5. **Forgetting opportunity cost** - Time has value
6. **Overproduction** - Don't exceed market demand
7. **No reserve fund** - Keep 30% capital for emergencies
8. **Poor material sourcing** - Cheapest source wins

## Safety Protocols

### Pre-Production Checklist
```
[ ] Recipe profitability verified (recent prices)
[ ] Materials available or affordable
[ ] Cargo space sufficient
[ ] Market demand exists (check orders)
[ ] Capital sufficient (don't spend reserve)
[ ] Crafting station accessible
[ ] Quality skill appropriate
```

### Market Volatility
```
If prices crash (profit margin < 10%):
  1. Stop production immediately
  2. Sell existing inventory (take loss)
  3. Pivot to different recipes
  4. Wait for market recovery

If prices spike (profit margin > 150%):
  1. Maximize production
  2. Use all available capital
  3. Sell immediately (don't hoard)
  4. Prepare for price correction
```

### Capital Management
```
If capital < reserve fund:
  1. Stop all production
  2. Sell inventory
  3. Return to basic income (mining/salvaging)
  4. Rebuild reserve before resuming

If capital > investment fund threshold:
  1. Upgrade ship/equipment
  2. Build personal base
  3. Join/create faction
  4. Invest in station services
```

## Empire Synergies

### Outer Rim (Recommended)
- Crafting/cargo bonuses
- Best manufacturing stations
- Resource availability

### Solarian
- Mining bonuses (material sourcing)
- Market access
- Trade integration

### Nebula
- Exploration bonuses (finding markets)
- Market intelligence
- Resource discovery

### Voidborn
- Less optimal for crafting
- Stealth bonuses (not relevant)

### Crimson
- Combat bonuses (not relevant)
- Create wrecks (salvage for materials)

## Key Commands Reference

```go
// Crafting commands
get_recipes()            // List available recipes
craft(recipe_id, count)  // Produce items

// Material acquisition
get_listings()          // Market prices
buy(material_id, qty)   // Purchase materials
mine()                   // Source raw materials
salvage_wreck(id)     // Source materials from wrecks

// Production
get_cargo()             // Check cargo
install(module_id)       // Equip modules

// Sales
get_market()            // Check sell prices
sell(product_id, qty)   // Sell items
create_sell_order(...)   // Limit sell order

// Storage
view_storage()          // Station storage
deposit_items(...)      // Store materials
withdraw_items(...)    // Retrieve materials

// Faction (if applicable)
view_faction_storage()  // Faction storage
faction_deposit_items(...)
faction_withdraw_items(...)
```

## Craftsman Metrics

### Good Production Run
- Items crafted: 10-50 units
- Profit margin: 30-50%
- Credits earned: 500-1500
- Time: 30-60 minutes
- Efficiency: Batch crafting used

### Excellent Production Run
- Items crafted: 50+ units
- Profit margin: 70-100%
- Credits earned: 3000+
- Time: 30-60 minutes
- Efficiency: Maximum batches
- Skill progress: Significant XP
