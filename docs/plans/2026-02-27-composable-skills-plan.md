# Composable Skills Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Extract game action sequences into composable YAML state machines with a Go executor, expression DSL, and DOT graph CLI tool.

**Architecture:** Skills are YAML files in `data/skills/` defining state machines. A Go package `pkg/skills/` loads, validates, evaluates conditions against `game.State`, and executes steps by calling `game.GameClient` methods. A CLI tool generates DOT graphs from skill definitions.

**Tech Stack:** Go 1.24, `gopkg.in/yaml.v3`, standard `testing`, `text/template` (for DOT output)

**Design doc:** `docs/plans/2026-02-27-composable-skills-design.md`

---

### Task 1: Skill YAML Types and Loader

**Files:**
- Create: `pkg/skills/skill.go`
- Create: `pkg/skills/skill_test.go`

**Step 1: Write the failing test**

Create `pkg/skills/skill_test.go`:

```go
package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSkill(t *testing.T) {
	yaml := `
name: mine
description: Gather resources from asteroid belt
prerequisites:
  - docked OR at_poi_type(asteroid_belt)
  - has_module_type(mining)
targets:
  mining_site:
    poi_type: [asteroid_belt, asteroid_field]
    description: Resource extraction location
  home_station:
    poi_type: [station]
    description: Nearest station
outputs:
  - cargo_full
  - docked
steps:
  - id: check_ready
    check: true
    conditions:
      fuel_pct < 0.1: goto emergency_dock
      not docked: goto travel_to_belt
      default: goto undock
  - id: undock
    action: undock
    next: travel_to_belt
  - id: mine_loop
    action: mine
    repeat:
      while:
        - cargo_pct < 0.97
        - fuel_pct > 0.1
    next: return_to_station
  - id: sub_step
    skill: sell
    next: done
  - id: done
    terminal: true
`
	dir := t.TempDir()
	path := filepath.Join(dir, "mine.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	skill, err := LoadSkill(path)
	if err != nil {
		t.Fatalf("LoadSkill() error: %v", err)
	}

	if skill.Name != "mine" {
		t.Errorf("Name = %q, want %q", skill.Name, "mine")
	}
	if skill.Description != "Gather resources from asteroid belt" {
		t.Errorf("Description = %q, want %q", skill.Description, "Gather resources from asteroid belt")
	}
	if len(skill.Prerequisites) != 2 {
		t.Errorf("Prerequisites count = %d, want 2", len(skill.Prerequisites))
	}
	if len(skill.Targets) != 2 {
		t.Errorf("Targets count = %d, want 2", len(skill.Targets))
	}
	if target, ok := skill.Targets["mining_site"]; !ok {
		t.Error("missing target 'mining_site'")
	} else {
		if len(target.POIType) != 2 {
			t.Errorf("mining_site POIType count = %d, want 2", len(target.POIType))
		}
	}
	if len(skill.Outputs) != 2 {
		t.Errorf("Outputs count = %d, want 2", len(skill.Outputs))
	}
	if len(skill.Steps) != 5 {
		t.Errorf("Steps count = %d, want 5", len(skill.Steps))
	}

	// Check step types
	checkReady := skill.Steps[0]
	if !checkReady.Check {
		t.Error("check_ready.Check should be true")
	}
	if len(checkReady.Conditions) != 3 {
		t.Errorf("check_ready conditions = %d, want 3", len(checkReady.Conditions))
	}

	undock := skill.Steps[1]
	if undock.Action != "undock" {
		t.Errorf("undock.Action = %q, want %q", undock.Action, "undock")
	}
	if undock.Next != "travel_to_belt" {
		t.Errorf("undock.Next = %q, want %q", undock.Next, "travel_to_belt")
	}

	mineLoop := skill.Steps[2]
	if mineLoop.Repeat == nil {
		t.Fatal("mine_loop.Repeat should not be nil")
	}
	if len(mineLoop.Repeat.While) != 2 {
		t.Errorf("mine_loop.Repeat.While = %d, want 2", len(mineLoop.Repeat.While))
	}

	subStep := skill.Steps[3]
	if subStep.Skill != "sell" {
		t.Errorf("sub_step.Skill = %q, want %q", subStep.Skill, "sell")
	}

	done := skill.Steps[4]
	if !done.Terminal {
		t.Error("done.Terminal should be true")
	}
}

func TestLoadSkill_Validation(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{
			name:    "missing name",
			yaml:    "description: test\nsteps:\n  - id: done\n    terminal: true\n",
			wantErr: "missing name",
		},
		{
			name:    "missing steps",
			yaml:    "name: test\ndescription: test\n",
			wantErr: "missing steps",
		},
		{
			name:    "duplicate step ID",
			yaml:    "name: test\ndescription: test\nsteps:\n  - id: a\n    action: mine\n    next: a\n  - id: a\n    terminal: true\n",
			wantErr: "duplicate step ID",
		},
		{
			name:    "step with no action, skill, check, or terminal",
			yaml:    "name: test\ndescription: test\nsteps:\n  - id: empty\n    next: empty\n",
			wantErr: "must have exactly one of",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "test.yaml")
			if err := os.WriteFile(path, []byte(tt.yaml), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := LoadSkill(path)
			if err == nil {
				t.Fatalf("LoadSkill() expected error containing %q", tt.wantErr)
			}
			if !contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want containing %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
```

**Step 2: Run test to verify it fails**

Run: `cd /home/robert/spacemolt/spacemolt && go test ./pkg/skills/ -v -run TestLoadSkill`
Expected: FAIL — package does not exist

**Step 3: Write minimal implementation**

Create `pkg/skills/skill.go`:

```go
package skills

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Skill defines a composable state machine for game actions.
type Skill struct {
	Name          string            `yaml:"name"`
	Description   string            `yaml:"description"`
	Prerequisites []string          `yaml:"prerequisites"`
	Targets       map[string]Target `yaml:"targets"`
	Outputs       []string          `yaml:"outputs"`
	Steps         []Step            `yaml:"steps"`
}

// Target defines a POI type reference resolved at runtime.
type Target struct {
	POIType     []string `yaml:"poi_type"`
	Description string   `yaml:"description"`
}

// Step is a single node in the skill state machine.
type Step struct {
	ID         string            `yaml:"id"`
	Action     string            `yaml:"action,omitempty"`
	Skill      string            `yaml:"skill,omitempty"`
	Check      bool              `yaml:"check,omitempty"`
	Terminal   bool              `yaml:"terminal,omitempty"`
	Target     string            `yaml:"target,omitempty"`
	Next       string            `yaml:"next,omitempty"`
	Conditions map[string]string `yaml:"conditions,omitempty"`
	Repeat     *Repeat           `yaml:"repeat,omitempty"`
}

// Repeat defines loop behavior for a step.
type Repeat struct {
	While []string `yaml:"while"`
}

// LoadSkill reads and validates a skill YAML file.
func LoadSkill(path string) (*Skill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading skill file: %w", err)
	}

	var skill Skill
	if err := yaml.Unmarshal(data, &skill); err != nil {
		return nil, fmt.Errorf("parsing skill YAML: %w", err)
	}

	if err := skill.Validate(); err != nil {
		return nil, err
	}

	return &skill, nil
}

// Validate checks that the skill definition is well-formed.
func (s *Skill) Validate() error {
	if s.Name == "" {
		return fmt.Errorf("missing name")
	}
	if len(s.Steps) == 0 {
		return fmt.Errorf("missing steps")
	}

	seen := make(map[string]bool)
	for _, step := range s.Steps {
		if step.ID == "" {
			return fmt.Errorf("step missing ID")
		}
		if seen[step.ID] {
			return fmt.Errorf("duplicate step ID: %q", step.ID)
		}
		seen[step.ID] = true

		kinds := 0
		if step.Action != "" {
			kinds++
		}
		if step.Skill != "" {
			kinds++
		}
		if step.Check {
			kinds++
		}
		if step.Terminal {
			kinds++
		}
		if kinds == 0 {
			return fmt.Errorf("step %q must have exactly one of: action, skill, check, terminal", step.ID)
		}
	}

	return nil
}

// StepByID returns the step with the given ID, or nil if not found.
func (s *Skill) StepByID(id string) *Step {
	for i := range s.Steps {
		if s.Steps[i].ID == id {
			return &s.Steps[i]
		}
	}
	return nil
}

// FirstStepID returns the ID of the first step.
func (s *Skill) FirstStepID() string {
	if len(s.Steps) == 0 {
		return ""
	}
	return s.Steps[0].ID
}
```

**Step 4: Run test to verify it passes**

Run: `cd /home/robert/spacemolt/spacemolt && go test ./pkg/skills/ -v -run TestLoadSkill`
Expected: PASS

**Step 5: Lint**

Run: `cd /home/robert/spacemolt/spacemolt && golangci-lint run ./pkg/skills/`
Expected: Clean

**Step 6: Commit**

```bash
git add pkg/skills/skill.go pkg/skills/skill_test.go
git commit -m "feat(skills): Add skill YAML types and loader with validation"
```

---

### Task 2: Expression DSL Parser and Evaluator

**Files:**
- Create: `pkg/skills/expr.go`
- Create: `pkg/skills/expr_test.go`

**Step 1: Write the failing test**

Create `pkg/skills/expr_test.go`:

```go
package skills

import (
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

func TestEvalExpr(t *testing.T) {
	state := &game.State{
		Doc:       true,
		Credits:   1500.0,
		Fuel:      80.0,
		MaxFuel:   100.0,
		Hull:      90.0,
		MaxHull:   100.0,
		CurrentPOI: "poi-123",
		Ship: game.Ship{
			CargoUsed:     45.0,
			CargoCapacity: 50.0,
			Cargo:         []game.CargoItem{{ItemID: "iron_ore", Quantity: 45}},
		},
		System: game.SystemData{
			Name: "Alpha",
			POIs: []game.POI{
				{ID: "poi-123", Type: "asteroid_belt"},
				{ID: "poi-456", Type: "station"},
			},
		},
	}

	tests := []struct {
		expr string
		want bool
	}{
		// Bare booleans
		{"docked", true},
		{"has_cargo", true},
		{"cargo_full", false},
		{"fuel_low", false},

		// Negation
		{"not docked", false},
		{"not fuel_low", true},

		// Comparisons
		{"fuel_pct > 0.5", true},
		{"fuel_pct < 0.5", false},
		{"cargo_pct >= 0.9", true},
		{"cargo_pct < 0.97", true},
		{"hull_pct >= 0.9", true},
		{"credits >= 1000", true},
		{"credits < 1000", false},
		{"cargo_count > 0", true},
		{"cargo_count == 1", true},

		// String comparisons
		{"current_poi == poi-123", true},
		{"current_poi != poi-456", true},
		{"current_poi_type == asteroid_belt", true},
		{"system_name == Alpha", true},

		// Default (always true)
		{"default", true},

		// Function-style
		{"at_poi_type(asteroid_belt, asteroid_field)", true},
		{"at_poi_type(station)", false},
	}

	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			got, err := EvalExpr(tt.expr, state)
			if err != nil {
				t.Fatalf("EvalExpr(%q) error: %v", tt.expr, err)
			}
			if got != tt.want {
				t.Errorf("EvalExpr(%q) = %v, want %v", tt.expr, got, tt.want)
			}
		})
	}
}

func TestEvalExpr_Undocked(t *testing.T) {
	state := &game.State{
		Doc:     false,
		Fuel:    5.0,
		MaxFuel: 100.0,
		Ship: game.Ship{
			CargoUsed:     0,
			CargoCapacity: 50.0,
		},
	}

	if got, _ := EvalExpr("docked", state); got {
		t.Error("docked should be false")
	}
	if got, _ := EvalExpr("not docked", state); !got {
		t.Error("not docked should be true")
	}
	if got, _ := EvalExpr("fuel_low", state); !got {
		t.Error("fuel_low should be true when fuel at 5%")
	}
}

func TestEvalExpr_InvalidExpr(t *testing.T) {
	state := &game.State{}
	_, err := EvalExpr("unknown_var > 5", state)
	if err == nil {
		t.Error("expected error for unknown variable")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /home/robert/spacemolt/spacemolt && go test ./pkg/skills/ -v -run TestEvalExpr`
Expected: FAIL — `EvalExpr` undefined

**Step 3: Write minimal implementation**

Create `pkg/skills/expr.go`:

```go
package skills

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/rsned/spacemolt/pkg/game"
)

// EvalExpr evaluates a condition expression against game state.
// Supported forms:
//   - Bare booleans: "docked", "has_cargo", "cargo_full", "fuel_low"
//   - Negation: "not docked"
//   - Comparisons: "fuel_pct < 0.1", "credits >= 5000"
//   - String comparisons: "current_poi == poi-123", "system_name == Alpha"
//   - Functions: "at_poi_type(station, asteroid_belt)"
//   - Special: "default" (always true)
func EvalExpr(expr string, state *game.State) (bool, error) {
	expr = strings.TrimSpace(expr)

	if expr == "default" {
		return true, nil
	}

	// Negation
	if strings.HasPrefix(expr, "not ") {
		inner := strings.TrimPrefix(expr, "not ")
		result, err := EvalExpr(inner, state)
		if err != nil {
			return false, err
		}
		return !result, nil
	}

	// Function-style: at_poi_type(a, b)
	if strings.HasPrefix(expr, "at_poi_type(") && strings.HasSuffix(expr, ")") {
		args := strings.TrimSuffix(strings.TrimPrefix(expr, "at_poi_type("), ")")
		types := parseArgs(args)
		poiType := resolveCurrentPOIType(state)
		for _, t := range types {
			if poiType == t {
				return true, nil
			}
		}
		return false, nil
	}

	if strings.HasPrefix(expr, "has_module_type(") && strings.HasSuffix(expr, ")") {
		args := strings.TrimSuffix(strings.TrimPrefix(expr, "has_module_type("), ")")
		moduleType := strings.TrimSpace(args)
		return hasModuleType(state, moduleType), nil
	}

	// Try comparison operators (ordered by length to avoid prefix issues)
	for _, op := range []string{">=", "<=", "!=", "==", ">", "<"} {
		if parts := strings.SplitN(expr, " "+op+" ", 2); len(parts) == 2 {
			return evalComparison(parts[0], op, parts[1], state)
		}
	}

	// Bare boolean
	val, err := resolveVar(expr, state)
	if err != nil {
		return false, err
	}
	return val.asBool()
}

type exprValue struct {
	floatVal  float64
	stringVal string
	boolVal   bool
	kind      string // "float", "string", "bool"
}

func (v exprValue) asBool() (bool, error) {
	switch v.kind {
	case "bool":
		return v.boolVal, nil
	case "float":
		return v.floatVal != 0, nil
	default:
		return false, fmt.Errorf("cannot use %q as boolean", v.stringVal)
	}
}

func resolveVar(name string, state *game.State) (exprValue, error) {
	name = strings.TrimSpace(name)

	fuelPct := safeDivide(state.Fuel, state.MaxFuel)
	hullPct := safeDivide(state.Hull, state.MaxHull)
	cargoPct := safeDivide(state.Ship.CargoUsed, state.Ship.CargoCapacity)

	switch name {
	case "fuel_pct":
		return exprValue{floatVal: fuelPct, kind: "float"}, nil
	case "hull_pct":
		return exprValue{floatVal: hullPct, kind: "float"}, nil
	case "cargo_pct":
		return exprValue{floatVal: cargoPct, kind: "float"}, nil
	case "cargo_count":
		return exprValue{floatVal: float64(len(state.Ship.Cargo)), kind: "float"}, nil
	case "credits":
		return exprValue{floatVal: state.Credits, kind: "float"}, nil
	case "docked":
		return exprValue{boolVal: state.Doc, kind: "bool"}, nil
	case "has_cargo":
		return exprValue{boolVal: len(state.Ship.Cargo) > 0, kind: "bool"}, nil
	case "cargo_full":
		return exprValue{boolVal: cargoPct >= 0.97, kind: "bool"}, nil
	case "fuel_low":
		return exprValue{boolVal: fuelPct < 0.1, kind: "bool"}, nil
	case "current_poi":
		return exprValue{stringVal: state.CurrentPOI, kind: "string"}, nil
	case "current_poi_type":
		return exprValue{stringVal: resolveCurrentPOIType(state), kind: "string"}, nil
	case "system_name":
		return exprValue{stringVal: state.System.Name, kind: "string"}, nil
	default:
		return exprValue{}, fmt.Errorf("unknown variable: %q", name)
	}
}

func evalComparison(lhs, op, rhs string, state *game.State) (bool, error) {
	left, err := resolveVar(lhs, state)
	if err != nil {
		return false, err
	}

	// Try to parse RHS as a number for float comparisons
	if left.kind == "float" {
		rightVal, err := strconv.ParseFloat(strings.TrimSpace(rhs), 64)
		if err != nil {
			return false, fmt.Errorf("cannot parse %q as number for comparison with %s", rhs, lhs)
		}
		return compareFloat(left.floatVal, op, rightVal)
	}

	// String comparison
	rightStr := strings.TrimSpace(rhs)
	switch op {
	case "==":
		return left.stringVal == rightStr, nil
	case "!=":
		return left.stringVal != rightStr, nil
	default:
		return false, fmt.Errorf("operator %s not supported for string comparison", op)
	}
}

func compareFloat(left float64, op string, right float64) (bool, error) {
	switch op {
	case "<":
		return left < right, nil
	case ">":
		return left > right, nil
	case "<=":
		return left <= right, nil
	case ">=":
		return left >= right, nil
	case "==":
		return left == right, nil
	case "!=":
		return left != right, nil
	default:
		return false, fmt.Errorf("unknown operator: %s", op)
	}
}

func resolveCurrentPOIType(state *game.State) string {
	for _, poi := range state.System.POIs {
		if poi.ID == state.CurrentPOI {
			return poi.Type
		}
	}
	return ""
}

func hasModuleType(state *game.State, moduleType string) bool {
	for _, moduleID := range state.Ship.Modules {
		if def, ok := state.ModuleDefinitions[moduleID]; ok {
			if strings.Contains(strings.ToLower(def.Type), strings.ToLower(moduleType)) {
				return true
			}
		}
	}
	return false
}

func safeDivide(a, b float64) float64 {
	if b == 0 {
		return 0
	}
	return a / b
}

func parseArgs(s string) []string {
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
```

**Step 4: Run test to verify it passes**

Run: `cd /home/robert/spacemolt/spacemolt && go test ./pkg/skills/ -v -run TestEvalExpr`
Expected: PASS

Note: The test references `game.CargoItem`, `game.Ship`, `game.POI`, `game.SystemData` — check the exact field names in `pkg/game/types.go` lines 110-165 and 168-206 before running. Adjust field names if they differ.

**Step 5: Lint**

Run: `cd /home/robert/spacemolt/spacemolt && golangci-lint run ./pkg/skills/`
Expected: Clean

**Step 6: Commit**

```bash
git add pkg/skills/expr.go pkg/skills/expr_test.go
git commit -m "feat(skills): Add expression DSL parser and evaluator"
```

---

### Task 3: Skill Registry

**Files:**
- Create: `pkg/skills/registry.go`
- Create: `pkg/skills/registry_test.go`

**Step 1: Write the failing test**

Create `pkg/skills/registry_test.go`:

```go
package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRegistry_LoadDir(t *testing.T) {
	dir := t.TempDir()

	mine := `
name: mine
description: Mine resources
steps:
  - id: do_mine
    action: mine
    next: done
  - id: done
    terminal: true
`
	sell := `
name: sell
description: Sell cargo
steps:
  - id: do_sell
    action: sell
    next: done
  - id: done
    terminal: true
`
	if err := os.WriteFile(filepath.Join(dir, "mine.yaml"), []byte(mine), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sell.yaml"), []byte(sell), 0o644); err != nil {
		t.Fatal(err)
	}
	// Non-yaml file should be ignored
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("ignore"), 0o644); err != nil {
		t.Fatal(err)
	}

	reg, err := LoadRegistry(dir)
	if err != nil {
		t.Fatalf("LoadRegistry() error: %v", err)
	}

	if reg.Get("mine") == nil {
		t.Error("registry missing 'mine' skill")
	}
	if reg.Get("sell") == nil {
		t.Error("registry missing 'sell' skill")
	}
	if reg.Get("nonexistent") != nil {
		t.Error("registry should return nil for unknown skill")
	}

	names := reg.Names()
	if len(names) != 2 {
		t.Errorf("Names() count = %d, want 2", len(names))
	}
}

func TestRegistry_Has(t *testing.T) {
	reg := &Registry{skills: map[string]*Skill{
		"mine": {Name: "mine"},
	}}
	if !reg.Has("mine") {
		t.Error("Has(mine) should be true")
	}
	if reg.Has("sell") {
		t.Error("Has(sell) should be false")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /home/robert/spacemolt/spacemolt && go test ./pkg/skills/ -v -run TestRegistry`
Expected: FAIL — `LoadRegistry` undefined

**Step 3: Write minimal implementation**

Create `pkg/skills/registry.go`:

```go
package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Registry holds loaded skill definitions indexed by name.
type Registry struct {
	skills map[string]*Skill
}

// LoadRegistry reads all .yaml files from a directory into a registry.
func LoadRegistry(dir string) (*Registry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading skills directory: %w", err)
	}

	reg := &Registry{skills: make(map[string]*Skill)}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext != ".yaml" && ext != ".yml" {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		skill, err := LoadSkill(path)
		if err != nil {
			return nil, fmt.Errorf("loading %s: %w", entry.Name(), err)
		}

		if _, exists := reg.skills[skill.Name]; exists {
			return nil, fmt.Errorf("duplicate skill name %q in %s", skill.Name, entry.Name())
		}
		reg.skills[skill.Name] = skill
	}

	return reg, nil
}

// Get returns a skill by name, or nil if not found.
func (r *Registry) Get(name string) *Skill {
	return r.skills[name]
}

// Has returns true if a skill with the given name exists.
func (r *Registry) Has(name string) bool {
	_, ok := r.skills[name]
	return ok
}

// Names returns all registered skill names sorted alphabetically.
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.skills))
	for name := range r.skills {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
```

**Step 4: Run test to verify it passes**

Run: `cd /home/robert/spacemolt/spacemolt && go test ./pkg/skills/ -v -run TestRegistry`
Expected: PASS

**Step 5: Lint**

Run: `cd /home/robert/spacemolt/spacemolt && golangci-lint run ./pkg/skills/`
Expected: Clean

**Step 6: Commit**

```bash
git add pkg/skills/registry.go pkg/skills/registry_test.go
git commit -m "feat(skills): Add skill registry for loading skill directory"
```

---

### Task 4: State Machine Executor

**Files:**
- Create: `pkg/skills/executor.go`
- Create: `pkg/skills/executor_test.go`

This is the core engine. It walks the state machine, evaluates conditions, dispatches actions to the game client, and handles sub-skill invocation.

**Step 1: Write the failing test**

Create `pkg/skills/executor_test.go`. This uses a mock game client that records calls:

```go
package skills

import (
	"context"
	"fmt"
	"log"
	"os"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

// mockGameClient records actions for testing.
type mockGameClient struct {
	calls []string
	state *game.State
}

func (m *mockGameClient) RecordCall(name string) {
	m.calls = append(m.calls, name)
}

func (m *mockGameClient) GetState() *game.State { return m.state }

// mockActionDispatcher records dispatched actions.
type mockActionDispatcher struct {
	calls []string
	state *game.State
}

func (d *mockActionDispatcher) Dispatch(ctx context.Context, action, target string) error {
	call := action
	if target != "" {
		call = action + ":" + target
	}
	d.calls = append(d.calls, call)

	// Simulate state changes
	switch action {
	case "undock":
		d.state.Doc = false
	case "dock":
		d.state.Doc = true
	case "mine":
		d.state.Ship.CargoUsed += 5.0
	case "travel":
		d.state.CurrentPOI = target
	}

	return nil
}

func (d *mockActionDispatcher) GetState() *game.State {
	return d.state
}

func TestExecutor_SimpleSequence(t *testing.T) {
	// Skill: undock -> mine -> dock -> done
	skill := &Skill{
		Name: "simple_mine",
		Steps: []Step{
			{ID: "undock", Action: "undock", Next: "mine_step"},
			{ID: "mine_step", Action: "mine", Next: "dock_step"},
			{ID: "dock_step", Action: "dock", Next: "done"},
			{ID: "done", Terminal: true},
		},
	}

	state := &game.State{
		Doc:  true,
		Fuel: 100, MaxFuel: 100,
		Ship: game.Ship{CargoCapacity: 50},
	}

	dispatcher := &mockActionDispatcher{state: state}
	reg := &Registry{skills: map[string]*Skill{"simple_mine": skill}}
	logger := log.New(os.Stderr, "[test] ", 0)
	exec := NewExecutor(reg, dispatcher, logger)

	err := exec.Run(context.Background(), "simple_mine")
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	want := []string{"undock", "mine", "dock"}
	if len(dispatcher.calls) != len(want) {
		t.Fatalf("calls = %v, want %v", dispatcher.calls, want)
	}
	for i, w := range want {
		if dispatcher.calls[i] != w {
			t.Errorf("call[%d] = %q, want %q", i, dispatcher.calls[i], w)
		}
	}
}

func TestExecutor_ConditionalBranching(t *testing.T) {
	skill := &Skill{
		Name: "branch_test",
		Steps: []Step{
			{
				ID: "check", Check: true,
				Conditions: map[string]string{
					"docked":  "goto undock",
					"default": "goto done",
				},
			},
			{ID: "undock", Action: "undock", Next: "done"},
			{ID: "done", Terminal: true},
		},
	}

	state := &game.State{
		Doc: true, Fuel: 100, MaxFuel: 100,
		Ship: game.Ship{CargoCapacity: 50},
	}

	dispatcher := &mockActionDispatcher{state: state}
	reg := &Registry{skills: map[string]*Skill{"branch_test": skill}}
	logger := log.New(os.Stderr, "[test] ", 0)
	exec := NewExecutor(reg, dispatcher, logger)

	err := exec.Run(context.Background(), "branch_test")
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	if len(dispatcher.calls) != 1 || dispatcher.calls[0] != "undock" {
		t.Errorf("calls = %v, want [undock]", dispatcher.calls)
	}
}

func TestExecutor_RepeatWhile(t *testing.T) {
	skill := &Skill{
		Name: "repeat_test",
		Steps: []Step{
			{
				ID: "mine_loop", Action: "mine",
				Repeat: &Repeat{While: []string{"cargo_pct < 0.5"}},
				Next:   "done",
			},
			{ID: "done", Terminal: true},
		},
	}

	state := &game.State{
		Fuel: 100, MaxFuel: 100,
		Ship: game.Ship{CargoUsed: 0, CargoCapacity: 50},
	}

	dispatcher := &mockActionDispatcher{state: state}
	reg := &Registry{skills: map[string]*Skill{"repeat_test": skill}}
	logger := log.New(os.Stderr, "[test] ", 0)
	exec := NewExecutor(reg, dispatcher, logger)

	err := exec.Run(context.Background(), "repeat_test")
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	// Each mine adds 5.0 cargo. Capacity=50, threshold=0.5 means stop at 25.
	// 5 mines: 0->5->10->15->20->25, then cargo_pct=0.5, loop exits.
	if len(dispatcher.calls) != 5 {
		t.Errorf("expected 5 mine calls, got %d: %v", len(dispatcher.calls), dispatcher.calls)
	}
}

func TestExecutor_SubSkill(t *testing.T) {
	subSkill := &Skill{
		Name: "sub",
		Steps: []Step{
			{ID: "sub_action", Action: "mine", Next: "sub_done"},
			{ID: "sub_done", Terminal: true},
		},
	}

	parentSkill := &Skill{
		Name: "parent",
		Steps: []Step{
			{ID: "do_sub", Skill: "sub", Next: "done"},
			{ID: "done", Terminal: true},
		},
	}

	state := &game.State{
		Fuel: 100, MaxFuel: 100,
		Ship: game.Ship{CargoCapacity: 50},
	}

	dispatcher := &mockActionDispatcher{state: state}
	reg := &Registry{skills: map[string]*Skill{
		"sub":    subSkill,
		"parent": parentSkill,
	}}
	logger := log.New(os.Stderr, "[test] ", 0)
	exec := NewExecutor(reg, dispatcher, logger)

	err := exec.Run(context.Background(), "parent")
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	if len(dispatcher.calls) != 1 || dispatcher.calls[0] != "mine" {
		t.Errorf("calls = %v, want [mine]", dispatcher.calls)
	}
}

func TestExecutor_TargetResolution(t *testing.T) {
	skill := &Skill{
		Name: "target_test",
		Targets: map[string]Target{
			"mining_site": {POIType: []string{"asteroid_belt"}},
		},
		Steps: []Step{
			{ID: "go", Action: "travel", Target: "$mining_site", Next: "done"},
			{ID: "done", Terminal: true},
		},
	}

	state := &game.State{
		Fuel: 100, MaxFuel: 100,
		Ship: game.Ship{CargoCapacity: 50},
		System: game.SystemData{
			POIs: []game.POI{
				{ID: "belt-1", Type: "asteroid_belt"},
				{ID: "station-1", Type: "station"},
			},
		},
	}

	dispatcher := &mockActionDispatcher{state: state}
	reg := &Registry{skills: map[string]*Skill{"target_test": skill}}
	logger := log.New(os.Stderr, "[test] ", 0)
	exec := NewExecutor(reg, dispatcher, logger)

	err := exec.Run(context.Background(), "target_test")
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	if len(dispatcher.calls) != 1 || dispatcher.calls[0] != "travel:belt-1" {
		t.Errorf("calls = %v, want [travel:belt-1]", dispatcher.calls)
	}
}

func TestExecutor_UnknownSkill(t *testing.T) {
	reg := &Registry{skills: map[string]*Skill{}}
	logger := log.New(os.Stderr, "[test] ", 0)
	dispatcher := &mockActionDispatcher{state: &game.State{}}
	exec := NewExecutor(reg, dispatcher, logger)

	err := exec.Run(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error for unknown skill")
	}
}

func TestExecutor_MaxStepsGuard(t *testing.T) {
	// Infinite loop skill — executor should bail after max steps
	skill := &Skill{
		Name: "infinite",
		Steps: []Step{
			{ID: "loop", Action: "mine", Next: "loop"},
		},
	}

	state := &game.State{
		Fuel: 100, MaxFuel: 100,
		Ship: game.Ship{CargoCapacity: 50},
	}

	// Override CargoCapacity to be huge so mine loop never fills
	state.Ship.CargoCapacity = 100000

	dispatcher := &mockActionDispatcher{state: state}
	reg := &Registry{skills: map[string]*Skill{"infinite": skill}}
	logger := log.New(os.Stderr, "[test] ", 0)
	exec := NewExecutor(reg, dispatcher, logger)
	exec.MaxSteps = 10

	err := exec.Run(context.Background(), "infinite")
	if err == nil {
		t.Error("expected error for max steps exceeded")
	}
	if len(dispatcher.calls) != 10 {
		t.Errorf("expected 10 calls before bail, got %d", len(dispatcher.calls))
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /home/robert/spacemolt/spacemolt && go test ./pkg/skills/ -v -run TestExecutor`
Expected: FAIL — `NewExecutor` undefined

**Step 3: Write minimal implementation**

Create `pkg/skills/executor.go`:

```go
package skills

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/rsned/spacemolt/pkg/game"
)

// ActionDispatcher executes a single game action. This interface decouples the
// executor from the game client, making testing possible and allowing the
// agent runner to provide its own dispatch logic.
type ActionDispatcher interface {
	Dispatch(ctx context.Context, action, target string) error
	GetState() *game.State
}

// Executor walks a skill state machine, evaluating conditions and dispatching
// actions through the ActionDispatcher.
type Executor struct {
	registry   *Registry
	dispatcher ActionDispatcher
	logger     *log.Logger
	MaxSteps   int // safety limit; 0 = default (1000)
	depth      int // current sub-skill nesting depth
}

const (
	defaultMaxSteps = 1000
	maxNestingDepth = 10
)

// NewExecutor creates a skill executor.
func NewExecutor(registry *Registry, dispatcher ActionDispatcher, logger *log.Logger) *Executor {
	return &Executor{
		registry:   registry,
		dispatcher: dispatcher,
		logger:     logger,
	}
}

// Run executes a skill by name to completion.
func (e *Executor) Run(ctx context.Context, skillName string) error {
	skill := e.registry.Get(skillName)
	if skill == nil {
		return fmt.Errorf("unknown skill: %q", skillName)
	}
	return e.runSkill(ctx, skill)
}

func (e *Executor) runSkill(ctx context.Context, skill *Skill) error {
	if e.depth >= maxNestingDepth {
		return fmt.Errorf("skill nesting depth exceeded (%d) — possible circular reference", maxNestingDepth)
	}
	e.depth++
	defer func() { e.depth-- }()

	maxSteps := e.MaxSteps
	if maxSteps == 0 {
		maxSteps = defaultMaxSteps
	}

	stepID := skill.FirstStepID()
	stepsExecuted := 0

	for stepID != "" {
		if err := ctx.Err(); err != nil {
			return err
		}

		if stepsExecuted >= maxSteps {
			return fmt.Errorf("skill %q exceeded max steps (%d)", skill.Name, maxSteps)
		}

		step := skill.StepByID(stepID)
		if step == nil {
			return fmt.Errorf("skill %q: step %q not found", skill.Name, stepID)
		}

		if step.Terminal {
			return nil
		}

		nextID, err := e.executeStep(ctx, skill, step)
		if err != nil {
			return fmt.Errorf("skill %q step %q: %w", skill.Name, step.ID, err)
		}

		stepsExecuted++
		stepID = nextID
	}

	return nil
}

func (e *Executor) executeStep(ctx context.Context, skill *Skill, step *Step) (string, error) {
	state := e.dispatcher.GetState()

	// Check node — evaluate conditions, no action
	if step.Check {
		return e.evalConditions(step.Conditions, state)
	}

	// Sub-skill invocation
	if step.Skill != "" {
		subSkill := e.registry.Get(step.Skill)
		if subSkill == nil {
			return "", fmt.Errorf("unknown sub-skill: %q", step.Skill)
		}
		if err := e.runSkill(ctx, subSkill); err != nil {
			return "", fmt.Errorf("sub-skill %q: %w", step.Skill, err)
		}
		return step.Next, nil
	}

	// Action node
	if step.Action != "" {
		target, err := e.resolveTarget(step.Target, skill, state)
		if err != nil {
			return "", err
		}

		// Handle repeat/while loop
		if step.Repeat != nil {
			return e.executeRepeat(ctx, skill, step, target)
		}

		// Single action execution
		if err := e.dispatcher.Dispatch(ctx, step.Action, target); err != nil {
			return "", err
		}

		// After action, evaluate conditions if present
		if len(step.Conditions) > 0 {
			state = e.dispatcher.GetState()
			return e.evalConditions(step.Conditions, state)
		}

		return step.Next, nil
	}

	return "", fmt.Errorf("step has no action, skill, check, or terminal")
}

func (e *Executor) executeRepeat(ctx context.Context, skill *Skill, step *Step, target string) (string, error) {
	maxSteps := e.MaxSteps
	if maxSteps == 0 {
		maxSteps = defaultMaxSteps
	}

	for i := range maxSteps {
		if err := ctx.Err(); err != nil {
			return "", err
		}

		// Check while conditions
		state := e.dispatcher.GetState()
		allTrue := true
		for _, cond := range step.Repeat.While {
			result, err := EvalExpr(cond, state)
			if err != nil {
				return "", fmt.Errorf("while condition %q: %w", cond, err)
			}
			if !result {
				allTrue = false
				break
			}
		}
		if !allTrue {
			break
		}

		if err := e.dispatcher.Dispatch(ctx, step.Action, target); err != nil {
			return "", err
		}

		_ = i // used by range
	}

	return step.Next, nil
}

func (e *Executor) evalConditions(conditions map[string]string, state *game.State) (string, error) {
	// Evaluate conditions in order. Note: Go map iteration is random,
	// but "default" is always evaluated last.
	var defaultTarget string

	for expr, target := range conditions {
		target = strings.TrimPrefix(strings.TrimSpace(target), "goto ")

		if strings.TrimSpace(expr) == "default" {
			defaultTarget = target
			continue
		}

		result, err := EvalExpr(expr, state)
		if err != nil {
			return "", fmt.Errorf("condition %q: %w", expr, err)
		}
		if result {
			return target, nil
		}
	}

	if defaultTarget != "" {
		return defaultTarget, nil
	}

	return "", fmt.Errorf("no condition matched and no default")
}

func (e *Executor) resolveTarget(target string, skill *Skill, state *game.State) (string, error) {
	if target == "" {
		return "", nil
	}

	if !strings.HasPrefix(target, "$") {
		return target, nil
	}

	varName := strings.TrimPrefix(target, "$")
	t, ok := skill.Targets[varName]
	if !ok {
		return "", fmt.Errorf("unknown target variable: %q", varName)
	}

	for _, poi := range state.System.POIs {
		for _, poiType := range t.POIType {
			if poi.Type == poiType {
				return poi.ID, nil
			}
		}
	}

	return "", fmt.Errorf("no POI of type %v found in current system", t.POIType)
}
```

**Step 4: Run test to verify it passes**

Run: `cd /home/robert/spacemolt/spacemolt && go test ./pkg/skills/ -v -run TestExecutor`
Expected: PASS

**Important note on map ordering:** The `evalConditions` method iterates a `map[string]string`, which has random order in Go. The `conditions` field in the YAML should be changed to an ordered type if condition evaluation order matters. Consider using `yaml.Node` or a `[]Condition` slice with `{Expr, Goto}` pairs instead. For now, the "default" case is handled explicitly as a fallback, and tests are written so only one non-default condition matches at a time. Flag this for a follow-up if deterministic ordering is needed.

**Step 5: Lint**

Run: `cd /home/robert/spacemolt/spacemolt && golangci-lint run ./pkg/skills/`
Expected: Clean

**Step 6: Commit**

```bash
git add pkg/skills/executor.go pkg/skills/executor_test.go
git commit -m "feat(skills): Add state machine executor with sub-skill support"
```

---

### Task 5: Fix Condition Ordering (map → slice)

The YAML `conditions` field uses `map[string]string` which loses ordering. Conditions must evaluate in YAML-defined order so that priority-based branching works correctly (e.g., check `fuel_pct < 0.1` before `default`).

**Files:**
- Modify: `pkg/skills/skill.go` — change `Conditions map[string]string` to ordered type
- Modify: `pkg/skills/executor.go` — update `evalConditions`
- Modify: `pkg/skills/skill_test.go` — update tests
- Modify: `pkg/skills/executor_test.go` — update tests

**Step 1: Update the Step type**

In `pkg/skills/skill.go`, replace `Conditions map[string]string` with:

```go
// Condition is an ordered expression → goto pair.
type Condition struct {
	Expr string `yaml:"expr"`
	Goto string `yaml:"goto"`
}

// Step changes:
type Step struct {
	// ... existing fields ...
	Conditions []Condition `yaml:"conditions,omitempty"` // was map[string]string
}
```

However, this changes the YAML format from the nice inline `conditions:` map to a verbose list. To keep the YAML ergonomic, implement a custom `UnmarshalYAML` on a `ConditionList` type that reads an ordered YAML map:

```go
// ConditionList preserves YAML map key ordering.
type ConditionList []Condition

func (cl *ConditionList) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.MappingNode {
		return fmt.Errorf("conditions must be a mapping")
	}
	for i := 0; i < len(value.Content)-1; i += 2 {
		*cl = append(*cl, Condition{
			Expr: value.Content[i].Value,
			Goto: value.Content[i+1].Value,
		})
	}
	return nil
}
```

Change `Step.Conditions` to `ConditionList`.

**Step 2: Update evalConditions in executor.go**

```go
func (e *Executor) evalConditions(conditions ConditionList, state *game.State) (string, error) {
	var defaultTarget string
	for _, cond := range conditions {
		target := strings.TrimPrefix(strings.TrimSpace(cond.Goto), "goto ")
		if strings.TrimSpace(cond.Expr) == "default" {
			defaultTarget = target
			continue
		}
		result, err := EvalExpr(cond.Expr, state)
		if err != nil {
			return "", fmt.Errorf("condition %q: %w", cond.Expr, err)
		}
		if result {
			return target, nil
		}
	}
	if defaultTarget != "" {
		return defaultTarget, nil
	}
	return "", fmt.Errorf("no condition matched and no default")
}
```

**Step 3: Update tests**

In `executor_test.go`, change all `Conditions: map[string]string{...}` to `Conditions: ConditionList{...}` using the `Condition` struct. Example:

```go
Conditions: ConditionList{
    {Expr: "docked", Goto: "goto undock"},
    {Expr: "default", Goto: "goto done"},
},
```

**Step 4: Run all tests**

Run: `cd /home/robert/spacemolt/spacemolt && go test ./pkg/skills/ -v`
Expected: PASS

**Step 5: Lint**

Run: `cd /home/robert/spacemolt/spacemolt && golangci-lint run ./pkg/skills/`

**Step 6: Commit**

```bash
git add pkg/skills/
git commit -m "fix(skills): Use ordered condition list instead of map for deterministic evaluation"
```

---

### Task 6: DOT Graph Generator

**Files:**
- Create: `pkg/skills/dot.go`
- Create: `pkg/skills/dot_test.go`

**Step 1: Write the failing test**

Create `pkg/skills/dot_test.go`:

```go
package skills

import (
	"strings"
	"testing"
)

func TestGenerateDOT_BasicSkill(t *testing.T) {
	skill := &Skill{
		Name:        "mine",
		Description: "Gather resources",
		Targets: map[string]Target{
			"mining_site": {POIType: []string{"asteroid_belt"}},
		},
		Steps: []Step{
			{ID: "check", Check: true, Conditions: ConditionList{
				{Expr: "docked", Goto: "goto undock"},
				{Expr: "default", Goto: "goto done"},
			}},
			{ID: "undock", Action: "undock", Next: "mine_loop"},
			{ID: "mine_loop", Action: "mine", Repeat: &Repeat{
				While: []string{"cargo_pct < 0.97"},
			}, Next: "done"},
			{ID: "done", Terminal: true},
		},
	}

	dot := GenerateDOT(skill)

	// Should contain digraph header
	if !strings.Contains(dot, "digraph mine") {
		t.Error("missing digraph header")
	}

	// Decision node should be diamond
	if !strings.Contains(dot, "shape=diamond") {
		t.Error("check node should be diamond shape")
	}

	// Terminal should be doublecircle
	if !strings.Contains(dot, "shape=doublecircle") {
		t.Error("done node should be doublecircle")
	}

	// Repeat node should be bold
	if !strings.Contains(dot, "style=bold") {
		t.Error("mine_loop should be bold")
	}

	// Should have self-edge for repeat
	if !strings.Contains(dot, "mine_loop -> mine_loop") {
		t.Error("mine_loop should have self-edge for while loop")
	}

	// Edges for conditions
	if !strings.Contains(dot, "check -> undock") {
		t.Error("missing edge check -> undock")
	}
}

func TestGenerateDOT_SubSkill(t *testing.T) {
	sub := &Skill{
		Name: "sell",
		Steps: []Step{
			{ID: "do_sell", Action: "sell", Next: "done"},
			{ID: "done", Terminal: true},
		},
	}
	parent := &Skill{
		Name: "trade_run",
		Steps: []Step{
			{ID: "mine_step", Skill: "mine", Next: "sell_step"},
			{ID: "sell_step", Skill: "sell", Next: "done"},
			{ID: "done", Terminal: true},
		},
	}

	reg := &Registry{skills: map[string]*Skill{"sell": sub, "trade_run": parent}}
	dot := GenerateDOTWithRegistry(parent, reg)

	// Sub-skill nodes should have special shape
	if !strings.Contains(dot, "shape=box") {
		t.Error("missing box shapes")
	}

	// Should reference sub-skill names
	if !strings.Contains(dot, "mine") {
		t.Error("should reference mine sub-skill")
	}
	if !strings.Contains(dot, "sell") {
		t.Error("should reference sell sub-skill")
	}
}

func TestGenerateDOT_ValidDOTSyntax(t *testing.T) {
	skill := &Skill{
		Name: "simple",
		Steps: []Step{
			{ID: "a", Action: "mine", Next: "b"},
			{ID: "b", Terminal: true},
		},
	}

	dot := GenerateDOT(skill)

	// Basic structure checks
	if !strings.HasPrefix(strings.TrimSpace(dot), "digraph") {
		t.Error("should start with digraph")
	}
	openBraces := strings.Count(dot, "{")
	closeBraces := strings.Count(dot, "}")
	if openBraces != closeBraces {
		t.Errorf("mismatched braces: %d open, %d close", openBraces, closeBraces)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /home/robert/spacemolt/spacemolt && go test ./pkg/skills/ -v -run TestGenerateDOT`
Expected: FAIL — `GenerateDOT` undefined

**Step 3: Write minimal implementation**

Create `pkg/skills/dot.go`:

```go
package skills

import (
	"fmt"
	"strings"
)

// GenerateDOT produces a DOT digraph string from a skill definition.
func GenerateDOT(skill *Skill) string {
	return GenerateDOTWithRegistry(skill, nil)
}

// GenerateDOTWithRegistry produces a DOT digraph, expanding sub-skill references
// from the registry for richer output.
func GenerateDOTWithRegistry(skill *Skill, registry *Registry) string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("digraph %s {\n", sanitizeID(skill.Name)))
	b.WriteString(fmt.Sprintf("    label=%q\n", skill.Name+": "+skill.Description))
	b.WriteString("    rankdir=TB\n")
	b.WriteString("    fontname=\"Helvetica\"\n")
	b.WriteString("    node [fontname=\"Helvetica\"]\n")
	b.WriteString("    edge [fontname=\"Helvetica\"]\n")
	b.WriteString("\n")

	// Emit nodes
	for _, step := range skill.Steps {
		b.WriteString("    ")
		b.WriteString(emitNode(step))
		b.WriteString("\n")
	}
	b.WriteString("\n")

	// Emit edges
	for _, step := range skill.Steps {
		edges := emitEdges(step)
		for _, edge := range edges {
			b.WriteString("    ")
			b.WriteString(edge)
			b.WriteString("\n")
		}
	}

	b.WriteString("}\n")
	return b.String()
}

func emitNode(step Step) string {
	id := sanitizeID(step.ID)

	switch {
	case step.Terminal:
		return fmt.Sprintf("%s [shape=doublecircle label=%q]", id, step.ID)

	case step.Check:
		return fmt.Sprintf("%s [shape=diamond label=%q]", id, step.ID)

	case step.Skill != "":
		return fmt.Sprintf("%s [shape=box style=rounded label=%q]",
			id, fmt.Sprintf("skill: %s", step.Skill))

	case step.Repeat != nil:
		whileLabel := strings.Join(step.Repeat.While, "\\nand ")
		label := fmt.Sprintf("%s\\n(while %s)", step.Action, whileLabel)
		if step.Target != "" {
			label = fmt.Sprintf("%s → %s\\n(while %s)", step.Action, step.Target, whileLabel)
		}
		return fmt.Sprintf("%s [shape=box style=bold label=%q]", id, label)

	default:
		label := step.Action
		if step.Target != "" {
			label = fmt.Sprintf("%s\\n→ %s", step.Action, step.Target)
		}
		return fmt.Sprintf("%s [shape=box label=%q]", id, label)
	}
}

func emitEdges(step Step) []string {
	if step.Terminal {
		return nil
	}

	var edges []string
	id := sanitizeID(step.ID)

	// Repeat self-edge
	if step.Repeat != nil {
		edges = append(edges, fmt.Sprintf("%s -> %s [style=dashed label=\"while\"]", id, id))
	}

	// Condition edges
	if len(step.Conditions) > 0 {
		for _, cond := range step.Conditions {
			target := sanitizeID(strings.TrimPrefix(strings.TrimSpace(cond.Goto), "goto "))
			label := cond.Expr
			edges = append(edges, fmt.Sprintf("%s -> %s [label=%q]", id, target, label))
		}
		return edges
	}

	// Simple next edge
	if step.Next != "" {
		target := sanitizeID(step.Next)
		// For repeat nodes, add label showing exit condition
		if step.Repeat != nil {
			whileNeg := negateWhile(step.Repeat.While)
			edges = append(edges, fmt.Sprintf("%s -> %s [label=%q]", id, target, whileNeg))
		} else {
			edges = append(edges, fmt.Sprintf("%s -> %s", id, target))
		}
	}

	return edges
}

func negateWhile(whileConds []string) string {
	parts := make([]string, len(whileConds))
	for i, c := range whileConds {
		parts[i] = "not(" + c + ")"
	}
	return strings.Join(parts, " or\\n")
}

func sanitizeID(id string) string {
	return strings.ReplaceAll(strings.ReplaceAll(id, "-", "_"), " ", "_")
}
```

**Step 4: Run test to verify it passes**

Run: `cd /home/robert/spacemolt/spacemolt && go test ./pkg/skills/ -v -run TestGenerateDOT`
Expected: PASS

**Step 5: Lint**

Run: `cd /home/robert/spacemolt/spacemolt && golangci-lint run ./pkg/skills/`

**Step 6: Commit**

```bash
git add pkg/skills/dot.go pkg/skills/dot_test.go
git commit -m "feat(skills): Add DOT graph generator for skill state machines"
```

---

### Task 7: Skill Graph CLI Tool

**Files:**
- Create: `cmd/tools/skill-graph/main.go`

**Step 1: Write the CLI**

Create `cmd/tools/skill-graph/main.go`:

```go
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rsned/spacemolt/pkg/skills"
)

func main() {
	output := flag.String("o", "", "output file (default: stdout)")
	skillsDir := flag.String("skills-dir", "", "directory of skill YAMLs for sub-skill resolution (optional)")
	flag.Parse()

	if flag.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "Usage: skill-graph [flags] <skill.yaml>\n")
		flag.PrintDefaults()
		os.Exit(1)
	}

	skillPath := flag.Arg(0)
	skill, err := skills.LoadSkill(skillPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading skill: %v\n", err)
		os.Exit(1)
	}

	var dot string
	if *skillsDir != "" {
		reg, err := skills.LoadRegistry(*skillsDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading skills registry: %v\n", err)
			os.Exit(1)
		}
		dot = skills.GenerateDOTWithRegistry(skill, reg)
	} else {
		// Try to load from same directory as the skill file
		dir := filepath.Dir(skillPath)
		reg, err := skills.LoadRegistry(dir)
		if err != nil {
			// Not fatal — just generate without registry
			dot = skills.GenerateDOT(skill)
		} else {
			dot = skills.GenerateDOTWithRegistry(skill, reg)
		}
	}

	if *output != "" {
		if err := os.WriteFile(*output, []byte(dot), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing output: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "Wrote %s\n", *output)
	} else {
		fmt.Print(dot)
	}
}
```

**Step 2: Build and test**

Run: `cd /home/robert/spacemolt/spacemolt && go build ./cmd/tools/skill-graph/`
Expected: Compiles successfully

**Step 3: Commit**

```bash
git add cmd/tools/skill-graph/main.go
git commit -m "feat(tools): Add skill-graph CLI for DOT generation from skill YAML"
```

---

### Task 8: Create mine.yaml Skill Definition

**Files:**
- Create: `data/skills/mine.yaml`

**Step 1: Write the skill YAML**

Create `data/skills/mine.yaml` based on the design doc (see `docs/plans/2026-02-27-composable-skills-design.md`, Section "Skill Definition Format"). This is the mine skill extracted from `pkg/game/mining.go`.

```yaml
name: mine
description: >
  Gather resources from an asteroid belt. Undocks, travels to the nearest
  asteroid belt, mines until cargo is full or fuel is low, returns to station,
  and docks. Extracted from the auto-miner's core mining loop.

prerequisites:
  - docked OR at_poi_type(asteroid_belt, asteroid_field)
  - has_module_type(mining)

targets:
  mining_site:
    poi_type: [asteroid_belt, asteroid_field]
    description: Resource extraction location
  home_station:
    poi_type: [station]
    description: Nearest station for docking and selling

outputs:
  - docked

steps:
  - id: check_ready
    check: true
    conditions:
      fuel_pct < 0.1: goto emergency_dock
      not docked: goto travel_to_belt
      default: goto undock

  - id: undock
    action: undock
    next: travel_to_belt

  - id: travel_to_belt
    action: travel
    target: $mining_site
    conditions:
      current_poi_type == asteroid_belt: goto mine_loop
      current_poi_type == asteroid_field: goto mine_loop
      default: goto travel_to_belt

  - id: mine_loop
    action: mine
    repeat:
      while:
        - cargo_pct < 0.97
        - fuel_pct > 0.1
    next: return_to_station

  - id: return_to_station
    action: travel
    target: $home_station
    next: dock

  - id: dock
    action: dock
    next: done

  - id: emergency_dock
    action: dock
    conditions:
      docked: goto done
      default: goto return_to_station

  - id: done
    terminal: true
```

**Step 2: Validate by loading**

Run: `cd /home/robert/spacemolt/spacemolt && go run cmd/tools/skill-graph/main.go data/skills/mine.yaml`
Expected: Outputs valid DOT graph to stdout

**Step 3: Commit**

```bash
git add data/skills/mine.yaml
git commit -m "feat(skills): Add mine.yaml skill definition extracted from mining loop"
```

---

### Task 9: Create Supporting Skill Definitions

**Files:**
- Create: `data/skills/sell.yaml`
- Create: `data/skills/refuel_repair.yaml`

**Step 1: Write sell.yaml**

```yaml
name: sell
description: >
  Sell all cargo at the current station. Must be docked.

prerequisites:
  - docked
  - has_cargo

steps:
  - id: check_docked
    check: true
    conditions:
      not docked: goto done
      not has_cargo: goto done
      default: goto sell_all

  - id: sell_all
    action: sell
    next: done

  - id: done
    terminal: true
```

**Step 2: Write refuel_repair.yaml**

```yaml
name: refuel_repair
description: >
  Refuel and repair the ship at the current station. Must be docked.

prerequisites:
  - docked

steps:
  - id: check_fuel
    check: true
    conditions:
      fuel_pct < 0.8: goto refuel
      default: goto check_hull

  - id: refuel
    action: refuel
    next: check_hull

  - id: check_hull
    check: true
    conditions:
      hull_pct < 0.9: goto repair
      default: goto done

  - id: repair
    action: repair
    next: done

  - id: done
    terminal: true
```

**Step 3: Validate by loading**

Run:
```bash
cd /home/robert/spacemolt/spacemolt
go run cmd/tools/skill-graph/main.go data/skills/sell.yaml
go run cmd/tools/skill-graph/main.go data/skills/refuel_repair.yaml
```
Expected: Valid DOT output for each

**Step 4: Commit**

```bash
git add data/skills/sell.yaml data/skills/refuel_repair.yaml
git commit -m "feat(skills): Add sell and refuel_repair skill definitions"
```

---

### Task 10: Generate DOT Graph for mine.yaml and Verify

**Step 1: Generate the DOT file**

Run:
```bash
cd /home/robert/spacemolt/spacemolt
go run cmd/tools/skill-graph/main.go data/skills/mine.yaml -o data/skills/mine.dot
```

**Step 2: Verify the DOT file is valid (if graphviz is installed)**

Run: `which dot && dot -Tsvg data/skills/mine.dot -o data/skills/mine.svg || echo "graphviz not installed, skipping SVG"`

**Step 3: Commit**

```bash
git add data/skills/mine.dot
git commit -m "feat(skills): Generate DOT graph for mine skill"
```

---

### Task 11: Run Full Test Suite and Lint

**Step 1: Run all skill tests**

Run: `cd /home/robert/spacemolt/spacemolt && go test ./pkg/skills/ -v`
Expected: All PASS

**Step 2: Run full project build**

Run: `cd /home/robert/spacemolt/spacemolt && go build ./...`
Expected: Clean

**Step 3: Run full project tests**

Run: `cd /home/robert/spacemolt/spacemolt && go test ./...`
Expected: All PASS (no regressions)

**Step 4: Lint**

Run: `cd /home/robert/spacemolt/spacemolt && golangci-lint run ./pkg/skills/ ./cmd/tools/skill-graph/`
Expected: Clean
