# SpaceMolt Career Playbook: Trader

## Overview
The Trader career focuses on identifying profitable trade routes, buying commodities at low prices, and selling them at high prices across different systems. This is a strategic, knowledge-intensive playstyle with low combat requirements.

## Core Strategy Loop

### 1. Market Data Collection
- Visit stations and collect market listings
- Record buy/sell prices for all commodities
- Note price modifiers per station/system
- Track daily price fluctuations

### 2. Trade Route Analysis
```
For each commodity:
  buy_price = lowest buy_price across accessible stations
  sell_price = highest sell_price across accessible stations

  profit_margin = sell_price - buy_price
  profit_per_cargo = profit_margin / commodity_size

  routes_sorted_by = profit_per_cargo DESC
```

### 3. Route Planning
```
For best trade route:
  origin_station = station with lowest buy_price
  destination_station = station with highest sell_price

  route_distance = calculate_jumps_between(origin, destination)
  fuel_cost = route_distance * fuel_per_jump

  total_cost = buy_price + fuel_cost + time_cost
  net_profit = sell_price - total_cost

If net_profit > minimum_profit_threshold:
  - Execute trade run
```

### 4. Trade Run Execution

**Step 1: Buy Commodities**
```
At origin_station:
  - Check cargo capacity
  - Calculate affordable quantity
  - buy(commodity_id, quantity)
  - Wait for transaction to complete
```

**Step 2: Travel to Destination**
```
- Plan route (may require multiple jumps)
- For each system in route:
    - travel() to jump_gate
    - jump() to next system
    - Wait 25 seconds per jump
```

**Step 3: Sell Commodities**
```
At destination_station:
  - dock()
  - sell(commodity_id, quantity)
  - Log profit earned
```

**Step 4: Return to Origin**
```
- Either return to origin (repeat loop)
- Or find new profitable route from current location
```

## Advanced Trading Strategy

### Commodity Categories

**High-Value, Low-Volume**
```
Examples: advanced_components, rare_elements
Characteristics:
  - High profit per unit
  - Low cargo space usage
  - Rare availability
  - Best for: small cargo ships, long-distance trading
```

**Medium-Value, Medium-Volume**
```
Examples: fuel_cells, refined_materials
Characteristics:
  - Moderate profit per unit
  - Moderate cargo space usage
  - Common availability
  - Best for: general trading
```

**Low-Value, High-Volume**
```
Examples: ore_iron, ore_copper
Characteristics:
  - Low profit per unit
  - High cargo space usage
  - Very common availability
  - Best for: large cargo ships, short-distance trading
```

### Market Dynamics

**Price Modifiers**
```
Station-specific factors:
  - Supply abundance (-10% to -30%)
  - Demand scarcity (+10% to +30%)
  - Empire tariffs (±5-15%)
  - Station services (-5% for refineries)

Temporal factors:
  - Daily fluctuations (±5%)
  - Weekly trends (supply/demand cycles)
  - Event-driven spikes (±20-50%)
```

**Cross-System Arbitrage**
```
Arbitrage opportunities:
  1. Find commodity with price gap > 30%
  2. Verify fuel cost < 20% of profit
  3. Ensure route safety (avoid hostile systems)
  4. Execute multiple runs before price equilibrium

Price equilibrium:
  - After 3-5 runs, prices may stabilize
  - Watch for diminishing returns
  - Rotate to different commodities
```

### Route Optimization

**Single-System Trading**
```
For same-station buy+sell:
  - Rare (usually NPC market orders)
  - Zero fuel cost
  - Fast turnover
  - Good for: small profit margins, quick flipping
```

**Adjacent-System Trading**
```
For 1-jump routes:
  - Low fuel cost
  - Fast turnover
  - Moderate profit
  - Best for: early game, capital building
```

**Long-Distance Trading**
```
For 3+ jump routes:
  - High fuel cost
  - Slow turnover
  - Highest profit margins
  - Best for: late game, large cargo ships
```

## Ship Selection for Trading

### Small Cargo Ships (50-100 capacity)
```
Best trades: High-value, low-volume
Strategy: Long-distance, rare commodities
Avoid: Bulk ore trading

Example trades:
  - advanced_components (10 space, 500+ profit each)
  - medical_supplies (5 space, 300+ profit each)
  - rare_elements (15 space, 800+ profit each)
```

### Medium Cargo Ships (100-200 capacity)
```
Best trades: Mixed strategy
Strategy: Adjacent-system arbitrage
Flexible: Can do both bulk and high-value

Example trades:
  - refined_materials (20 space, 100+ profit each)
  - fuel_cells (5 space, 20+ profit each)
  - consumer_goods (10 space, 50+ profit each)
```

### Large Cargo Ships (200+ capacity)
```
Best trades: Low-value, high-volume
Strategy: Bulk trading, short distances
Focus: Minimize fuel cost per cargo unit

Example trades:
  - ore_iron (5 space, 10+ profit each, but 1000+ per run)
  - ore_copper (5 space, 15+ profit each)
  - food_supplies (10 space, 25+ profit each)
```

## Profitability Analysis

### Cost Calculation
```
Total cost per run:
  = (commodity_cost * quantity)
    + fuel_cost
    + time_cost
    + tolls (if any)
    + repair_cost (if combat damage)
    + opportunity_cost

Net profit:
  = revenue - total_cost

Profit per hour:
  = net_profit / total_run_time

Profit per cargo unit:
  = net_profit / cargo_capacity_used
```

### Minimum Profit Thresholds
```
Small cargo (50-100): 500+ credits per run
Medium cargo (100-200): 1000+ credits per run
Large cargo (200+): 2000+ credits per run

Profit per hour targets:
  Early game: 1000+ credits/hour
  Mid game: 3000+ credits/hour
  Late game: 10000+ credits/hour
```

### Break-Even Analysis
```
Never execute trade if:
  fuel_cost > 40% of profit_margin
  OR
  total_jumps > 10 AND profit_margin < 1000
  OR
  route goes through hostile systems AND profit_margin < 2000
```

## Advanced Tactics

### Market Intelligence
```
Build knowledge base:
  - Track price history (last 7 days)
  - Identify price cycles
  - Note seasonal events
  - Record NPC trade patterns

Predict price movements:
  - Supply shortage: prices rise next 2-3 days
  - Demand spike: prices rise immediately
  - Price equilibrium coming: rotate commodities
```

### Trade Networks
```
Hub-and-spoke model:
  - Home system = hub (major station)
  - Outposts = spokes (satellite stations)
  - Run hub → spoke → hub routes
  - Efficient fuel usage

Linear route model:
  - A → B → C → D → A
  - Different commodities each leg
  - Never empty cargo on return trips
  - Maximum cargo utilization
```

### Bulk Trading
```
For maximum profit efficiency:
  1. Fill 100% cargo with single commodity
  2. Sell all at destination
  3. Don't mix (unless profitable backhaul)

Backhaul opportunities:
  - Find profitable commodity for return trip
  - Fill empty cargo space
  - Increases total profit per cycle
```

### Faction Trading
```
Join trading faction:
  - Access to faction stations
  - Faction tariffs may be lower
  - Faction trade intel (price data)
  - Faction bulk discounts

Faction missions:
  - Courier missions (directional profit)
  - Trade missions (guaranteed profit)
  - Combine with personal trading
```

## Progression Goals

### Short Term (0-50 runs)
- Achieve 1000+ credits per run average
- Build capital (10000+ credits)
- Identify 5+ profitable routes
- Upgrade to medium cargo ship

### Medium Term (50-200 runs)
- Achieve 5000+ credits per run average
- Build capital (50000+ credits)
- Map price data for 20+ stations
- Upgrade to large cargo ship
- Consider faction membership

### Long Term (200+ runs)
- Achieve 10000+ credits per run average
- Build capital (200000+ credits)
- Complete market knowledge base (100+ stations)
- Build personal trading post
- Consider manufacturing (crafting → selling)

## Common Pitfalls to Avoid

1. **Ignoring fuel costs** - Always include fuel in profitability calc
2. **Empty return trips** - Find backhaul cargo
3. **Single-commodity focus** - Diversify when prices drop
4. **Unsafe routes** - Avoid hostile systems unless profit > 2000
5. **No market data** - Always collect prices before trading
6. **Small margins on long routes** - Fuel eats profit
7. **Forgetting tolls/tariffs** - Check empire modifiers
8. **Overtrading same route** - Watch for price equilibrium

## Safety Protocols

### Route Safety Check
```
Before executing trade route:
  [ ] All systems in route have safe police level
  [ ] No recent combat reports in route systems
  [ ] Fuel sufficient for round trip (2x distance)
  [ ] Known station at destination (confirmed recently)
  [ ] Backup route planned (if destination unsafe)
```

### Capital Management
```
Reserve fund strategy:
  - Operating capital: 50% of total credits
  - Reserve fund: 30% of total credits (emergency)
  - Investment fund: 20% of total credits (ship upgrades)

Never trade below reserve fund threshold
```

### Emergency Procedures
```
If attacked during trade run:
  1. Jettison valuable cargo (if threatened)
  2. Flee to nearest safe system
  3. Accept cargo loss (ship > cargo)
  4. Rebuild from reserve fund

If station destroyed/no market:
  1. Check backup destination
  2. Execute alternative route
  3. Update market knowledge base
```

## Empire Synergies

### Solarian (Recommended)
- Mining/trade bonuses
- Best station networks
- Lower tariffs on trade

### Nebula
- Exploration bonuses
- Finding new markets
- Price intelligence

### Outer Rim
- Cargo bonuses
- Increased capacity
- Manufacturing integration

### Voidborn
- Stealth bonuses
- Avoid combat
- Safe route traversal

### Crimson
- Combat bonuses
- Less optimal for trading
- Hostile systems common

## Key Commands Reference

```go
// Market commands
get_listings()       // Get station market data
get_market(item_id)   // Filter by item
buy(item_id, qty)    // Purchase commodities
sell(item_id, qty)   // Sell commodities

// Navigation
get_system()         // System POI data
travel(poi_id)       // Intra-system travel
jump(system_id)       // Inter-system travel
dock()               // Arrive at station
undock()             // Leave station

// Cargo management
get_cargo()          // Check cargo contents
jettison(item, qty)  // Dump cargo (emergency)

// Orders
create_sell_order(item, qty, price)  // Limit sell order
create_buy_order(item, qty, price)   // Limit buy order
view_orders()        // Check your orders
cancel_order(order_id) // Cancel order

// Market analysis
analyze_market()      // Get price trends
get_map()             // Galaxy map
find_route(target)    // Shortest path
```

## Trader Metrics

### Good Trade Run
- Net profit: 1000-3000 credits
- Profit per hour: 2000-5000
- Cargo utilization: 80%+
- No combat damage

### Excellent Trade Run
- Net profit: 5000+ credits
- Profit per hour: 10000+
- Cargo utilization: 95%+
- Backhaul cargo found
