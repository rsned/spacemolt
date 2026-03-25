package api

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/rsned/spacemolt/pkg/agent"
	"github.com/rsned/spacemolt/pkg/llm"
)

// Server provides HTTP API for agent-server
type Server struct {
	manager       *agent.Manager
	streamManager *StreamManager
	router        *http.ServeMux
	server        *http.Server
	port          int
	logger        *log.Logger
	webDir        string // Path to web UI static files (empty = disabled)
	llmClient     *llm.Client
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

	// Auto-detect web UI directory
	s.webDir = findWebDir()

	// Register routes
	s.registerRoutes()

	// Create HTTP server with CORS middleware
	s.server = &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      corsMiddleware(s.router),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 0, // Disable for SSE streams
		IdleTimeout:  60 * time.Second,
	}

	return s
}

// SetLLMClient sets the LLM client for model hot-swapping via the API.
func (s *Server) SetLLMClient(client *llm.Client) {
	s.llmClient = client
}

// registerRoutes sets up HTTP route handlers
func (s *Server) registerRoutes() {
	// Agent endpoints
	s.router.HandleFunc("/api/agents", s.handleListAgents)
	s.router.HandleFunc("/api/agents/", s.handleAgentRoute) // Handles /api/agents/{id}/*
	s.router.HandleFunc("/api/model", s.handleModel)

	// Serve web UI static files if available
	if s.webDir != "" {
		s.logger.Printf("Serving web UI from %s", s.webDir)
		fs := http.FileServer(http.Dir(s.webDir))
		s.router.Handle("/", fs)
	}
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

// corsMiddleware adds CORS headers to all responses
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, PUT, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// findWebDir looks for the web/ directory relative to the executable or cwd
func findWebDir() string {
	// Check relative to current working directory
	candidates := []string{"web", "../../web"}

	// Also check relative to executable location
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(exeDir, "web"),
			filepath.Join(exeDir, "..", "web"),
			filepath.Join(exeDir, "..", "..", "web"),
		)
	}

	for _, dir := range candidates {
		absDir, err := filepath.Abs(dir)
		if err != nil {
			continue
		}
		if info, err := os.Stat(filepath.Join(absDir, "index.html")); err == nil && !info.IsDir() {
			return absDir
		}
	}

	return ""
}
