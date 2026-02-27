package skills

import (
	"fmt"
	"strings"
)

// GenerateDOT produces a DOT digraph representation of a single skill.
func GenerateDOT(skill *Skill) string {
	return GenerateDOTWithRegistry(skill, nil)
}

// GenerateDOTWithRegistry produces a DOT digraph representation of a skill,
// optionally expanding sub-skill references using the provided registry.
func GenerateDOTWithRegistry(skill *Skill, registry *Registry) string {
	var b strings.Builder

	name := sanitizeID(skill.Name)
	fmt.Fprintf(&b, "digraph %s {\n", name)
	fmt.Fprintf(&b, "  label=%q\n", skill.Name+": "+skill.Description)
	b.WriteString("  rankdir=TB\n")
	b.WriteString("\n")

	for _, step := range skill.Steps {
		writeNode(&b, step)
	}

	b.WriteString("\n")

	for _, step := range skill.Steps {
		writeEdges(&b, step)
	}

	b.WriteString("}\n")
	return b.String()
}

// sanitizeID converts hyphens and spaces to underscores for valid DOT identifiers.
func sanitizeID(s string) string {
	r := strings.NewReplacer("-", "_", " ", "_")
	return r.Replace(s)
}

// writeNode emits a DOT node declaration for a step.
func writeNode(b *strings.Builder, step Step) {
	id := sanitizeID(step.ID)

	switch {
	case step.Terminal:
		fmt.Fprintf(b, "  %s [shape=doublecircle label=%q]\n", id, step.ID)

	case step.Check:
		fmt.Fprintf(b, "  %s [shape=diamond label=%q]\n", id, step.ID)

	case step.Skill != "":
		label := "skill: " + step.Skill
		fmt.Fprintf(b, "  %s [shape=box style=rounded label=%q]\n", id, label)

	case step.Repeat != nil:
		label := step.Action
		if len(step.Repeat.While) > 0 {
			label += "\n(while " + strings.Join(step.Repeat.While, "\nand ") + ")"
		}
		if step.Target != "" {
			label += "\n-> " + step.Target
		}
		fmt.Fprintf(b, "  %s [shape=box style=bold label=%q]\n", id, label)

	default:
		// Plain action node.
		label := step.Action
		if step.Target != "" {
			label += "\n-> " + step.Target
		}
		fmt.Fprintf(b, "  %s [shape=box label=%q]\n", id, label)
	}
}

// writeEdges emits DOT edge declarations for a step's transitions.
func writeEdges(b *strings.Builder, step Step) {
	id := sanitizeID(step.ID)

	// Condition-based transitions.
	for _, cond := range step.Conditions {
		gotoTarget := strings.TrimPrefix(cond.Goto, "goto ")
		target := sanitizeID(gotoTarget)
		fmt.Fprintf(b, "  %s -> %s [label=%q]\n", id, target, cond.Expr)
	}

	// Repeat/while loop: self-edge + exit edge.
	if step.Repeat != nil && len(step.Repeat.While) > 0 {
		fmt.Fprintf(b, "  %s -> %s [style=dashed label=\"while\"]\n", id, id)
		if step.Next != "" {
			negated := negateWhile(step.Repeat.While)
			next := sanitizeID(step.Next)
			fmt.Fprintf(b, "  %s -> %s [label=%q]\n", id, next, negated)
		}
		return
	}

	// Simple next transition.
	if step.Next != "" {
		next := sanitizeID(step.Next)
		fmt.Fprintf(b, "  %s -> %s\n", id, next)
	}
}

// negateWhile produces a negated label for while conditions.
func negateWhile(conditions []string) string {
	if len(conditions) == 1 {
		return "not(" + conditions[0] + ")"
	}
	return "not(" + strings.Join(conditions, " and ") + ")"
}
