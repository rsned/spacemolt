# Travel & Recall Skills Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement YAML-based travel and recall navigation skills with persistence, fuel management, and disconnect recovery.

**Architecture:** Extend the existing skill system with new actions, expression variables, and functions. Implement route persistence via JSON files and agent memory. Create two YAML skills: travel.yaml (general navigation) and recall.yaml (return to capital).

**Tech Stack:** Go 1.24, YAML skill definitions, SQLite knowledge base, file system persistence

---

## Task 1: Add New Expression Variables

**Files:**
- Modify: `pkg/skills/evaluator.go`
- Test: `pkg/skills/evaluator_test.go`

**Step 1: Write failing tests for new variables**

```go
func TestExpressionVariables_CurrentSystem(t *testing.T) {
    state := &game.State{
        System: game.SystemData{ID: "test-system-1"},
    }
    result, err := EvalExpr("current_system == 'test-system-1'", state)
    if err != nil {
        t.Fatalf("EvalExpr failed: %v", err)
    }
    if !result {
        t.Error("Expected current_system to match")
    }
}

func TestExpressionVariables_PlayerEmpire(t *testing.T) {
    state := &game.State{
        Player: game.Player{Empire: "Solarian"},
    }
    result, err := EvalExpr("player_empire == 'solarian'", state)
    if err != nil {
        t.Fatalf("EvalExpr failed: %v", err)
    }
    if !result {
        t.Error("Expected player_empire to be lowercased")
    }
}

func TestExpressionVariables_FuelMaxJumps(t *testing.T) {
    state := &game.State{
        Fuel: 15.0,
    }
    result, err := EvalExpr("fuel_max_jumps == 5", state)
    if err != nil {
        t.Fatalf("EvalExpr failed: %v", err)
    }
    if !result {
        t.Error("Expected fuel_max_jumps to be 5 (15/3)")
    }
}

func TestExpressionVariables_CapitalSystemID(t *testing.T) {
    state := &game.State{
        Player: game.Player{Empire: "Solarian"},
    }
    result, err := EvalExpr("capital_system_id == 'sol'", state)
    if err != nil {
        t.Fatalf("EvalExpr failed: %v", err)
    }
    if !result {
        t.Error("Expected capital_system_id to be 'sol' for Solarian")
    }
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./pkg/skills/evaluator_test.go ./pkg/skills/evaluator.go -v -run "TestExpressionVariables_"`
Expected: FAIL with "unknown variable" errors

**Step 3: Implement new expression variables**

Add to the `evalExpr` function or variable lookup table in `pkg/skills/evaluator.go`:

```go
// In the variable lookup switch or map, add:
case "current_system":
    return state.System.ID, nil
case "player_empire":
    return strings.ToLower(state.Player.Empire), nil
case "fuel_max_jumps":
    return int(state.Fuel / 3.0), nil
case "capital_system_id":
    // Import empireCapitalSystem from pkg/agent if needed
    return empireCapitalSystem(state.Player.Empire), nil
```

Add helper function if not accessible:

```go
// empireCapitalSystem returns the capital system ID for an empire
func empireCapitalSystem(empire string) string {
    switch strings.ToLower(empire) {
    case "solarian":
        return "sol"
    case "crimson":
        return "krynn"
    case "nebula":
        return "haven"
    case "voidborn":
        return "nexus"
    case "outerrim":
        return "frontier"
    default:
        return ""
    }
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./pkg/skills/evaluator_test.go ./pkg/skills/evaluator.go -v -run "TestExpressionVariables_"`
Expected: PASS

**Step 5: Commit**

```bash
git add pkg/skills/evaluator.go pkg/skills/evaluator_test.go
git commit -m "feat(skills): add navigation expression variables

- Add current_system, player_empire, fuel_max_jumps variables
- Add capital_system_id derived from player empire
- Lowercase empire names for case-insensitive comparison

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

## Task 2: Add New Expression Functions

**Files:**
- Modify: `pkg/skills/evaluator.go`
- Test: `pkg/skills/evaluator_test.go`

**Step 1: Write failing tests for new functions**

```go
func TestFunction_FuelSufficientForJumps(t *testing.T) {
    state := &game.State{Fuel: 12.0}

    // Exactly enough
    result, err := EvalExpr("fuel_sufficient_for_jumps(4)", state)
    if err != nil {
        t.Fatalf("EvalExpr failed: %v", err)
    }
    if !result {
        t.Error("Expected 12 fuel to be sufficient for 4 jumps")
    }

    // Not enough
    result, err = EvalExpr("fuel_sufficient_for_jumps(5)", state)
    if err != nil {
        t.Fatalf("EvalExpr failed: %v", err)
    }
    if result {
        t.Error("Expected 12 fuel to be insufficient for 5 jumps")
    }
}

func TestFunction_AtSystem(t *testing.T) {
    state := &game.State{
        System: game.SystemData{ID: "sol"},
    }
    result, err := EvalExpr("at_system('sol')", state)
    if err != nil {
        t.Fatalf("EvalExpr failed: %v", err)
    }
    if !result {
        t.Error("Expected at_system to return true for matching system")
    }

    result, err = EvalExpr("at_system('haven')", state)
    if err != nil {
        t.Fatalf("EvalExpr failed: %v", err)
    }
    if result {
        t.Error("Expected at_system to return false for different system")
    }
}

func TestFunction_POIIsDockable(t *testing.T) {
    state := &game.State{
        System: game.SystemData{
            POIs: []game.POI{
                {ID: "station-1", Type: "station"},
            },
        },
    }
    state.Ship.DockedAt = "station-1"

    result, err := EvalExpr("poi_is_dockable()", state)
    if err != nil {
        t.Fatalf("EvalExpr failed: %v", err)
    }
    if !result {
        t.Error("Expected poi_is_dockable to return true for station")
    }
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./pkg/skills/evaluator_test.go ./pkg/skills/evaluator.go -v -run "TestFunction_"`
Expected: FAIL with "unknown function" errors

**Step 3: Implement new functions**

Add function parser to handle function calls in `pkg/skills/evaluator.go`:

```go
// parseFunctionCall handles function(arg1, arg2, ...) expressions
func parseFunctionCall(expr string, state *game.State) (bool, error) {
    // Match: function_name(args)
    openParen := strings.Index(expr, "(")
    closeParen := strings.LastIndex(expr, ")")
    if openParen == -1 || closeParen == -1 {
        return false, fmt.Errorf("invalid function call: %s", expr)
    }

    funcName := strings.TrimSpace(expr[:openParen])
    argsStr := strings.TrimSpace(expr[openParen+1 : closeParen])

    // Parse arguments
    var args []string
    if argsStr != "" {
        args = strings.Split(argsStr, ",")
        for i := range args {
            args[i] = strings.TrimSpace(args[i])
        }
    }

    // Dispatch to handler
    switch funcName {
    case "fuel_sufficient_for_jumps":
        return fuelSufficientForJumps(args, state)
    case "at_system":
        return atSystem(args, state)
    case "poi_is_dockable":
        return poiIsDockable(state)
    default:
        return false, fmt.Errorf("unknown function: %s", funcName)
    }
}

func fuelSufficientForJumps(args []string, state *game.State) (bool, error) {
    if len(args) != 1 {
        return false, fmt.Errorf("fuel_sufficient_for_jumps requires 1 argument")
    }
    jumps, err := strconv.Atoi(args[0])
    if err != nil {
        return false, fmt.Errorf("invalid jump count: %s", args[0])
    }
    requiredFuel := float64(jumps) * 3.0
    return state.Fuel >= requiredFuel, nil
}

func atSystem(args []string, state *game.State) (bool, error) {
    if len(args) != 1 {
        return false, fmt.Errorf("at_system requires 1 argument")
    }
    targetSystem := args[0]
    return state.System.ID == targetSystem, nil
}

func poiIsDockable(state *game.State) (bool, error) {
    if state.Ship.DockedAt == "" {
        return false, nil
    }
    for _, poi := range state.System.POIs {
        if poi.ID == state.Ship.DockedAt {
            return poi.Type == "station" || poi.Type == "base", nil
        }
    }
    return false, nil
}
```

Integrate into `EvalExpr`:

```go
// Check if expression is a function call
if strings.Contains(expr, "(") && strings.HasSuffix(expr, ")") {
    return parseFunctionCall(expr, state)
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./pkg/skills/evaluator_test.go ./pkg/skills/evaluator.go -v -run "TestFunction_"`
Expected: PASS

**Step 5: Commit**

```bash
git add pkg/skills/evaluator.go pkg/skills/evaluator_test.go
git commit -m "feat(skills): add navigation expression functions

- Add fuel_sufficient_for_jumps(n) for fuel range checks
- Add at_system(system_id) for system comparison
- Add poi_is_dockable() for POI type checking
- Parse function call syntax: name(arg1, arg2)

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

## Task 3: Create Route Persistence Types

**Files:**
- Create: `pkg/skills/route_persistence.go`
- Test: `pkg/skills/route_persistence_test.go`

**Step 1: Write failing tests for persistence**

```go
func TestRouteProgress_SaveAndLoad(t *testing.T) {
    tmpDir := t.TempDir()
    agentID := "test-agent"

    route := &RouteProgress{
        DestinationSystem: "haven",
        DestinationPOI:    "Haven Station",
        Route: []RouteStep{
            {SystemID: "sol", Name: "Sol", Jumps: 1},
            {SystemID: "haven", Name: "Haven", Jumps: 0},
        },
        CurrentStep: 0,
        Timestamp:   time.Now(),
    }

    // Save
    err := SaveRouteProgress(tmpDir, agentID, route)
    if err != nil {
        t.Fatalf("SaveRouteProgress failed: %v", err)
    }

    // Load
    loaded, err := LoadRouteProgress(tmpDir, agentID)
    if err != nil {
        t.Fatalf("LoadRouteProgress failed: %v", err)
    }

    if loaded.DestinationSystem != route.DestinationSystem {
        t.Errorf("DestinationSystem = %s, want %s", loaded.DestinationSystem, route.DestinationSystem)
    }
    if len(loaded.Route) != len(route.Route) {
        t.Errorf("Route length = %d, want %d", len(loaded.Route), len(route.Route))
    }
}

func TestRouteProgress_Clear(t *testing.T) {
    tmpDir := t.TempDir()
    agentID := "test-agent"

    route := &RouteProgress{
        DestinationSystem: "haven",
        Route:             []RouteStep{{SystemID: "sol", Name: "Sol", Jumps: 1}},
        CurrentStep:       0,
        Timestamp:         time.Now(),
    }

    SaveRouteProgress(tmpDir, agentID, route)
    ClearRouteProgress(tmpDir, agentID)

    // Should return error
    _, err := LoadRouteProgress(tmpDir, agentID)
    if err == nil {
        t.Error("Expected error after clearing route")
    }
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./pkg/skills/ -v -run "TestRouteProgress_"`
Expected: FAIL with "undefined" errors

**Step 3: Implement persistence types and functions**

Create `pkg/skills/route_persistence.go`:

```go
package skills

import (
    "encoding/json"
    "fmt"
    "os"
    "path/filepath"
    "time"
)

// RouteProgress stores navigation state for disconnect recovery
type RouteProgress struct {
    DestinationSystem string     `json:"destination_system"`
    DestinationPOI    string     `json:"destination_poi,omitempty"`
    Route             []RouteStep `json:"route"`
    CurrentStep       int         `json:"current_step"`
    Timestamp         time.Time   `json:"timestamp"`
}

// RouteStep represents one system in a multi-jump route
type RouteStep struct {
    SystemID string `json:"system_id"`
    Name     string `json:"name"`
    Jumps    int    `json:"jumps"`
}

// SaveRouteProgress writes route state to agent's route.json
func SaveRouteProgress(baseDir, agentID string, route *RouteProgress) error {
    if route == nil {
        return fmt.Errorf("route cannot be nil")
    }

    agentDir := filepath.Join(baseDir, "agents", agentID)
    if err := os.MkdirAll(agentDir, 0755); err != nil {
        return fmt.Errorf("create agent dir: %w", err)
    }

    routeFile := filepath.Join(agentDir, "route.json")
    route.Timestamp = time.Now()

    data, err := json.MarshalIndent(route, "", "  ")
    if err != nil {
        return fmt.Errorf("marshal route: %w", err)
    }

    if err := os.WriteFile(routeFile, data, 0644); err != nil {
        return fmt.Errorf("write route file: %w", err)
    }

    return nil
}

// LoadRouteProgress reads route state from agent's route.json
func LoadRouteProgress(baseDir, agentID string) (*RouteProgress, error) {
    routeFile := filepath.Join(baseDir, "agents", agentID, "route.json")

    data, err := os.ReadFile(routeFile)
    if err != nil {
        return nil, fmt.Errorf("read route file: %w", err)
    }

    var route RouteProgress
    if err := json.Unmarshal(data, &route); err != nil {
        return nil, fmt.Errorf("unmarshal route: %w", err)
    }

    return &route, nil
}

// ClearRouteProgress removes the agent's route.json file
func ClearRouteProgress(baseDir, agentID string) error {
    routeFile := filepath.Join(baseDir, "agents", agentID, "route.json")

    if err := os.Remove(routeFile); err != nil && !os.IsNotExist(err) {
        return fmt.Errorf("remove route file: %w", err)
    }

    return nil
}

// HasRouteProgress checks if an agent has a saved route
func HasRouteProgress(baseDir, agentID string) bool {
    routeFile := filepath.Join(baseDir, "agents", agentID, "route.json")
    _, err := os.Stat(routeFile)
    return err == nil
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./pkg/skills/ -v -run "TestRouteProgress_"`
Expected: PASS

**Step 5: Commit**

```bash
git add pkg/skills/route_persistence.go pkg/skills/route_persistence_test.go
git commit -m "feat(skills): add route persistence layer

- Add RouteProgress and RouteStep types
- Implement SaveRouteProgress to agent route.json
- Implement LoadRouteProgress with JSON unmarshaling
- Implement ClearRouteProgress and HasRouteProgress
- Support disconnect recovery for multi-jump routes

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

## Task 4: Add find_route Action

**Files:**
- Modify: `pkg/skills/client_dispatcher.go`
- Test: `pkg/skills/client_dispatcher_test.go`

**Step 1: Write failing test for find_route**

```go
func TestDispatch_FindRoute(t *testing.T) {
    mockClient := &MockGameClient{
        state: &game.State{
            System: game.SystemData{ID: "sol"},
        },
        routes: map[string][]game.RouteStep{
            "haven": {
                {SystemID: "haven", Name: "Haven", Jumps: 1},
            },
        },
    }

    dispatcher := NewClientDispatcher(mockClient, nil)
    ctx := context.Background()

    // Action: find_route with target_system parameter
    err := dispatcher.Dispatch(ctx, "find_route", "haven")
    if err != nil {
        t.Fatalf("Dispatch failed: %v", err)
    }

    // Verify route was stored
    if dispatcher.Route == nil {
        t.Error("Expected route to be stored in dispatcher")
    }
    if dispatcher.Route.DestinationSystem != "haven" {
        t.Errorf("Destination = %s, want haven", dispatcher.Route.DestinationSystem)
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/skills/ -v -run "TestDispatch_FindRoute"`
Expected: FAIL with route field missing

**Step 3: Extend ClientDispatcher with route storage**

Modify `pkg/skills/client_dispatcher.go`:

```go
// ClientDispatcher executes game actions via GameClient
type ClientDispatcher struct {
    client    game.GameClient
    agentID   string
    baseDir   string
    logger    *log.Logger
    Route     *RouteProgress  // Add this field
    // ... existing fields
}

// Add to NewClientDispatcher:
func NewClientDispatcher(client game.GameClient, agentID, baseDir string, logger *log.Logger) *ClientDispatcher {
    return &ClientDispatcher{
        client:  client,
        agentID: agentID,
        baseDir: baseDir,
        logger:  logger,
        // ... other initialization
    }
}
```

**Step 4: Add find_route action handler**

```go
func (d *ClientDispatcher) Dispatch(ctx context.Context, action, target string) error {
    // ... existing cases ...

    case "find_route":
        return d.doFindRoute(ctx, target)

    // ... existing cases ...
}

func (d *ClientDispatcher) doFindRoute(ctx context.Context, targetSystem string) error {
    route, err := d.client.FindRoute(ctx, targetSystem)
    if err != nil {
        return fmt.Errorf("find route to %s: %w", targetSystem, err)
    }

    // Convert to RouteStep format
    steps := make([]RouteStep, len(route))
    for i, r := range route {
        steps[i] = RouteStep{
            SystemID: r.SystemID,
            Name:     r.Name,
            Jumps:    r.Jumps,
        }
    }

    d.Route = &RouteProgress{
        DestinationSystem: targetSystem,
        Route:             steps,
        CurrentStep:       0,
        Timestamp:         time.Now(),
    }

    d.logger.Printf("[route] Found route to %s (%d steps)", targetSystem, len(steps))
    return nil
}
```

**Step 5: Run test to verify it passes**

Run: `go test ./pkg/skills/ -v -run "TestDispatch_FindRoute"`
Expected: PASS

**Step 6: Commit**

```bash
git add pkg/skills/client_dispatcher.go pkg/skills/client_dispatcher_test.go
git commit -m "feat(skills): add find_route action

- Add Route field to ClientDispatcher
- Implement find_route action handler
- Convert game.RouteStep to skills.RouteStep
- Store route in dispatcher for use by persistence actions

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

## Task 5: Add store_route_progress Action

**Files:**
- Modify: `pkg/skills/client_dispatcher.go`
- Test: `pkg/skills/client_dispatcher_test.go`

**Step 1: Write failing test**

```go
func TestDispatch_StoreRouteProgress(t *testing.T) {
    tmpDir := t.TempDir()
    agentID := "test-agent"

    mockClient := &MockGameClient{
        state: &game.State{System: game.SystemData{ID: "sol"}},
    }

    dispatcher := NewClientDispatcher(mockClient, agentID, tmpDir, nil)
    dispatcher.Route = &RouteProgress{
        DestinationSystem: "haven",
        Route: []RouteStep{{SystemID: "haven", Name: "Haven", Jumps: 1}},
        CurrentStep:       0,
    }

    ctx := context.Background()
    err := dispatcher.Dispatch(ctx, "store_route_progress", "")
    if err != nil {
        t.Fatalf("Dispatch failed: %v", err)
    }

    // Verify file exists
    if !HasRouteProgress(tmpDir, agentID) {
        t.Error("Expected route progress to be saved")
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/skills/ -v -run "TestDispatch_StoreRouteProgress"`
Expected: FAIL

**Step 3: Implement store_route_progress action**

```go
func (d *ClientDispatcher) Dispatch(ctx context.Context, action, target string) error {
    // ... existing cases ...

    case "store_route_progress":
        return d.doStoreRouteProgress()

    // ... existing cases ...
}

func (d *ClientDispatcher) doStoreRouteProgress() error {
    if d.Route == nil {
        return fmt.Errorf("no route to store")
    }

    if err := SaveRouteProgress(d.baseDir, d.agentID, d.Route); err != nil {
        return fmt.Errorf("save route progress: %w", err)
    }

    d.logger.Printf("[route] Saved progress (step %d/%d)",
        d.Route.CurrentStep, len(d.Route.Route))
    return nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./pkg/skills/ -v -run "TestDispatch_StoreRouteProgress"`
Expected: PASS

**Step 5: Commit**

```bash
git add pkg/skills/client_dispatcher.go pkg/skills/client_dispatcher_test.go
git commit -m "feat(skills): add store_route_progress action

- Implement store_route_progress action
- Save current route to agent's route.json
- Log step count on save

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

## Task 6: Add load_route_progress Action

**Files:**
- Modify: `pkg/skills/client_dispatcher.go`
- Test: `pkg/skills/client_dispatcher_test.go`

**Step 1: Write failing test**

```go
func TestDispatch_LoadRouteProgress(t *testing.T) {
    tmpDir := t.TempDir()
    agentID := "test-agent"

    // Pre-save a route
    route := &RouteProgress{
        DestinationSystem: "haven",
        Route: []RouteStep{{SystemID: "haven", Name: "Haven", Jumps: 1}},
        CurrentStep:       0,
    }
    SaveRouteProgress(tmpDir, agentID, route)

    mockClient := &MockGameClient{
        state: &game.State{System: game.SystemData{ID: "sol"}},
    }

    dispatcher := NewClientDispatcher(mockClient, agentID, tmpDir, nil)
    ctx := context.Background()

    err := dispatcher.Dispatch(ctx, "load_route_progress", "")
    if err != nil {
        t.Fatalf("Dispatch failed: %v", err)
    }

    if dispatcher.Route == nil {
        t.Error("Expected route to be loaded")
    }
    if dispatcher.Route.DestinationSystem != "haven" {
        t.Errorf("Destination = %s, want haven", dispatcher.Route.DestinationSystem)
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/skills/ -v -run "TestDispatch_LoadRouteProgress"`
Expected: FAIL

**Step 3: Implement load_route_progress action**

```go
func (d *ClientDispatcher) Dispatch(ctx context.Context, action, target string) error {
    // ... existing cases ...

    case "load_route_progress":
        return d.doLoadRouteProgress()

    // ... existing cases ...
}

func (d *ClientDispatcher) doLoadRouteProgress() error {
    route, err := LoadRouteProgress(d.baseDir, d.agentID)
    if err != nil {
        return fmt.Errorf("load route progress: %w", err)
    }

    // Validate route against current position
    state := d.client.GetState()
    if len(route.Route) > 0 && route.CurrentStep < len(route.Route) {
        expectedSystem := route.Route[route.CurrentStep].SystemID
        if state.System.ID != expectedSystem {
            // Try to find our position in the route
            found := false
            for i, step := range route.Route {
                if step.SystemID == state.System.ID {
                    route.CurrentStep = i
                    found = true
                    d.logger.Printf("[route] Adjusted step to %d based on current position", i)
                    break
                }
            }
            if !found {
                return fmt.Errorf("current system %s not in saved route", state.System.ID)
            }
        }
    }

    d.Route = route
    d.logger.Printf("[route] Loaded progress (step %d/%d to %s)",
        route.CurrentStep, len(route.Route), route.DestinationSystem)
    return nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./pkg/skills/ -v -run "TestDispatch_LoadRouteProgress"`
Expected: PASS

**Step 5: Commit**

```bash
git add pkg/skills/client_dispatcher.go pkg/skills/client_dispatcher_test.go
git commit -m "feat(skills): add load_route_progress action

- Implement load_route_progress action
- Load route from agent's route.json
- Validate and adjust current_step based on position
- Return error if current system not in route

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

## Task 7: Add clear_route_progress Action

**Files:**
- Modify: `pkg/skills/client_dispatcher.go`
- Test: `pkg/skills/client_dispatcher_test.go`

**Step 1: Write failing test**

```go
func TestDispatch_ClearRouteProgress(t *testing.T) {
    tmpDir := t.TempDir()
    agentID := "test-agent"

    // Pre-save a route
    route := &RouteProgress{
        DestinationSystem: "haven",
        Route: []RouteStep{{SystemID: "haven", Name: "Haven", Jumps: 1}},
    }
    SaveRouteProgress(tmpDir, agentID, route)

    mockClient := &MockGameClient{
        state: &game.State{System: game.SystemData{ID: "sol"}},
    }

    dispatcher := NewClientDispatcher(mockClient, agentID, tmpDir, nil)
    ctx := context.Background()

    err := dispatcher.Dispatch(ctx, "clear_route_progress", "")
    if err != nil {
        t.Fatalf("Dispatch failed: %v", err)
    }

    if HasRouteProgress(tmpDir, agentID) {
        t.Error("Expected route progress to be cleared")
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/skills/ -v -run "TestDispatch_ClearRouteProgress"`
Expected: FAIL

**Step 3: Implement clear_route_progress action**

```go
func (d *ClientDispatcher) Dispatch(ctx context.Context, action, target string) error {
    // ... existing cases ...

    case "clear_route_progress":
        return d.doClearRouteProgress()

    // ... existing cases ...
}

func (d *ClientDispatcher) doClearRouteProgress() error {
    if err := ClearRouteProgress(d.baseDir, d.agentID); err != nil {
        return fmt.Errorf("clear route progress: %w", err)
    }

    d.Route = nil
    d.logger.Printf("[route] Cleared progress")
    return nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./pkg/skills/ -v -run "TestDispatch_ClearRouteProgress"`
Expected: PASS

**Step 5: Commit**

```bash
git add pkg/skills/client_dispatcher.go pkg/skills/client_dispatcher_test.go
git commit -m "feat(skills): add clear_route_progress action

- Implement clear_route_progress action
- Remove agent's route.json file
- Clear route from dispatcher state

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

## Task 8: Add find_poi_in_system Action

**Files:**
- Modify: `pkg/skills/client_dispatcher.go`
- Test: `pkg/skills/client_dispatcher_test.go`

**Step 1: Write failing test**

```go
func TestDispatch_FindPOIInSystem(t *testing.T) {
    mockClient := &MockGameClient{
        state: &game.State{
            System: game.SystemData{
                POIs: []game.POI{
                    {ID: "poi-1", Name: "Trading Post", Type: "station"},
                    {ID: "poi-2", Name: "Asteroid Belt", Type: "asteroid"},
                },
            },
        },
    }

    dispatcher := NewClientDispatcher(mockClient, "test", "", nil)
    ctx := context.Background()

    err := dispatcher.Dispatch(ctx, "find_poi_in_system", "Trading Post")
    if err != nil {
        t.Fatalf("Dispatch failed: %v", err)
    }

    if dispatcher.FoundPOI != "poi-1" {
        t.Errorf("FoundPOI = %s, want poi-1", dispatcher.FoundPOI)
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/skills/ -v -run "TestDispatch_FindPOIInSystem"`
Expected: FAIL (FoundPOI field missing)

**Step 3: Add FoundPOI field to dispatcher**

```go
type ClientDispatcher struct {
    client    game.GameClient
    agentID   string
    baseDir   string
    logger    *log.Logger
    Route     *RouteProgress
    FoundPOI  string  // Add this field
    // ... existing fields
}
```

**Step 4: Implement find_poi_in_system action**

```go
func (d *ClientDispatcher) Dispatch(ctx context.Context, action, target string) error {
    // ... existing cases ...

    case "find_poi_in_system":
        return d.doFindPOIInSystem(target)

    // ... existing cases ...
}

func (d *ClientDispatcher) doFindPOIInSystem(poiName string) error {
    state := d.client.GetState()

    for _, poi := range state.System.POIs {
        if strings.EqualFold(poi.Name, poiName) {
            d.FoundPOI = poi.ID
            d.logger.Printf("[poi] Found POI: %s (%s)", poi.Name, poi.ID)
            return nil
        }
    }

    return fmt.Errorf("POI %q not found in system %s", poiName, state.System.Name)
}
```

**Step 5: Run test to verify it passes**

Run: `go test ./pkg/skills/ -v -run "TestDispatch_FindPOIInSystem"`
Expected: PASS

**Step 6: Commit**

```bash
git add pkg/skills/client_dispatcher.go pkg/skills/client_dispatcher_test.go
git commit -m "feat(skills): add find_poi_in_system action

- Add FoundPOI field to ClientDispatcher
- Implement find_poi_in_system action with case-insensitive search
- Store found POI ID for use by travel/dock actions

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

## Task 9: Add Route State Expression Variables

**Files:**
- Modify: `pkg/skills/evaluator.go`
- Test: `pkg/skills/evaluator_test.go`

**Step 1: Write failing tests**

```go
func TestExpressionVariables_RouteState(t *testing.T) {
    // This test requires access to dispatcher state
    // For now, test the variable resolution logic
    state := &game.State{}
    route := &RouteProgress{
        DestinationSystem: "haven",
        DestinationPOI:    "Haven Station",
        Route:             []RouteStep{{SystemID: "sol"}, {SystemID: "haven"}},
        CurrentStep:       1,
    }

    // Test with context containing route
    result, err := EvalExprWithRoute("route_destination_system == 'haven'", state, route)
    if err != nil {
        t.Fatalf("EvalExpr failed: %v", err)
    }
    if !result {
        t.Error("Expected route_destination_system to match")
    }

    result, err = EvalExprWithRoute("route_step_count == 2", state, route)
    if err != nil {
        t.Fatalf("EvalExpr failed: %v", err)
    }
    if !result {
        t.Error("Expected route_step_count to be 2")
    }

    result, err = EvalExprWithRoute("route_current_step == 1", state, route)
    if err != nil {
        t.Fatalf("EvalExpr failed: %v", err)
    }
    if !result {
        t.Error("Expected route_current_step to be 1")
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/skills/ -v -run "TestExpressionVariables_RouteState"`
Expected: FAIL

**Step 3: Extend evaluator with route context**

```go
// Add new function that accepts route context
func EvalExprWithRoute(expr string, state *game.State, route *RouteProgress) (bool, error) {
    // Store route in a context that's accessible during evaluation
    // For simplicity, we'll extend the variable lookup

    // ... existing logic, add new cases ...
    case "route_destination_system":
        if route == nil {
            return false, nil
        }
        return route.DestinationSystem, nil
    case "route_destination_poi":
        if route == nil {
            return "", nil
        }
        return route.DestinationPOI, nil
    case "route_step_count":
        if route == nil {
            return 0, nil
        }
        return len(route.Route), nil
    case "route_current_step":
        if route == nil {
            return 0, nil
        }
        return route.CurrentStep, nil
    // ...
}
```

**Step 4: Update executor to pass route context**

Modify `pkg/skills/executor.go` to pass route from dispatcher:

```go
func (e *Executor) evalConditions(conditions ConditionList, state *game.State) (string, error) {
    // Get route from dispatcher if it's a ClientDispatcher
    var route *RouteProgress
    if dispatcher, ok := e.dispatcher.(*ClientDispatcher); ok {
        route = dispatcher.Route
    }

    // Use route in condition evaluation...
}
```

**Step 5: Run test to verify it passes**

Run: `go test ./pkg/skills/ -v -run "TestExpressionVariables_RouteState"`
Expected: PASS

**Step 6: Commit**

```bash
git add pkg/skills/evaluator.go pkg/skills/evaluator_test.go pkg/skills/executor.go
git commit -m "feat(skills): add route state expression variables

- Add route_destination_system, route_destination_poi variables
- Add route_step_count, route_current_step variables
- Extend EvalExpr to accept optional route context
- Update executor to pass route from dispatcher

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

## Task 10: Create travel.yaml Skill

**Files:**
- Create: `data/skills/travel.yaml`

**Step 1: Create travel.yaml**

```yaml
name: travel
description: >
  Navigate to a destination system and optionally dock at a named POI.
  Supports resume after disconnect via route persistence.

prerequisites:
  - at_poi_type(station, base) OR not docked

# Destination provided via:
# 1. Stored route from previous run (resume)
# 2. Skill invocation parameter (future)
# 3. For now, assume destination is set in dispatcher context

steps:
  # Check for existing route progress
  - id: check_resume
    check: true
    conditions:
      has_route_progress(): goto validate_resume
      default: goto plan_route

  # Try to load saved route
  - id: validate_resume
    action: load_route_progress
    conditions:
      route_current_step < route_step_count: goto fuel_check
      default: goto plan_route  # Route complete or invalid, replan

  # Plan fresh route
  - id: plan_route
    action: find_route
    # Note: destination needs to be passed via context/parameter
    next: store_and_fuel_check

  - id: store_and_fuel_check
    action: store_route_progress
    next: fuel_check

  # Fuel pre-flight check
  - id: fuel_check
    check: true
    conditions:
      fuel_sufficient_for_jumps(route_step_count - route_current_step): goto begin_route
      docked AND fuel_pct < 1.0: goto refuel_now
      default: goto fail_no_fuel

  # Refuel if possible
  - id: refuel_now
    action: refuel
    next: fuel_check

  # Fail if can't proceed
  - id: fail_no_fuel
    terminal: true

  # Begin route execution
  - id: begin_route
    check: true
    conditions:
      route_current_step >= route_step_count: goto route_complete
      default: goto check_need_jump

  # Check if we need to jump or already at target
  - id: check_need_jump
    check: true
    conditions:
      at_system(route_destination_system): goto route_complete
      default: goto do_jump

  # Execute jump and save progress
  - id: do_jump
    action: jump
    target: $next_system  # This needs to resolve from route
    next: save_progress

  - id: save_progress
    action: store_route_progress
    next: begin_route

  # Route complete, handle POI if specified
  - id: route_complete
    check: true
    conditions:
      route_destination_poi == "": goto done
      default: goto find_poi

  # Find POI in destination system
  - id: find_poi
    action: find_poi_in_system
    target: $destination_poi  # From stored route
    next: travel_to_poi

  # Travel to POI
  - id: travel_to_poi
    action: travel
    target: $found_poi  # From dispatcher.FoundPOI
    next: check_dockable

  # Check if we should dock
  - id: check_dockable
    check: true
    conditions:
      poi_is_dockable(): goto dock
      default: goto done

  # Dock at station/base
  - id: dock
    action: dock
    next: done

  # Clear route and complete
  - id: done
    action: clear_route_progress
    terminal: true
```

**Step 2: Validate YAML syntax**

Run: `yamllint data/skills/travel.yaml` or `python -c "import yaml; yaml.safe_load(open('data/skills/travel.yaml'))"`
Expected: No errors

**Step 3: Commit**

```bash
git add data/skills/travel.yaml
git commit -m "feat(skills): add travel.yaml skill

- Implement general navigation skill with persistence
- Support route resume after disconnect
- Pre-flight fuel checks with auto-refuel when docked
- Handle POI docking (station/base) vs arrival (other types)
- Clear route progress on completion

Note: Requires destination parameter mechanism (follow-up)

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

## Task 11: Create recall.yaml Skill

**Files:**
- Create: `data/skills/recall.yaml`

**Step 1: Create recall.yaml**

```yaml
name: recall
description: >
  Return the agent to their empire's capital system and dock at
  the home base. Uses travel.yaml sub-skill for navigation.

prerequisites: []

steps:
  # Check if already at capital and docked
  - id: check_already_home
    check: true
    conditions:
      at_system(capital_system_id) AND docked: goto done
      default: goto prepare_travel

  # Prepare for travel
  - id: prepare_travel
    check: true
    conditions:
      docked: goto undock
      default: goto travel_home

  # Undock if needed
  - id: undock
    action: undock
    next: travel_home

  # Navigate to capital using travel sub-skill
  - id: travel_home
    skill: travel
    # Note: travel needs to know destination is capital_system_id
    # For now, this may need special handling in executor
    next: ensure_docked

  # Ensure we're docked at a base or station
  - id: ensure_docked
    check: true
    conditions:
      docked: goto done
      default: goto find_base

  # Find a base or station in current system
  - id: find_base
    action: find_poi_in_system
    target: $capital_base  # May need parameterization
    next: travel_to_base

  # Travel to the base
  - id: travel_to_base
    action: travel
    target: $found_poi
    next: dock_at_base

  # Dock at the base
  - id: dock_at_base
    action: dock
    next: done

  - id: done
    terminal: true
```

**Step 2: Validate YAML syntax**

Run: `yamllint data/skills/recall.yaml` or equivalent
Expected: No errors

**Step 3: Commit**

```bash
git add data/skills/recall.yaml
git commit -m "feat(skills): add recall.yaml skill

- Implement return-to-capital navigation skill
- Uses travel.yaml as sub-skill for navigation
- Handles undock if currently docked elsewhere
- Ensures docked at base/station in capital system
- Simplifies agent return-to-home behavior

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

## Task 12: Add Skill Parameter Mechanism

**Files:**
- Modify: `pkg/skills/skill.go`
- Modify: `pkg/skills/executor.go`
- Test: `pkg/skills/executor_test.go`

**Step 1: Write failing test for skill parameters**

```go
func TestExecutor_RunWithParameters(t *testing.T) {
    registry := NewRegistry()
    skill := &Skill{
        Name: "test_param",
        Steps: []Step{
            {ID: "check", Check: true, Conditions: ConditionList{
                {Expr: "$destination == 'test'", Goto: "done"},
                {Expr: "default", Goto: "fail"},
            }},
            {ID: "done", Terminal: true},
            {ID: "fail", Terminal: true},
        },
        FirstStep: "check",
    }
    registry.Register(skill)

    mockClient := &MockGameClient{state: &game.State{}}
    dispatcher := NewClientDispatcher(mockClient, "test", "", nil)
    executor := NewExecutor(registry, dispatcher, nil)

    params := map[string]string{"destination": "test"}
    err := executor.RunWithParams(context.Background(), "test_param", params)
    if err != nil {
        t.Fatalf("RunWithParams failed: %v", err)
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/skills/ -v -run "TestExecutor_RunWithParams"`
Expected: FAIL (method doesn't exist)

**Step 3: Add parameter support to Skill type**

```go
// In pkg/skills/skill.go:
type Skill struct {
    Name         string
    Description  string
    Prerequisites []string
    Steps        []Step
    Parameters   []ParameterDefinition  // Add this
    // ...
}

type ParameterDefinition struct {
    Name        string `yaml:"name"`
    Description string `yaml:"description"`
    Required    bool   `yaml:"required"`
    Default     string `yaml:"default,omitempty"`
}
```

**Step 4: Add RunWithParams to executor**

```go
// In pkg/skills/executor.go:
func (e *Executor) RunWithParams(ctx context.Context, skillName string, params map[string]string) error {
    skill := e.registry.Get(skillName)
    if skill == nil {
        return fmt.Errorf("unknown skill: %q", skillName)
    }

    // Store params in dispatcher or context
    if dispatcher, ok := e.dispatcher.(*ClientDispatcher); ok {
        dispatcher.SkillParams = params
    }

    return e.runSkill(ctx, skill)
}
```

**Step 5: Update dispatcher to store parameters**

```go
// In pkg/skills/client_dispatcher.go:
type ClientDispatcher struct {
    client      game.GameClient
    agentID     string
    baseDir     string
    logger      *log.Logger
    Route       *RouteProgress
    FoundPOI    string
    SkillParams map[string]string  // Add this
}
```

**Step 6: Run test to verify it passes**

Run: `go test ./pkg/skills/ -v -run "TestExecutor_RunWithParams"`
Expected: PASS

**Step 7: Commit**

```bash
git add pkg/skills/skill.go pkg/skills/executor.go pkg/skills/client_dispatcher.go pkg/skills/executor_test.go
git commit -m "feat(skills): add skill parameter mechanism

- Add ParameterDefinition type to Skill
- Add SkillParams field to ClientDispatcher
- Implement RunWithParams method for parameterized skills
- Enable travel.yaml to accept destination parameter

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

## Task 13: Update travel.yaml to Use Parameters

**Files:**
- Modify: `data/skills/travel.yaml`

**Step 1: Add parameter definition to travel.yaml**

```yaml
name: travel
description: >
  Navigate to a destination system and optionally dock at a named POI.
  Supports resume after disconnect via route persistence.

parameters:
  - name: destination_system
    description: Target system ID to navigate to
    required: true
  - name: destination_poi
    description: Optional POI name to dock at after arrival
    required: false

prerequisites:
  - at_poi_type(station, base) OR not docked

steps:
  # Check for existing route progress
  - id: check_resume
    check: true
    conditions:
      has_route_progress(): goto validate_resume
      default: goto plan_route

  # Try to load saved route
  - id: validate_resume
    action: load_route_progress
    conditions:
      route_current_step < route_step_count: goto fuel_check
      default: goto plan_route

  # Plan fresh route using parameter
  - id: plan_route
    action: find_route
    target: $destination_system  # From skill parameter
    next: store_and_fuel_check

  # ... rest of steps remain the same ...
```

**Step 2: Validate YAML**

Run: YAML validation
Expected: No errors

**Step 3: Commit**

```bash
git add data/skills/travel.yaml
git commit -m "feat(skills): add parameters to travel.yaml

- Define destination_system and destination_poi parameters
- Use $destination_system in find_route action
- Make destination explicit rather than implicit

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

## Task 14: Update recall.yaml to Pass Parameters

**Files:**
- Modify: `pkg/skills/executor.go`
- Modify: `data/skills/recall.yaml`

**Step 1: Add sub-skill parameter passing to executor**

```go
// In pkg/skills/executor.go:
func (e *Executor) executeStep(ctx context.Context, skill *Step, step *Step) (string, error) {
    // ... existing code ...

    // Sub-skill invocation with parameters
    if step.Skill != "" {
        subSkill := e.registry.Get(step.Skill)
        if subSkill == nil {
            return "", fmt.Errorf("unknown sub-skill: %q", step.Skill)
        }

        // Build parameters from step config
        params := e.buildSubSkillParams(step)

        // Create sub-executor with parameters
        subExec := &Executor{
            registry:   e.registry,
            dispatcher: e.dispatcher,
            logger:     e.logger,
            MaxSteps:   e.MaxSteps,
        }

        if err := subExec.RunWithParams(ctx, step.Skill, params); err != nil {
            return "", fmt.Errorf("sub-skill %q: %w", step.Skill, err)
        }
        return step.Next, nil
    }

    // ...
}

func (e *Executor) buildSubSkillParams(step *Step) map[string]string {
    params := make(map[string]string)
    if dispatcher, ok := e.dispatcher.(*ClientDispatcher); ok {
        // Copy current skill params
        for k, v := range dispatcher.SkillParams {
            params[k] = v
        }
    }
    // Add step-specific overrides (if we add this feature to Step)
    return params
}
```

**Step 2: Update recall.yaml to pass capital_system_id**

```yaml
name: recall
description: >
  Return the agent to their empire's capital system and dock at
  the home base. Uses travel.yaml sub-skill for navigation.

parameters: []

steps:
  # Check if already at capital and docked
  - id: check_already_home
    check: true
    conditions:
      at_system(capital_system_id) AND docked: goto done
      default: goto prepare_travel

  # Prepare for travel
  - id: prepare_travel
    check: true
    conditions:
      docked: goto undock
      default: goto travel_home

  # Undock if needed
  - id: undock
    action: undock
    next: travel_home

  # Navigate to capital using travel sub-skill
  # Note: executor needs to resolve capital_system_id and pass as parameter
  - id: travel_home
    skill: travel
    # Pass capital_system_id as destination_system parameter
    next: ensure_docked

  # ... rest of steps ...
```

**Step 3: Add Step parameter field for sub-skill calls**

```go
// In pkg/skills/skill.go:
type Step struct {
    ID         string
    Check      bool
    Action     string
    Target     string
    Skill      string
    SkillParams map[string]string `yaml:"skill_params,omitempty"`  // Add this
    Repeat     *RepeatClause
    Conditions ConditionList
    Next       string
    Terminal   bool
}
```

**Step 4: Update recall.yaml with explicit parameters**

```yaml
  - id: travel_home
    skill: travel
    skill_params:
      destination_system: $capital_system_id  # Resolved from expression
    next: ensure_docked
```

**Step 5: Update executor to resolve expressions in skill_params**

```go
func (e *Executor) buildSubSkillParams(step *Step) map[string]string {
    state := e.dispatcher.GetState()
    params := make(map[string]string)

    for k, v := range step.SkillParams {
        // Resolve $variables in parameter values
        if strings.HasPrefix(v, "$") {
            varName := strings.TrimPrefix(v, "$")
            // Resolve variable from state
            resolved := e.resolveVariable(varName, state)
            params[k] = resolved
        } else {
            params[k] = v
        }
    }

    return params
}
```

**Step 6: Commit**

```bash
git add pkg/skills/skill.go pkg/skills/executor.go pkg/skills/evaluator.go data/skills/recall.yaml
git commit -m "feat(skills): add sub-skill parameter passing

- Add SkillParams field to Step
- Add buildSubSkillParams to executor
- Resolve $variables in skill parameter values
- Update recall.yaml to pass capital_system_id to travel
- Enable parameterized sub-skill invocation

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

## Task 15: Integration Test - Full Travel Flow

**Files:**
- Create: `pkg/skills/integration_test.go`

**Step 1: Write integration test**

```go
func TestIntegration_TravelSkill(t *testing.T) {
    if testing.Short() {
        t.Skip("integration test")
    }

    // Setup
    tmpDir := t.TempDir()
    registry := NewRegistry()

    // Load skills from data/skills
    if err := registry.LoadFromDir("data/skills"); err != nil {
        t.Fatalf("Load skills: %v", err)
    }

    // Mock client
    mockClient := &MockGameClient{
        state: &game.State{
            Fuel: 30.0,
            System: game.SystemData{
                ID:   "sol",
                Name: "Sol",
                POIs: []game.POI{
                    {ID: "haven-station", Name: "Haven Station", Type: "station"},
                },
            },
            Player: game.Player{Empire: "Solarian"},
            Ship: game.Ship{
                DockedAt: "",
            },
        },
        routes: map[string][]game.RouteStep{
            "haven": {
                {SystemID: "haven", Name: "Haven", Jumps: 1},
            },
        },
    }

    dispatcher := NewClientDispatcher(mockClient, "test-agent", tmpDir, nil)
    executor := NewExecutor(registry, dispatcher, log.New(os.Stderr, "", 0))

    // Execute travel skill
    params := map[string]string{
        "destination_system": "haven",
        "destination_poi":    "Haven Station",
    }

    ctx := context.Background()
    err := executor.RunWithParams(ctx, "travel", params)
    if err != nil {
        t.Fatalf("Travel skill failed: %v", err)
    }

    // Verify route was cleared
    if HasRouteProgress(tmpDir, "test-agent") {
        t.Error("Expected route to be cleared after completion")
    }
}
```

**Step 2: Run integration test**

Run: `go test ./pkg/skills/ -v -run "TestIntegration_TravelSkill"`
Expected: May fail due to missing pieces, iterate to fix

**Step 3: Debug and fix any issues**

Based on test output, fix any missing expression resolvers, action handlers, etc.

**Step 4: Run test again to verify it passes**

Run: `go test ./pkg/skills/ -v -run "TestIntegration_TravelSkill"`
Expected: PASS

**Step 5: Commit**

```bash
git add pkg/skills/integration_test.go
git commit -m "test(skills): add travel skill integration test

- Test full travel flow with parameters
- Mock route finding and POI discovery
- Verify route persistence cleanup
- Test POI docking at destination

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

## Task 16: Integration Test - Resume After Disconnect

**Files:**
- Modify: `pkg/skills/integration_test.go`

**Step 1: Add resume test**

```go
func TestIntegration_TravelResume(t *testing.T) {
    if testing.Short() {
        t.Skip("integration test")
    }

    tmpDir := t.TempDir()

    // Phase 1: Start travel and save progress mid-route
    mockClient1 := &MockGameClient{
        state: &game.State{
            Fuel: 30.0,
            System: game.SystemData{ID: "sol", Name: "Sol"},
            Player: game.Player{Empire: "Solarian"},
        },
        routes: map[string][]game.RouteStep{
            "haven": {
                {SystemID: "sol", Name: "Sol", Jumps: 0},
                {SystemID: "haven", Name: "Haven", Jumps: 1},
            },
        },
    }

    registry := NewRegistry()
    // Load skills...

    dispatcher1 := NewClientDispatcher(mockClient1, "test-agent", tmpDir, nil)
    executor1 := NewExecutor(registry, dispatcher1, nil)

    // Start travel (simulates interruption)
    ctx := context.Background()
    params := map[string]string{"destination_system": "haven"}

    // For this test, manually set up partial route
    route := &RouteProgress{
        DestinationSystem: "haven",
        Route: []RouteStep{
            {SystemID: "sol", Name: "Sol", Jumps: 0},
            {SystemID: "haven", Name: "Haven", Jumps: 1},
        },
        CurrentStep: 0,
    }
    SaveRouteProgress(tmpDir, "test-agent", route)

    // Phase 2: Resume from saved state
    mockClient2 := &MockGameClient{
        state: &game.State{
            Fuel: 27.0,  // After one jump
            System: game.SystemData{
                ID:   "haven",
                Name: "Haven",
                POIs: []game.POI{{ID: "station", Name: "Haven Station", Type: "station"}},
            },
            Player: game.Player{Empire: "Solarian"},
            Ship:   game.Ship{DockedAt: ""},
        },
        routes: mockClient1.routes,
    }

    dispatcher2 := NewClientDispatcher(mockClient2, "test-agent", tmpDir, nil)
    executor2 := NewExecutor(registry, dispatcher2, nil)

    err := executor2.RunWithParams(ctx, "travel", params)
    if err != nil {
        t.Fatalf("Travel resume failed: %v", err)
    }

    // Should have loaded route, recognized we're at destination, and completed
    if HasRouteProgress(tmpDir, "test-agent") {
        t.Error("Expected route to be cleared after completion")
    }
}
```

**Step 2: Run test**

Run: `go test ./pkg/skills/ -v -run "TestIntegration_TravelResume"`
Expected: PASS

**Step 3: Commit**

```bash
git add pkg/skills/integration_test.go
git commit -m "test(skills): add travel resume integration test

- Test route persistence and recovery
- Simulate disconnect mid-journey
- Verify resume from correct position
- Test route validation logic

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

## Task 17: Update Registry to Load New Skills

**Files:**
- Modify: `pkg/skills/registry.go`
- Test: `pkg/skills/registry_test.go`

**Step 1: Verify registry auto-loads new skills**

```bash
# Run the run-skill tool to verify it finds travel and recall
go run cmd/tools/run-skill/main.go list
```

Expected: Should show travel.yaml and recall.yaml

**Step 2: Add test for skill loading**

```go
func TestRegistry_LoadTravelAndRecall(t *testing.T) {
    registry := NewRegistry()

    // Default loading from data/skills
    if err := registry.LoadFromDir("data/skills"); err != nil {
        t.Fatalf("LoadFromDir failed: %v", err)
    }

    travel := registry.Get("travel")
    if travel == nil {
        t.Error("travel skill not loaded")
    }

    recall := registry.Get("recall")
    if recall == nil {
        t.Error("recall skill not loaded")
    }
}
```

**Step 3: Run test**

Run: `go test ./pkg/skills/ -v -run "TestRegistry_LoadTravelAndRecall"`
Expected: PASS

**Step 4: Commit**

```bash
git add pkg/skills/registry.go pkg/skills/registry_test.go
git commit -m "feat(skills): ensure travel and recall skills load correctly

- Verify travel.yaml loads from data/skills
- Verify recall.yaml loads from data/skills
- Add registry test for new skills

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

## Task 18: Documentation and Examples

**Files:**
- Create: `docs/skills/travel.md`
- Create: `docs/skills/recall.md`
- Modify: `README.md` (if applicable)

**Step 1: Create travel skill documentation**

Create `docs/skills/travel.md`:

```markdown
# Travel Skill

## Description

Navigate to any destination system with optional POI docking. Supports smart resume after disconnect.

## Parameters

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `destination_system` | string | Yes | Target system ID (e.g., "haven", "sol") |
| `destination_poi` | string | No | POI name to dock at (e.g., "Haven Station") |

## Usage

### Basic travel to system

\`\`\`yaml
steps:
  - id: go_to_haven
    skill: travel
    skill_params:
      destination_system: "haven"
\`\`\`

### Travel with POI docking

\`\`\`yaml
steps:
  - id: visit_station
    skill: travel
    skill_params:
      destination_system: "haven"
      destination_poi: "Haven Station"
\`\`\`

## Behavior

1. **Resume check**: Looks for saved route from previous run
2. **Route planning**: Uses server pathfinding to calculate route
3. **Fuel check**: Verifies sufficient fuel for remaining jumps
4. **Auto-refuel**: If docked and fuel low, refuels before departure
5. **Jump execution**: Navigates route step-by-step
6. **Progress saving**: Updates route.json after each jump
7. **POI handling**: Finds and travels to POI, docks if station/base

## Persistence

Route state saved to: `data/agents/<agent-id>/route.json`

## Example

\`\`\`go
params := map[string]string{
    "destination_system": "haven",
    "destination_poi": "Haven Station",
}
err := executor.RunWithParams(ctx, "travel", params)
\`\`\`
```

**Step 2: Create recall skill documentation**

Create `docs/skills/recall.md`:

```markdown
# Recall Skill

## Description

Return the agent to their empire's capital system and dock at the home base.

## Behavior

1. **Check current state**: Returns early if already at capital and docked
2. **Undock**: Leaves current POI if docked elsewhere
3. **Navigate**: Uses travel skill to reach capital system
4. **Dock**: Ensures docked at base or station

## Capital Systems

| Empire | Capital System |
|--------|----------------|
| Solarian | Sol |
| Crimson | Krynn |
| Nebula | Haven |
| Voidborn | Nexus Prime |
| Outerrim | Frontier |

## Usage

\`\`\`yaml
steps:
  - id: return_home
    skill: recall
\`\`\`

## Example

\`\`\`go
err := executor.Run(ctx, "recall")
\`\`\`
```

**Step 3: Commit**

```bash
git add docs/skills/travel.md docs/skills/recall.md
git commit -m "docs(skills): add travel and recall skill documentation

- Document travel skill parameters and behavior
- Document recall skill and capital system mapping
- Add usage examples for both skills

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

## Task 19: Final Integration Test with Real Agent

**Files:**
- Create: `cmd/tools/test-travel/main.go`

**Step 1: Create test tool**

```go
package main

import (
    "context"
    "log"
    "os"

    "github.com/rsned/spacemolt/pkg/game"
    "github.com/rsned/spacemolt/pkg/skills"
)

func main() {
    logger := log.New(os.Stderr, "[test-travel] ", log.LstdFlags)

    // Load skills
    registry := skills.NewRegistry()
    if err := registry.LoadFromDir("data/skills"); err != nil {
        logger.Fatalf("Load skills: %v", err)
    }

    // Connect to game
    client := game.NewClient()
    ctx := context.Background()

    if err := client.Connect(ctx); err != nil {
        logger.Fatalf("Connect: %v", err)
    }
    defer client.Disconnect()

    // Create dispatcher
    dispatcher := skills.NewClientDispatcher(
        client,
        "test-agent",
        "data",
        logger,
    )

    executor := skills.NewExecutor(registry, dispatcher, logger)

    // Test travel skill
    params := map[string]string{
        "destination_system": "haven",
    }

    logger.Println("Starting travel to haven...")
    if err := executor.RunWithParams(ctx, "travel", params); err != nil {
        logger.Fatalf("Travel failed: %v", err)
    }

    logger.Println("Travel complete!")
}
```

**Step 2: Build test tool**

Run: `go build -o bin/test-travel ./cmd/tools/test-travel/`
Expected: No errors

**Step 3: Commit**

```bash
git add cmd/tools/test-travel/main.go
git commit -m "feat(tools): add test-travel tool for manual testing

- Create command-line tool for testing travel skill
- Connects to real game server
- Useful for manual integration testing

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

## Task 20: Build and Test All Packages

**Step 1: Build all packages**

Run: `go build ./...`
Expected: No errors

**Step 2: Run all tests**

Run: `go test ./...`
Expected: All tests pass

**Step 3: Run linter**

Run: `golangci-lint run ./...`
Expected: No new findings

**Step 4: Final commit**

```bash
git add .
git commit -m "feat(skills): complete travel and recall skills implementation

- Implement travel.yaml for general navigation
- Implement recall.yaml for return-to-capital
- Add route persistence for disconnect recovery
- Add fuel management and pre-flight checks
- Add skill parameter mechanism
- Add expression variables and functions for navigation
- Comprehensive tests and documentation

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

## Summary

This implementation plan delivers:

1. **Enhanced Skill System**: Parameter passing, expression variables, and functions
2. **Route Persistence**: JSON file + agent memory for disconnect recovery
3. **Fuel Management**: Pre-flight checks with auto-refuel when possible
4. **travel.yaml**: Reusable navigation skill with POI docking
5. **recall.yaml**: One-click return to empire capital
6. **Comprehensive Tests**: Unit, integration, and manual testing tools
7. **Documentation**: Skill usage guides and examples

**Total Tasks:** 20
**Estimated Time:** 4-6 hours (depending on familiarity with codebase)

---

## References

- Design doc: `docs/plans/2026-02-27-travel-recall-skills-design.md`
- Existing skills: `data/skills/mine.yaml`, `data/skills/refuel_repair.yaml`
- Skill executor: `pkg/skills/executor.go`
- Client dispatcher: `pkg/skills/client_dispatcher.go`
- Expression evaluator: `pkg/skills/evaluator.go`
- Auto-recall logic: `cmd/auto-recall/main.go`
- Empire capital function: `pkg/agent/base.go` (EmpireCapitalSystem)
