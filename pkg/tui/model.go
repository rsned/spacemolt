package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rsned/spacemolt/pkg/game"
)

const (
	// Panel height constraints
	minPanelHeight      = 4  // Minimum height for any panel (prevents collapse)
	maxLogPanelHeight   = 12 // Maximum height for log panel
	minAgentPanelHeight = 6  // Minimum height for agent panel
	minMapPanelHeight   = 8  // Minimum height for map panel
)

// WsMsg wraps protocol.Response for Bubbletea (exported for use in cmd/watcher)
type WsMsg struct {
	AgentID string // Agent that sent this message
	Type    string
	Payload map[string]any
}

// AgentStatusMsg is sent when an agent's status changes
type AgentStatusMsg struct {
	AgentID string
	Status  string
}

// tickMsg is sent every second to update the UI
type tickMsg time.Time

func tickEvery(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// AgentInfo represents information about an agent for the TUI
type AgentInfo struct {
	ID      string
	Name    string
	Role    string
	Status  string
	Action  string
	Empire  string
	Faction string
}

// WatcherModel is the main TUI model for the watcher interface
type WatcherModel struct {
	// Multi-agent state tracking
	agentStates map[string]*game.State // agent ID -> game state
	agentLogs   map[string][]string    // agent ID -> log lines

	viewportWidth  int
	viewportHeight int
	quitting       bool

	// Panel models
	logPanel    logPanelModel
	mapPanel    mapPanelModel
	statusPanel statusPanelModel
	agentPanel  agentPanelModel

	// Agent tracking
	agents          []AgentInfo
	selectedAgentID string
	selectedIndex   int // Cache of selected agent index for performance

	// Remote mode support
	remoteMode   bool
	serverClient *AgentServerClient

	// Ready signal - closed when TUI is initialized and ready
	readyChan chan struct{}
}

// NewWatcherModel creates a new watcher TUI model
// The readyChan will be closed when the TUI is initialized and ready for use
// Note: The state parameter is kept for backward compatibility but is now optional (can be nil)
func NewWatcherModel(state *game.State, readyChan chan struct{}) *WatcherModel {
	model := &WatcherModel{
		agentStates: make(map[string]*game.State),
		agentLogs:   make(map[string][]string),
		logPanel: logPanelModel{
			lines:        []string{},
			maxLines:     100,
			scrollOffset: 0,
			lastUpdate:   time.Time{},
			cachedRender: "",
		},
		mapPanel: mapPanelModel{
			lastUpdate:   time.Time{},
			cachedRender: "",
		},
		statusPanel: statusPanelModel{
			lastUpdate:   time.Time{},
			cachedRender: "",
			compactMode:  false,
		},
		agentPanel: agentPanelModel{
			agents:      []string{},
			selected:    0,
			showDetails: false,
		},
		agents:        []AgentInfo{},
		selectedIndex: 0,
		readyChan:     readyChan,
	}

	// If a state was provided (backward compatibility), use it for a default agent
	if state != nil {
		model.agentStates["default"] = state
		model.agentLogs["default"] = []string{}
	}

	return model
}

// Init initializes the TUI model
func (m *WatcherModel) Init() tea.Cmd {
	// Signal that the TUI is now initialized and ready
	if m.readyChan != nil {
		close(m.readyChan)
	}
	return tickEvery(time.Second)
}

// Update handles messages and updates the model
func (m *WatcherModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "up", "k":
			// Scroll log up
			if m.logPanel.scrollOffset < len(m.logPanel.lines)-1 {
				m.logPanel.scrollOffset++
			}
			return m, nil
		case "down", "j":
			// Scroll log down
			if m.logPanel.scrollOffset > 0 {
				m.logPanel.scrollOffset--
			}
			return m, nil
		case "tab", "n":
			// Cycle to next agent
			m.selectNextAgent()
			return m, nil
		case "shift+tab", "p":
			// Cycle to previous agent
			m.selectPreviousAgent()
			return m, nil
		case "1", "2", "3", "4", "5", "6", "7", "8", "9":
			// Jump to specific agent (1-indexed)
			idx := int(msg.String()[0] - '1')
			m.selectAgentByIndex(idx)
			return m, nil
		}

	case tickMsg:
		// Just trigger a redraw
		return m, tickEvery(time.Second)

	case WsMsg:
		// Handle WebSocket message
		m.handleWebSocketMessage(msg)
		return m, nil

	case AgentStatusMsg:
		// Handle agent status update
		m.UpdateAgentStatus(msg.AgentID, msg.Status)
		return m, nil

	case tea.WindowSizeMsg:
		m.viewportHeight = msg.Height
		m.viewportWidth = msg.Width
		return m, nil
	}

	return m, nil
}

// View renders the TUI
func (m *WatcherModel) View() string {
	if m.quitting {
		return "Goodbye!\n"
	}

	layout := m.calculateLayout()

	// Render each panel
	agentContent := m.renderAgentPanel(layout.agentWidth, layout.agentHeight)
	logContent := m.renderLogPanel(layout.logWidth, layout.logHeight)
	mapContent := m.renderMapPanelFull(layout.mapWidth, layout.mapHeight)
	statusContent := m.renderStatusPanel(layout.statusWidth, layout.statusHeight)

	// Join log and map horizontally (top section)
	topSection := lipgloss.JoinHorizontal(lipgloss.Top, logContent, mapContent)

	// Join top section, status, and agents vertically (full layout)
	fullLayout := lipgloss.JoinVertical(lipgloss.Left, topSection, statusContent, agentContent)

	return fullLayout
}

// calculateLayout computes the dimensions for each panel
func (m *WatcherModel) calculateLayout() panelLayout {
	// Available space after accounting for borders and padding
	availableHeight := m.viewportHeight - 4 // Reserve space for borders

	// Status panel gets fixed height (6-12 lines)
	statusHeight := availableHeight * 30 / 100
	if statusHeight < 6 {
		statusHeight = 6
	}
	if statusHeight > 12 {
		statusHeight = 12
	}

	// Agents panel gets fixed height at bottom
	const agentsPanelHeight = 6
	agentsHeight := agentsPanelHeight

	// Top section (Log + Map) gets all remaining space
	topSectionHeight := availableHeight - statusHeight - agentsHeight

	// Ensure top section has minimum height
	if topSectionHeight < minMapPanelHeight+minPanelHeight {
		// Reduce status panel to make room
		statusHeight = 6
	}

	// Distribute top section space between log and map
	// Top row gets remaining space
	topRowHeight := availableHeight - statusHeight

	// Calculate minimum required height for top row panels
	minTopRowHeight := minAgentPanelHeight + maxLogPanelHeight + minMapPanelHeight

	// If top row is too small, reduce log panel max height
	effectiveMaxLogHeight := maxLogPanelHeight
	if topRowHeight < minTopRowHeight {
		// Reduce log panel to fit, but never below minimum
		effectiveMaxLogHeight = topRowHeight - minAgentPanelHeight - minMapPanelHeight
		if effectiveMaxLogHeight < minPanelHeight {
			effectiveMaxLogHeight = minPanelHeight
		}
	}

	// Distribute top row space with constraints
	// Start with minimum allocations
	agentHeight := minAgentPanelHeight
	logHeight := effectiveMaxLogHeight
	mapHeight := minMapPanelHeight

	// Calculate remaining space after minimum allocations
	remainingSpace := topRowHeight - (agentHeight + logHeight + mapHeight)

	// Expand map panel first (highest priority)
	mapHeight += remainingSpace

	// Width calculations
	agentWidth := m.viewportWidth * 25 / 100
	if agentWidth < 20 {
		agentWidth = 20
	}

	remainingWidth := m.viewportWidth - agentWidth - 4 // Account for borders
	logWidth := remainingWidth * 40 / 100

	if logWidth < 20 {
		logWidth = 20
	}

	mapWidth := remainingWidth - logWidth - 2 // Account for borders
	if mapWidth < 20 {
		mapWidth = 20
	}

	return panelLayout{
		agentWidth:  agentWidth,
		agentHeight: agentHeight,

		logWidth:     logWidth,
		logHeight:    logHeight,
		mapWidth:     mapWidth,
		mapHeight:    mapHeight,
		statusWidth:  m.viewportWidth,
		statusHeight: statusHeight,
	}
}

// renderAgentPanel renders the agent list panel
func (m *WatcherModel) renderAgentPanel(width, height int) string {
	var sb strings.Builder
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("62"))
	sb.WriteString(titleStyle.Render("Agents"))
	sb.WriteString("\n")

	if len(m.agents) == 0 {
		sb.WriteString("\n")
		sb.WriteString(lipgloss.NewStyle().Faint(true).Render("No agents active"))
	} else {
		sb.WriteString("\n")
		for i, agent := range m.agents {
			// Determine status color
			var statusColor string
			switch agent.Status {
			case "Idle":
				statusColor = "245"
			case "Deciding", "Acting":
				statusColor = "226" // Yellow
			case "Error":
				statusColor = "160" // Red
			default:
				statusColor = "86" // Green
			}

			// Highlight selected agent
			if i == m.agentPanel.selected {
				sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("226")).Render("→ "))
			} else {
				sb.WriteString("  ")
			}

			// Format agent entry
			entry := fmt.Sprintf("%s\n    %s\n",
				lipgloss.NewStyle().Foreground(lipgloss.Color(statusColor)).Render(agent.Name),
				agent.Action,
			)
			sb.WriteString(entry)
		}
	}

	// Add hot key hints at bottom
	hintStyle := lipgloss.NewStyle().Faint(true).Foreground(lipgloss.Color("245"))
	hints := hintStyle.Render("Tab: Next | Shift+Tab: Prev | 1-9: Jump")
	sb.WriteString("\n")
	sb.WriteString(hints)

	return sb.String()
}

// sortAgents sorts the agents list by Faction, then Empire, then Name
func (m *WatcherModel) sortAgents() {
	sort.Slice(m.agents, func(i, j int) bool {
		// First sort by Faction
		if m.agents[i].Faction != m.agents[j].Faction {
			return m.agents[i].Faction < m.agents[j].Faction
		}
		// Then by Empire
		if m.agents[i].Empire != m.agents[j].Empire {
			return m.agents[i].Empire < m.agents[j].Empire
		}
		// Finally by Name
		return m.agents[i].Name < m.agents[j].Name
	})

	// Update selectedIndex to match the new position of selectedAgentID
	for i, agent := range m.agents {
		if agent.ID == m.selectedAgentID {
			m.selectedIndex = i
			break
		}
	}
}

// AddAgent adds an agent to the watcher
func (m *WatcherModel) AddAgent(info AgentInfo) {
	m.agents = append(m.agents, info)

	// Initialize state and logs for this agent if not exists
	if _, exists := m.agentStates[info.ID]; !exists {
		m.agentStates[info.ID] = &game.State{
			CurrentTick: 0,
			System: game.SystemData{
				POIs:        []game.POI{},
				Connections: []string{},
			},
		}
		m.agentLogs[info.ID] = []string{}
	}

	// Sort agents by Faction, Empire, Name
	m.sortAgents()

	// Auto-select first agent
	if len(m.agents) == 1 {
		m.selectedAgentID = info.ID
		m.selectedIndex = 0
	}
}

// UpdateAgentState updates the game state for a specific agent
func (m *WatcherModel) UpdateAgentState(agentID string, state *game.State) {
	if state != nil {
		m.agentStates[agentID] = state
		// Mark panels for update
		m.mapPanel.lastUpdate = time.Now()
		m.statusPanel.lastUpdate = time.Now()
	}
}

// GetCurrentState returns the game state for the currently selected agent
func (m *WatcherModel) GetCurrentState() *game.State {
	if m.selectedAgentID == "" {
		return nil
	}
	return m.agentStates[m.selectedAgentID]
}

// AddLogForAgent adds a log line for a specific agent
func (m *WatcherModel) AddLogForAgent(agentID string, line string) {
	logs, exists := m.agentLogs[agentID]
	if !exists {
		logs = []string{}
		m.agentLogs[agentID] = logs
	}

	logs = append(logs, line)
	if len(logs) > m.logPanel.maxLines {
		logs = logs[1:]
	}

	m.agentLogs[agentID] = logs

	// If this is the currently selected agent, update the log panel
	if agentID == m.selectedAgentID {
		m.logPanel.lines = logs
		m.logPanel.lastUpdate = time.Now()
		m.logPanel.cachedRender = ""
	}
}

// UpdateAgentStatus updates an agent's status
func (m *WatcherModel) UpdateAgentStatus(agentID string, status string) {
	for i := range m.agents {
		if m.agents[i].ID == agentID {
			m.agents[i].Status = status
			break
		}
	}
}

// selectNextAgent cycles to the next agent
func (m *WatcherModel) selectNextAgent() {
	if len(m.agents) == 0 {
		return
	}
	m.selectedIndex = (m.selectedIndex + 1) % len(m.agents)
	idx := m.selectedIndex
	m.selectAgentByIndex(idx)
}

// selectPreviousAgent cycles to the previous agent
func (m *WatcherModel) selectPreviousAgent() {
	if len(m.agents) == 0 {
		return
	}
	m.selectedIndex = (m.selectedIndex - 1 + len(m.agents)) % len(m.agents)
	idx := m.selectedIndex
	m.selectAgentByIndex(idx)
}

// selectAgentByIndex selects an agent by index (0-based)
func (m *WatcherModel) selectAgentByIndex(idx int) {
	if idx < 0 || idx >= len(m.agents) {
		return
	}
	m.selectedIndex = idx
	m.agentPanel.selected = idx
	m.selectedAgentID = m.agents[idx].ID

	// Switch to the selected agent's logs
	if logs, exists := m.agentLogs[m.selectedAgentID]; exists {
		m.logPanel.lines = logs
		m.logPanel.scrollOffset = 0 // Reset scroll
		m.logPanel.cachedRender = ""
	}

	// Force refresh of all panels
	m.mapPanel.cachedRender = ""
	m.statusPanel.cachedRender = ""
}

// SetRemoteMode configures the model for remote operation
func (m *WatcherModel) SetRemoteMode(client *AgentServerClient) {
	m.remoteMode = true
	m.serverClient = client
}

// IsRemoteMode returns whether the model is in remote mode
func (m *WatcherModel) IsRemoteMode() bool {
	return m.remoteMode
}

// handleWebSocketMessage processes WebSocket messages and updates the model
func (m *WatcherModel) handleWebSocketMessage(resp WsMsg) {
	// Add log message based on response type
	line := ""
	switch resp.Type {
	// Agent-server event types (from EventCallback)
	case "decision":
		if action, ok := resp.Payload["action"].(string); ok {
			target := ""
			if t, ok := resp.Payload["target"].(string); ok && t != "" {
				target = fmt.Sprintf(" → %s", t)
			}
			confidence := ""
			if c, ok := resp.Payload["confidence"].(float64); ok {
				confidence = fmt.Sprintf(" (%.0f%%)", c*100)
			}
			line = fmt.Sprintf("[DECISION] %s%s%s", action, target, confidence)
			if reasoning, ok := resp.Payload["reasoning"].(string); ok && reasoning != "" {
				// Truncate reasoning if too long
				if len(reasoning) > 60 {
					reasoning = reasoning[:57] + "..."
				}
				line = fmt.Sprintf("[DECISION] %s%s%s: %s", action, target, confidence, reasoning)
			}
		}
	case "action":
		if action, ok := resp.Payload["action"].(string); ok {
			target := ""
			if t, ok := resp.Payload["target"].(string); ok && t != "" {
				target = fmt.Sprintf(" → %s", t)
			}
			status := "executed"
			if s, ok := resp.Payload["status"].(string); ok {
				status = s
			}
			line = fmt.Sprintf("✓ [ACTION] %s%s (%s)", action, target, status)
		}
	case "error":
		// Check if this is an agent error or game error
		if errorMsg, ok := resp.Payload["error"].(string); ok {
			line = fmt.Sprintf("✗ [ERROR] %s", errorMsg)
		} else {
			line = fmt.Sprintf("✗ [ERROR] %v", resp.Payload)
		}
	// Game server message types
	case "welcome":
		if v, ok := resp.Payload["version"].(string); ok {
			// Capture initial tick from welcome message
			if tick, ok := resp.Payload["current_tick"].(float64); ok {
				if state := m.GetCurrentState(); state != nil {
					state.Mu.Lock()
					state.CurrentTick = int64(tick)
					state.Mu.Unlock()
				}
			}
			line = fmt.Sprintf("[CONNECTED] Server v%s | Tick: %v", v, resp.Payload["current_tick"])
		}
	case "logged_in":
		line = "[LOGGED IN] Successfully authenticated"
	case "ok":
		if action, ok := resp.Payload["action"].(string); ok {
			switch action {
			case "undock":
				line = "✓ Undocked - free to explore"
			case "dock":
				line = "✓ Docked - safe at station"
			case "travel":
				if msg, ok := resp.Payload["message"].(string); ok {
					line = fmt.Sprintf("✓ Travel: %s", msg)
				} else {
					line = "✓ Travel complete"
				}
			case "jump":
				if msg, ok := resp.Payload["message"].(string); ok {
					line = fmt.Sprintf("✓ Jump: %s", msg)
				} else {
					line = "✓ Jump complete"
				}
			case "mine":
				if msg, ok := resp.Payload["message"].(string); ok {
					line = fmt.Sprintf("⛏ Mining: %s", msg)
				} else {
					line = "⛏ Mining..."
				}
			case "scan":
				line = "📡 Scan complete"
			case "get_system":
				line = "📊 System info retrieved"
			case "get_status":
				line = "📊 Status retrieved"
			default:
				if msg, ok := resp.Payload["message"].(string); ok {
					line = fmt.Sprintf("✓ %s: %s", action, msg)
				} else {
					line = fmt.Sprintf("✓ %s", action)
				}
			}
		}
	case "docked":
		line = "[DOCKED] Secure at station"
	case "undocked":
		line = "[UNDOCKED] Now in open space"
	case "state_update":
		// Status update - mark status panel for update
		m.statusPanel.lastUpdate = time.Now()

		// Check for travel progress
		if progress, ok := resp.Payload["travel_progress"].(float64); ok {
			destination := "unknown"
			if dest, ok := resp.Payload["travel_destination"].(string); ok {
				destination = dest
			}
			travelType := "travel"
			if tt, ok := resp.Payload["travel_type"].(string); ok {
				travelType = tt
			}
			line = fmt.Sprintf("[TRAVEL] %s to %s: %.0f%%", travelType, destination, progress*100)
		} else {
			// Regular status update - only log occasionally or on significant changes
			// Don't add to log for routine state updates to avoid spam
			// The status panel will show this info anyway
		}
	case "chat_message":
		if from, ok := resp.Payload["from"].(string); ok {
			if msg, ok := resp.Payload["message"].(string); ok {
				line = fmt.Sprintf("[CHAT] %s: %s", from, msg)
			}
		}
	case "poi":
		if name, ok := resp.Payload["name"].(string); ok {
			line = fmt.Sprintf("[POI] %s", name)
			if poiType, ok := resp.Payload["type"].(string); ok {
				line += fmt.Sprintf(" (%s)", poiType)
			}
		}
		// Mark map for update
		m.mapPanel.lastUpdate = time.Now()
	case "system":
		// Mark map for update
		m.mapPanel.lastUpdate = time.Now()
	case "mining":
		if amount, ok := resp.Payload["amount"].(float64); ok {
			line = fmt.Sprintf("⛛ Mined: %.0f units", amount)
		}
	}

	if line != "" {
		// Get tick from the agent's state
		var currentTick int64
		if resp.AgentID != "" {
			if state, exists := m.agentStates[resp.AgentID]; exists && state != nil {
				state.Mu.Lock()
				currentTick = state.CurrentTick
				state.Mu.Unlock()
			}
		}

		// Prefix line with tick count and agent name (if multiple agents)
		if len(m.agents) > 1 && resp.AgentID != "" {
			// Find agent name
			agentName := resp.AgentID
			for _, agent := range m.agents {
				if agent.ID == resp.AgentID {
					agentName = agent.Name
					break
				}
			}
			line = fmt.Sprintf("[%s T:%d] %s", agentName, currentTick, line)
		} else {
			line = fmt.Sprintf("[T:%d] %s", currentTick, line)
		}

		// Add log to the specific agent
		if resp.AgentID != "" {
			m.AddLogForAgent(resp.AgentID, line)
		}
	}
}
