# PR Fixes: Multi-Agent System Architecture

**Commit:** 64faa28
**Generated:** 2026-02-01
**Source:** Code review swarm (opus, gemini, glm)

---

## Priority 1: Blocking (Fix Before Merge)

### 1.1 Extract Ship Data Parsing Helper

**File:** `cmd/watcher/main.go`
**Lines:** 214-252, 351-384, 420-460, 473-506
**Severity:** HIGH
**Reviewers:** All three

**Problem:**
Ship data parsing logic (fuel, maxFuel, hull, maxHull, cargo_capacity, cargo) is copy-pasted 5 times across different message type handlers (`TypeLoggedIn`, `TypeOK`, `TypeStateUpdate`, `TypeMining`). Each copy is 30-40 lines with identical type assertions.

**Current pattern (repeated 5 times):**
```go
if fuel, ok := ship["fuel"].(float64); ok {
    state.Fuel = fuel
}
if maxFuel, ok := ship["max_fuel"].(float64); ok {
    state.MaxFuel = maxFuel
}
if hull, ok := ship["hull"].(float64); ok {
    state.Hull = hull
}
// ... 25+ more lines handling maxHull, cargo_capacity (with 3 type variants), cargo slice
```

**Fix:**
Create a helper function and call it from each handler:

```go
// Add this function near the State type or in a helpers section
func updateShipState(state *game.State, ship map[string]any) {
    if fuel, ok := ship["fuel"].(float64); ok {
        state.Fuel = fuel
    }
    if maxFuel, ok := ship["max_fuel"].(float64); ok {
        state.MaxFuel = maxFuel
    }
    if hull, ok := ship["hull"].(float64); ok {
        state.Hull = hull
    }
    if maxHull, ok := ship["max_hull"].(float64); ok {
        state.MaxHull = maxHull
    }
    // Handle cargo_capacity with all three type variants
    if maxCargo, ok := ship["cargo_capacity"].(float64); ok {
        state.MaxCargo = int(maxCargo)
    } else if maxCargo, ok := ship["cargo_capacity"].(int64); ok {
        state.MaxCargo = int(maxCargo)
    } else if maxCargo, ok := ship["cargo_capacity"].(int); ok {
        state.MaxCargo = maxCargo
    }
    // Handle cargo slice
    if cargo, ok := ship["cargo"].([]any); ok {
        state.Cargo = make([]map[string]any, 0, len(cargo))
        for _, item := range cargo {
            if itemMap, ok := item.(map[string]any); ok {
                state.Cargo = append(state.Cargo, itemMap)
            }
        }
    }
}
```

Then replace each 30-40 line block with:
```go
if ship, ok := resp.Payload["ship"].(map[string]any); ok {
    updateShipState(state, ship)
}
```

**Verification:**
- Run the watcher and verify ship stats display correctly
- Check that fuel, hull, and cargo update on state changes

---

### 1.2 Add Deduplication to RememberConnection

**File:** `pkg/knowledge/memory.go`
**Lines:** 92-98
**Severity:** HIGH
**Reviewers:** All three

**Problem:**
`RememberConnection` blindly appends connections without checking for duplicates. Calling `RememberConnection("A", "B")` multiple times creates duplicate entries, causing:
- Unbounded memory growth
- `GetUnknownConnections` returns duplicate system IDs

**Current code:**
```go
func (kb *MemoryKB) RememberConnection(ctx context.Context, fromSystem, toSystem string) error {
    kb.mu.Lock()
    defer kb.mu.Unlock()
    kb.connections[fromSystem] = append(kb.connections[fromSystem], toSystem)
    return nil
}
```

**Fix:**
```go
func (kb *MemoryKB) RememberConnection(ctx context.Context, fromSystem, toSystem string) error {
    kb.mu.Lock()
    defer kb.mu.Unlock()

    // Check for existing connection
    for _, existing := range kb.connections[fromSystem] {
        if existing == toSystem {
            return nil // Already exists
        }
    }

    kb.connections[fromSystem] = append(kb.connections[fromSystem], toSystem)
    return nil
}
```

**Verification:**
- Write a test that calls `RememberConnection("A", "B")` 3 times
- Assert that `kb.connections["A"]` has length 1, not 3

---

### 1.3 Fix Shallow Copy in State.Clone()

**File:** `pkg/game/types.go`
**Lines:** 55-56
**Severity:** HIGH
**Reviewers:** All three

**Problem:**
`Clone()` uses `copy()` on a `[]map[string]any` slice, which copies the map pointers, not the underlying maps. The cloned state shares cargo map references with the original, causing data races when accessed concurrently.

**Current code:**
```go
cargoCopy := make([]map[string]any, len(s.Cargo))
copy(cargoCopy, s.Cargo)  // Copies pointers, not maps!
```

**Fix:**
```go
cargoCopy := make([]map[string]any, len(s.Cargo))
for i, item := range s.Cargo {
    cargoCopy[i] = make(map[string]any, len(item))
    for k, v := range item {
        cargoCopy[i][k] = v
    }
}
```

**Verification:**
- Write a test that clones state, modifies cargo in the clone
- Assert that the original state's cargo is unchanged

---

### 1.4 Replace indexOf with strings.Index

**File:** `pkg/llm/client.go`
**Lines:** 200-208
**Severity:** MEDIUM
**Reviewers:** All three

**Problem:**
Custom `indexOf` function reimplements `strings.Index()` from the standard library.

**Current code:**
```go
func indexOf(s, substr string) int {
    for i := 0; i <= len(s)-len(substr); i++ {
        if s[i:i+len(substr)] == substr {
            return i
        }
    }
    return -1
}
```

**Fix:**
1. Delete the `indexOf` function
2. Replace all calls to `indexOf(s, substr)` with `strings.Index(s, substr)`
3. Add import `"strings"` if not present

**Verification:**
- Run `go build ./...` to ensure no compilation errors
- Test LLM response parsing still works

---

## Priority 2: High (Fix Soon After Merge)

### 2.1 Replace containsIgnoreCase with stdlib Implementation

**File:** `cmd/watcher/main.go`
**Lines:** 517-544
**Severity:** MEDIUM
**Reviewers:** All three

**Problem:**
27-line manual case-insensitive substring search that only handles ASCII (A-Z). Will fail to match Unicode characters like `é`, `ü`, etc.

**Current code:** Manual byte-by-byte comparison with ASCII-only case conversion.

**Fix:**
```go
func containsIgnoreCase(text string, substrings []string) bool {
    lower := strings.ToLower(text)
    for _, s := range substrings {
        if strings.Contains(lower, strings.ToLower(s)) {
            return true
        }
    }
    return false
}
```

**Verification:**
- Test with Unicode input: `containsIgnoreCase("Café", []string{"café"})` should return true

---

### 2.2 Add sync.Once to BaseAgent.Stop()

**File:** `pkg/agent/base.go`
**Lines:** 186-187
**Severity:** HIGH
**Reviewers:** gemini, opus

**Problem:**
Calling `Stop()` twice panics because you cannot close an already-closed channel.

**Current code:**
```go
func (a *BaseAgent) Stop() error {
    close(a.stopCh)  // Panics on second call
    return nil
}
```

**Fix:**
```go
type BaseAgent struct {
    // ... existing fields ...
    stopOnce sync.Once
}

func (a *BaseAgent) Stop() error {
    a.stopOnce.Do(func() {
        close(a.stopCh)
    })
    return nil
}
```

Also add `"sync"` to imports.

**Verification:**
- Write a test that calls `Stop()` twice on the same agent
- Assert no panic occurs

---

### 2.3 Remove or Document Unused Channels

**File:** `pkg/agent/base.go`
**Lines:** 22-24, 42-44
**Severity:** MEDIUM
**Reviewers:** All three

**Problem:**
`decisionCh`, `resultCh`, and `stopCh` are created in `NewBaseAgent` but never used. The `Start()` method (lines 172-183) does nothing with them.

**Options:**

**Option A - Remove if not needed:**
```go
// Remove from struct definition
type BaseAgent struct {
    // Remove: decisionCh, resultCh, stopCh
}

// Remove from constructor
func NewBaseAgent(...) *BaseAgent {
    return &BaseAgent{
        // Remove channel initialization
    }
}
```

**Option B - Document future intent:**
```go
// TODO: These channels are scaffolding for the async decision/action loop
// that will be implemented in Phase 2. See docs/design.md section 4.3.
decisionCh:  make(chan Decision, 10),
resultCh:    make(chan ActionResult, 10),
stopCh:      make(chan struct{}),
```

**Verification:**
- `go build ./...` succeeds
- `golangci-lint run` shows no new issues

---

## Priority 3: Medium (Next Sprint)

### 3.1 Add TODO Comments for Magic Sleep Values

**Files:** Create new test files
**Severity:** MEDIUM
**Reviewers:** All three

**Problem:**
Zero test files in the PR. Critical functions lack test coverage.

**Tasks:**

1. **Create `pkg/game/types_test.go`:**
```go
func TestStateClone_DeepCopiesCargo(t *testing.T) {
    original := &State{
        Cargo: []map[string]any{{"item": "ore", "qty": 10}},
    }
    clone := original.Clone()
    clone.Cargo[0]["qty"] = 20

    if original.Cargo[0]["qty"] != 10 {
        t.Error("Clone modified original cargo")
    }
}
```

2. **Create `pkg/knowledge/memory_test.go`:**
```go
func TestRememberConnection_Deduplicates(t *testing.T) {
    kb := NewMemoryKB()
    ctx := context.Background()

    kb.RememberConnection(ctx, "A", "B")
    kb.RememberConnection(ctx, "A", "B")
    kb.RememberConnection(ctx, "A", "B")

    if len(kb.connections["A"]) != 1 {
        t.Errorf("Expected 1 connection, got %d", len(kb.connections["A"]))
    }
}
```

3. **Create `cmd/watcher/helpers_test.go`:** (after extracting ship parsing)
```go
func TestUpdateShipState(t *testing.T) {
    state := &game.State{}
    ship := map[string]any{
        "fuel": 75.5,
        "max_fuel": 100.0,
        "cargo_capacity": float64(50),
    }
    updateShipState(state, ship)

    if state.Fuel != 75.5 {
        t.Errorf("Expected fuel 75.5, got %f", state.Fuel)
    }
}
```

---

## Priority 4: Unique Findings

### 4.1 Lock Held Across I/O in saveCredentials (opus)

**File:** `cmd/watcher/main.go`
**Lines:** 286-370
**Severity:** MEDIUM

**Problem:**
`handleResponse` holds `state.Mu.Lock()` for the entire function, including the `saveCredentials` call which performs file I/O. This is bad practice and could cause deadlocks if `saveCredentials` ever accesses state.

**Current flow:**
```go
func handleResponse(resp protocol.Response, state *game.State) {
    state.Mu.Lock()
    defer state.Mu.Unlock()
    // ... 100+ lines of processing ...
    saveCredentials(state.Username, state.Token)  // I/O while holding lock
}
```

**Fix:**
Extract values before I/O, minimize lock scope:
```go
func handleResponse(resp protocol.Response, state *game.State) {
    var username, token string

    state.Mu.Lock()
    // ... process response, update state ...
    username = state.Username
    token = state.Token
    state.Mu.Unlock()

    // I/O outside the lock
    if username != "" && token != "" {
        saveCredentials(username, token)
    }
}
```

---

### 4.2 Ship Position Sentinel Value Bug (opus)

**File:** `pkg/tui/map.go`
**Lines:** 98-108
**Severity:** LOW

**Problem:**
Ship position uses `-1.0` as a sentinel for "not found", but `shipY` could legitimately be negative in game coordinates. The check `if shipX >= 0` conflates "not found" with "negative coordinate".

**Current code:**
```go
shipX, shipY := -1.0, -1.0  // Sentinel values
// ... later ...
if shipX >= 0 {  // This fails for valid negative coordinates
    gridShipX, gridShipY := worldToGrid(shipX, shipY)
    grid[gridShipY][gridShipX] = '@'
}
```

**Fix:**
Use a boolean flag or pointer:
```go
var shipX, shipY float64
shipFound := false

// When ship is found:
shipX, shipY = actualX, actualY
shipFound = true

// When rendering:
if shipFound {
    gridShipX, gridShipY := worldToGrid(shipX, shipY)
    grid[gridShipY][gridShipX] = '@'
}
```

---

### 4.3 File Extension Mismatch (opus)

**File:** `data/agents/explorer-7/personality.md`
**Severity:** LOW

**Problem:**
File is named `.md` but contains YAML front matter. There's also a duplicate `personality.json` with the same data.

**Fix:**
Either:
1. Rename to `personality.yaml` and update any references
2. Convert contents to actual Markdown documentation
3. Delete the duplicate file

---

### 4.4 Status Panel Update Race (gemini)

**File:** `pkg/agent/base.go`
**Lines:** 70, 92-93
**Severity:** MEDIUM

**Problem:**
Lock is released at line 70, but status is updated at line 93 outside the lock.

**Current flow:**
```go
a.stateMu.Lock()
// ... protected work ...
a.stateMu.Unlock()  // Line 70

// ... more work ...

a.status.CurrentAction = fmt.Sprintf("Decided: %s", response.Action)  // Line 93 - unprotected!
```

**Fix:**
Move status update inside the lock, or add a separate lock for status:
```go
a.stateMu.Lock()
a.status.CurrentAction = fmt.Sprintf("Decided: %s (%.1f%% confidence)", response.Action, response.Confidence*100)
a.stateMu.Unlock()
```

---

### 4.5 Fragile JSON Parsing in LLM Client (gemini)

**File:** `pkg/llm/client.go`
**Lines:** 117-176
**Severity:** MEDIUM

**Problem:**
`parseDecision` and `extractField` implement a brittle custom JSON parser that will break on:
- Escaped quotes in values
- Whitespace variations
- Nested objects
- Unicode characters

**Fix:**
Use proper JSON parsing:
```go
func parseDecision(response string) (*DecisionResponse, error) {
    // Find JSON block in response
    start := strings.Index(response, "{")
    end := strings.LastIndex(response, "}")
    if start == -1 || end == -1 || end <= start {
        return nil, fmt.Errorf("no JSON block found")
    }

    jsonStr := response[start : end+1]

    var decision DecisionResponse
    if err := json.Unmarshal([]byte(jsonStr), &decision); err != nil {
        return nil, fmt.Errorf("failed to parse decision JSON: %w", err)
    }

    return &decision, nil
}
```

---

### 4.6 Duplicate Debug Key Extraction (gemini)

**File:** `cmd/watcher/main.go`
**Lines:** 179-184 vs 198-202
**Severity:** LOW

**Problem:**
The `keys` variable for debugging is extracted twice - once generically for all messages, then again specifically for `logged_in`.

**Fix:**
Remove the duplicate extraction in the `case protocol.TypeLoggedIn:` block since the generic logging already covers it.

---

### 4.7 Design Doc Doesn't Match Implementation (glm)

**File:** `docs/design.md`
**Severity:** LOW

**Problem:**
The design document is 1,097 lines with code samples that don't match the actual implementation. For example, the `BaseAgent` constructor in the design doc takes a `*game.Client` parameter that doesn't exist in the actual code.

**Fix:**
Either:
1. Update design doc to match implementation
2. Add a header noting it's aspirational, not current
3. Move outdated sections to an `archive` folder

---

### 4.8 Tight Coupling to Concrete LLM Client (glm)

**File:** `pkg/agent/base.go`
**Line:** 22
**Severity:** MEDIUM

**Problem:**
`BaseAgent` depends on concrete `*llm.Client` instead of an interface, making testing difficult without a real LLM.

**Current:**
```go
type BaseAgent struct {
    llm *llm.Client
}
```

**Fix:**
Define an interface:
```go
type LLMClient interface {
    GetDecision(ctx context.Context, prompt string) (*llm.DecisionResponse, error)
    TestConnection(ctx context.Context) error
}

type BaseAgent struct {
    llm LLMClient  // Interface instead of concrete type
}
```

This allows injecting a mock client in tests:
```go
type mockLLM struct{}
func (m *mockLLM) GetDecision(ctx context.Context, prompt string) (*llm.DecisionResponse, error) {
    return &llm.DecisionResponse{Action: "explore", Confidence: 0.9}, nil
}
```

---

### 4.9 Excessive Variable Redeclaration (glm)

**File:** `cmd/watcher/main.go`
**Lines:** 291-295, 309-312, 349-353, 556-560
**Severity:** LOW

**Problem:**
The `keys` debug variable is declared 4+ times in the same function with identical logic.

**Fix:**
Create a helper function:
```go
func logPayloadKeys(logger *log.Logger, prefix string, payload map[string]any) {
    var keys []string
    for k := range payload {
        keys = append(keys, k)
    }
    logger.Printf("%s payload keys: %v", prefix, keys)
}
```

Then use: `logPayloadKeys(debugLogger, "logged_in", resp.Payload)`

---

### 4.10 Add Tests for Critical Functions

**Files:** Create new test files
**Severity:** MEDIUM
**Reviewers:** All three

**Problem:**
Zero test files in the PR. Critical functions lack test coverage.

**Tasks:**

1. **Create `pkg/game/types_test.go`:**
```go
func TestStateClone_DeepCopiesCargo(t *testing.T) {
    original := &State{
        Cargo: []map[string]any{{"item": "ore", "qty": 10}},
    }
    clone := original.Clone()
    clone.Cargo[0]["qty"] = 20

    if original.Cargo[0]["qty"] != 10 {
        t.Error("Clone modified original cargo")
    }
}
```

2. **Create `pkg/knowledge/memory_test.go`:**
```go
func TestRememberConnection_Deduplicates(t *testing.T) {
    kb := NewMemoryKB()
    ctx := context.Background()

    kb.RememberConnection(ctx, "A", "B")
    kb.RememberConnection(ctx, "A", "B")
    kb.RememberConnection(ctx, "A", "B")

    if len(kb.connections["A"]) != 1 {
        t.Errorf("Expected 1 connection, got %d", len(kb.connections["A"]))
    }
}
```

3. **Create `cmd/watcher/helpers_test.go`:** (after extracting ship parsing)
```go
func TestUpdateShipState(t *testing.T) {
    state := &game.State{}
    ship := map[string]any{
        "fuel": 75.5,
        "max_fuel": 100.0,
        "cargo_capacity": float64(50),
    }
    updateShipState(state, ship)

    if state.Fuel != 75.5 {
        t.Errorf("Expected fuel 75.5, got %f", state.Fuel)
    }
}
```

---

## Summary Checklist

### Blocking
- [x] 1.1 Extract ship data parsing helper
- [x] 1.2 Add deduplication to RememberConnection
- [x] 1.3 Fix shallow copy in State.Clone()
- [x] 1.4 Replace indexOf with strings.Index

### High Priority
- [x] 2.1 Replace containsIgnoreCase with stdlib
- [x] 2.2 Add sync.Once to BaseAgent.Stop()
- [x] 2.3 Remove unused channels (decisionCh, resultCh removed)

### Medium Priority
- [x] 3.1 Add TODO comments for magic sleeps

### Unique Findings
- [x] 4.1 Lock held across I/O (opus)
- [x] 4.2 Ship position sentinel bug (opus)
- [x] 4.3 File extension mismatch (opus)
- [x] 4.4 Status panel update race (gemini)
- [x] 4.5 Fragile JSON parsing (gemini)
- [x] 4.6 Duplicate debug key extraction (gemini) - (Not present in current code)
- [x] 4.7 Design doc mismatch (glm)
- [x] 4.8 Tight coupling to concrete LLM (glm)
- [x] 4.9 Excessive variable redeclaration (glm) - (Not present in current code)
- [x] 4.10 Add tests for critical functions
