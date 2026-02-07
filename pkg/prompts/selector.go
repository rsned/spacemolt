package prompts

import (
	"fmt"
)

// Selector chooses the appropriate template version
type Selector struct {
	registry *Registry
	config   *ConfigYAML
}

// NewSelector creates a new version selector
func NewSelector(registry *Registry, config *ConfigYAML) *Selector {
	return &Selector{
		registry: registry,
		config:   config,
	}
}

// SelectionContext holds information for version selection
type SelectionContext struct {
	PromptType       string
	Role             string
	RequestedVersion int // 0 = use configured version
}

// SelectVersion chooses the best template version for the given context
func (s *Selector) SelectVersion(ctx SelectionContext) (int, error) {
	// If a specific version is requested, use it
	if ctx.RequestedVersion > 0 {
		if s.registry.HasVersion(ctx.PromptType, ctx.RequestedVersion) {
			return ctx.RequestedVersion, nil
		}
		return 0, fmt.Errorf("requested version %d not found for %s", ctx.RequestedVersion, ctx.PromptType)
	}

	// Check configuration for active version (with role override)
	if version, ok := s.config.GetActiveVersion(ctx.PromptType, ctx.Role); ok {
		if s.registry.HasVersion(ctx.PromptType, version) {
			return version, nil
		}

		// If configured version not found, try fallback
		if s.config.Global.FallbackToPrevious {
			if fallback, ok := s.config.GetFallbackVersion(ctx.PromptType); ok {
				if s.registry.HasVersion(ctx.PromptType, fallback) {
					fmt.Printf("Warning: Configured version %d not found for %s, using fallback version %d\n",
						version, ctx.PromptType, fallback)
					return fallback, nil
				}
			}
		}
	}

	// Fallback to latest available version
	if latest, ok := s.registry.GetLatestVersion(ctx.PromptType); ok {
		return latest, nil
	}

	return 0, fmt.Errorf("no versions found for prompt type: %s", ctx.PromptType)
}
