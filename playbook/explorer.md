# SpaceMolt Career Playbook: Explorer

## Overview
The Explorer career focuses on discovering new systems, mapping POIs, and gathering intelligence about the galaxy. Explorers use a two-phase approach: first building wealth through mining, then conducting systematic exploration using depth-first search (DFS).

## Phase 1: Wealth Building (Mining & Upgrades)

### Objective
Accumulate 2000+ credits and acquire exploration equipment before setting out.

### Upgrade Path
1. **Scanner (600+ credits)** - Essential for system scanning
2. **Mining Laser (300+ credits)** - Income source during Phase 1
3. **Drillship (2000+ credits)** - Mining vessel with 3 utility slots
4. **Triple Mining Lasers** - Maximizes Phase 1 income

### Completion Criteria
Phase 1 complete when:
- Drillship (mining_enhanced) acquired with 3 mining lasers, OR
- Scanner equipped + at least one mining laser

## Phase 2: Galaxy Exploration (DFS)

### Exploration Strategy
Use Depth-First Search (DFS) to systematically explore the galaxy:

```
exploration_state = {
  visited_systems: map[SystemID]bool
  visited_pois: map[POI]bool  // Reset per system
  dfs_stack: []SystemID       // Backtracking stack
  home_system: SystemID         // Starting point
  last_fuel_station: SystemID   // Safety refuel point
  previous_system: SystemID     // Escape routes
  under_attack: bool
  agent_id: string
}
```

### Core Exploration Loop

**1. Assess Current System**
```
If current system not visited:
  - Mark as visited
  - Collect system data (get_system, scan)
  - Explore all POIs in system
  - Handle stations (market data, refuel)
```

**2. Find Unvisited Neighbors**
```
- Check system.connections
- Filter against visited_systems
- If unvisited neighbors exist:
    - Push current system to DFS stack
    - Navigate to first unvisited neighbor
- Else (all neighbors visited):
    - Pop from DFS stack (backtrack)
    - Navigate to previous system
```

**3. Handle Complete Exploration**
```
If DFS stack empty:
  - All reachable systems explored
  - Return to home system
  - Reset visited_systems (start fresh)
  - Continue exploration
```

## POI Exploration Protocol

### POI Visit Sequence
For each POI in system:

**1. Travel to POI**
```
- travel() to POI
- Wait 3 seconds
```

**2. Gather POI Data**
```
- get_poi() for detailed information
- Check for nearby players (get_nearby())
- Save POI data to knowledge base
```

**3. Station-Specific Actions**
```
If POI type == "station":
  - dock()
  - Collect market listings (get_listings)
  - Collect ship listings (get_ships)
  - Save market/ship data with timestamp
  - Update last_fuel_station
  - refuel() if needed (< 30%)
  - undock()
```

**4. Record Knowledge**
```
- Save POI data to knowledge base
- Save POI data to file (legacy compatibility)
- Mark POI as visited
```

## Combat & Survival

### Damage Assessment
```
is_damaged() = hull / max_hull < 50%
If damaged:
  - Find nearest station (current or last_fuel_station)
  - Navigate to station
  - repair()
  - refuel() if needed
```

### Combat Evasion
```
check_and_evade_combat():
  If in_combat:
    Assess situation:
      - Hull % (hull/max_hull * 100)
      - Shield % (shield/max_shield * 100)
      - Has weapons?

    Decision tree:
      - Hull < 40%: FLEE
      - No weapons: FLEE
      - Hull < 70% AND shield < 10%: FLEE
      - Else: FIGHT BACK

    If flee:
      - attempt_flee_to_station()
    If fight:
      - attack() nearest hostile
      - Reassess after each attack
```

### Flee Protocol
```
Priority 1: Station in current system
Priority 2: Jump to last_fuel_station
Priority 3: Jump to previous_system
Priority 4: Jump to any visited connected system
Priority 5: Jump to any connected system (last resort)
```

## Navigation Protocol

### Inter-System Jumps
```
navigate_to_system(target_system):
  1. Store current_system as previous_system
  2. Check fuel (need 10+ for jump)
  3. If fuel low: find_and_refuel()
  4. undock() if docked
  5. Travel to jump_gate POI
  6. jump() to target_system
  7. Wait 25 seconds
```

### Fuel Management
```
find_and_refuel():
  Priority 1: Station in current system
  Priority 2: Jump to last_fuel_station
  Priority 3: Backtrack through visited systems

If no jump gate in current system:
  - Cannot escape (trapped!)
  - Tip: Consider self-destruct to respawn at home
```

## Knowledge Base Integration

### Data Collection
```
Per system:
  - System ID, name, position
  - Empire/faction
  - Police level
  - Connections (adjacent systems)
  - All POIs with full details
  - Market listings (timestamped)
  - Ship listings (timestamped)
  - Resource availability
```

### Timestamp Strategy
- Market data: YYYYMMDD format
- Check has_market_today() before collecting
- Check has_ships_today() before collecting
- Avoids redundant data collection

### File Storage Format
```
data/server/systems/{SYSTEM_NAME}.{TIMESTAMP}.json
data/server/listings/{SYSTEM}.{POI}.{TIMESTAMP}.market.listing.json
data/server/listings/{SYSTEM}.{POI}.{TIMESTAMP}.ships.listing.json
```

## System Selection Strategy

### Home System
- Start point and safe harbor
- Good market/refuel access
- Police protection (empire home systems)

### Frontier Systems
- Higher risk, higher reward
- Fewer competitors for resources
- May contain rare resources

### Empire Systems
- Police presence = safety
- Better station services
- More market competition

## Advanced Tactics

### Efficient Route Planning
```
- Always keep escape route (previous_system)
- Track last_fuel_station for safety
- Use DFS stack for systematic coverage
- Don't revisit explored systems unnecessarily
```

### Combat Recovery
```
If attacked and damaged:
  1. Flee to safe system
  2. Repair at station
  3. Reassess ship equipment
  4. Consider combat upgrades if repeated attacks
```

### Market Intelligence
```
- Save market data daily per station
- Track price variations across systems
- Note resource availability
- Identify arbitrage opportunities
```

## Progression Goals

### Short Term (Phase 1)
- [ ] 600 credits for scanner
- [ ] Drillship with 3 mining lasers
- [ ] Full cargo hold of ore sold

### Medium Term (First 20 systems)
- [ ] Visit 20 unique systems
- [ ] Collect market data from 10 stations
- [ ] Map all POIs in home sector
- [ ] Establish last_fuel_station network

### Long Term (100+ systems)
- [ ] Visit 100+ systems
- [ ] Complete galaxy map (all reachable systems)
- [ ] Build comprehensive knowledge base
- [ ] Identify trade routes for allies

## Common Pitfalls

1. **No escape route** - Always track previous_system
2. **Fuel exhaustion** - Refuel at 30%, not 10%
3. **No scanner** - Can't explore effectively without it
4. **Ignoring damage** - Repair at 50%, don't wait for critical
5. **Forgetting refuel** - Always refuel at stations before jumping
6. **No backup plan** - Self-destruct if truly trapped

## Safety Protocols

### Minimum Requirements Before Exploration
- 600+ credits (scanner cost)
- Fuel above 30%
- Hull above 50%
- Knowledge of at least 1 station system

### Emergency Procedures
```
If trapped (no fuel, no station):
  1. Check for jump gate
  2. If no gate: self-destruct()
  3. Respawn at home/base
  4. Rebuild from savings
```

### Combat Decision Matrix
```
                | Has Weapon | No Weapon
----------------+------------+------------
Hull > 70%     | FIGHT      | FLEE
Hull 40-70%    | FIGHT*     | FLEE
Hull < 40%     | FLEE       | FLEE

*Only if shields > 10%
```

## Key Commands Reference

```go
// Exploration commands
get_system()         // Get system/POI data
scan()               // Scan system (requires scanner)
get_poi(poi_id)      // Detailed POI info
get_nearby()         // Nearby players/ships
jump(system_id)       // Inter-system travel
travel(poi_id)       // Intra-system travel

// Station commands
get_listings()       // Market data
get_ships()          // Ship listings
dock()               // Arrive at station
undock()             // Leave station
refuel()             // Fill fuel
repair()             // Fix hull

// Knowledge commands
save_system_data()    // File system info
save_market_data()    // File market listings
save_poi_data()      // File POI details
save_combat_data()    // File combat encounters

// Combat commands
attack(target_id)     // Engage hostile
get_status()          // Check combat state
```

## Empire Synergies

### Nebula (Recommended)
- Exploration/scanning bonuses
- Best suited for explorer career

### Solarian
- Mining bonuses (useful in Phase 1)
- Trade bonuses for market data

### Voidborn
- Stealth bonuses (avoid combat)
- Shield bonuses (survivability)

### Crimson
- Combat bonuses (less optimal)
- Good for combat-heavy exploration

### Outer Rim
- Cargo bonuses (more supplies)
- Crafting bonuses (self-sufficiency)
