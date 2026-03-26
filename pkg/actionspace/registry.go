package actionspace

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
)

// Registry holds the complete set of actions loaded from an OpenAPI spec
// and enriched with preconditions and target generators.
type Registry struct {
	Actions []Action
	// Warnings collected during loading (unknown commands, missing annotations).
	Warnings []string
}

// openAPISpec is the minimal structure we need from the OpenAPI JSON.
type openAPISpec struct {
	Paths map[string]openAPIPath `json:"paths"`
}

type openAPIPath struct {
	Post openAPIOperation `json:"post"`
}

type openAPIOperation struct {
	OperationID string   `json:"operationId"`
	Summary     string   `json:"summary"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	IsMutation  bool     `json:"x-is-mutation"`
}

// LoadFromOpenAPI reads an OpenAPI spec file and builds a Registry by merging
// the spec's commands with the annotated preconditions and target generators.
//
// Commands in the spec that have no annotation get a default precondition
// (RequiresNotInTransit). Commands in the annotation map not found in the spec
// generate warnings (stale annotations).
func LoadFromOpenAPI(path string) (*Registry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read openapi spec: %w", err)
	}

	var spec openAPISpec
	if err := json.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("parse openapi spec: %w", err)
	}

	annotations := CommandAnnotations()
	r := &Registry{}

	// Track which annotations were consumed.
	consumed := make(map[string]bool)

	for pathKey, pathVal := range spec.Paths {
		op := pathVal.Post
		name := strings.TrimPrefix(pathKey, "/")
		if name == "" {
			continue
		}

		// Determine category from tags.
		category := "other"
		if len(op.Tags) > 0 {
			category = op.Tags[0]
		}

		// Check if this is a mutation or a known non-mutation state-changer.
		ann, hasAnnotation := annotations[name]
		if !op.IsMutation && !hasAnnotation {
			// Pure query, skip.
			continue
		}
		if !op.IsMutation && hasAnnotation && !ann.IncludeNonMutation {
			// Has annotation but not flagged for inclusion.
			continue
		}

		action := Action{
			Name:       name,
			Summary:    op.Summary,
			Category:   category,
			IsMutation: true, // Everything in the registry is treated as a decision.
		}

		if hasAnnotation {
			consumed[name] = true
			action.Preconditions = ann.Preconditions
			action.Targets = ann.Targets
			// Allow annotation to override category if specified.
			if ann.Category != "" {
				action.Category = ann.Category
			}
		} else {
			// Mutation with no annotation — use safe default.
			action.Preconditions = []Precondition{RequiresNotInTransit}
			r.Warnings = append(r.Warnings,
				fmt.Sprintf("no annotation for mutation %q — using default preconditions", name))
		}

		r.Actions = append(r.Actions, action)
	}

	// Check for virtual actions (not in OpenAPI but needed, e.g. battle sub-actions).
	for name, ann := range annotations {
		if consumed[name] {
			continue
		}
		if ann.Virtual {
			r.Actions = append(r.Actions, Action{
				Name:          name,
				Summary:       ann.Summary,
				Category:      ann.Category,
				IsMutation:    true,
				Preconditions: ann.Preconditions,
				Targets:       ann.Targets,
			})
			consumed[name] = true
			continue
		}
		// Annotation exists but command not in spec (and not virtual).
		if !consumed[name] {
			r.Warnings = append(r.Warnings,
				fmt.Sprintf("annotation for %q not found in OpenAPI spec (stale?)", name))
		}
	}

	return r, nil
}

// LogWarnings prints any loading warnings to the default logger.
func (r *Registry) LogWarnings() {
	for _, w := range r.Warnings {
		log.Printf("[actionspace] WARNING: %s", w)
	}
}

// Evaluate computes the full action space using this registry's actions.
func (r *Registry) Evaluate(gc GameContext) ActionSpace {
	return evaluateActions(r.Actions, gc)
}

// DefaultRegistry returns a registry built from the hardcoded AllActions.
// Used when no OpenAPI spec is available.
func DefaultRegistry() *Registry {
	return &Registry{Actions: AllActions}
}
