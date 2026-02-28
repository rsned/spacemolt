# Travel Skill

Navigate to any destination system with optional POI docking.

## Parameters

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| destination_system | string | Yes | Target system ID |
| destination_poi | string | No | POI name to dock at |

## Usage

```go
params := map[string]string{
    "destination_system": "haven",
    "destination_poi": "Haven Station",
}
executor.RunWithParams(ctx, "travel", params)
```

## Behavior

1. Checks for saved route (resume after disconnect)
2. Plans route if needed
3. Checks fuel, refuels if docked
4. Executes route with progress saving
5. Finds and travels to POI if specified
6. Docks if POI is station/base

## Route Persistence

The travel skill automatically saves route progress to `data/agents/{agent-id}/route.json`.
This allows the agent to resume travel after a disconnect or restart. The saved route includes:

- Destination system and POI
- Full route with all intermediate systems
- Current step in the route
- Timestamp of last progress update

## Example

```yaml
# Travel to Haven system and dock at Haven Station
destination_system: haven
destination_poi: Haven Station
```

## Error Handling

- If fuel is low and not docked, travel will fail
- If route cannot be found, travel will fail
- If POI is not found in destination system, travel will complete without docking
- Progress is saved after each jump for recovery
