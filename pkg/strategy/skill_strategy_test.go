package strategy

import (
	"testing"
)

func TestSkillStrategyName(t *testing.T) {
	ss := &SkillStrategy{
		skillName: "mine",
	}
	if got := ss.Name(); got != "mine" {
		t.Errorf("Name() = %q, want %q", got, "mine")
	}
}

func TestSkillStrategyDescription(t *testing.T) {
	ss := &SkillStrategy{
		skillName:   "mine",
		description: "Mine resources from asteroid belt",
	}
	if got := ss.Description(); got != "Mine resources from asteroid belt" {
		t.Errorf("Description() = %q, want %q", got, "Mine resources from asteroid belt")
	}
}

func TestSkillStrategyCurrentStatus(t *testing.T) {
	ss := &SkillStrategy{
		skillName: "mine",
	}
	status := ss.CurrentStatus()
	if status == "" {
		t.Error("expected non-empty status")
	}
}

func TestSkillStrategyImplementsInterface(t *testing.T) {
	var _ Strategy = &SkillStrategy{}
}
