package strategy

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/skills"
)

// SkillStrategy adapts a YAML skill into the Strategy interface. It wraps
// a skills.Executor to run a named skill from the YAML skill registry.
type SkillStrategy struct {
	skillName   string
	description string
	registry    *skills.Registry
	dispatcher  skills.ActionDispatcher
	logger      *log.Logger
	params      map[string]string

	mu     sync.RWMutex
	status string
}

// SkillStrategyConfig holds the configuration for creating a SkillStrategy.
type SkillStrategyConfig struct {
	SkillName   string
	Description string
	Registry    *skills.Registry
	Dispatcher  skills.ActionDispatcher
	Logger      *log.Logger
	Params      map[string]string
}

// NewSkillStrategy creates a Strategy that runs a YAML skill.
func NewSkillStrategy(cfg SkillStrategyConfig) *SkillStrategy {
	return &SkillStrategy{
		skillName:   cfg.SkillName,
		description: cfg.Description,
		registry:    cfg.Registry,
		dispatcher:  cfg.Dispatcher,
		logger:      cfg.Logger,
		params:      cfg.Params,
	}
}

func (s *SkillStrategy) Name() string { return s.skillName }

func (s *SkillStrategy) Description() string {
	if s.description != "" {
		return s.description
	}
	return fmt.Sprintf("YAML skill: %s", s.skillName)
}

func (s *SkillStrategy) CurrentStatus() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.status != "" {
		return s.status
	}
	return fmt.Sprintf("skill:%s", s.skillName)
}

func (s *SkillStrategy) Run(ctx context.Context, _ game.GameClient, _ Config) error {
	s.setStatus(fmt.Sprintf("running:%s", s.skillName))
	defer s.setStatus(fmt.Sprintf("idle:%s", s.skillName))

	executor := skills.NewExecutor(s.registry, s.dispatcher, s.logger)
	return executor.RunWithParams(ctx, s.skillName, s.params)
}

func (s *SkillStrategy) setStatus(status string) {
	s.mu.Lock()
	s.status = status
	s.mu.Unlock()
}
