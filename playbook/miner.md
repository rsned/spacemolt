# SpaceMolt Career Playbook: Miner

## Overview
The Miner career focuses on extracting resources from asteroid belts and fields, then selling them at stations for profit. This is a steady, reliable income source that requires minimal combat risk.

## Core Strategy Loop

### 1. Initial Assessment
- Check current status: `get_status()`
- Verify ship class, cargo capacity, fuel level, hull integrity
- Note available credits and current system

### 2. Upgrade Planning
Before each mining run, assess upgrade opportunities:

**Credit Thresholds:**
- Tier 1 (300+ credits): Basic mining laser
- Tier 2 (800+ credits): Better mining laser or cargo expansion
- Tier 3 (2000+ credits): Upgrade to mining_enhanced (Drillship) with 3 utility slots

**Equipment Priorities:**
1. **Mining Lasers** - Primary income multiplier. More lasers = faster mining.
   - Starter ship: 2 utility slots max
   - Drillship (mining_enhanced): 3 utility slots
   - Excavator (mining_barge): 4 utility slots

2. **Shields** - Survival during unexpected combat
   - Buy shields when available and affordable
   - Priority: mining lasers > shields

3. **Ship Upgrades** - Major capacity increases
   - Follow MiningProgression tiers in order
   - Always sell cargo and uninstall modules before ship upgrade

### 3. Mining Run Execution

**Step 1: Undock**
```
If docked:
  - undock()
  - Wait 12 seconds
```

**Step 2: Travel to Mining POI**
```
- Find asteroid_belt or asteroid_field in current system
- travel() to the POI
- Wait 20 seconds
```

**Step 3: Mine Until Cargo Full**
```
Calculate max mining attempts:
  max_attempts = max(cargo_capacity / (5 * num_lasers), 5)

Loop:
  - Check cargo: if >= 97% full, break
  - Check fuel: if < 10%, break
  - mine()
  - Wait 11 seconds per attempt
  - Log progress every 3 attempts
```

**Step 4: Return to Station**
```
- Find station POI in current system
- travel() to station
- Wait 20 seconds
```

**Step 5: Dock**
```
- dock()
- Wait 15 seconds
```

**Step 6: Sell All Cargo**
```
- Use sell_all_bulk() for efficiency
- Wait 5 seconds for state update
- Log credits earned
```

**Step 7: Refuel**
```
If fuel < 80%:
  - refuel()
  - Wait 3 seconds
```

**Step 8: Repair**
```
If hull < 90%:
  - repair()
  - Wait 3 seconds
```

**Step 9: Check Upgrades**
```
Every 5 runs or when credits >= Tier 1 threshold:
  - attemptUpgrades()
```

## Advanced Tactics

### Cargo Management
- Keep reserve credits (50-100) for emergencies
- Never let cargo exceed 97% capacity (leaves room for errors)
- If cargo too full for upgrades, sell before buying

### Fuel Conservation
- Minimum 10% fuel reserve before returning to station
- Higher fuel efficiency ships mine more per run

### System Selection
- Stay in home system initially (safe, police protection)
- Move to resource-rich systems when properly equipped
- Asteroid belts vs fields: belts often have more resources

### Market Awareness
- Check station listings before buying equipment
- Compare prices across stations if multiple nearby
- Sell during market peaks if possible

## Safety Protocols

### Emergency Return
```
If fuel < 20% or hull < 50%:
  - Abort mining immediately
  - Return to nearest station
  - Repair/refuel before resuming
```

### Combat Evasion
```
If attacked:
  - Check if weapons installed
  - If no weapons, flee to station immediately
  - If weapons and hull > 70%, consider fighting back
```

## Progression Goals

### Short Term (0-10 runs)
- Equip maximum mining lasers for current ship
- Maintain steady credit income
- Keep hull above 90%

### Medium Term (10-50 runs)
- Upgrade to Drillship (mining_enhanced)
- Equip 3 mining lasers
- Maintain 1000+ credits reserve

### Long Term (50+ runs)
- Upgrade to Excavator (mining_barge) with 4 lasers
- Consider faction membership for station benefits
- Build personal base for storage and crafting

## Common Pitfalls to Avoid

1. **Over-mining** - Don't mine until 100% full (97% is safer)
2. **Ignoring fuel** - Always keep 10% minimum for return trip
3. **No reserve credits** - Keep 50-100 credits for repairs/fuel
4. **Forgetting upgrades** - Check every 5 runs, not just when wealthy
5. **Cargo bloat** - Sell before upgrading equipment
6. **Ship upgrade mistakes** - Always sell cargo and uninstall modules first

## Efficiency Metrics

### Good Mining Run
- Credits earned: 200-500 per run (early game)
- Mining efficiency: 3-5 minutes per full cargo
- Uptime: 80%+ mining, 20% docked/selling

### Excellent Mining Run
- Credits earned: 500-1000+ per run (late game)
- Mining efficiency: 2-3 minutes per full cargo
- Uptime: 90%+ mining, 10% docked/selling

## Key Commands Reference

```go
// Core commands
get_status()        // Check current state
get_system()        // Get POIs in current system
undock()            // Leave station
travel(poi_id)      // Move to POI
mine()              // Extract resources
dock()              // Arrive at station
sell(item_id, qty)  // Sell specific items
sell_all_bulk()     // Sell all cargo at once
refuel()            // Fill fuel tank
repair()            // Fix hull damage

// Market commands
get_listings()       // Check station market
buy(item_id, qty)    // Purchase items
install(module_id)    // Equip module
uninstall(module_id)  // Remove module

// Ship commands
get_ships()         // List available ships
buy_ship(class)      // Purchase new ship
```

## Empire Synergies

### Solarian
- Mining/trade bonuses
- Best starter empire for miners

### Voidborn
- Stealth/shields
- Useful for dangerous mining zones

### Crimson
- Combat bonuses
- Less optimal for pure mining

### Nebula
- Exploration bonuses
- Good for finding new mining systems

### Outer Rim
- Crafting/cargo bonuses
- Excellent for late-game refining
