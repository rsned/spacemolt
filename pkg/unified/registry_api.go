package unified

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/rsned/spacemolt/pkg/registry"
)

// registerWithRegistry registers this server with the status registry
// and starts a heartbeat goroutine.
func (s *Server) registerWithRegistry() {
	toolID := "spacemolt-server"
	s.registry = registry.NewClient(s.config.Server.RegistryURL, toolID)

	reg := registry.ToolRegistration{
		ToolID:   toolID,
		ToolType: registry.ToolTypeAgentServer,
		PID:      os.Getpid(),
		Status:   "running",
		Capabilities: map[string]any{
			"http_api": fmt.Sprintf("http://localhost:%d", s.config.Server.HTTPPort),
		},
		Metadata: map[string]any{
			"server_url":        s.config.Game.ServerURL,
			"decision_interval": s.config.Agents.DecisionInterval,
			"max_agents":        s.config.Agents.Max,
		},
	}

	if err := s.registry.Register(reg); err != nil {
		s.logger.Printf("warning: failed to register with status registry: %v", err)
		s.registry = nil
		return
	}
	s.logger.Printf("registered with status registry: %s", s.config.Server.RegistryURL)

	ctx := context.Background()
	s.registry.StartHeartbeat(ctx, 5*time.Second, func() (string, string) {
		runners := s.manager.ListRunners()
		stratRunners := s.manager.ListStrategyRunners()
		return "running", fmt.Sprintf("Managing %d LLM + %d strategy agents", len(runners), len(stratRunners))
	})
}
