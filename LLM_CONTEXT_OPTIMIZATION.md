# LLM Context Optimization: Removed Full Galaxy Map

## Problem
The LLM prompt was including all 505+ galaxy systems in the context, which:
- Wasted massive amounts of tokens
- Slowed down LLM processing
- Provided unnecessary information (most systems are irrelevant to current decisions)
- Increased costs without improving decision quality

## Solution
Modified `pkg/agent/base.go` to only include **relevant systems** in the LLM context:

### What's Included Now

1. **Current System** - Where the agent is now
2. **Connected Systems** - Direct neighbors (jump destinations)
3. **Recent Systems** - Up to 10 recently visited systems
4. **Total**: ~10-20 systems instead of 505+

### Changes Made

#### File: `pkg/agent/base.go`

**Function: `buildKnowledgeContext()`** (line 280-313)
- Added filtering logic to only include relevant systems
- Builds a map of relevant system IDs
- Filters the full system list to only those IDs
- Reduces context from 505 systems to ~10-20 systems

**Function: `buildFallbackPrompt()`** (line 445-533)
- Same filtering logic applied
- Added note in prompt: "(showing relevant systems)"
- Prevents dumping all systems in fallback prompt

### Filtering Logic

```go
// Build a set of relevant system IDs
relevantSystemIDs := make(map[string]bool)
maxRecentSystems := 10

// Always include current system
relevantSystemIDs[state.System.ID] = true

// Include connected systems (neighbors)
for _, conn := range state.System.Connections {
    relevantSystemIDs[conn.SystemID] = true
}

// Include recently visited systems
experiences, _ := a.memory.GetRecentExperiences(maxRecentSystems)
for _, exp := range experiences {
    if exp.Location != "" {
        // Match location to known system
        for _, sys := range systems {
            if sys.Name == exp.Location || sys.ID == exp.Location {
                relevantSystemIDs[sys.ID] = true
                break
            }
        }
    }
}

// Filter to only relevant systems
filteredSystems := make([]game.SystemData, 0, len(relevantSystemIDs))
for _, sys := range systems {
    if relevantSystemIDs[sys.ID] {
        filteredSystems = append(filteredSystems, sys)
    }
}
```

## Benefits

### Before
- **Systems sent**: 505+
- **Tokens used**: ~5,000-10,000 tokens for system list alone
- **Relevance**: 95%+ of systems irrelevant to current decision
- **Cost**: High token usage = higher LLM costs

### After
- **Systems sent**: ~10-20 (relevant only)
- **Tokens used**: ~100-200 tokens for system list
- **Relevance**: 100% of systems are actionable (current, neighbors, or recent)
- **Cost**: 95%+ reduction in system-related tokens

## Impact on Decision Quality

**No negative impact expected** because:
- Agent can still access any system via memory/database queries
- Most decisions only need current + nearby systems
- Long-range navigation can query for specific systems as needed
- The knowledge base still contains all 505 systems - just not in the prompt

## Example Output

### Before (LLM Context)
```
Known Systems: 505
  - Sol (sol)
  - Frontier (frontier)
  - Nexus Prime (nexus)
  - Haven (haven)
  - Krynn (krynn)
  - ... 500 more systems ...
```

### After (LLM Context)
```
Known Systems: 15 (showing relevant systems)
  - Sol (sol)                    # Current system
  - Krynn (krynn)                # Connected/neighbor
  - Nexus Prime (nexus)          # Connected/neighbor
  - ... 12 more recent systems ...
```

## Testing

Compile and run:
```bash
go build ./pkg/agent/...
go test ./pkg/agent/...
```

## Future Improvements

1. **Smart Relevance Scoring**: Rank systems by relevance (distance, resources, etc.)
2. **Dynamic Limits**: Adjust number of systems based on task complexity
3. **System Summaries**: Instead of full details, provide brief summaries
4. **Lazy Loading**: Query systems on-demand when agent needs specific information

## Files Modified

- `pkg/agent/base.go` - Two functions modified
  - `buildKnowledgeContext()` - Filters systems for template context
  - `buildFallbackPrompt()` - Filters systems for fallback prompt

## Backward Compatibility

✅ Fully backward compatible - no API changes, only internal optimization
