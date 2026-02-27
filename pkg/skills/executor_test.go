package skills

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

// mockActionDispatcher records dispatched actions and simulates state changes.
type mockActionDispatcher struct {
	calls []string
	state *game.State
}

func (d *mockActionDispatcher) Dispatch(_ context.Context, action, target string) error {
	call := action
	if target != "" {
		call = action + ":" + target
	}
	d.calls = append(d.calls, call)

	// Simulate state changes.
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

func newTestLogger() *log.Logger {
	return log.New(os.Stderr, "[test] ", 0)
}

func TestExecutor_SimpleSequence(t *testing.T) {
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
	exec := NewExecutor(reg, dispatcher, newTestLogger())

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
				Conditions: ConditionList{
					{Expr: "docked", Goto: "goto undock"},
					{Expr: "default", Goto: "goto done"},
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
	exec := NewExecutor(reg, dispatcher, newTestLogger())

	err := exec.Run(context.Background(), "branch_test")
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	if len(dispatcher.calls) != 1 || dispatcher.calls[0] != "undock" {
		t.Errorf("calls = %v, want [undock]", dispatcher.calls)
	}
}

func TestExecutor_ConditionalBranching_DefaultPath(t *testing.T) {
	skill := &Skill{
		Name: "default_test",
		Steps: []Step{
			{
				ID: "check", Check: true,
				Conditions: ConditionList{
					{Expr: "docked", Goto: "goto undock"},
					{Expr: "default", Goto: "goto done"},
				},
			},
			{ID: "undock", Action: "undock", Next: "done"},
			{ID: "done", Terminal: true},
		},
	}

	state := &game.State{
		Doc: false, Fuel: 100, MaxFuel: 100,
		Ship: game.Ship{CargoCapacity: 50},
	}

	dispatcher := &mockActionDispatcher{state: state}
	reg := &Registry{skills: map[string]*Skill{"default_test": skill}}
	exec := NewExecutor(reg, dispatcher, newTestLogger())

	err := exec.Run(context.Background(), "default_test")
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	// Should take the default path to done, no actions dispatched.
	if len(dispatcher.calls) != 0 {
		t.Errorf("calls = %v, want empty", dispatcher.calls)
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
	exec := NewExecutor(reg, dispatcher, newTestLogger())

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
	exec := NewExecutor(reg, dispatcher, newTestLogger())

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
	exec := NewExecutor(reg, dispatcher, newTestLogger())

	err := exec.Run(context.Background(), "target_test")
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	if len(dispatcher.calls) != 1 || dispatcher.calls[0] != "travel:belt-1" {
		t.Errorf("calls = %v, want [travel:belt-1]", dispatcher.calls)
	}
}

func TestExecutor_LiteralTarget(t *testing.T) {
	skill := &Skill{
		Name: "literal_target",
		Steps: []Step{
			{ID: "go", Action: "travel", Target: "poi-123", Next: "done"},
			{ID: "done", Terminal: true},
		},
	}

	state := &game.State{
		Fuel: 100, MaxFuel: 100,
		Ship: game.Ship{CargoCapacity: 50},
	}

	dispatcher := &mockActionDispatcher{state: state}
	reg := &Registry{skills: map[string]*Skill{"literal_target": skill}}
	exec := NewExecutor(reg, dispatcher, newTestLogger())

	err := exec.Run(context.Background(), "literal_target")
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	if len(dispatcher.calls) != 1 || dispatcher.calls[0] != "travel:poi-123" {
		t.Errorf("calls = %v, want [travel:poi-123]", dispatcher.calls)
	}
}

func TestExecutor_UnknownSkill(t *testing.T) {
	reg := &Registry{skills: map[string]*Skill{}}
	dispatcher := &mockActionDispatcher{state: &game.State{}}
	exec := NewExecutor(reg, dispatcher, newTestLogger())

	err := exec.Run(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error for unknown skill")
	}
}

func TestExecutor_MaxStepsGuard(t *testing.T) {
	skill := &Skill{
		Name: "infinite",
		Steps: []Step{
			{ID: "loop", Action: "mine", Next: "loop"},
		},
	}

	state := &game.State{
		Fuel: 100, MaxFuel: 100,
		Ship: game.Ship{CargoCapacity: 100000},
	}

	dispatcher := &mockActionDispatcher{state: state}
	reg := &Registry{skills: map[string]*Skill{"infinite": skill}}
	exec := NewExecutor(reg, dispatcher, newTestLogger())
	exec.MaxSteps = 10

	err := exec.Run(context.Background(), "infinite")
	if err == nil {
		t.Error("expected error for max steps exceeded")
	}
	if len(dispatcher.calls) != 10 {
		t.Errorf("expected 10 calls before bail, got %d", len(dispatcher.calls))
	}
}

func TestExecutor_ContextCancellation(t *testing.T) {
	skill := &Skill{
		Name: "cancel_test",
		Steps: []Step{
			{ID: "loop", Action: "mine", Next: "loop"},
		},
	}

	state := &game.State{
		Fuel: 100, MaxFuel: 100,
		Ship: game.Ship{CargoCapacity: 100000},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	dispatcher := &mockActionDispatcher{state: state}
	reg := &Registry{skills: map[string]*Skill{"cancel_test": skill}}
	exec := NewExecutor(reg, dispatcher, newTestLogger())

	err := exec.Run(ctx, "cancel_test")
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}

func TestExecutor_UnknownSubSkill(t *testing.T) {
	skill := &Skill{
		Name: "parent",
		Steps: []Step{
			{ID: "do_sub", Skill: "missing_sub", Next: "done"},
			{ID: "done", Terminal: true},
		},
	}

	state := &game.State{
		Fuel: 100, MaxFuel: 100,
		Ship: game.Ship{CargoCapacity: 50},
	}

	dispatcher := &mockActionDispatcher{state: state}
	reg := &Registry{skills: map[string]*Skill{"parent": skill}}
	exec := NewExecutor(reg, dispatcher, newTestLogger())

	err := exec.Run(context.Background(), "parent")
	if err == nil {
		t.Error("expected error for unknown sub-skill")
	}
}

func TestExecutor_NestingDepthGuard(t *testing.T) {
	// Create a chain of skills that each invoke the next.
	reg := &Registry{skills: make(map[string]*Skill)}
	for i := range 15 {
		name := "skill_" + string(rune('a'+i))
		nextName := "skill_" + string(rune('a'+i+1))
		if i == 14 {
			nextName = "skill_a" // Circular reference.
		}
		reg.skills[name] = &Skill{
			Name: name,
			Steps: []Step{
				{ID: "invoke", Skill: nextName, Next: "done"},
				{ID: "done", Terminal: true},
			},
		}
	}

	state := &game.State{
		Fuel: 100, MaxFuel: 100,
		Ship: game.Ship{CargoCapacity: 50},
	}

	dispatcher := &mockActionDispatcher{state: state}
	exec := NewExecutor(reg, dispatcher, newTestLogger())

	err := exec.Run(context.Background(), "skill_a")
	if err == nil {
		t.Error("expected error for nesting depth exceeded")
	}
}

func TestExecutor_ActionWithConditions(t *testing.T) {
	// An action step that evaluates conditions after dispatch.
	skill := &Skill{
		Name: "action_cond",
		Steps: []Step{
			{
				ID: "travel_step", Action: "travel", Target: "poi-station",
				Conditions: ConditionList{
					{Expr: "current_poi == poi-station", Goto: "goto dock_step"},
					{Expr: "default", Goto: "goto travel_step"},
				},
			},
			{ID: "dock_step", Action: "dock", Next: "done"},
			{ID: "done", Terminal: true},
		},
	}

	state := &game.State{
		Fuel: 100, MaxFuel: 100,
		Ship: game.Ship{CargoCapacity: 50},
	}

	dispatcher := &mockActionDispatcher{state: state}
	reg := &Registry{skills: map[string]*Skill{"action_cond": skill}}
	exec := NewExecutor(reg, dispatcher, newTestLogger())

	err := exec.Run(context.Background(), "action_cond")
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	// travel sets CurrentPOI to target, then condition matches, then dock.
	if len(dispatcher.calls) != 2 {
		t.Fatalf("calls = %v, want 2 calls", dispatcher.calls)
	}
	if dispatcher.calls[0] != "travel:poi-station" {
		t.Errorf("call[0] = %q, want %q", dispatcher.calls[0], "travel:poi-station")
	}
	if dispatcher.calls[1] != "dock" {
		t.Errorf("call[1] = %q, want %q", dispatcher.calls[1], "dock")
	}
}

func TestExecutor_MissingStep(t *testing.T) {
	skill := &Skill{
		Name: "missing_step",
		Steps: []Step{
			{ID: "start", Action: "mine", Next: "nonexistent"},
		},
	}

	state := &game.State{
		Fuel: 100, MaxFuel: 100,
		Ship: game.Ship{CargoCapacity: 50},
	}

	dispatcher := &mockActionDispatcher{state: state}
	reg := &Registry{skills: map[string]*Skill{"missing_step": skill}}
	exec := NewExecutor(reg, dispatcher, newTestLogger())

	err := exec.Run(context.Background(), "missing_step")
	if err == nil {
		t.Error("expected error for missing step")
	}
}

func TestExecutor_TargetResolution_NoPOIMatch(t *testing.T) {
	skill := &Skill{
		Name: "no_match",
		Targets: map[string]Target{
			"site": {POIType: []string{"wormhole"}},
		},
		Steps: []Step{
			{ID: "go", Action: "travel", Target: "$site", Next: "done"},
			{ID: "done", Terminal: true},
		},
	}

	state := &game.State{
		Fuel: 100, MaxFuel: 100,
		Ship: game.Ship{CargoCapacity: 50},
		System: game.SystemData{
			POIs: []game.POI{
				{ID: "station-1", Type: "station"},
			},
		},
	}

	dispatcher := &mockActionDispatcher{state: state}
	reg := &Registry{skills: map[string]*Skill{"no_match": skill}}
	exec := NewExecutor(reg, dispatcher, newTestLogger())

	err := exec.Run(context.Background(), "no_match")
	if err == nil {
		t.Error("expected error for no matching POI type")
	}
}
