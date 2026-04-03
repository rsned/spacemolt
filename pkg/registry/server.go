package registry

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

// ToolType represents the type of tool registered
type ToolType string

const (
	ToolTypeAgentServer  ToolType = "agent-server"
	ToolTypeAutoExplorer ToolType = "auto-explorer"
	ToolTypeAutoMiner    ToolType = "auto-miner"
	ToolTypePlayAs       ToolType = "play-as"
)

// ToolRegistration represents a registered tool
type ToolRegistration struct {
	ToolID        string                 `json:"tool_id"`
	ToolType      ToolType               `json:"tool_type"`
	PID           int                    `json:"pid"`
	AgentID       string                 `json:"agent_id,omitempty"`
	AgentName     string                 `json:"agent_name,omitempty"`
	AgentRole     string                 `json:"agent_role,omitempty"`
	Status        string                 `json:"status"`
	CurrentAction string                 `json:"current_action,omitempty"`
	Capabilities  map[string]interface{} `json:"capabilities"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
	RegisteredAt  time.Time              `json:"registered_at"`
	LastHeartbeat time.Time              `json:"last_heartbeat"`
}

// HeartbeatRequest is sent by tools to update their status
type HeartbeatRequest struct {
	Status        string `json:"status"`
	CurrentAction string `json:"current_action,omitempty"`
	Health        string `json:"health,omitempty"`
	Timestamp     string `json:"timestamp,omitempty"`
}

// RegistryServer manages tool registrations and status broadcasting
type RegistryServer struct {
	mu        sync.RWMutex
	tools     map[string]*ToolRegistration // tool_id -> registration
	streamMgr *StreamManager
	router    *http.ServeMux
	server    *http.Server
	port      int
	logger    *log.Logger
}

// NewServer creates a new status registry server
func NewServer(port int) *RegistryServer {
	s := &RegistryServer{
		tools:     make(map[string]*ToolRegistration),
		streamMgr: NewStreamManager(),
		router:    http.NewServeMux(),
		port:      port,
		logger:    log.Default(),
	}

	s.registerRoutes()

	s.server = &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      corsMiddleware(s.router),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	return s
}

// registerRoutes sets up HTTP route handlers
func (s *RegistryServer) registerRoutes() {
	s.router.HandleFunc("/api/tools/register", s.handleRegister)
	s.router.HandleFunc("/api/tools/", s.handleToolRoute) // Handles /api/tools/{id}/*
	s.router.HandleFunc("/api/tools", s.handleListTools)
	s.router.HandleFunc("/api/status/stream", s.handleStatusStream)
}

// Start begins serving HTTP requests
func (s *RegistryServer) Start() error {
	s.logger.Printf("Status registry starting on port %d", s.port)
	return s.server.ListenAndServe()
}

// Shutdown gracefully shuts down the server
func (s *RegistryServer) Shutdown(ctx context.Context) error {
	s.logger.Printf("Status registry shutting down...")
	return s.server.Shutdown(ctx)
}

// GetStreamManager returns the stream manager
func (s *RegistryServer) GetStreamManager() *StreamManager {
	return s.streamMgr
}

// ListTools returns all registered tools
func (s *RegistryServer) ListTools() []*ToolRegistration {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tools := make([]*ToolRegistration, 0, len(s.tools))
	for _, tool := range s.tools {
		tools = append(tools, tool)
	}
	return tools
}

// GetTool returns a specific tool by ID
func (s *RegistryServer) GetTool(toolID string) (*ToolRegistration, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tool, exists := s.tools[toolID]
	return tool, exists
}

// cleanupInactiveTools removes tools that haven't sent heartbeat recently
func (s *RegistryServer) cleanupInactiveTools(heartbeatTimeout time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for toolID, tool := range s.tools {
		if now.Sub(tool.LastHeartbeat) > heartbeatTimeout {
			s.logger.Printf("Tool %s inactive, removing", toolID)
			delete(s.tools, toolID)

			// Broadcast deregistration event
			s.streamMgr.Publish(Event{
				Type: "tool_deregistered",
				Data: map[string]interface{}{
					"tool_id": toolID,
					"reason":  "heartbeat_timeout",
				},
			})
		}
	}
}

// StartCleanupTask begins periodic cleanup of inactive tools
func (s *RegistryServer) StartCleanupTask(interval, timeout time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			s.cleanupInactiveTools(timeout)
		}
	}()
}
