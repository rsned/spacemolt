package strategy

import (
	"fmt"
	"sort"
	"sync"

	"github.com/rsned/spacemolt/pkg/skills"
)

// StrategyFactory creates a new Strategy instance.
type StrategyFactory func() Strategy

// UnifiedRegistry resolves skill/strategy names to runnable Strategy instances.
// It checks Go strategy factories first, then falls back to YAML skills.
type UnifiedRegistry struct {
	yamlRegistry *skills.Registry
	goStrategies map[string]StrategyFactory
	mu           sync.RWMutex
}

// NewUnifiedRegistry creates a registry. Pass nil for yamlRegistry if not using YAML skills.
func NewUnifiedRegistry(yamlRegistry *skills.Registry) *UnifiedRegistry {
	return &UnifiedRegistry{
		yamlRegistry: yamlRegistry,
		goStrategies: make(map[string]StrategyFactory),
	}
}

// RegisterGoStrategy adds a Go strategy factory.
func (r *UnifiedRegistry) RegisterGoStrategy(name string, factory StrategyFactory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.goStrategies[name] = factory
}

// Resolve returns a Strategy for the given name.
// Priority: Go strategies first, then YAML skills.
// For YAML skills, returns an error indicating the caller should use ResolveSkill.
func (r *UnifiedRegistry) Resolve(name string) (Strategy, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if factory, ok := r.goStrategies[name]; ok {
		return factory(), nil
	}

	if r.yamlRegistry != nil && r.yamlRegistry.Has(name) {
		return nil, fmt.Errorf("yaml:%s (use SkillStrategy for YAML skills)", name)
	}

	return nil, fmt.Errorf("unknown strategy or skill: %q", name)
}

// Has returns true if the name can be resolved (Go or YAML).
func (r *UnifiedRegistry) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if _, ok := r.goStrategies[name]; ok {
		return true
	}
	if r.yamlRegistry != nil && r.yamlRegistry.Has(name) {
		return true
	}
	return false
}

// IsGoStrategy returns true if the name is a registered Go strategy.
func (r *UnifiedRegistry) IsGoStrategy(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.goStrategies[name]
	return ok
}

// Names returns all available strategy and skill names, sorted.
func (r *UnifiedRegistry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	seen := make(map[string]bool)
	var names []string

	for name := range r.goStrategies {
		if !seen[name] {
			names = append(names, name)
			seen[name] = true
		}
	}

	if r.yamlRegistry != nil {
		for _, name := range r.yamlRegistry.Names() {
			if !seen[name] {
				names = append(names, name)
				seen[name] = true
			}
		}
	}

	sort.Strings(names)
	return names
}
