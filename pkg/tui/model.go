package tui

import (
	"fmt"
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

// tickMsg is sent every second to update the UI
type tickMsg time.Time

func tickEvery(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// WatcherModel is the main TUI model for the watcher interface
type WatcherModel struct {
	gameState      *game.State
	viewportWidth  int
	viewportHeight int
	quitting       bool

	// Panel models
	logPanel    logPanelModel
	mapPanel    mapPanelModel
	statusPanel statusPanelModel
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
		}

	case tickMsg:
		// Just trigger a redraw
		return m, tickEvery(time.Second)

	case WsMsg:
		// Handle WebSocket message
		m.handleWebSocketMessage(msg)
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
	logContent := m.renderLogPanel(layout.logWidth, layout.logHeight)
	mapContent := m.renderMapPanelFull(layout.mapWidth, layout.mapHeight)
	statusContent := m.renderStatusPanel(layout.statusWidth, layout.statusHeight)

	// Join log and map horizontally (top row)
	topRow := lipgloss.JoinHorizontal(lipgloss.Top, logContent, mapContent)

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

	// Log panel gets 40% of top row
	logHeight := topHeight * 40 / 100
	if logHeight < 10 {
		logHeight = 10
	}

	// Map panel gets rest of top row
	mapHeight := topHeight - logHeight

	// Width calculations
	logWidth := m.viewportWidth * 40 / 100
	mapWidth := m.viewportWidth - logWidth - 3 // Account for borders

	return panelLayout{
		logWidth:     logWidth,
		logHeight:    logHeight,
		mapWidth:     mapWidth,
		mapHeight:    mapHeight,
		statusWidth:  m.viewportWidth,
		statusHeight: statusHeight,
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
