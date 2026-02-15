# SpaceMolt Career Playbook: Pirate

## Overview
The Pirate career focuses on aggressive predation: attacking other players, stealing cargo, looting wrecks, and evading authorities. This is a high-risk, high-reward playstyle requiring strong combat skills and strategic escape routes.

## Core Strategy Loop

### 1. Target Selection
- Use `get_nearby()` to scan for prey
- Evaluate target wealth (cargo value, ship class)
- Assess target threat level (weapons, shields, reputation)
- Select targets: wealthy + weak + isolated

### 2. Attack Execution
```
Pre-combat:
  [ ] Hull > 80%
  [ ] Shields regenerated
  [ ] Weapons armed
  [ ] Escape route planned
  [ ] Not in safe system (high police)

Engagement:
  1. travel() to target POI (if different)
  2. scan(target_id) for threat assessment
  3. attack(target_id) to initiate combat
  4. Monitor hull/shield each tick
  5. Disengage if hull < 40%
  6. Flee if target has superior firepower
```

### 3. Loot Collection
```
After target destruction:
  - get_wrecks() for ship wrecks
  - loot_wreck() for valuable items
  - Prioritize: weapons > modules > resources > common ore

If target was player:
  - May drop all cargo
  - High-value loot common
  - Collect quickly before others arrive
```

### 4. Escape & Safety
```
Immediate priorities after attack:
  1. Check for nearby hostiles (get_nearby())
  2. If hostiles present: flee immediately
  3. Return to safe system (low police, no stations)
  4. Dock at friendly station (if aligned with faction)
  5. Repair/refuel before next attack
```

## Advanced Tactics

### Target Evaluation Matrix
```
Target Wealth Score:
  = (cargo_value / 1000)
    + (ship_tier * 2)
    + (cargo_fullness * 5)

Target Threat Score:
  = (weapon_count * 3)
    + (shield_percent * 0.5)
    + (hull_percent * 0.3)
    + (reputation_hostile_to_you * 10)

Engagement Decision:
  If wealth_score > threat_score * 1.5: ATTACK
  If wealth_score > threat_score: CONSIDER
  Else: AVOID
```

### Optimal Hunting Grounds
```
Best locations:
  - Asteroid belts (miners isolated, cargo full)
  - Resource fields (farmers isolated)
  - Low-police systems (no intervention)
  - Border systems (trade routes)

Avoid:
  - Empire home systems (high police)
  - Station POIs (security response)
  - Faction strongholds (group retaliation)
```

### Combat Tactics

**Surprise Attack**
```
- Arrive at POI before target
- Wait for target to begin mining/working
- Attack when target distracted (mining/traveling)
- First strike advantage = +20% effective combat power
```

**Hit and Run**
```
1. Attack until hull < 50% or target hull < 30%
2. Break off (travel to different POI)
3. Wait for shields to regenerate
4. Return and finish target
5. Reduces received damage
```

**Ganking**
```
If multiple hostiles:
  - Target weakest first
  - Reduce threat score quickly
  - Use target's hull as cover
  - Drive into asteroid belt (environment hazards)
```

**Flee Optimization**
```
Perfect flee sequence:
  1. Afterburn (immediate travel)
  2. Don't wait for combat to end
  3. Accept 1-2 ticks of damage while fleeing
  4. Jump to safe system ASAP
  5. Know safe system in advance
```

### Loot Management

**Immediate Loot**
```
Priority during combat:
  1. Weapons (immediate combat value)
  2. High-value modules (shields, scanners)
  3. Rare resources (sell for profit)
  4. Common ore (bulk sell)
```

**Loot vs Escape**
```
If hull < 30% or hostile nearby:
  - Grab 1-2 most valuable items
  - Flee immediately
  - Ship > loot

If hull > 50% and clear:
  - Loot everything
  - Take time to sort valuable items
```

## Reputation & Consequences

### Reputation Management
```
Attacking players:
  - Lowers reputation with their empire
  - May trigger bounties
  - Station security may attack on sight
  - Faction members become hostile

Mitigation:
  - Hunt outside empire space
  - Attack only neutral/unknown players
  - Consider faction membership (protection)
```

### Bounty Awareness
```
If bounty placed:
  - Bounty hunters may attack
  - Check nearby players for "bounty_hunter" ships
  - Avoid systems with high security
  - Consider paying off bounty (if wealthy enough)
```

### Safe Harbors
```
Locations for repair/safe dock:
  - Criminal bases (if available)
  - Faction stations (if member)
  - Low-police systems
  - Deep space (no stations = no police)

Avoid:
  - Empire stations (bounty claimed)
  - High-security systems
```

## Ship Selection for Piracy

### Fast Attack Ships
```
Best: Light Fighter, Interceptor
Advantages:
  - High speed (catch prey)
  - Good escape capability
  - Lower cost to replace

Disadvantages:
  - Limited weapon slots
  - Weak hull (don't take hits)
```

### Balanced Combat Ships
```
Best: Medium Fighter, Heavy Fighter
Advantages:
  - Multiple weapon slots
  - Good hull/shield
  - Can handle multiple targets

Disadvantages:
  - Slower (prey may escape)
  - Higher replacement cost
```

## Profitability Analysis

### Cost of Piracy
```
Combat costs:
  - Hull repair: damage * 5 credits/point
  - Shield recharge: free at station
  - Fuel: 10-50 per attack run
  - Ammunition: (if applicable) 10-100 per fight

Ship loss risk:
  - Replace ship cost: 50% of base value
  - Lost modules: full replacement cost
  - Lost cargo: total loss

Risk/reward ratio:
  Minimum acceptable: 1:3 (risk:reward)
  Target: 1:5 or better
```

### Target Profit Thresholds
```
Minimum target:
  - Loot value > 500 credits
  - Hull damage < 40%
  - Ship loss risk < 10%

Good target:
  - Loot value > 2000 credits
  - Hull damage < 20%
  - Ship loss risk < 5%

Excellent target:
  - Loot value > 5000 credits
  - Hull damage < 10%
  - Ship loss risk < 1%
```

## Advanced Tactics

### Stalking
```
1. Identify wealthy system (trade/mining hub)
2. Move to adjacent system
3. Wait for targets to arrive (full cargo)
4. Attack before they reach station
5. Flee back to adjacent system
```

### Gate Camping
```
1. Position at jump gate in unsafe system
2. Wait for arriving ships (damaged from NPCs)
3. Attack weakened targets
4. Loot and repeat

Risks:
  - Multiple targets may arrive together
  - Targets may call for help
  - High traffic = high detection risk
```

### Bait Trap
```
1. Position at resource POI (appear to be mining)
2. Wait for pirate to attack YOU
3. Counter-attack (you're prepared)
4. Loot the would-be pirate

Requires:
  - Strong combat ship
  - Good hull/shield
  - Weapons already installed
```

## Progression Goals

### Short Term (0-25 attacks)
- Achieve positive loot/kill ratio
- Maintain 50%+ hull after attacks
- Build capital (3000+ credits)
- Learn valuable systems/routes

### Medium Term (25-100 attacks)
- Achieve 2000+ average loot per attack
- Maintain 70%+ hull after attacks
- Build capital (15000+ credits)
- Upgrade to Medium Fighter
- Establish safe harbor(s)

### Long Term (100+ attacks)
- Achieve 5000+ average loot per attack
- Maintain 80%+ hull after attacks
- Build capital (50000+ credits)
- Upgrade to Heavy Fighter or better
- Consider criminal faction (if available)
- Map all wealthy systems

## Common Pitfalls to Avoid

1. **Attacking in safe systems** - High police response
2. **No escape route** - Always know flee path
3. **Greed over loot** - Ship > cargo, flee when damaged
4. **Attacking stronger targets** - Scan first, know threat level
5. **Staying too long** - Loot and leave immediately
6. **No reputation management** - Becoming KOS everywhere
7. **Pirating while poor** - Need capital for repairs
8. **Forgetting fuel** - Can't flee if stranded

## Safety Protocols

### Pre-Attack Checklist
```
[ ] Hull > 80%
[ ] Shields at 100%
[ ] Fuel > 50%
[ ] Weapons equipped
[ ] Escape route planned
[ ] Known safe harbor
[ ] Reputation acceptable (not KOS everywhere)
[ ] Target wealth verified (scan first)
[ ] No police in current system
```

### Combat Emergency
```
If hull < 30%:
  1. Break off immediately
  2. Afterburn to safe system
  3. Accept damage during flee
  4. Don't loot (ship priority)
  5. Repair at safe harbor
  6. Reassess targets

If multiple hostiles:
  1. Flee immediately
  2. Don't fight 2v1
  3. Come back later when alone
```

### Reputation Management
```
If KOS (Kill-on-Sight) everywhere:
  - Consider starting new character
  - OR pay off bounties (if possible)
  - OR stay in deepest space (no stations)

If bounty hunters active:
  - Avoid major stations
  - Hunt in fringe systems
  - Travel in nebula/void (if possible)
```

## Empire Synergies

### Crimson (Recommended)
- Combat bonuses
- Pirate-friendly reputation
- Criminal factions available

### Voidborn
- Stealth/shield bonuses
- Evasion bonuses
- Escape superiority

### Nebula
- Exploration bonuses
- Finding remote hunting grounds
- Avoiding authorities

### Solarian
- Mining bonuses (not pirate-focused)
- Good for learning game mechanics
- High police presence (bad for pirates)

### Outer Rim
- Cargo bonuses (more loot capacity)
- Lawless systems
- Criminal hideouts

## Key Commands Reference

```go
// Combat commands
get_nearby()        // Find targets
scan(target_id)     // Target details
attack(target_id)    // Initiate combat
get_status()          // Check combat state

// Looting
get_wrecks()        // List wrecks
loot_wreck(id, item, qty)  // Take loot
salvage_wreck(id)  // Destroy for materials

// Escape
travel(poi_id)      // Flee to POI
jump(system_id)       // Flee to system
dock()               // Safe harbor
undock()             // Leave safe harbor

// Safety
get_map()             // Check police levels
get_system()         // System details
cloak()              // Hide (if module equipped)

// Market (fence)
get_listings()       // Check black market prices
sell(item_id, qty)   // Fence loot
```

## Pirate Metrics

### Good Attack Run
- Loot value: 1000-3000 credits
- Hull damage: < 30%
- Time to loot: < 5 minutes
- No escape needed (dominated fight)

### Excellent Attack Run
- Loot value: 5000+ credits
- Hull damage: < 10%
- Time to loot: < 3 minutes
- Multiple targets defeated
