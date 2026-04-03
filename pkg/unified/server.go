package unified

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/rsned/spacemolt/pkg/agent"
	"github.com/rsned/spacemolt/pkg/agentstate"
	"github.com/rsned/spacemolt/pkg/api"
	"github.com/rsned/spacemolt/pkg/credentials"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/llm"
	"github.com/rsned/spacemolt/pkg/monitor"
	"github.com/rsned/spacemolt/pkg/observe"
	"github.com/rsned/spacemolt/pkg/registry"
	"github.com/rsned/spacemolt/pkg/strategy"
	"github.com/rsned/spacemolt/pkg/team"
	"github.com/rsned/spacemolt/pkg/tot"
)

// Server is the unified spacemolt server that combines agent management,
// the observer/frontend server, and shared resources.
type Server struct {
	config          Config
	observer        *observe.ObserverServer
	manager         *agent.Manager
	strategies      *strategy.Registry
	monitor         *monitor.Collector
	teamCoordinator *team.TeamCoordinator
	streams         *api.StreamManager
	registry        *registry.Client
	kb              knowledge.Base
	llm             *llm.Client
	creds           credentials.Provider
	mux             *http.ServeMux
	http            *http.Server
	logger          *log.Logger
}

// New creates a new unified server from the given config.
func New(cfg Config) (*Server, error) {
	logger := log.New(os.Stdout, "[spacemolt] ", log.LstdFlags|log.Lmsgprefix)

	// Initialize knowledge base.
	kb, err := initKB(cfg.Database)
	if err != nil {
		return nil, fmt.Errorf("initializing knowledge base: %w", err)
	}
	logger.Printf("knowledge base initialized (%s)", cfg.Database.Backend)

	// Initialize credentials provider.
	creds, err := initCreds(cfg.Credentials)
	if err != nil {
		_ = kb.Close()
		return nil, fmt.Errorf("initializing credentials: %w", err)
	}
	logger.Printf("credentials provider initialized (%s)", cfg.Credentials.Backend)

	// Initialize LLM client.
	llmClient, err := initLLM(cfg.LLM)
	if err != nil {
		_ = kb.Close()
		return nil, fmt.Errorf("initializing LLM client: %w", err)
	}
	logger.Printf("LLM client initialized (%s @ %s)", cfg.LLM.Model, cfg.LLM.URL)

	// Create observer server.
	obs := observe.NewObserverServer(creds, kb, cfg.Game.ServerURL, logger)

	// Create agent manager.
	managerCfg := agent.DefaultManagerConfig()
	managerCfg.MaxAgents = cfg.Agents.Max
	managerCfg.GameServerURL = cfg.Game.ServerURL
	managerCfg.AgentsDataDir = cfg.Agents.Dir
	managerCfg.RunnerConfig.DecisionInterval = cfg.Agents.DecisionInterval
	managerCfg.Transport = cfg.Game.Transport
	managerCfg.DebugLogger = logger
	managerCfg.EnrichedStateFactory = func(state *game.State, kb knowledge.Base) agent.EnrichedState {
		return agentstate.New(state, kb)
	}

	mgr := agent.NewManager(kb, llmClient, creds, managerCfg)
	logger.Printf("agent manager created (max agents: %d)", cfg.Agents.Max)

	// Create strategy registry.
	strategies := strategy.DefaultRegistry()
	logger.Printf("strategy registry initialized (%d strategies)", len(strategies.List()))

	// Create monitoring collector.
	mon := monitor.NewCollector()
	mon.SetAgentCountFunc(func() (int, int) {
		return len(mgr.ListRunners()), len(mgr.ListStrategyRunners())
	})

	// Create team coordinator.
	tc := team.NewCoordinator(func(agentID string) *game.Client {
		// Try strategy runners first.
		if sr, ok := mgr.GetStrategyRunner(agentID); ok {
			if c, ok := sr.GameClient().(*game.Client); ok {
				return c
			}
		}
		// Try LLM runners.
		if r, ok := mgr.GetRunner(agentID); ok {
			gc := r.GetGameClient()
			if c, ok := gc.(*game.Client); ok {
				return c
			}
		}
		return nil
	}, logger)

	// Register configured teams.
	for i, tcfg := range cfg.Teams {
		teamID := fmt.Sprintf("team-%d", i+1)
		members := make([]team.Member, len(tcfg.Members))
		for j, m := range tcfg.Members {
			members[j] = team.Member{
				AgentID: m.AgentID,
				Role:    m.Role,
			}
		}
		tc.AddTeam(team.Team{
			ID:       teamID,
			Name:     tcfg.Name,
			LeaderID: tcfg.Leader,
			Members:  members,
		})
	}

	// Build HTTP mux.
	mux := http.NewServeMux()
	obs.RegisterRoutes(mux)

	// Serve static frontend files.
	if cfg.Server.StaticDir != "" {
		if info, statErr := os.Stat(cfg.Server.StaticDir); statErr == nil && info.IsDir() {
			logger.Printf("serving static files from %s", cfg.Server.StaticDir)
			mux.Handle("/", observe.NewSPAFileServer(cfg.Server.StaticDir))
		} else {
			logger.Printf("static directory %q not found, skipping", cfg.Server.StaticDir)
		}
	}

	// Serve admin static files.
	if cfg.Server.AdminStaticDir != "" {
		if info, statErr := os.Stat(cfg.Server.AdminStaticDir); statErr == nil && info.IsDir() {
			logger.Printf("serving admin files from %s", cfg.Server.AdminStaticDir)
			mux.Handle("/admin/", http.StripPrefix("/admin/", http.FileServer(http.Dir(cfg.Server.AdminStaticDir))))
		}
	}

	httpServer := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Server.HTTPPort),
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	srv := &Server{
		config:          cfg,
		observer:        obs,
		manager:         mgr,
		strategies:      strategies,
		monitor:         mon,
		teamCoordinator: tc,
		streams:         api.NewStreamManager(),
		kb:              kb,
		llm:             llmClient,
		creds:           creds,
		mux:             mux,
		http:            httpServer,
		logger:          logger,
	}

	// Register additional API routes.
	srv.registerStrategyRoutes()
	srv.registerTeamRoutes()
	srv.registerModelRoutes()
	srv.registerStreamRoutes()
	srv.registerAgentDetailRoutes()
	monitor.RegisterRoutes(mux, mon)

	return srv, nil
}

// Observer returns the underlying ObserverServer.
func (s *Server) Observer() *observe.ObserverServer { return s.observer }

// Manager returns the agent manager.
func (s *Server) Manager() *agent.Manager { return s.manager }

// KB returns the knowledge base.
func (s *Server) KB() knowledge.Base { return s.kb }

// Mux returns the HTTP mux for adding additional routes.
func (s *Server) Mux() *http.ServeMux { return s.mux }

// SpawnConfiguredAgents spawns all agents listed in the config.
func (s *Server) SpawnConfiguredAgents(ctx context.Context) (int, []string) {
	var successCount int
	var failedAgents []string

	publish := s.makeEventPublisher()

	for _, agentID := range s.config.Agents.Enabled {
		personality, err := agent.LoadPersonalityJSON(
			fmt.Sprintf("%s/%s/personality.json", s.config.Agents.Dir, agentID),
		)
		if err != nil {
			s.logger.Printf("[%s] failed to load personality: %v", agentID, err)
			failedAgents = append(failedAgents, agentID)
			continue
		}

		s.logger.Printf("[%s] spawning agent: %s (%s)", agentID, personality.Name, personality.Role)
		runner, err := s.manager.SpawnAgentWithGame(ctx, personality)
		if err != nil {
			s.logger.Printf("[%s] failed to spawn: %v", agentID, err)
			failedAgents = append(failedAgents, agentID)
			continue
		}

		// Wire event streaming callback.
		runner.SetEventCallback(publish)

		// Wire Tree-of-Thought evaluator if agent has decision_mode="tot"
		if personality.DecisionMode == "tot" && s.llm != nil {
			adapter := &tot.RunnerAdapter{
				Eval:     tot.NewEvaluator(s.llm, s.llm.Model()),
				OnUpdate: publish,
			}
			runner.SetToTEvaluator(adapter)
			s.logger.Printf("[%s] Thought Engine enabled", agentID)
		}

		s.logger.Printf("[%s] started successfully", agentID)
		successCount++
	}

	return successCount, failedAgents
}

// Start begins serving HTTP and returns immediately. Use Shutdown for graceful stop.
func (s *Server) Start() error {
	s.logger.Printf("starting unified server on %s", s.http.Addr)
	go func() {
		if err := s.http.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Printf("HTTP server error: %v", err)
		}
	}()

	// Register with status registry if configured.
	if s.config.Server.RegistryURL != "" {
		s.registerWithRegistry()
	}

	return nil
}

// Shutdown gracefully shuts down the server, agents, and resources.
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Println("shutting down unified server...")

	// Stop agent manager.
	if err := s.manager.StopAll(); err != nil {
		s.logger.Printf("error stopping agents: %v", err)
	}

	// Deregister from status registry.
	if s.registry != nil {
		if err := s.registry.Deregister(); err != nil {
			s.logger.Printf("registry deregister error: %v", err)
		} else {
			s.logger.Println("deregistered from status registry")
		}
	}

	// Close observer sessions.
	s.observer.Close()

	// Shutdown HTTP.
	if err := s.http.Shutdown(ctx); err != nil {
		s.logger.Printf("HTTP shutdown error: %v", err)
	}

	// Close knowledge base.
	if err := s.kb.Close(); err != nil {
		s.logger.Printf("knowledge base close error: %v", err)
	}

	s.logger.Println("unified server stopped")
	return nil
}

func initKB(cfg DatabaseConfig) (knowledge.Base, error) {
	switch cfg.Backend {
	case "sqlite":
		return knowledge.NewSQLiteKB(knowledge.Config{
			DBPath:       cfg.Path,
			WAL:          true,
			MaxOpenConns: 25,
			MaxIdleConns: 5,
			BusyTimeout:  5 * time.Second,
		})
	case "memory":
		return knowledge.NewMemoryKB(), nil
	default:
		return nil, fmt.Errorf("unknown database backend: %s", cfg.Backend)
	}
}

func initCreds(cfg CredentialsConfig) (credentials.Provider, error) {
	switch cfg.Backend {
	case "file":
		path := cfg.Path
		if path == "" {
			path = "data/credentials"
		}
		return credentials.NewFileProvider(path), nil
	case "sqlite":
		path := cfg.Path
		if path == "" {
			path = "credentials.db"
		}
		enc, err := credentials.LoadOrGenerateKey()
		if err != nil {
			return nil, fmt.Errorf("loading encryption key: %w", err)
		}
		return credentials.NewSQLiteProvider(path, enc)
	case "env":
		return credentials.NewEnvProvider("SPACEMOLT"), nil
	case "keyring":
		return credentials.NewKeyringProvider("spacemolt"), nil
	default:
		return nil, fmt.Errorf("unknown credentials backend: %s", cfg.Backend)
	}
}

func initLLM(cfg LLMConfig) (*llm.Client, error) {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}
	return llm.New(llm.Config{
		BaseURL:       cfg.URL,
		Model:         cfg.Model,
		Timeout:       timeout,
		PromptsDir:    "data/prompts/templates",
		PromptsConfig: "data/prompts/config.yaml",
	})
}
