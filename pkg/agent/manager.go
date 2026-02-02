package agent

import (
	"context"
	"fmt"
	"sync"

	"github.com/user/spacemolt/pkg/knowledge"
	"github.com/user/spacemolt/pkg/llm"
)

// Manager manages multiple agents
type Manager struct {
	agents   map[string]Agent
	kb       knowledge.Base
	llm      *llm.Client
	mu       sync.RWMutex

	// Configuration
	maxAgents int
}

// NewManager creates a new agent manager
func NewManager(kb knowledge.Base, llmClient *llm.Client, maxAgents int) *Manager {
	if maxAgents <= 0 {
		maxAgents = 10 // Default max
	}

	return &Manager{
		agents:   make(map[string]Agent),
		kb:       kb,
		llm:      llmClient,
		maxAgents: maxAgents,
	}
}

// SpawnAgent creates and starts a new agent
func (m *Manager) SpawnAgent(ctx context.Context, personality Personality) (Agent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.agents) >= m.maxAgents {
		return nil, fmt.Errorf("max agents limit reached: %d", m.maxAgents)
	}

	// Check if agent already exists
	if _, exists := m.agents[personality.ID]; exists {
		return nil, fmt.Errorf("agent %s already exists", personality.ID)
	}

	// Create agent memory
	memory := NewKBMemory(m.kb, personality.ID)

	// Create agent
	agent := NewBaseAgent(
		personality.ID,
		personality,
		memory,
		m.llm,
	)

	// Register agent in knowledge base
	if err := m.kb.RegisterAgent(
		ctx,
		personality.ID,
		personality.Name,
		personality.Role,
		personality.Faction,
		nil, // personality data
	); err != nil {
		return nil, fmt.Errorf("failed to register agent: %w", err)
	}

	// Start agent
	if err := agent.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start agent: %w", err)
	}

	m.agents[agent.ID()] = agent

	return agent, nil
}

// GetAgent retrieves an agent by ID
func (m *Manager) GetAgent(id string) (Agent, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	agent, ok := m.agents[id]
	return agent, ok
}

// ListAgents returns all agents
func (m *Manager) ListAgents() []Agent {
	m.mu.RLock()
	defer m.mu.RUnlock()

	agents := make([]Agent, 0, len(m.agents))
	for _, agent := range m.agents {
		agents = append(agents, agent)
	}
	return agents
}

// StopAgent stops and removes an agent
func (m *Manager) StopAgent(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	agent, ok := m.agents[id]
	if !ok {
		return fmt.Errorf("agent not found: %s", id)
	}

	if err := agent.Stop(); err != nil {
		return fmt.Errorf("failed to stop agent: %w", err)
	}

	delete(m.agents, id)
	return nil
}

// StopAll stops all agents
func (m *Manager) StopAll() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var firstErr error
	for id, agent := range m.agents {
		if err := agent.Stop(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("failed to stop agent %s: %w", id, err)
		}
	}

	m.agents = make(map[string]Agent)
	return firstErr
}

// AgentCount returns the number of active agents
func (m *Manager) AgentCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.agents)
}

// GetStatus returns the status of all agents
func (m *Manager) GetStatus() map[string]Status {
	m.mu.RLock()
	defer m.mu.RUnlock()

	statuses := make(map[string]Status, len(m.agents))
	for id, agent := range m.agents {
		statuses[id] = agent.Status()
	}
	return statuses
}
