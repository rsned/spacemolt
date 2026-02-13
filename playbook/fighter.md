# SpaceMolt Career Playbook: Fighter

## Overview
The Fighter career focuses on hunting hostile ships (pirates, raiders), defeating them in combat, collecting loot from wrecks, and progressively upgrading combat capabilities. This is a high-risk, high-reward playstyle.

## Core Strategy Loop

### 1. Initial Assessment
- Check status: `get_status()`
- Verify ship class, weapon loadout, hull integrity, shields
- Note combat capabilities vs current threat level

### 2. Upgrade Planning (Combat-Focused)

**Credit Thresholds:**
- Tier 1 (500+ credits): Weapon upgrades
- Tier 2 (5000+ credits): Shield upgrades
- Tier 3 (10000+ credits): Ship upgrades

**Equipment Priorities:**
1. **Weapons** - Primary combat effectiveness
   - Starter ship: 2 weapon slots max
   - Light Fighter: 2 weapon slots
   - Medium Fighter: 3 weapon slots
   - Heavy Fighter: 4 weapon slots
   - Elite Fighter: 5 weapon slots
   - Ultimate Fighter: 6 weapon slots

2. **Shields** - Combat survivability
   - Priority: weapons > shields

3. **Ship Upgrades** - Major combat improvements
   - Follow CombatProgression tiers in order
   - Sell loot and uninstall modules before upgrade

### 3. Combat Run Execution

**Step 1: Undock**
```
If docked:
  - undock()
  - Wait 12 seconds
```

**Step 2: Travel to Combat POI**
```
- Find asteroid_belt or asteroid_field in current system
- travel() to the POI
- Wait 20 seconds
```

**Step 3: Hunt Hostiles**
```
- get_nearby() to find targets
- Scan for hostiles (pirate, bandit, raider ships)
- Combat decision tree:
  * Hull > 70% AND shields > 10%: ENGAGE
  * Hull < 40%: FLEE immediately
  * Hull 40-70%: Flee if shields < 10%

- attack(target_id) when hostile found
- Reassess after each attack (10s tick)
```

**Step 4: Collect Combat Intel**
```
combat_actions = count of attacks made
loot_value = combat_actions * ~1000 (estimated)
```

**Step 5: Loot Wrecks**
```
- get_wrecks() or get_base_wrecks() at current POI
- loot_wreck() or loot_base_wreck() for each wreck
- Prioritize: weapons > modules > resources
```

**Step 6: Return to Station**
```
- Find station POI
- travel() to station
- Wait 20 seconds
```

**Step 7: Dock**
```
- dock()
- Wait 15 seconds
```

**Step 8: Sell All Loot**
```
- sell_all_bulk() for efficiency
- Log credits earned from loot
```

**Step 9: Refuel**
```
If fuel < 80%:
  - refuel()
  - Wait 3 seconds
```

**Step 10: Repair**
```
If hull < 90%:
  - repair()
  - Wait 3 seconds
```

**Step 11: Check Upgrades**
```
Every 3 runs OR when credits >= Tier 1:
  - attemptUpgrades()
```

## Advanced Tactics

### Combat Assessment
Before engaging, evaluate:
```
hull_percent = (hull / max_hull) * 100
shield_percent = (shield / max_shield) * 100
has_weapon = weapon_count > 0
max_weapons = ship.weapon_slots
weapons_filled = count weapons installed

Combat Power Score:
  = (hull_percent * 0.3)
    + (shield_percent * 0.3)
    + (weapons_filled / max_weapons * 40)

Score > 70: Can fight multiple hostiles
Score 50-70: Can fight 1 hostile
Score < 50: Avoid combat
```

### Target Selection
```
Priority targets:
  1. pirate ships (most loot, hostile)
  2. bandit ships (medium loot, hostile)
  3. raider ships (medium loot, hostile)

Avoid targets:
  - Same empire as you (faction penalties)
  - Neutral/faction ships (reputation loss)
  - Players with significantly better ships
```

### Flee Protocol
```
Trigger flee when:
  - Hull < 40%
  - Hull < 70% AND shields depleted
  - Shields = 0 AND hull < 50%

Flee sequence:
  1. Break off combat (move away)
  2. Return to station immediately
  3. Repair fully before re-engaging
```

## Weapon Loadouts

### Early Game (500-2000 credits)
```
Recommended: weapon_laser_1 x2
Strategy: Focus on one target at a time, retreat if outgunned
```

### Mid Game (2000-10000 credits)
```
Recommended: weapon_laser_2 x3 OR weapon_cannon_1 x2
Strategy: Can handle 2-3 hostiles, use hit-and-run
```

### Late Game (10000+ credits)
```
Recommended: weapon_laser_3 x4 OR advanced weapons
Strategy: Handle multiple hostiles, aggressive hunting
```

## Progression Goals

### Short Term (0-10 combat runs)
- Equip maximum weapons for current ship
- Achieve positive kill/death ratio
- Maintain 1000+ credits

### Medium Term (10-50 runs)
- Upgrade to Medium Fighter (3 weapon slots)
- Equip quality weapons (tier 2+)
- Maintain 5000+ credits reserve
- Kill 50+ hostiles

### Long Term (50+ runs)
- Upgrade to Elite Fighter (5 weapon slots)
- Equip advanced weapons
- Maintain 10000+ credits reserve
- Kill 200+ hostiles
- Consider faction membership for combat bonuses

## Combat Mathematics

### Damage Output
```
DPS = (weapon_damage * weapon_count) / 10

Time to kill hostile:
  = hostile_hull / DPS
  + travel_time_to_target

Example:
  weapon_laser_1: ~20 damage every 10s = 2 DPS
  2 weapons = 4 DPS
  Hostile with 100 hull: 25 seconds to kill
```

### Survivability
```
EHP = hull + shields

Survivability time:
  = EHP / enemy_DPS

Rule of thumb:
  Need 2:1 EHP advantage for fair fight
  Need 3:1 EHP advantage for safe fight
```

### Profitability
```
Net profit per kill:
  = loot_value - (repair_cost + fuel_cost + ammo_cost)

Minimum profitable target:
  - Loot > 500 credits
  - Hull damage < 30%
  - No ammunition wasted

Ideal target:
  - Loot > 2000 credits
  - Hull damage < 10%
```

## Common Pitfalls to Avoid

1. **Fighting when damaged** - Never engage with hull < 70%
2. **No escape route** - Always know nearest station
3. **Outnumbered** - Don't fight 2+ hostiles alone (early game)
4. **Weapon mismatch** - Don't engage long-range with short-range weapons
5. **Ignoring shields** - Shields regenerate, hull doesn't
6. **No reserve credits** - Keep 1000+ for repairs
7. **Poor target selection** - Scan before shooting
8. **Forgetting repair** - Full repair between each combat run

## Safety Protocols

### Pre-Combat Checklist
```
[ ] Hull > 90%
[ ] Shields regenerated
[ ] Fuel > 50%
[ ] Weapons installed
[ ] Known station nearby
[ ] Credits > 500 (for repairs)
[ ] No active combat debuffs
```

### Combat Emergency
```
If hull < 30% during combat:
  1. Immediately disengage
  2. Afterburn to station (don't wait)
  3. Accept hull damage on return trip
  4. Full repair at station
  5. Reassess combat capability
```

### Hull Critical
```
If hull < 10%:
  - Consider self-destruct if respawn is cheaper
  - Respawn at home/base with fresh ship
  - Lose all cargo and credits spent on current run
```

## Empire Synergies

### Crimson (Recommended)
- Combat bonuses
- Best weapons/shields pricing
- Hostile targets give reputation bonuses

### Solarian
- Mining bonuses (not combat-focused)
- Trade bonuses for selling loot
- Better station access

### Voidborn
- Stealth/shield bonuses
- Combat survivability
- Evasion bonuses

### Nebula
- Exploration bonuses
- Finding hostile hunting grounds
- Scanning for prey

### Outer Rim
- Cargo bonuses (more loot capacity)
- Good for extended hunting trips

## Key Commands Reference

```go
// Core commands
get_status()        // Check current state
get_nearby()        // Find nearby players/ships
undock()            // Leave station
travel(poi_id)      // Move to POI
attack(target_id)    // Engage hostile
dock()              // Arrive at station
sell(item_id, qty)  // Sell loot
sell_all_bulk()     // Sell all loot
refuel()            // Fill fuel tank
repair()            // Fix hull damage

// Wreck commands
get_wrecks()        // List ship wrecks
get_base_wrecks()   // List base wrecks
loot_wreck(id, item, qty)     // Loot from ship wreck
loot_base_wreck()  // Loot credits/items from base wreck
salvage_wreck(id)  // Destroy wreck for materials

// Combat commands
scan(target_id)     // Get ship details
cloak()             // Hide from enemies (if module installed)

// Market commands
get_listings()       // Check station market
buy(item_id, qty)    // Purchase items
install(module_id)    // Equip module
uninstall(module_id)  // Remove module
```

## Fighter Metrics

### Good Combat Run
- Kills: 1-2 hostiles
- Credits earned: 500-1500 per run
- Survivability: Hull > 70% at end
- Efficiency: 2-3 minutes per kill

### Excellent Combat Run
- Kills: 3+ hostiles
- Credits earned: 2000+ per run
- Survivability: Hull > 90% at end
- Efficiency: 1-2 minutes per kill
