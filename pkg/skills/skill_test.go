package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadSkill(t *testing.T) {
	yamlData := `
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
	if err := os.WriteFile(path, []byte(yamlData), 0o644); err != nil {
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
	} else if len(target.POIType) != 2 {
		t.Errorf("mining_site POIType count = %d, want 2", len(target.POIType))
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

func TestLoadSkill_ConditionOrder(t *testing.T) {
	yamlData := `
name: order_test
description: Test condition ordering
steps:
  - id: check
    check: true
    conditions:
      fuel_pct < 0.1: goto emergency
      hull_pct < 0.5: goto repair
      cargo_full: goto sell
      default: goto mine
`
	dir := t.TempDir()
	path := filepath.Join(dir, "order.yaml")
	if err := os.WriteFile(path, []byte(yamlData), 0o644); err != nil {
		t.Fatal(err)
	}

	skill, err := LoadSkill(path)
	if err != nil {
		t.Fatalf("LoadSkill() error: %v", err)
	}

	step := skill.Steps[0]
	if len(step.Conditions) != 4 {
		t.Fatalf("conditions count = %d, want 4", len(step.Conditions))
	}

	// Verify order is preserved from YAML
	wantExprs := []string{"fuel_pct < 0.1", "hull_pct < 0.5", "cargo_full", "default"}
	wantGotos := []string{"goto emergency", "goto repair", "goto sell", "goto mine"}
	for i, cond := range step.Conditions {
		if cond.Expr != wantExprs[i] {
			t.Errorf("condition[%d].Expr = %q, want %q", i, cond.Expr, wantExprs[i])
		}
		if cond.Goto != wantGotos[i] {
			t.Errorf("condition[%d].Goto = %q, want %q", i, cond.Goto, wantGotos[i])
		}
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
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want containing %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestParseSkill(t *testing.T) {
	data := []byte("name: test\ndescription: a test\nsteps:\n  - id: done\n    terminal: true\n")
	skill, err := ParseSkill(data)
	if err != nil {
		t.Fatalf("ParseSkill() error: %v", err)
	}
	if skill.Name != "test" {
		t.Errorf("Name = %q, want %q", skill.Name, "test")
	}
}

func TestSkill_StepByID(t *testing.T) {
	skill := &Skill{
		Steps: []Step{
			{ID: "a", Action: "mine"},
			{ID: "b", Terminal: true},
		},
	}
	if step := skill.StepByID("a"); step == nil || step.ID != "a" {
		t.Error("StepByID(a) should return step a")
	}
	if step := skill.StepByID("missing"); step != nil {
		t.Error("StepByID(missing) should return nil")
	}
}

func TestSkill_FirstStepID(t *testing.T) {
	skill := &Skill{Steps: []Step{{ID: "first"}, {ID: "second"}}}
	if got := skill.FirstStepID(); got != "first" {
		t.Errorf("FirstStepID() = %q, want %q", got, "first")
	}

	empty := &Skill{}
	if got := empty.FirstStepID(); got != "" {
		t.Errorf("FirstStepID() on empty = %q, want empty", got)
	}
}

func TestParseSkillWithBackgroundSlot(t *testing.T) {
	yamlData := []byte(`
name: test_with_background
description: A skill that declares a background slot
background_slot:
  description: "Runs during idle windows"
  interrupt: graceful
  cleanup_outputs:
    - docked
  idle_steps:
    - sleep_cycle
  min_idle_duration: 60
steps:
  - id: do_work
    action: mine
    next: sleep_cycle
  - id: sleep_cycle
    action: sleep_scan_interval
    next: done
  - id: done
    terminal: true
`)
	skill, err := ParseSkill(yamlData)
	if err != nil {
		t.Fatalf("ParseSkill failed: %v", err)
	}

	if skill.BackgroundSlot == nil {
		t.Fatal("expected BackgroundSlot to be non-nil")
	}
	if skill.BackgroundSlot.Interrupt != "graceful" {
		t.Errorf("interrupt = %q, want %q", skill.BackgroundSlot.Interrupt, "graceful")
	}
	if len(skill.BackgroundSlot.CleanupOutputs) != 1 || skill.BackgroundSlot.CleanupOutputs[0] != "docked" {
		t.Errorf("cleanup_outputs = %v, want [docked]", skill.BackgroundSlot.CleanupOutputs)
	}
	if len(skill.BackgroundSlot.IdleSteps) != 1 || skill.BackgroundSlot.IdleSteps[0] != "sleep_cycle" {
		t.Errorf("idle_steps = %v, want [sleep_cycle]", skill.BackgroundSlot.IdleSteps)
	}
	if skill.BackgroundSlot.MinIdleDuration != 60 {
		t.Errorf("min_idle_duration = %d, want 60", skill.BackgroundSlot.MinIdleDuration)
	}
}

func TestParseSkillBackgroundSlotValidation(t *testing.T) {
	yamlData := []byte(`
name: bad_background
description: References non-existent idle step
background_slot:
  interrupt: graceful
  idle_steps:
    - nonexistent_step
steps:
  - id: work
    action: mine
    next: done
  - id: done
    terminal: true
`)
	_, err := ParseSkill(yamlData)
	if err == nil {
		t.Fatal("expected validation error for non-existent idle step")
	}
}

func TestParseSkillWithoutBackgroundSlot(t *testing.T) {
	yamlData := []byte(`
name: simple_skill
description: No background slot
steps:
  - id: done
    terminal: true
`)
	skill, err := ParseSkill(yamlData)
	if err != nil {
		t.Fatalf("ParseSkill failed: %v", err)
	}
	if skill.BackgroundSlot != nil {
		t.Fatal("expected BackgroundSlot to be nil for skill without it")
	}
}
