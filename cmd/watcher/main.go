package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/rsned/spacemolt/internal/protocol"
	"github.com/rsned/spacemolt/pkg/agent"
	"github.com/rsned/spacemolt/pkg/credentials"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/llm"
	"github.com/rsned/spacemolt/pkg/tui"
)

var (
	// Command-line flags
	debugMode           = flag.Bool("debug", false, "Enable debug logging to stderr")
	logFile             = flag.String("log-file", "", "Write debug logs to file instead of stderr")
	agents              = flag.String("agents", "", "Comma-separated list of agent IDs to spawn (e.g., 'explorer-7,miner-2') - local mode only")
	agentServerURL      = flag.String("agent-server-url", "", "Agent-server HTTP API URL (e.g., http://localhost:8080) - remote mode")
	dbBackend           = flag.String("db-backend", "sqlite", "Database backend: 'sqlite' or 'memory'")
	dbPath              = flag.String("db-path", "spacemolt-knowledge.db", "Path to SQLite database file (only used with sqlite backend)")
	credsProvider       = flag.String("credentials-provider", "auto", "Credentials provider: 'auto', 'file', 'sqlite', 'env', 'legacy', 'static'")
	credsFile           = flag.String("credentials-file", "data/agents/*/credentials.json", "Glob pattern for file provider")
	credsSQLitePath     = flag.String("credentials-db", "", "Path to SQLite credentials database (for sqlite provider)")
	credsKeyFile        = flag.String("credentials-key", "", "Path to encryption key file (for sqlite provider)")
	staticUsername      = flag.String("static-username", "", "Username for static provider")
	staticToken         = flag.String("static-token", "", "Token for static provider")
	staticEmpire        = flag.String("static-empire", "voidborn", "Empire for static provider")
)

var (
	// debugLogger is the logger for debug messages (can be disabled)
	debugLogger *log.Logger = log.New(io.Discard, "", log.LstdFlags)
)

var credentialsFile = ".spacemolt-credentials.json"

// AgentWrapper wraps an agent with its game client
type AgentWrapper struct {
	Agent   agent.Agent
	Client  *game.Client
	Program *tea.Program
}

// initCredentialsProvider initializes the credential provider based on flags
func initCredentialsProvider() (credentials.Provider, error) {
	switch *credsProvider {
	case "auto", "":
		// Auto mode: try file, then legacy
		return credentials.NewFallbackProvider(
			credentials.NewFileProvider("data/agents"),
			credentials.NewLegacyProvider(credentialsFile),
		), nil

	case "file":
		return credentials.NewFileProvider(*credsFile), nil

	case "sqlite":
		if *credsSQLitePath == "" {
			return nil, fmt.Errorf("sqlite provider requires --credentials-db")
		}
		var encryptor *credentials.Encryptor
		if *credsKeyFile != "" {
			key, err := credentials.LoadKey(*credsKeyFile)
			if err != nil {
				return nil, fmt.Errorf("failed to load encryption key: %w", err)
			}
			encryptor, err = credentials.NewEncryptor(key)
			if err != nil {
				return nil, fmt.Errorf("failed to create encryptor: %w", err)
			}
		} else {
			// Try to load or generate key
			var err error
			encryptor, err = credentials.LoadOrGenerateKey()
			if err != nil {
				return nil, fmt.Errorf("failed to initialize encryption: %w", err)
			}
		}
		return credentials.NewSQLiteProvider(*credsSQLitePath, encryptor)

	case "env":
		return credentials.NewEnvProvider("SPACEMOLT_AGENT_"), nil

	case "legacy":
		return credentials.NewLegacyProvider(credentialsFile), nil

	case "static":
		if *staticUsername == "" || *staticToken == "" {
			return nil, fmt.Errorf("static provider requires --static-username and --static-token")
		}
		return credentials.NewStaticProvider(*staticUsername, *staticToken, *staticEmpire), nil

	default:
		return nil, fmt.Errorf("unknown credentials provider: %s", *credsProvider)
	}
}

func main() {
	flag.Parse()

	// Initialize debug logger
	if *debugMode || *logFile != "" {
		var writer io.Writer = os.Stderr
		if *logFile != "" {
			f, err := os.OpenFile(*logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
			if err != nil {
				log.Fatalf("Failed to open log file: %v", err)
			}
			defer func() { _ = f.Close() }()
			writer = f
		}
		debugLogger = log.New(writer, "[DEBUG] ", log.LstdFlags)
	}

	ctx := context.Background()

	// Determine operating mode
	remoteMode := *agentServerURL != ""
	localMode := *agents != ""

	if !remoteMode && !localMode {
		log.Fatal("Must provide either --agent-server-url (remote mode) or --agents (local mode)")
	}

	if remoteMode && localMode {
		log.Fatal("Cannot use both --agent-server-url and --agents. Choose remote or local mode.")
	}

	// Check for existing credentials (local mode only)
	var username, password string
	if localMode {
		if data, err := os.ReadFile(credentialsFile); err == nil {
			var creds map[string]string
			if err := json.Unmarshal(data, &creds); err == nil {
				username = creds["username"]
				password = creds["password"]
			// Backward compatibility: check for "token" field
			if password == "" {
				password = creds["token"]
			}
			}
		}
	}

	// Local mode setup (spawn agents directly)
	var agentMgr *agent.Manager
	var kb knowledge.Base

	if localMode {
		debugLogger.Printf("Local mode: spawning agents %s", *agents)

		// Create LLM client
		llmClient, err := llm.New(llm.Config{
			BaseURL: "http://localhost:11434",
			Model:   "llama3.2",
			Timeout: 60 * time.Second,
		})
		debugLogger.Printf("Created LLM client")

		// Test LLM connection
		if err := llmClient.TestConnection(ctx); err != nil {
			log.Printf("Warning: Could not connect to Ollama: %v", err)
			log.Printf("Agents will not be able to make decisions without Ollama running")
		} else {
			debugLogger.Printf("Connected to Ollama successfully")
		}

		// Create knowledge base
		switch *dbBackend {
		case "sqlite":
			sqliteKB, err := knowledge.NewSQLiteKB(knowledge.Config{
				DBPath:       *dbPath,
				WAL:          true,
				MaxOpenConns: 25,
				MaxIdleConns: 5,
				BusyTimeout:  5 * time.Second,
			})
			if err != nil {
				log.Fatalf("Failed to create SQLite knowledge base: %v", err)
			}
			kb = sqliteKB
			debugLogger.Printf("Created SQLite knowledge base at %s", *dbPath)
			defer func() { _ = kb.Close() }()
		case "memory":
			kb = knowledge.NewMemoryKB()
			debugLogger.Printf("Created in-memory knowledge base")
		default:
			log.Fatalf("Unknown db-backend: %s (use 'sqlite' or 'memory')", *dbBackend)
		}
		debugLogger.Printf("Created knowledge base")

		// Initialize credential provider
		credsProv, err := initCredentialsProvider()
		if err != nil {
			log.Fatalf("Failed to initialize LLM client: %v", err)
		}
		debugLogger.Printf("Created LLM client")

		// Test LLM connection
		if err := llmClient.TestConnection(ctx); err != nil {
			log.Printf("Warning: Could not connect to Ollama: %v", err)
			log.Printf("Agents will not be able to make decisions without Ollama running")
		} else {
			debugLogger.Printf("Connected to Ollama successfully")
		}

		// Create knowledge base
		switch *dbBackend {
		case "sqlite":
			sqliteKB, err := knowledge.NewSQLiteKB(knowledge.Config{
				DBPath:       *dbPath,
				WAL:          true,
				MaxOpenConns: 25,
				MaxIdleConns: 5,
				BusyTimeout:  5 * time.Second,
			})
			if err != nil {
				log.Fatalf("Failed to create SQLite knowledge base: %v", err)
			}
			kb = sqliteKB
			debugLogger.Printf("Created SQLite knowledge base at %s", *dbPath)
			defer func() { _ = kb.Close() }()
		case "memory":
			kb = knowledge.NewMemoryKB()
			debugLogger.Printf("Created in-memory knowledge base")
		default:
			log.Fatalf("Unknown db-backend: %s (use 'sqlite' or 'memory')", *dbBackend)
		}
		debugLogger.Printf("Created knowledge base")

		// Initialize credential provider
		credsProv, err := initCredentialsProvider()
		if err != nil {
			log.Fatalf("Failed to initialize credentials provider: %v", err)
		}
		debugLogger.Printf("Initialized credentials provider: %s", *credsProvider)

		// Create agent manager
		agentMgr = agent.NewManagerLegacy(kb, llmClient, credsProv, 10)
		debugLogger.Printf("Created agent manager")

		// Parse agent IDs
		agentIDs := strings.Split(*agents, ",")
		debugLogger.Printf("Spawning agents: %v", agentIDs)

		// Spawn agents
		for _, agentID := range agentIDs {
			agentID = strings.TrimSpace(agentID)
			if agentID == "" {
				continue
			}

			// Load personality
			personalityPath := fmt.Sprintf("data/agents/%s/personality.json", agentID)
			personality, err := agent.LoadPersonalityJSON(personalityPath)
			if err != nil {
				log.Printf("Failed to load personality for %s: %v", agentID, err)
				continue
			}

			// Spawn agent
			agt, err := agentMgr.SpawnAgent(ctx, personality)
			if err != nil {
				log.Printf("Failed to spawn agent %s: %v", agentID, err)
				continue
			}

			debugLogger.Printf("Spawned agent: %s (%s)", agt.Name(), agt.ID())
		}
	}

	// Create TUI model
	tuiReadyChan := make(chan struct{})
	var model *tui.WatcherModel
	var serverClient *tui.AgentServerClient

	if remoteMode {
		// Remote mode: connect to agent-server
		debugLogger.Printf("Remote mode: connecting to %s", *agentServerURL)
		serverClient = tui.NewAgentServerClient(*agentServerURL)

		// Test connection
		if err := serverClient.Ping(); err != nil {
			log.Fatalf("Failed to connect to agent-server: %v", err)
		}

		// Fetch agent list
		agentInfos, err := serverClient.ListAgents()
		if err != nil {
			log.Fatalf("Failed to fetch agents from server: %v", err)
		}

		if len(agentInfos) == 0 {
			log.Fatal("No agents available on agent-server")
		}

		debugLogger.Printf("Found %d agents on server", len(agentInfos))

		// Create model with nil state (remote mode doesn't need watcher state)
		tempModel := tui.NewWatcherModel(nil, tuiReadyChan)
		tempModel.SetRemoteMode(serverClient)
		model = &tempModel

		// Add remote agents to TUI
		for _, info := range agentInfos {
			model.AddAgent(tui.AgentInfo{
				ID:     info.ID,
				Name:   info.Name,
				Role:   info.Role,
				Status: info.Status,
				Action: "",
			})
		}

	} else {
		// Local mode: create watcher state
		state := &game.State{
			Doc:         true,
			MaxCargo:    10,
			CurrentTick: 0,
			System: game.SystemData{
				POIs:        []game.POI{},
				Connections: []string{},
			},
			Username: username,
			Password: password,
		}

		tempModel := tui.NewWatcherModel(state, tuiReadyChan)
		model = &tempModel

		// Add local agents to TUI
		for _, agt := range agentMgr.ListAgents() {
			status := agt.Status()
			model.AddAgent(tui.AgentInfo{
				ID:     agt.ID(),
				Name:   agt.Name(),
				Role:   agt.Personality().Role,
				Status: status.State.String(),
				Action: status.CurrentAction,
			})
		}
	}

	// Start Bubbletea program
	p := tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	if localMode {
		// Local mode: start agent connections
		go func() {
			<-tuiReadyChan
			startAgentConnections(ctx, agentMgr, p)
		}()

		// Connect watcher client for display
		state := model.GetCurrentState()
		if state != nil {
			go startWatcherClient(ctx, state, p)
		}
	} else {
		// Remote mode: subscribe to SSE streams
		go func() {
			<-tuiReadyChan
			startRemoteStreaming(ctx, serverClient, model, p)
		}()
	}

	// Run the TUI
	if _, err := p.Run(); err != nil {
		log.Printf("Alas, there's been an error: %v", err)
	}

	// Cleanup
	if agentMgr != nil {
		debugLogger.Printf("Stopping all agents...")
		if err := agentMgr.StopAll(); err != nil {
			log.Printf("Error stopping agents: %v", err)
		}
	}
}

// startAgentConnections connects all agents to the game
func startAgentConnections(ctx context.Context, agentMgr *agent.Manager, kb knowledge.Base, p *tea.Program) {
	for _, agt := range agentMgr.ListAgents() {
		go func(a agent.Agent) {
			// Get credentials for this specific agent
			creds, err := agentMgr.GetCredentials(ctx, a.ID())
			if err != nil {
				log.Printf("[%s] Failed to get credentials: %v", a.ID(), err)
				p.Send(tui.AgentStatusMsg{
					AgentID: a.ID(),
					Status:  "Error",
				})
				return
			}

			debugLogger.Printf("[%s] Using credentials: username=%s", a.ID(), creds.Username)

			// Create game client for this agent with its own credentials
			client := game.NewClient(
				"wss://game.spacemolt.com/ws",
				creds.Username,
				creds.Password,
				debugLogger,
			)

			// Connect
			if err := client.Connect(ctx); err != nil {
				log.Printf("[%s] Failed to connect: %v", a.ID(), err)
				p.Send(tui.AgentStatusMsg{
					AgentID: a.ID(),
					Status:  "Error",
				})
				return
			}

			// Wait for connection to be ready (first message received)
			select {
			case <-client.Ready():
				// Connection is ready, proceed with authentication
			case <-time.After(10 * time.Second):
				log.Printf("[%s] Timeout waiting for connection ready", a.ID())
				p.Send(tui.AgentStatusMsg{
					AgentID: a.ID(),
					Status:  "Error",
				})
				return
			case <-ctx.Done():
				return
			}

			// Set message handler
			client.SetHandler(&agentMessageHandler{
				agent:   a,
				program: p,
				client:  client,
			})

			// Authenticate (synchronous - waits for response)
			if creds.Password != "" {
				if err := client.Login(ctx); err != nil {
					log.Printf("[%s] Login failed: %v", a.ID(), err)
					p.Send(tui.AgentStatusMsg{
						AgentID: a.ID(),
						Status:  "Error",
					})
					return
				}
			} else {
				if err := client.Register(ctx, "voidborn"); err != nil {
					log.Printf("[%s] Registration failed: %v", a.ID(), err)
					p.Send(tui.AgentStatusMsg{
						AgentID: a.ID(),
						Status:  "Error",
					})
					return
				}
			}

			// Request system info
			if err := client.GetSystem(ctx); err != nil {
				log.Printf("[%s] Failed to get system: %v", a.ID(), err)
			}

			// Start agent decision loop
			runAgentLoop(ctx, a, client, kb, p)
		}(agt)
	}
}

// runAgentLoop runs the autonomous agent decision loop
func runAgentLoop(ctx context.Context, agt agent.Agent, client *game.Client, kb knowledge.Base, p *tea.Program) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Wait for next tick
		<-ticker.C

		// Get current state
		state := client.GetState()

		// Make decision
		p.Send(tui.AgentStatusMsg{
			AgentID: agt.ID(),
			Status:  "Deciding",
		})

		decision, err := agt.Decide(ctx, state)
		if err != nil {
			log.Printf("[%s] Decision failed: %v", agt.ID(), err)
			p.Send(tui.AgentStatusMsg{
				AgentID: agt.ID(),
				Status:  "Error",
			})
			continue
		}

		log.Printf("[%s] Decided: %s (%.2f confidence) - %s", agt.ID(), decision.Action, decision.Confidence, decision.Reasoning)

		// Execute action
		p.Send(tui.AgentStatusMsg{
			AgentID: agt.ID(),
			Status:  "Acting",
		})

		if err := executeAction(ctx, client, decision); err != nil {
			log.Printf("[%s] Action failed: %v", agt.ID(), err)

			// Learn from failure
			if err := agt.Learn(agent.ActionResult{
				Success: false,
				Message: err.Error(),
				Error:   err,
			}); err != nil {
				log.Printf("[%s] Failed to learn from failure: %v", agt.ID(), err)
			}

			p.Send(tui.AgentStatusMsg{
				AgentID: agt.ID(),
				Status:  "Error",
			})
			continue
		}

		// Learn from success
		if err := agt.Learn(agent.ActionResult{
			Success:  true,
			Message:  fmt.Sprintf("Executed %s", decision.Action),
			NewState: state,
		}); err != nil {
			log.Printf("[%s] Failed to learn from success: %v", agt.ID(), err)
		}

		// Check if we should capture market data
		updatedState := client.GetState()
		if agent.ShouldCaptureMarket(updatedState, agt.Status()) {
			log.Printf("[%s] Capturing market data", agt.ID())
			if err := agent.CaptureMarketData(ctx, client, kb, agt.ID()); err != nil {
				log.Printf("[%s] Failed to capture market data: %v", agt.ID(), err)
				// Don't fail the loop on market capture error
			} else {
				log.Printf("[%s] Market data captured successfully", agt.ID())
			}
		}

		p.Send(tui.AgentStatusMsg{
			AgentID: agt.ID(),
			Status:  "Idle",
		})
	}
}

// executeAction executes a decision through the game client
func executeAction(ctx context.Context, client *game.Client, decision agent.Decision) error {
	switch decision.Action {
	case "undock":
		return client.Undock(ctx)
	case "dock":
		return client.Dock(ctx)
	case "travel":
		if decision.Target != "" {
			return client.Travel(ctx, decision.Target)
		}
		return fmt.Errorf("travel requires target")
	case "jump":
		if decision.Target != "" {
			return client.Jump(ctx, decision.Target)
		}
		return fmt.Errorf("jump requires target")
	case "mine":
		return client.Mine(ctx)
	case "scan":
		return client.Scan(ctx)
	case "wait":
		return nil
	case "get_system":
		return client.GetSystem(ctx)
	case "get_status":
		return client.GetStatus(ctx)
	default:
		return fmt.Errorf("unknown action: %s", decision.Action)
	}
}

// startWatcherClient connects a client for the watcher display
func startWatcherClient(ctx context.Context, state *game.State, p *tea.Program) {
	// Determine username for watcher
	watcherUsername := state.Username
	if watcherUsername == "" {
		watcherUsername = fmt.Sprintf("Watcher_%d", time.Now().Unix())
	}

	// Create game client for watcher
	client := game.NewClient(
		"wss://game.spacemolt.com/ws",
		watcherUsername,
		state.Password,
		debugLogger,
	)

	// Connect
	if err := client.Connect(ctx); err != nil {
		log.Printf("Watcher client failed to connect: %v", err)
		return
	}

	// Wait for connection ready
	select {
	case <-client.Ready():
		// Connection ready, proceed
	case <-time.After(10 * time.Second):
		log.Printf("Watcher: Timeout waiting for connection ready")
		return
	case <-ctx.Done():
		return
	}

	// Set message handler
	client.SetHandler(&watcherMessageHandler{
		state:   state,
		program: p,
	})

	// Authenticate (synchronous - waits for response)
	if state.Password != "" {
		if err := client.Login(ctx); err != nil {
			log.Printf("Watcher login failed: %v", err)
			return
		}
	} else {
		if err := client.Register(ctx, "voidborn"); err != nil {
			log.Printf("Watcher registration failed: %v", err)
			return
		}
		// Update state with new username and token
		state.Mu.Lock()
		state.Username = watcherUsername
		state.Password = client.GetState().Password
		state.Mu.Unlock()
	}

	// Request system info
	if err := client.GetSystem(ctx); err != nil {
		log.Printf("Watcher: Failed to get system: %v", err)
	}

	// Keep connection alive and handle messages
	// The handler will process messages as they arrive
	<-ctx.Done()
	_ = client.Close()
}

// agentMessageHandler handles game messages for an agent
type agentMessageHandler struct {
	agent   agent.Agent
	program *tea.Program
	client  *game.Client
}

func (h *agentMessageHandler) OnConnected(state *game.State) {
	log.Printf("[%s] Connected", h.agent.ID())
}

func (h *agentMessageHandler) OnMessage(resp protocol.Response) {
	// State is automatically updated by the client
	// Log significant events
	switch resp.Type {
	case "ok":
		if action, ok := resp.Payload["action"].(string); ok {
			log.Printf("[%s] Action completed: %s", h.agent.ID(), action)
		}
	case "error":
		log.Printf("[%s] Error: %v", h.agent.ID(), resp.Payload)
	}
}

func (h *agentMessageHandler) OnDisconnected(err error) {
	log.Printf("[%s] Disconnected: %v", h.agent.ID(), err)
	h.program.Send(tui.AgentStatusMsg{
		AgentID: h.agent.ID(),
		Status:  "Error",
	})
}

// watcherMessageHandler handles game messages for the watcher display
type watcherMessageHandler struct {
	state   *game.State
	program *tea.Program
}

func (h *watcherMessageHandler) OnConnected(state *game.State) {
	log.Printf("Watcher connected")
}

func (h *watcherMessageHandler) OnMessage(resp protocol.Response) {
	// Update watcher state
	handleResponse(resp, h.state)

	// Send to TUI
	h.program.Send(tui.WsMsg{
		Type:    resp.Type,
		Payload: resp.Payload,
	})
}

func (h *watcherMessageHandler) OnDisconnected(err error) {
	log.Printf("Watcher disconnected: %v", err)
	h.program.Send(tui.WsMsg{
		Type:    "error",
		Payload: map[string]any{"error": err.Error()},
	})
}

// handleResponse updates state from server responses
// This is a simplified version that updates the watcher's state
func handleResponse(resp protocol.Response, state *game.State) {
	var username, password string

	state.Mu.Lock()

	switch resp.Type {
	case protocol.TypeWelcome:
		if tick, ok := resp.Payload["current_tick"].(float64); ok {
			state.CurrentTick = int64(tick)
		}

	case protocol.TypeRegistered:
		// Support both 'password' (new API) and 'token' (legacy) for backward compatibility
		if pass, ok := resp.Payload["password"].(string); ok {
			state.Password = pass
			password = pass
			username = state.Username
		} else if tok, ok := resp.Payload["token"].(string); ok {
			// Legacy support
			state.Password = tok
			password = tok
			username = state.Username
		}

	case protocol.TypeLoggedIn:
		if player, ok := resp.Payload["player"].(map[string]any); ok {
			if pUsername, ok := player["username"].(string); ok {
				state.Username = pUsername
			}
			if currentPoi, ok := player["current_poi"].(string); ok {
				state.CurrentPOI = currentPoi
				state.System.ShipPOI = currentPoi
			}
		}
		if ship, ok := resp.Payload["ship"].(map[string]any); ok {
			updateShipState(state, ship, "logged_in")
		}
		if system, ok := resp.Payload["system"].(map[string]any); ok {
			if sysName, ok := system["name"].(string); ok {
				state.CurrentSystem = sysName
			}
		}
		username = state.Username
		password = state.Password

	case protocol.TypeOK:
		if player, ok := resp.Payload["player"].(map[string]any); ok {
			if credits, ok := player["credits"].(float64); ok {
				state.Credits = credits
			}
			if currentPoi, ok := player["current_poi"].(string); ok {
				state.CurrentPOI = currentPoi
				state.System.ShipPOI = currentPoi
			}
		}

		if poisData, ok := resp.Payload["pois"].([]any); ok {
			state.System.POIs = state.System.POIs[:0]
			for _, p := range poisData {
				if poi, ok := p.(map[string]any); ok {
					poiObj := game.POI{}
					if id, ok := poi["id"].(string); ok {
						poiObj.ID = id
					}
					if name, ok := poi["name"].(string); ok {
						poiObj.Name = name
					}
					if poiType, ok := poi["type"].(string); ok {
						poiObj.Type = poiType
					}
					if desc, ok := poi["description"].(string); ok {
						poiObj.Description = desc
					}
					if sysID, ok := poi["system_id"].(string); ok {
						poiObj.SystemID = sysID
					}
					if baseID, ok := poi["base_id"].(string); ok {
						poiObj.BaseID = baseID
					}
					if pos, ok := poi["position"].(map[string]any); ok {
						if x, ok := pos["x"].(float64); ok {
							poiObj.Position.X = x
						}
						if y, ok := pos["y"].(float64); ok {
							poiObj.Position.Y = y
						}
					}
					if resources, ok := poi["resources"].([]any); ok {
						for _, r := range resources {
							if res, ok := r.(map[string]any); ok {
								resObj := game.POIResource{}
								if resourceID, ok := res["resource_id"].(string); ok {
									resObj.ResourceID = resourceID
								}
								if richness, ok := res["richness"].(float64); ok {
									resObj.Richness = richness
								}
								if remaining, ok := res["remaining"].(float64); ok {
									resObj.Remaining = remaining
								}
								poiObj.Resources = append(poiObj.Resources, resObj)
							}
						}
					}
					state.System.POIs = append(state.System.POIs, poiObj)
				}
			}
			state.LastMapUpdate = time.Now()
		}

		if sysData, ok := resp.Payload["system"].(map[string]any); ok {
			if name, ok := sysData["name"].(string); ok {
				state.System.Name = name
			}
			if desc, ok := sysData["description"].(string); ok {
				state.System.Description = desc
			}
			if connections, ok := sysData["connections"].([]any); ok {
				state.System.Connections = state.System.Connections[:0]
				for _, c := range connections {
					if connStr, ok := c.(string); ok {
						state.System.Connections = append(state.System.Connections, connStr)
					}
				}
			}
		}

		if ship, ok := resp.Payload["ship"].(map[string]any); ok {
			updateShipState(state, ship, "ok")
		}

		if action, ok := resp.Payload["action"].(string); ok {
			switch action {
			case "undock":
				state.Doc = false
			case "dock":
				state.Doc = true
			case "travel":
				state.Traveling = false
			}
		}

	case protocol.TypeDocked:
		state.Doc = true
		state.Traveling = false

	case protocol.TypeUndocked:
		state.Doc = false

	case protocol.TypeStateUpdate:
		if tick, ok := resp.Payload["tick"].(float64); ok {
			state.CurrentTick = int64(tick)
		}

		if player, ok := resp.Payload["player"].(map[string]any); ok {
			if currentPoi, ok := player["current_poi"].(string); ok {
				state.CurrentPOI = currentPoi
				state.System.ShipPOI = currentPoi
			}
			if credits, ok := player["credits"].(float64); ok {
				state.Credits = credits
			}
		}
		if ship, ok := resp.Payload["ship"].(map[string]any); ok {
			updateShipState(state, ship, "state_update")
		}

	case protocol.TypeMining:
		if ship, ok := resp.Payload["ship"].(map[string]any); ok {
			updateShipState(state, ship, "mining")
		}
		if player, ok := resp.Payload["player"].(map[string]any); ok {
			if credits, ok := player["credits"].(float64); ok {
				state.Credits = credits
			}
		}
	}

	state.Mu.Unlock()

	// I/O outside the lock
	if username != "" && password != "" {
		saveCredentials(username, password)
	}
}

func updateShipState(state *game.State, ship map[string]any, logPrefix string) {
	if fuel, ok := ship["fuel"].(float64); ok {
		state.Fuel = fuel
	}
	if maxFuel, ok := ship["max_fuel"].(float64); ok {
		state.MaxFuel = maxFuel
	}
	if hull, ok := ship["hull"].(float64); ok {
		state.Hull = hull
	}
	if maxHull, ok := ship["max_hull"].(float64); ok {
		state.MaxHull = maxHull
	}
	if maxCargo, ok := ship["cargo_capacity"].(float64); ok {
		state.MaxCargo = int(maxCargo)
	} else if maxCargo, ok := ship["cargo_capacity"].(int64); ok {
		state.MaxCargo = int(maxCargo)
	} else if maxCargo, ok := ship["cargo_capacity"].(int); ok {
		state.MaxCargo = maxCargo
	}
	if cargo, ok := ship["cargo"].([]any); ok {
		state.Cargo = state.Cargo[:0]
		for _, c := range cargo {
			if item, ok := c.(map[string]any); ok {
				state.Cargo = append(state.Cargo, item)
			}
		}
	}
}

func saveCredentials(username, password string) {
	creds := map[string]string{
		"username": username,
		"password": password,
	}
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		log.Printf("Failed to marshal credentials: %v", err)
		return
	}
	if err := os.WriteFile(credentialsFile, data, 0600); err != nil {
		log.Printf("Failed to save credentials: %v", err)
	}
}

// startRemoteStreaming subscribes to SSE streams for all remote agents
func startRemoteStreaming(ctx context.Context, client *tui.AgentServerClient, model *tui.WatcherModel, p *tea.Program) {
	// Get list of agents from model
	agentInfos, err := client.ListAgents()
	if err != nil {
		log.Printf("Failed to list agents: %v", err)
		return
	}

	// Subscribe to each agent's stream
	for _, info := range agentInfos {
		agentID := info.ID
		go func(id string) {
			debugLogger.Printf("[%s] Subscribing to event stream", id)

			eventCh, err := client.StreamEvents(id)
			if err != nil {
				log.Printf("[%s] Failed to subscribe to stream: %v", id, err)
				return
			}

			// Process events
			for event := range eventCh {
				// Convert SSE event to WsMsg for TUI
				p.Send(tui.WsMsg{
					AgentID: id,
					Type:    event.Type,
					Payload: event.Data,
				})

				// Update agent status if it's a status-changing event
				if event.Type == "decision" {
					p.Send(tui.AgentStatusMsg{
						AgentID: id,
						Status:  "Deciding",
					})
				} else if event.Type == "action" {
					if status, ok := event.Data["status"].(string); ok && status == "success" {
						p.Send(tui.AgentStatusMsg{
							AgentID: id,
							Status:  "Idle",
						})
					}
				} else if event.Type == "error" {
					p.Send(tui.AgentStatusMsg{
						AgentID: id,
						Status:  "Error",
					})
				}
			}

			debugLogger.Printf("[%s] Event stream closed", id)
		}(agentID)

		// Also poll state periodically for this agent
		go func(id string) {
			ticker := time.NewTicker(5 * time.Second)
			defer ticker.Stop()

			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					state, err := client.GetState(id)
					if err != nil {
						debugLogger.Printf("[%s] Failed to fetch state: %v", id, err)
						continue
					}

					// Update model state
					model.UpdateAgentState(id, state)

					// Trigger UI refresh
					p.Send(tui.WsMsg{
						AgentID: id,
						Type:    "state_update",
						Payload: map[string]any{
							"tick": state.CurrentTick,
						},
					})
				}
			}
		}(agentID)
	}
}
