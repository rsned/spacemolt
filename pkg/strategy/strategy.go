package strategy

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/llm"
)

// Strategy defines a reusable game behavior loop that can be assigned to agents.
type Strategy interface {
	// Name returns the strategy's unique identifier.
	Name() string

	// Description returns a human-readable description.
	Description() string

	// Run executes the strategy loop. It should block until ctx is canceled
	// or the strategy completes. It returns nil on graceful shutdown.
	Run(ctx context.Context, client game.GameClient, cfg Config) error

	// CurrentStatus returns a short human-readable status string.
	CurrentStatus() string
}

// Config holds configuration passed to a strategy when it runs.
type Config struct {
	AgentID        string
	Logger         *log.Logger
	KnowledgeBase  knowledge.Base
	LLMClient      *llm.Client // optional, used by SmartIdleStrategy
	OnStatusUpdate func(status string)
	Parameters     map[string]string // e.g., "station_action": "craft-sell"
}

// Registry holds all available strategies.
type Registry struct {
	strategies map[string]Strategy
	mu         sync.RWMutex
}

// NewRegistry creates an empty strategy registry.
func NewRegistry() *Registry {
	return &Registry{
		strategies: make(map[string]Strategy),
	}
}

// Register adds a strategy to the registry. Panics on duplicate names.
func (r *Registry) Register(s Strategy) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.strategies[s.Name()]; exists {
		panic(fmt.Sprintf("strategy %q already registered", s.Name()))
	}
	r.strategies[s.Name()] = s
}

// Get returns a strategy by name, or nil if not found.
func (r *Registry) Get(name string) Strategy {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.strategies[name]
}

// List returns all registered strategy names and descriptions.
func (r *Registry) List() []Info {
	r.mu.RLock()
	defer r.mu.RUnlock()

	infos := make([]Info, 0, len(r.strategies))
	for _, s := range r.strategies {
		infos = append(infos, Info{
			Name:        s.Name(),
			Description: s.Description(),
		})
	}
	return infos
}

// Info is a summary of a registered strategy.
type Info struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// DefaultRegistry creates a registry pre-loaded with all built-in strategies.
func DefaultRegistry() *Registry {
	r := NewRegistry()
	r.Register(&MiningStrategy{})
	r.Register(&ExplorerStrategy{})
	r.Register(&FighterStrategy{})
	r.Register(&TraderStrategy{})
	r.Register(&SmartIdleStrategy{})
	return r
}
