# Travel & Recall Skills Design

**Date:** 2026-02-27
**Author:** Claude + User
**Status:** Approved

## Overview

Design for two YAML-based navigation skills: `travel.yaml` (general-purpose navigation) and `recall.yaml` (return to empire capital). Both skills require enhancements to the skill execution system including new actions, expression variables, and persistence mechanisms.

## Goals

1. **travel.yaml**: Reusable skill to navigate to any destination system with optional POI docking, supporting smart resume after disconnect.
2. **recall.yaml**: Specialized skill to return agent to empire capital and dock at home base.
3. **Persistence**: Hybrid approach using JSON files + agent memory for route state.
4. **Fuel management**: Pre-flight fuel checks with refuel when possible, fail-fast when route exceeds range.

## Architecture

### System Enhancements Required

#### 1. New Actions (ClientDispatcher)

| Action | Parameters | Behavior |
|--------|------------|----------|
| `find_route` | `destination_system` | Calls `client.FindRoute()`, stores route in memory & persistence file |
| `store_route_progress` | (none) | Saves `route.json` with current step index |
| `load_route_progress` | (none) | Loads route from persistence, validates current position |
| `clear_route_progress` | (none) | Removes `route.json` file |
| `find_poi_in_system` | `poi_name` | Searches `state.System.POIs` for matching name, stores ID |

#### 2. New Expression Variables

| Variable | Type | Source |
|----------|------|--------|
| `current_system` | string | `state.System.ID` |
| `player_empire` | string | `state.Player.Empire` (lowercased) |
| `fuel_max_jumps` | int | `int(state.Fuel / 3.0)` |
| `capital_system_id` | string | Derived from empire via `EmpireCapitalSystem()` |
| `route_destination_system` | string | From loaded route |
| `route_destination_poi` | string | From loaded route |
| `route_step_count` | int | From loaded route |
| `route_current_step` | int | From loaded route |

#### 3. New Expression Functions

| Function | Parameters | Returns |
|----------|------------|---------|
| `fuel_sufficient_for_jumps(n)` | int | `state.Fuel >= n * 3.0` |
| `has_route_progress()` | - | true if valid `route.json` exists |
| `at_system(system_id)` | string | `state.System.ID == system_id` |
| `poi_is_dockable()` | - | `current_poi_type IN (station, base)` |

#### 4. Persistence Format

**File:** `data/agents/<agent-id>/route.json`

```json
{
  "destination_system": "haven",
  "destination_poi": "Haven Station",
  "route": [
    {"system_id": "sol", "name": "Sol", "jumps": 1},
    {"system_id": "haven", "name": "Haven", "jumps": 0}
  ],
  "current_step": 0,
  "timestamp": "2026-02-27T10:00:00Z"
}
```

**Agent Memory:** `Memory.SetString("skill:travel:route", jsonBytes)`

### Capital System Mapping

| Empire | Capital System ID |
|--------|-------------------|
| Solarian | sol |
| Crimson | krynn |
| Nebula | haven |
| Voidborn | nexus |
| Outerrim | frontier |

## Skill Designs

### travel.yaml

**Purpose:** Navigate to any destination system with optional POI docking.

**Inputs:** (via runtime context or stored)
- `destination_system`: System ID to reach
- `destination_poi`: Optional POI name to dock at after arrival

**Prerequisites:**
- Can start from anywhere (docked or undocked)

**Flow:**

```
1. Check for existing route progress
   ├─ Valid route found? → Load and verify position
   └─ No route/invalid? → Calculate fresh route

2. Fuel check (pre-flight)
   ├─ Calculate total jumps needed
   ├─ Check fuel >= (jumps × 3)
   ├─ If docked AND fuel insufficient but refuelable → refuel
   └─ If insufficient AND cannot refuel → fail

3. Execute route (loop)
   For each step from current_step:
     ├─ If at target system → skip
     ├─ Jump to next system
     ├─ Save progress after jump
     └─ Continue until destination reached

4. In-system navigation (if POI specified)
   ├─ Find POI by name
   ├─ Travel to POI
   └─ If station/base → dock, else → end

5. Cleanup
   └─ Clear route persistence
```

**YAML Structure:**

```yaml
name: travel
description: >
  Navigate to a destination system and optionally dock at a named POI.
  Supports resume after disconnect via route persistence.

prerequisites:
  - at_poi_type(station, base) OR not docked

steps:
  - id: check_resume
    check: true
    conditions:
      has_route_progress(): goto validate_resume
      default: goto plan_route

  - id: validate_resume
    action: load_route_progress
    conditions:
      route_current_step < route_step_count: goto fuel_check
      default: goto plan_route

  - id: plan_route
    action: find_route
    next: fuel_check

  - id: fuel_check
    check: true
    conditions:
      fuel_sufficient_for_jumps(route_step_count - route_current_step): goto begin_route
      docked AND fuel_pct < 1.0: goto refuel_now
      default: goto fail_no_fuel

  - id: refuel_now
    action: refuel
    next: fuel_check

  - id: fail_no_fuel
    terminal: true

  - id: begin_route
    # Jump loop with persistence
    # (detailed step logic in implementation)
    ...
```

### recall.yaml

**Purpose:** Return agent to empire capital and dock at home base.

**Prerequisites:**
- None (can start from anywhere)

**Flow:**

```
1. Determine home base
   └─ Capital system from player empire

2. Check if already home
   ├─ In capital system AND docked? → Done
   └─ Otherwise → Continue

3. Invoke travel.yaml sub-skill
   └─ Navigate to capital system

4. Dock at base/station
   └─ Ensure docked
```

**YAML Structure:**

```yaml
name: recall
description: >
  Return the agent to their empire's capital system and dock at
  the home base. Uses travel.yaml sub-skill for navigation.

prerequisites: []

steps:
  - id: check_already_home
    check: true
    conditions:
      at_system(capital_system_id) AND docked: goto done
      default: goto travel_home

  - id: travel_home
    skill: travel
    next: dock_at_base

  - id: dock_at_base
    action: dock
    next: done

  - id: done
    terminal: true
```

## Error Handling

### Travel Error Scenarios

| Scenario | Handling |
|----------|----------|
| No route found (disconnected systems) | Fail: "No route to {destination}" |
| Insufficient fuel, not docked | Fail: "Need {x} fuel, have {y}. Find nearest station." |
| Route longer than max fuel range | Fail: "Route requires {n} jumps, max range is {m}" |
| POI not found in destination | Fail: "POI '{name}' not found in {system}" |
| Disconnect during route | Resume from saved step on next run |
| Invalid saved route | Discard and replan from scratch |

### Resume Validation

When loading a saved route:
1. Check if `current_system` matches the expected system for `current_step`
2. If not, search the route array for a matching system and update `current_step`
3. If current system not in route, discard and replan entirely
4. Validate timestamp (optional: expire routes older than X hours)

## Testing Strategy

### Unit Tests
- New actions: `find_route`, `store_route_progress`, `load_route_progress`
- Expression functions: fuel calculations, system comparisons
- Route file I/O and JSON parsing
- Resume validation logic

### Integration Tests
- Full travel.yaml execution in test environment
- Disconnect/reconnect resume scenario
- Fuel edge cases (exactly enough, one short, empty)
- Invalid destinations (nonexistent systems, POIs)
- Multi-jump routes (3+ systems)

### Manual Testing
- recall.yaml from each empire starting point
- Travel to various POI types (station, asteroid, gate)
- Resume after forced disconnect
- Fuel depletion scenarios

## Implementation Notes

### Fuel Calculation
- Each jump costs exactly 3 fuel
- Max jumps = `int(state.Fuel / 3.0)`
- Pre-flight check: `state.Fuel >= (remaining_jumps * 3.0)`

### Route Persistence Lifecycle
1. `find_route` action creates `route.json`
2. After each successful jump, `store_route_progress` updates `current_step`
3. On completion or failure, `clear_route_progress` removes the file
4. On skill start, `has_route_progress()` checks for file existence

### Sub-skill Chaining
- `recall.yaml` invokes `travel.yaml` via `skill: travel`
- Destination must be communicated through:
  - Agent memory context, OR
  - Pre-stored route file, OR
  - New skill parameter mechanism (future enhancement)

### Capital System Detection
- Use existing `EmpireCapitalSystem(empire string) string` function
- Add `capital_system_id` as expression variable
- Fallback to empty string if empire unknown

## Future Enhancements

1. **Skill Parameters:** Allow travel.yaml to accept destination as explicit parameter
2. **Auto-refuel Detours:** Calculate nearest station with refueling capability
3. **Route Caching:** Store common routes in knowledge base
4. **Multi-destination Routes:** Support waypoints and multi-stop journeys
5. **Travel Time Estimation:** Calculate and display ETA based on jump distance
