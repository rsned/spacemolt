package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"

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

	if err := gameClient.Connect(agentCtx); err != nil {
		cancel()
		return fmt.Errorf("connecting agent %q: %w", username, err)
	}

	if err := gameClient.Login(agentCtx); err != nil {
		cancel()
		_ = gameClient.Close()
		return fmt.Errorf("logging in agent %q: %w", username, err)
	}

	s.mu.Lock()
	s.agents[username] = session
	s.mu.Unlock()

	s.logger.Printf("agent %q added", username)
	return nil
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

// HandleAPIAgents handles REST API requests for agent management.
func (s *ObserverServer) HandleAPIAgents(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		agents := s.ListAgents()
		writeJSON(w, http.StatusOK, agents)

	case http.MethodPost:
		var req struct {
			Username string `json:"username"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
			return
		}
		if req.Username == "" {
			http.Error(w, `{"error":"username is required"}`, http.StatusBadRequest)
			return
		}
		if err := s.AddAgent(r.Context(), req.Username); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{"status": "ok", "username": req.Username})

	case http.MethodDelete:
		username := r.PathValue("username")
		if username == "" {
			http.Error(w, `{"error":"username is required"}`, http.StatusBadRequest)
			return
		}
		if err := s.RemoveAgent(username); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})

	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("failed to write JSON response: %v", err)
	}
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
