# auto-recall

Automatically returns an agent to their Empire's capital base.

## Usage

```bash
auto-recall <agent-id>
```

## Arguments

- `agent-id` - Agent identifier (e.g., `miner-1`, `trader-1`, `craftsman-1`)

## What It Does

1. **Determines current location** - Gets the agent's current system and POI
2. **Finds route to capital** - Uses the `find_route` API to calculate the shortest path to the Empire's capital:
   - Solarian → **Sol**
   - Crimson → **Krynn**
   - Voidborn → **Nexus Prime**
   - Nebula → **Haven**
   - Outerrim → **Frontier**
3. **Performs jump sequence** - Executes each jump in the route, waiting for completion after each:
   - Sends `jump` command
   - Monitors `state_update` messages for travel progress
   - Waits until the jump completes (traveling status becomes false)
4. **Travels to base** - Within the capital system, travels to the base POI
5. **Docks at base** - Docks the ship at the capital base

## Example

```bash
# Return a miner agent to Sol
auto-recall miner-1

# Return a trader to Krynn (Crimson capital)
auto-recall trader-1

# Return a craftsman to Haven (Nebula capital)
auto-recall craftsman-1
```

## Output Example

```
[miner-1] 2025/02/21 10:30:00 🚀 Starting auto-recall to home base...
[miner-1] 2025/02/21 10:30:00 Agent: miner-1 | Empire: Solarian | Current System: alpha_centauri | Credits: 1250.50
[miner-1] 2025/02/21 10:30:00 🏠 Capital System: Sol (sol)
[miner-1] 2025/02/21 10:30:00 📤 Undocking from current location...
[miner-1] 2025/02/21 10:30:05 ✓ Undocked successfully
[miner-1] 2025/02/21 10:30:05 🗺️  Finding route to Sol...
[miner-1] 2025/02/21 10:30:06 ✓ Route found: 3 jumps
[miner-1] 2025/02/21 10:30:06    1. Barnard's Star (barnard)
[miner-1] 2025/02/21 10:30:06    2. Sirius (sirius)
[miner-1] 2025/02/21 10:30:06    3. Sol (sol)
[miner-1] 2025/02/21 10:30:06 🌟 Jump 1/3: alpha_centauri -> Barnard's Star
[miner-1] 2025/02/21 10:30:06    Initiating jump...
[miner-1] 2025/02/21 10:30:06    ⚡ jump to barnard... (0%)
[miner-1] 2025/02/21 10:30:15    ⚡ jump to barnard... (50%)
[miner-1] 2025/02/21 10:30:25    ✓ Travel complete (now in barnard)
[miner-1] 2025/02/21 10:30:25 ✓ Arrived at Barnard's Star
[miner-1] 2025/02/21 10:30:26 🌟 Jump 2/3: barnard -> Sirius
[miner-1] 2025/02/21 10:30:26    Initiating jump...
[miner-1] 2025/02/21 10:30:51    ✓ Travel complete (now in sirius)
[miner-1] 2025/02/21 10:30:51 ✓ Arrived at Sirius
[miner-1] 2025/02/21 10:30:52 🌟 Jump 3/3: sirius -> Sol
[miner-1] 2025/02/21 10:30:52    Initiating jump...
[miner-1] 2025/02/21 10:31:17    ✓ Travel complete (now in sol)
[miner-1] 2025/02/21 10:31:17 ✓ Arrived at Sol
[miner-1] 2025/02/21 10:31:17 ✓ Successfully reached capital system Sol
[miner-1] 2025/02/21 10:31:17 🏢 Base found: Sol Base
[miner-1] 2025/02/21 10:31:17 🛤️  Traveling to base...
[miner-1] 2025/02/21 10:31:20    ✓ Travel complete (now in sol_base)
[miner-1] 2025/02/21 10:31:20 ✓ Arrived at base location
[miner-1] 2025/02/21 10:31:20 🔄 Docking at base...
[miner-1] 2025/02/21 10:31:23 ✓ Docked successfully
[miner-1] 2025/02/21 10:31:23 ✅ Recall complete! Agent is now docked at Sol base
```

## Implementation Details

- Uses `find_route` API for pathfinding
- Monitors `state_update` server messages to track travel progress
- Waits for each jump to complete (approximately 25 seconds per jump)
- Handles edge cases like:
  - Already docked (undocks first)
  - Already at capital (skips jumps)
  - Already in transit (waits for completion)
  - Already at base location (skips travel)

## Dependencies

- `pkg/game` - Game client and state management
- `pkg/agent` - Agent utilities (EmpireCapitalSystem)
