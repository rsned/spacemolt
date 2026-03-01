# Skills Extraction — Next Steps

Potential skills identified from `cmd/auto-*` agents and `pkg/strategy/` that have not yet been converted to skill YAML files.

## Completed Skills

| Skill | Status |
|-------|--------|
| sell | Done |
| refuel_repair | Done |
| deposit_cargo | Done |
| mine | Done |
| craft_items | Done |
| travel | Done |
| recall | Done |
| scan_for_distress | Done |
| assist_deliver | Done |

---

## Pending Skills by Category

### Navigation

| Skill | Source Agents | Description | Status |
|-------|--------------|-------------|--------|
| explore_system | explorer | Get system data, map connections, survey for hidden POIs, scan area | Pending |
| explore_poi | explorer | Travel to POI, collect details, track freshness in knowledge base | Pending |
| seek_populated_system | prophet | Query map for most populated system to travel to | Pending |

### Station Operations

| Skill | Source Agents | Description | Status |
|-------|--------------|-------------|--------|
| collect_station_data | explorer | Dock, fetch market/ship listings, store snapshots to knowledge base | Done |
| ensure_docked | trader, prophet, fighter | Find nearest station, travel to it, dock — universal "get me to safety" | Done |
| buy_items | trader | Budget-aware purchasing with cargo space calculation | Pending |
| sell_cargo_smart | trader | Market-aware selling into buy orders with price floor logic | Pending |
| create_market_orders | trader | Create sell orders at calculated prices for high-value cargo | Pending |
| switch_ship | trader | List ships at station, find best cargo capacity, switch to it | Pending |

### Combat & Salvage

| Skill | Source Agents | Description | Status |
|-------|--------------|-------------|--------|
| hunt_and_engage | fighter, pirate | Travel to combat zone, scan for targets, engage in battle | Pending |
| loot_wreck | fighter, salvager | Get wrecks at location, loot each one for equipment/materials | Pending |
| scan_for_threats | random, fighter | Scan nearby entities, classify threat levels | Pending |

### Ship Management

| Skill | Source Agents | Description | Status |
|-------|--------------|-------------|--------|
| manage_upgrades | miner, fighter | Check credits vs thresholds, buy and install better equipment | Pending |
| install_module | fighter | Install equipment from cargo into available ship slots | Pending |
| equip_and_sell_extras | fighter | Install what fits, sell remaining equipment | Pending |

### Resource & Storage

| Skill | Source Agents | Description | Status |
|-------|--------------|-------------|--------|
| withdraw_from_storage | craftsman | View storage, withdraw specific items by quantity | Pending |
| check_fuel_before_jump | explorer | Verify fuel level meets minimum before allowing a jump | Pending |

### Assist

| Skill | Source Agents | Description | Status |
|-------|--------------|-------------|--------|
| scan_for_distress | assist-* | Monitor chat channels for fuel/repair help requests, extract requester details | Done |
| assist_deliver | assist-* | Buy supplies, travel to stranded pilot, jettison items, notify via chat, return | Done |

### Social

| Skill | Source Agents | Description | Status |
|-------|--------------|-------------|--------|
| preach | prophet | Deliver sermon messages to system chat channel | Pending |
| chat_random | random | Send random chat messages with configurable probability | Pending |

### Survival

| Skill | Source Agents | Description | Status |
|-------|--------------|-------------|--------|
| emergency_dock | prophet, fighter | If fuel or hull critically low, override current task and dock immediately | Done |

---

## Recommended Priority Order

Skills ranked by reuse potential across agents:

1. **ensure_docked** — 3+ agents need "find station and dock"
2. **collect_station_data** — explorer uses it, but any agent visiting stations could benefit
3. **buy_items** / **sell_cargo_smart** — core trading primitives reusable by trader, craftsman, miner
4. **loot_wreck** — fighter and salvager both need this
5. **emergency_dock** — safety-critical, any agent could use it
6. **scan_for_threats** — situational awareness for combat and non-combat agents
7. **withdraw_from_storage** — craftsman and any future storage-aware agent
8. **explore_system** / **explore_poi** — explorer-specific but high value for knowledge base
9. **manage_upgrades** / **install_module** — miner and fighter progression
10. **hunt_and_engage** — combat agents

---

## Out of Scope

The following top-level agent loops are orchestrators that compose multiple skills. They should remain as strategies, not be converted to individual skills:

- **Explorer DFS loop** — exhaustive galaxy mapping via depth-first search
- **Trader route phases** — multi-leg buy/travel/sell/return cycle with crash recovery
- **Mining cycle** — mine/return/sell/upgrade loop
- **Craftsman production loop** — withdraw/craft/deposit with market analysis
- **Prophet preaching phases** — seek congregation, preach, minister, move on
- **Fighter combat loop** — hunt/engage/loot/upgrade cycle
