package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/user/spacemolt/pkg/game"
)

// WsMsg wraps protocol.Response for Bubbletea (exported for use in cmd/watcher)
type WsMsg struct {
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
	ID     string
	Name   string
	Role   string
	Status string
	Action string
}

// WatcherModel is the main TUI model for the watcher interface
type WatcherModel struct {
	gameState       *game.State
	viewportWidth   int
	viewportHeight  int
	quitting        bool

	// Panel models
	logPanel        logPanelModel
	mapPanel        mapPanelModel
	statusPanel     statusPanelModel
	agentPanel      agentPanelModel

	// Agent tracking
	agents          []AgentInfo
	selectedAgentID string
}

// NewWatcherModel creates a new watcher TUI model
func NewWatcherModel(state *game.State) WatcherModel {
	return WatcherModel{
		gameState: state,
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
		agents: []AgentInfo{},
	}
}

// Init initializes the TUI model
func (m WatcherModel) Init() tea.Cmd {
	return tickEvery(time.Second)
}

// Update handles messages and updates the model
func (m WatcherModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
		case "tab":
			// Cycle through agents
			if len(m.agents) > 0 {
				m.agentPanel.selected = (m.agentPanel.selected + 1) % len(m.agents)
				if m.agentPanel.selected < len(m.agents) {
					m.selectedAgentID = m.agents[m.agentPanel.selected].ID
				}
			}
			return m, nil
		case "shift+tab":
			// Cycle backwards through agents
			if len(m.agents) > 0 {
				m.agentPanel.selected = (m.agentPanel.selected - 1 + len(m.agents)) % len(m.agents)
				if m.agentPanel.selected < len(m.agents) {
					m.selectedAgentID = m.agents[m.agentPanel.selected].ID
				}
			}
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
		m.updateAgentStatus(msg.AgentID, msg.Status)
		return m, nil

	case tea.WindowSizeMsg:
		m.viewportHeight = msg.Height
		m.viewportWidth = msg.Width
		return m, nil
	}

	return m, nil
}

// View renders the TUI
func (m WatcherModel) View() string {
	if m.quitting {
		return "Goodbye!\n"
	}

	layout := m.calculateLayout()

	// Render each panel
	agentContent := m.renderAgentPanel(layout.agentWidth, layout.agentHeight)
	logContent := m.renderLogPanel(layout.logWidth, layout.logHeight)
	mapContent := m.renderMapPanelFull(layout.mapWidth, layout.mapHeight)
	statusContent := m.renderStatusPanel(layout.statusWidth, layout.statusHeight)

	// Join agent and log horizontally (top-left section)
	topLeft := lipgloss.JoinHorizontal(lipgloss.Top, agentContent, logContent)

	// Join topLeft and map horizontally (top row)
	topRow := lipgloss.JoinHorizontal(lipgloss.Top, topLeft, mapContent)

	// Join top row and status panel vertically
	fullLayout := lipgloss.JoinVertical(lipgloss.Left, topRow, statusContent)

	return fullLayout
}

// calculateLayout computes the dimensions for each panel
func (m WatcherModel) calculateLayout() panelLayout {
	// Available space after accounting for borders and padding
	availableHeight := m.viewportHeight - 4 // Reserve space for borders

	// Status panel gets bottom 30% (capped at 12 lines, minimum 6)
	statusHeight := availableHeight * 30 / 100
	if statusHeight < 6 {
		statusHeight = 6
	}
	if statusHeight > 12 {
		statusHeight = 12
	}

	// Top row gets remaining space
	topHeight := availableHeight - statusHeight

	// Agent panel gets 25% of top row (or 20 chars minimum)
	agentWidth := m.viewportWidth * 25 / 100
	if agentWidth < 20 {
		agentWidth = 20
	}

	// Log panel gets 35% of remaining top row
	remainingWidth := m.viewportWidth - agentWidth - 4 // Account for borders
	logWidth := remainingWidth * 40 / 100
	if logWidth < 20 {
		logWidth = 20
	}

	// Map panel gets rest of top row
	mapWidth := remainingWidth - logWidth - 2 // Account for borders

	// All panels in top row share the same height
	topPanelHeight := topHeight

	return panelLayout{
		agentWidth:   agentWidth,
		agentHeight:  topPanelHeight,
		logWidth:     logWidth,
		logHeight:    topPanelHeight,
		mapWidth:     mapWidth,
		mapHeight:    topPanelHeight,
		statusWidth:  m.viewportWidth,
		statusHeight: statusHeight,
	}
}

// renderAgentPanel renders the agent list panel
func (m WatcherModel) renderAgentPanel(width, height int) string {
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Width(width).
		Height(height)

	var sb strings.Builder
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("62"))
	sb.WriteString(titleStyle.Render("Agents"))
	sb.WriteString("\n\n")

	if len(m.agents) == 0 {
		sb.WriteString(lipgloss.NewStyle().Faint(true).Render("No agents active"))
	} else {
		for i, agent := range m.agents {
			// Highlight selected agent
			if i == m.agentPanel.selected {
				sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("226")).Render("→ "))
			} else {
				sb.WriteString("  ")
			}

			// Show agent name and status
			statusColor := "241" // gray for idle
			switch agent.Status {
			case "Acting":
				statusColor = "226" // yellow for acting
			case "Deciding":
				statusColor = "217" // pink for deciding
			case "Error":
				statusColor = "160" // red for error
			}

			sb.WriteString(fmt.Sprintf("%s\n", lipgloss.NewStyle().Foreground(lipgloss.Color(statusColor)).Render(agent.Name)))
			sb.WriteString(fmt.Sprintf("    %s\n", agent.Action))
		}
	}

	return style.Render(sb.String())
}

// AddAgent adds an agent to the watcher
func (m *WatcherModel) AddAgent(info AgentInfo) {
	m.agents = append(m.agents, info)
	// Auto-select first agent
	if len(m.agents) == 1 {
		m.selectedAgentID = info.ID
	}
}

// updateAgentStatus updates an agent's status
func (m *WatcherModel) updateAgentStatus(agentID string, status string) {
	for i := range m.agents {
		if m.agents[i].ID == agentID {
			m.agents[i].Status = status
			break
		}
	}
}

// handleWebSocketMessage processes WebSocket messages and updates the model
func (m *WatcherModel) handleWebSocketMessage(resp WsMsg) {
	// Add log message based on response type
	line := ""
	switch resp.Type {
	case "welcome":
		if v, ok := resp.Payload["version"].(string); ok {
			// Capture initial tick from welcome message
			if tick, ok := resp.Payload["current_tick"].(float64); ok {
				m.gameState.Mu.Lock()
				m.gameState.CurrentTick = int64(tick)
				m.gameState.Mu.Unlock()
			}
			line = fmt.Sprintf("[CONNECTED] Server v%s | Tick: %v", v, resp.Payload["current_tick"])
		}
	case "logged_in":
		line = "[LOGGED IN] Successfully authenticated"
	case "error":
		line = fmt.Sprintf("[ERROR] %v", resp.Payload)
	case "ok":
		if action, ok := resp.Payload["action"].(string); ok {
			switch action {
			case "undock":
				line = "✓ Undocked - free to explore"
			case "dock":
				line = "✓ Docked - safe at station"
			case "travel":
				line = "✓ Travel complete"
			case "mine":
				line = "⛏ Mining..."
			}
		}
	case "docked":
		line = "[DOCKED] Secure at station"
	case "undocked":
		line = "[UNDOCKED] Now in open space"
	case "state_update":
		// Status update - mark status panel for update
		m.statusPanel.lastUpdate = time.Now()
		// Also add compact status to log
		m.gameState.Mu.Lock()
		line = fmt.Sprintf("[Credits: %.0f | Fuel: %.0f/%.0f | Hull: %.0f/%.0f]",
			m.gameState.Credits, m.gameState.Fuel, m.gameState.MaxFuel,
			m.gameState.Hull, m.gameState.MaxHull)
		if len(m.gameState.Cargo) > 0 {
			line += fmt.Sprintf(" | Cargo: %d items", len(m.gameState.Cargo))
		}
		m.gameState.Mu.Unlock()
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
		m.gameState.Mu.Lock()
		currentTick := m.gameState.CurrentTick
		m.gameState.Mu.Unlock()

		// Prefix line with tick count
		line = fmt.Sprintf("[T:%d] %s", currentTick, line)

		m.logPanel.lines = append(m.logPanel.lines, line)
		if len(m.logPanel.lines) > m.logPanel.maxLines {
			m.logPanel.lines = m.logPanel.lines[1:]
		}
		m.logPanel.lastUpdate = time.Now()
		m.logPanel.cachedRender = "" // Invalidate cache
	}
}
