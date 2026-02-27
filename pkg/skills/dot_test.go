package skills

import (
	"strings"
	"testing"
)

func TestGenerateDOT_BasicSkill(t *testing.T) {
	skill := &Skill{
		Name:        "mine-ore",
		Description: "Mine ore from an asteroid",
		Steps: []Step{
			{ID: "check-fuel", Check: true, Conditions: ConditionList{
				{Expr: "fuel > 10", Goto: "fly-to-asteroid"},
				{Expr: "fuel <= 10", Goto: "done"},
			}},
			{ID: "fly-to-asteroid", Action: "travel", Target: "asteroid", Next: "mine-loop"},
			{ID: "mine-loop", Action: "mine", Target: "asteroid", Repeat: &Repeat{While: []string{"cargo.free > 0"}}, Next: "done"},
			{ID: "done", Terminal: true},
		},
	}

	dot := GenerateDOT(skill)

	// Check node shapes.
	if !strings.Contains(dot, "shape=diamond") {
		t.Error("expected diamond shape for check node")
	}
	if !strings.Contains(dot, "shape=doublecircle") {
		t.Error("expected doublecircle shape for terminal node")
	}
	if !strings.Contains(dot, "style=bold") {
		t.Error("expected bold style for repeat node")
	}

	// Check self-edge for repeat.
	if !strings.Contains(dot, "mine_loop -> mine_loop [style=dashed") {
		t.Error("expected dashed self-edge for repeat node")
	}

	// Check condition edges.
	if !strings.Contains(dot, `[label="fuel > 10"]`) {
		t.Error("expected condition edge label for fuel > 10")
	}
	if !strings.Contains(dot, `[label="fuel <= 10"]`) {
		t.Error("expected condition edge label for fuel <= 10")
	}

	// Check negated while exit edge.
	if !strings.Contains(dot, `not(cargo.free > 0)`) {
		t.Error("expected negated while condition on exit edge")
	}
}

func TestGenerateDOT_SubSkill(t *testing.T) {
	skill := &Skill{
		Name:        "complex-mission",
		Description: "A mission with sub-skills",
		Steps: []Step{
			{ID: "start", Skill: "mine-ore", Next: "finish"},
			{ID: "finish", Terminal: true},
		},
	}

	dot := GenerateDOT(skill)

	if !strings.Contains(dot, "style=rounded") {
		t.Error("expected rounded style for sub-skill node")
	}
	if !strings.Contains(dot, `skill: mine-ore`) {
		t.Error("expected sub-skill label")
	}
}

func TestGenerateDOT_ValidDOTSyntax(t *testing.T) {
	skill := &Skill{
		Name:        "simple",
		Description: "A simple skill",
		Steps: []Step{
			{ID: "go", Action: "travel", Next: "end"},
			{ID: "end", Terminal: true},
		},
	}

	dot := GenerateDOT(skill)

	if !strings.HasPrefix(dot, "digraph ") {
		t.Error("expected DOT output to start with 'digraph'")
	}

	opens := strings.Count(dot, "{")
	closes := strings.Count(dot, "}")
	if opens != closes {
		t.Errorf("unbalanced braces: %d opens vs %d closes", opens, closes)
	}
}
