# SpaceMolt Career Playbook: Salvager

## Overview
The Salvager career focuses on finding wrecks (destroyed ships and bases), extracting valuable materials and modules from them, and either selling the loot or using it for crafting. This is a scavenger playstyle with low direct conflict but high exploration requirements.

## Core Strategy Loop

### 1. Wreck Discovery
- Travel between systems scanning for wrecks
- Check both `get_wrecks()` (ship wrecks) and `get_base_wrecks()` (base wrecks)
- Note wreck types (player vs NPC destroyed)
- Prioritize fresh wrecks (despawn after 1 hour)

### 2. Wreck Evaluation
```
For each wreck:
  1. scan_wreck_contents()
  2. Calculate total value
  3. Assess cargo space needed
  4. Decide: loot fully, partially, or salvage for materials

Value categories:
  - High value: weapons, shields, advanced modules
  - Medium value: refined materials, fuel cells
  - Low value: common ore, basic materials
  - Bulk value: large quantities of common items
```

### 3. Looting Strategy

**Full Looting (high value, fits in cargo)**
```
For each item in wreck:
  - loot_wreck(wreck_id, item_id, quantity)
  - Wait 2-3 seconds per item
  - Prioritize by value-per-cargo-unit

Continue until:
  - Cargo full
  - OR wreck empty
  - OR higher-value wreck found
```

**Selective Looting (mixed value)**
```
1. Take only high-value items (weapons, shields)
2. Take medium-value if cargo space remaining
3. Leave low-value bulk items
4. Salvage wreck for materials (destroy it)
```

**Salvage Only (low value or full cargo)**
```
1. Don't loot any items
2. Use salvaging skill for materials
3. salvage_wreck() or salvage_base_wreck()
4. Get metal scrap, components, rare materials
5. Materials quantity scales with salvaging skill level
```

### 4. Market & Sell
```
Return to station:
  1. dock()
  2. sell(item_id, quantity) for valuable items
  3. sell_all_bulk() for common materials
  4. Keep rare materials for crafting
```

## Advanced Tactics

### Wreck Types & Values

**Ship Wrecks**
```
Player ship wrecks:
  - Best loot (weapons, modules from fit-out)
  - May contain cargo they were carrying
  - Common in combat zones

NPC ship wrecks:
  - Moderate loot (basic modules, resources)
  - Predictable contents
  - Common everywhere

Wreck value = modules_value + cargo_value
```

**Base Wrecks**
```
Player base wrecks:
  - Excellent loot (credits + modules)
  - Larger loot tables
  - Credits scale with base tier

NPC base wrecks:
  - Good loot (materials, some modules)
  - More common than player base wrecks

Base wrecks contain:
  - Credits (loot_base_wreck with credits=true)
  - Modules (loot_base_wreck with item_id)
  - Materials (salvage_base_wreck)
```

### Efficient Route Planning

**Wreck Hunting Route**
```
Strategy: Loop through dangerous systems
  1. Start in safe system (resupply)
  2. Jump to adjacent hostile system
  3. Check for wrecks at all POIs
  4. Loot/salvage all wrecks
  5. Jump to next hostile system
  6. Return when cargo full or damaged

Route optimization:
  - Plan circular route (end at start)
  - Include 1 safe system for repairs
  - Maximize wrecks per jump
```

**System Selection Criteria**
```
High wreck probability:
  - Asteroid belts (pirates vs miners)
  - Faction war zones (player combat)
  - Border systems (raiding)
  - Resource fields (NPC spawns)

Low wreck probability:
  - Empire home systems (safe)
  - High-police systems (lawful)
  - Deep space (no spawns)
```

### Salvaging Skill
```
Salvaging skill level benefits:
  Level 1: Base materials (1x multiplier)
  Level 2: Better materials (1.5x multiplier)
  Level 3: Rare materials (2x multiplier)
  Level 4: Excellent materials (2.5x multiplier)
  Level 5: Exceptional materials (3x multiplier)

Training:
  - Salvage wrecks (salvage_wreck)
  - Skill XP scales with materials obtained
  - Focus on skill early for better returns
```

### Cargo Management

**Valuable Cargo Priorities**
```
Tier 1 (Must keep):
  - Advanced weapons (weapon_laser_2+)
  - Advanced shields (shield_2+)
  - Rare modules (scanner_2+, advanced_mining_laser)

Tier 2 (Keep if crafting):
  - Rare materials (rare_elements)
  - Advanced components
  - Blueprint fragments (if implemented)

Tier 3 (Sell immediately):
  - Common weapons
  - Basic shields
  - Standard materials

Tier 4 (Bulk sell):
  - Common ore (ore_iron, ore_copper)
  - Basic materials (metal_scrap)
```

**Material Stacking Strategy**
```
If crafting:
  - Keep rare materials (limit: 100 of each)
  - Keep advanced components (limit: 50 of each)
  - Sell excess above limits
  - Buy materials if cheaper than sell price

If not crafting:
  - Sell everything
  - Don't hoard (opportunity cost)
```

## Profitability Analysis

### Wreck Value Estimation
```
Ship wreck average value:
  - NPC wreck: 200-500 credits worth
  - Player wreck: 500-2000 credits worth
  - Exceptional player wreck: 2000-5000 credits worth

Base wreck average value:
  - NPC base: 500-1000 credits worth
  - Player base: 2000-10000 credits worth
  - Tier scales with base size

Salvage value:
  - Level 1: 50-100 credits worth
  - Level 3: 200-400 credits worth
  - Level 5: 500-1000 credits worth
```

### Cost of Salvaging
```
Fuel cost per system:
  ~10 fuel per jump
  = 10 credits if buying fuel

Repair cost:
  ~5 credits per hull point damage
  (salvagers take some damage from NPCs)

Time cost:
  ~5-10 minutes per system scanned
  (opportunity cost of other activities)

Break-even analysis:
  Minimum wrecks per system: 2-3 wrecks
  Target wrecks per system: 5+ wrecks
```

### Profit Per Hour
```
Good salvaging run:
  - 10-20 wrecks found
  - 2000-5000 credits total value
  - 1-2 hours duration
  - 1500-3000 credits/hour

Excellent salvaging run:
  - 30+ wrecks found
  - 10000+ credits total value
  - 2-3 hours duration
  - 4000-5000 credits/hour
```

## Advanced Tactics

### Combat Zone Salvaging
```
Strategy:
  1. Wait for major battle to finish
  2. Scan system for wrecks (get_wrecks)
  3. Quick loot run (grab best items, leave)
  4. Don't engage in combat
  5. Flee if new combat starts

Risks:
  - Getting attacked
  - Wrecks despawning while you loot
  - Other salvagers competing
```

### Competitive Salvaging
```
If multiple salvagers in system:
  - Focus on highest-value wrecks first
  - Use selective looting (grab only tier 1 items)
  - Leave bulk items for others
  - Move to next system faster than competitors

Advantage:
  - Better salvaging skill (faster looting)
  - Larger cargo capacity (more per run)
  - Faster ship (travel quicker between wrecks)
```

### Market Arbitrage
```
Loot → Craft → Sell cycle:
  1. Salvage wrecks for materials
  2. Craft items (refined materials, components)
  3. Sell crafted items at profit

Example:
  - Salvage: 100 metal_scrap (worth ~500 credits)
  - Craft: 5 refined_steel (costs 20 metal_scrap each)
  - Sell: 5 refined_steel for ~1000 credits
  - Profit: +500 credits (doubled value)

Works best when:
  - Crafting skill is high
  - Materials are cheap (abundant salvaging)
  - Crafted items are expensive (high demand)
```

## Progression Goals

### Short Term (0-50 wrecks)
- Salvage 50+ wrecks
- Build capital (2000+ credits)
- Upgrade salvaging skill to level 3
- Learn wreck locations (systems)

### Medium Term (50-200 wrecks)
- Salvage 200+ wrecks
- Build capital (10000+ credits)
- Upgrade salvaging skill to level 5
- Upgrade to medium cargo ship
- Identify 10+ good wreck systems

### Long Term (200+ wrecks)
- Salvage 1000+ wrecks
- Build capital (50000+ credits)
- Max salvaging skill
- Upgrade to large cargo ship
- Build personal base for storage
- Consider crafting (manufacture goods)

## Common Pitfalls to Avoid

1. **Slow looting** - Wrecks despawn in 1 hour, prioritize
2. **Ignoring base wrecks** - Often most valuable, always check
3. **No cargo management** - Fill with junk, miss valuable items
4. **Forgetting salvaging skill** - Critical for profit
5. **Solo in combat zones** - Get ganked by winners
6. **No route planning** - Inefficient system hopping
7. **Hoarding materials** - Sell unless crafting profitably
8. **Low-value focus** - Time better spent hunting player wrecks

## Safety Protocols

### Pre-Salvage Checklist
```
[ ] Hull > 70%
[ ] Fuel > 40%
[ ] Cargo space > 20%
[ ] Salvaging skill known
[ ] Escape route planned
[ ] Station nearby for selling
[ ] Not in active combat zone
```

### Emergency Procedures
```
If attacked while looting:
  1. Grab 1-2 most valuable items immediately
  2. Flee to different POI or system
  3. Don't finish looting (ship > loot)
  4. Return later if wreck remains

If cargo full and high-value wreck found:
  1. Return to station immediately
  2. Sell all cargo
  3. Return to wreck fast (wrecks last 1 hour)
  4. Priority: valuable wrecks > time
```

## Empire Synergies

### Nebula (Recommended)
- Exploration bonuses
- Finding wrecks efficiently
- Scanning for wrecks

### Outer Rim
- Cargo bonuses
- Salvaging bonuses
- Crafting integration

### Solarian
- Mining bonuses (related skills)
- Market access for selling

### Voidborn
- Stealth (avoid combat while looting)
- Shield bonuses (survive NPC wrecks)

### Crimson
- Combat bonuses (create wrecks yourself)
- Less optimal for pure salvager

## Key Commands Reference

```go
// Wreck discovery
get_wrecks()              // List ship wrecks
get_base_wrecks()         // List base wrecks

// Wreck interaction
loot_wreck(id, item, qty) // Take items
salvage_wreck(id)         // Destroy for materials

// Base wreck interaction
loot_base_wreck()         // Loot credits/items
salvage_base_wreck()       // Destroy for materials

// Cargo management
get_cargo()                // Check cargo
sell(item_id, qty)         // Sell specific items
sell_all_bulk()            // Sell all cargo

// Navigation
get_system()                // System POIs
travel(poi_id)             // Move to POI
jump(system_id)             // Move to system
dock()                     // At station
undock()                   // Leave station
```

## Salvager Metrics

### Good Salvage Run
- Wrecks found: 5-10
- Credits earned: 1000-3000
- Time: 1-2 hours
- Profit per hour: 1000-2000

### Excellent Salvage Run
- Wrecks found: 20+
- Credits earned: 5000+
- Time: 2-3 hours
- Profit per hour: 2000-4000
