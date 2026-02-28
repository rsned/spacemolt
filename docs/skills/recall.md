# Recall Skill

Return agent to empire capital system and dock at home base.

## Capital Systems

| Empire | Capital System |
|--------|----------------|
| Solarian | Sol |
| Crimson | Krynn |
| Nebula | Haven |
| Voidborn | Nexus Prime |
| Outerrim | Frontier |

## Usage

```go
executor.Run(ctx, "recall")
```

## Behavior

1. Determine capital system from player's empire
2. Plan route to capital (multi-jump if needed)
3. Execute route with progress saving
4. Dock at home base if set
5. Dock at any station if home base not set

## Example

```go
// Agent will automatically navigate to their empire's capital
err := executor.Run(ctx, "recall")
if err != nil {
    log.Printf("Recall failed: %v", err)
}
```

## Route Persistence

Like the travel skill, recall saves route progress to `data/agents/{agent-id}/route.json`.
This allows recovery from disconnects during the return journey.

## Error Handling

- If agent is already at capital, skill completes immediately
- If fuel is insufficient, recall will attempt to refuel if docked
- If home base is not set, will dock at any available station
- Progress is saved after each jump for recovery
