// Package skills provides composable state machine definitions for game actions.
// Skills are loaded from YAML files and define sequences of game commands with
// conditional branching, looping, target resolution, and sub-skill composition.
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
	Prerequisites []string          `yaml:"prerequisites,omitempty"`
	Parameters    []ParameterDefinition `yaml:"parameters,omitempty"`
	Targets       map[string]Target `yaml:"targets,omitempty"`
	Outputs        []string          `yaml:"outputs,omitempty"`
	BackgroundSlot *BackgroundSlot   `yaml:"background_slot,omitempty"`
	Steps          []Step            `yaml:"steps"`
}

// BackgroundSlot declares that this skill has idle windows where a background
// skill can run. The agent personality config fills in which skill to use.
type BackgroundSlot struct {
	Description     string   `yaml:"description,omitempty"`
	Interrupt       string   `yaml:"interrupt"`
	CleanupOutputs  []string `yaml:"cleanup_outputs,omitempty"`
	IdleSteps       []string `yaml:"idle_steps"`
	MinIdleDuration int      `yaml:"min_idle_duration,omitempty"`
}

// ParameterDefinition defines a skill parameter with metadata.
type ParameterDefinition struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Required    bool   `yaml:"required"`
	Default     string `yaml:"default,omitempty"`
}

// Target defines a POI type reference resolved at runtime.
type Target struct {
	POIType     []string `yaml:"poi_type"`
	Description string   `yaml:"description"`
}

// Step is a single node in the skill state machine.
// Exactly one of Action, Skill, Check, or Terminal must be set.
type Step struct {
	ID         string        `yaml:"id"`
	Action     string        `yaml:"action,omitempty"`
	Skill      string        `yaml:"skill,omitempty"`
	Check      bool          `yaml:"check,omitempty"`
	Terminal   bool          `yaml:"terminal,omitempty"`
	Target     string        `yaml:"target,omitempty"`
	Next       string        `yaml:"next,omitempty"`
	Conditions ConditionList `yaml:"conditions,omitempty"`
	Args       map[string]string `yaml:"args,omitempty"`
	Repeat     *Repeat       `yaml:"repeat,omitempty"`
	SkillParams map[string]string `yaml:"skill_params,omitempty"`
}

// Repeat defines loop behavior for a step.
type Repeat struct {
	While []string `yaml:"while"`
}

// Condition is an ordered expression/goto pair preserving YAML key order.
type Condition struct {
	Expr string
	Goto string
}

// ConditionList preserves YAML mapping key ordering for deterministic evaluation.
type ConditionList []Condition

// UnmarshalYAML reads a YAML mapping node while preserving key order.
func (cl *ConditionList) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.MappingNode {
		return fmt.Errorf("conditions must be a YAML mapping, got %d", value.Kind)
	}
	for i := 0; i < len(value.Content)-1; i += 2 {
		*cl = append(*cl, Condition{
			Expr: value.Content[i].Value,
			Goto: value.Content[i+1].Value,
		})
	}
	return nil
}

// LoadSkill reads and validates a skill YAML file.
func LoadSkill(path string) (*Skill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading skill file: %w", err)
	}

	return ParseSkill(data)
}

// ParseSkill parses and validates a skill from YAML bytes.
func ParseSkill(data []byte) (*Skill, error) {
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

	seen := make(map[string]bool, len(s.Steps))
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

	if s.BackgroundSlot != nil {
		for _, idleStep := range s.BackgroundSlot.IdleSteps {
			if !seen[idleStep] {
				return fmt.Errorf("background_slot.idle_steps references unknown step: %q", idleStep)
			}
		}
		validInterrupts := map[string]bool{"graceful": true, "immediate": true, "abandon": true}
		if s.BackgroundSlot.Interrupt != "" && !validInterrupts[s.BackgroundSlot.Interrupt] {
			return fmt.Errorf("background_slot.interrupt must be one of: graceful, immediate, abandon; got %q", s.BackgroundSlot.Interrupt)
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
