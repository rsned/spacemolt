package tui

import (
	"github.com/rsned/spacemolt/pkg/game"
)

// AgentPanelModel represents the agent list panel for the watcher interface
type AgentPanelModel struct {
	agents      []string
	selected    int
	showDetails bool
}

// NewAgentPanelModel creates a new agent panel model
func NewAgentPanelModel() AgentPanelModel {
	return AgentPanelModel{
		agents:      []string{},
		selected:    0,
		showDetails: false,
	}
}

// MultiAgentWatcherModel extends WatcherModel with multi-agent support
// This will be used in phase 3 when we have multiple agents
type MultiAgentWatcherModel struct {
	*WatcherModel

	// Additional fields for multi-agent support
	// agentManager  *agent.Manager
	// selectedAgent agent.Agent
	// agentPanel    AgentPanelModel
}

// NewMultiAgentWatcherModel creates a new multi-agent watcher model
func NewMultiAgentWatcherModel(state *game.State, readyChan chan struct{}) *MultiAgentWatcherModel {
	return &MultiAgentWatcherModel{
		WatcherModel: NewWatcherModel(state, readyChan),
		// agentPanel:    NewAgentPanelModel(),
	}
}
