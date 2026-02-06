package api

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/rsned/spacemolt/pkg/agent"
)

// Server provides HTTP API for agent-server
type Server struct {
	manager       *agent.Manager
	streamManager *StreamManager
	router        *http.ServeMux
	server        *http.Server
	port          int
	logger        *log.Logger
}

// NewServer creates a new HTTP API server
func NewServer(manager *agent.Manager, port int) *Server {
	s := &Server{
		manager:       manager,
		streamManager: NewStreamManager(),
		router:        http.NewServeMux(),
		port:          port,
		logger:        log.Default(),
	}

	// Register routes
	s.registerRoutes()

	// Create HTTP server
	s.server = &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      s.router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	return s
}

// registerRoutes sets up HTTP route handlers
func (s *Server) registerRoutes() {
	// Agent endpoints
	s.router.HandleFunc("/api/agents", s.handleListAgents)
	s.router.HandleFunc("/api/agents/", s.handleAgentRoute) // Handles /api/agents/{id}/*
}

// Start begins serving HTTP requests
func (s *Server) Start() error {
	s.logger.Printf("HTTP API starting on port %d", s.port)
	return s.server.ListenAndServe()
}

// Shutdown gracefully shuts down the HTTP server
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Printf("HTTP API shutting down...")
	return s.server.Shutdown(ctx)
}

// GetStreamManager returns the stream manager (for integration with runners)
func (s *Server) GetStreamManager() *StreamManager {
	return s.streamManager
}
