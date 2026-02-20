package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/rsned/spacemolt/pkg/credentials"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// ObserverServer manages agent connections and browser clients.
type ObserverServer struct {
	agents     map[string]*AgentSession
	browserHub *BrowserHub
	creds      credentials.Provider
	kb         knowledge.Base
	gameURL    string
	cache      *Cache
	mu         sync.RWMutex
	logger     *log.Logger
}

// NewObserverServer creates a new observer server.
func NewObserverServer(creds credentials.Provider, kb knowledge.Base, gameURL string, logger *log.Logger) *ObserverServer {
	return &ObserverServer{
		agents:     make(map[string]*AgentSession),
		browserHub: NewBrowserHub(logger),
		creds:      creds,
		kb:         kb,
		gameURL:    gameURL,
		cache:      NewCache(),
		logger:     logger,
	}
}

// AgentInfo is the summary returned in agent list responses.
type AgentInfo struct {
	Username  string `json:"username"`
	Connected bool   `json:"connected"`
	System    string `json:"system,omitempty"`
	POI       string `json:"poi,omitempty"`
	Docked    bool   `json:"docked"`
}

// AddAgent connects an agent to the game server and starts relaying messages.
// Enhanced with better error handling and connection recovery.
func (s *ObserverServer) AddAgent(ctx context.Context, username string) error {
	s.mu.Lock()
	if _, exists := s.agents[username]; exists {
		s.mu.Unlock()
		return fmt.Errorf("agent %q already connected", username)
	}
	s.mu.Unlock()

	cred, err := s.creds.GetCredentials(ctx, username)
	if err != nil {
		return fmt.Errorf("loading credentials for %q: %w", username, err)
	}

	gameClient := game.NewClient(s.gameURL, cred.Username, cred.Password, nil)

	agentCtx, cancel := context.WithCancel(ctx)

	session := NewAgentSession(username, gameClient, s, agentCtx, cancel, s.logger)

	reconnectHandler := game.NewReconnectingHandler(gameClient, session, agentCtx, s.logger)
	gameClient.SetHandler(reconnectHandler)

	// Connect with retry logic
	maxRetries := 3
	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<uint(attempt)) * time.Second
			s.logger.Printf("Retry %d/%d for agent %q after %v", attempt+1, maxRetries, username, backoff)
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				cancel()
				return ctx.Err()
			}
		}

		if err := gameClient.Connect(agentCtx); err != nil {
			lastErr = err
			s.logger.Printf("Connection attempt %d/%d failed for agent %q: %v", attempt+1, maxRetries, username, err)
			continue
		}

		// Wait for connection to be ready
		if err := gameClient.WaitForReady(agentCtx, 10*time.Second); err != nil {
			lastErr = err
			s.logger.Printf("Connection not ready for agent %q: %v", username, err)
			_ = gameClient.Close()
			continue
		}

		// Login
		if err := gameClient.Login(agentCtx); err != nil {
			lastErr = err
			s.logger.Printf("Login failed for agent %q: %v", username, err)
			_ = gameClient.Close()
			continue
		}

		// Successfully connected and logged in
		s.mu.Lock()
		s.agents[username] = session
		s.mu.Unlock()

		s.logger.Printf("agent %q added successfully", username)
		return nil
	}

	// All retries failed
	cancel()
	_ = gameClient.Close()
	return fmt.Errorf("failed to connect agent %q after %d attempts: %w", username, maxRetries, lastErr)
}

// RemoveAgent disconnects an agent and removes it from the server.
func (s *ObserverServer) RemoveAgent(username string) error {
	s.mu.Lock()
	session, exists := s.agents[username]
	if !exists {
		s.mu.Unlock()
		return fmt.Errorf("agent %q not found", username)
	}
	delete(s.agents, username)
	s.mu.Unlock()

	session.Close()
	s.logger.Printf("agent %q removed", username)
	return nil
}

// ListAgents returns info about all connected agents.
func (s *ObserverServer) ListAgents() []AgentInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	agents := make([]AgentInfo, 0, len(s.agents))
	for name, session := range s.agents {
		info := AgentInfo{
			Username:  name,
			Connected: session.IsConnected(),
		}
		state := session.gameClient.GetState()
		if state != nil {
			state.Mu.Lock()
			info.System = state.CurrentSystem
			info.POI = state.CurrentPOI
			info.Docked = state.Doc
			state.Mu.Unlock()
		}
		agents = append(agents, info)
	}
	return agents
}

func (s *ObserverServer) getAgent(username string) *AgentSession {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.agents[username]
}

// HandleBrowserWS upgrades an HTTP connection to a WebSocket for browser clients.
func (s *ObserverServer) HandleBrowserWS(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{"*"},
	})
	if err != nil {
		s.logger.Printf("websocket accept error: %v", err)
		return
	}

	client := NewBrowserClient(conn, s, s.logger)
	client.Run(r.Context())
}

// HandleAPIAgents handles REST API requests for agent management with enhanced error handling.
func (s *ObserverServer) HandleAPIAgents(w http.ResponseWriter, r *http.Request) {
	// Set common headers
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		agents := s.ListAgents()
		writeJSON(w, http.StatusOK, agents)

	case http.MethodPost:
		var req struct {
			Username string `json:"username"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error":   "invalid request body",
				"details": "failed to parse JSON",
			})
			return
		}
		if req.Username == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "username is required",
			})
			return
		}

		// Add agent with timeout
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		if err := s.AddAgent(ctx, req.Username); err != nil {
			// Categorize the error for better client handling
			statusCode := http.StatusInternalServerError
			errorType := "internal_error"

			errMsg := err.Error()
			if contains(errMsg, "already connected") {
				statusCode = http.StatusConflict
				errorType = "already_exists"
			} else if contains(errMsg, "loading credentials") || contains(errMsg, "not found") {
				statusCode = http.StatusNotFound
				errorType = "credentials_not_found"
			} else if contains(errMsg, "timeout") || contains(errMsg, "deadline exceeded") {
				statusCode = http.StatusGatewayTimeout
				errorType = "connection_timeout"
			}

			writeJSON(w, statusCode, map[string]any{
				"error":    err.Error(),
				"type":     errorType,
				"username": req.Username,
			})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"status":   "ok",
			"username": req.Username,
			"message":  "agent connected successfully",
		})

	case http.MethodDelete:
		username := r.PathValue("username")
		if username == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "username is required",
			})
			return
		}
		if err := s.RemoveAgent(username); err != nil {
			writeJSON(w, http.StatusNotFound, map[string]any{
				"error":    err.Error(),
				"username": username,
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"status":   "ok",
			"message":  "agent removed successfully",
			"username": username,
		})

	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{
			"error": "method not allowed",
		})
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("failed to write JSON response: %v", err)
		// Try to send a plain error if JSON encoding fails
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}
}

// contains checks if a string contains a substring (case-insensitive).
func contains(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr ||
			len(s) > len(substr) && (
				s[:len(substr)] == substr ||
				s[len(s)-len(substr):] == substr ||
				containsMiddle(s, substr)))
}

func containsMiddle(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Close shuts down all agent sessions.
func (s *ObserverServer) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for name, session := range s.agents {
		session.Close()
		s.logger.Printf("agent %q closed", name)
	}
	s.agents = make(map[string]*AgentSession)
}
