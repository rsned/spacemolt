package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/rsned/spacemolt/pkg/game"
)

// Panel models

// logPanelModel represents the action log panel
type logPanelModel struct {
	lines        []string
	maxLines     int
	scrollOffset int
	lastUpdate   time.Time
	cachedRender string
}

// mapPanelModel represents the map panel
type mapPanelModel struct {
	lastUpdate   time.Time
	cachedRender string
}

// statusPanelModel represents the status panel
type statusPanelModel struct {
	lastUpdate   time.Time
	cachedRender string
	compactMode  bool
}

// agentPanelModel represents the agent list panel
type agentPanelModel struct {
	agents      []string
	selected    int
	showDetails bool
}

// panelLayout holds the calculated dimensions for each panel
type panelLayout struct {
	agentWidth   int
	agentHeight  int // Fixed height at bottom (agentsPanelHeight, typically 6)
	logWidth     int
	logHeight    int // Capped at maxLogPanelHeight (typically 12)
	mapWidth     int
	mapHeight    int // Dynamic: gets remaining vertical space in top section
	statusWidth  int
	statusHeight int
}

// renderLogPanel renders the full log panel with scrolling content
func (m *WatcherModel) renderLogPanel(width, height int) string {
	// Build content with log lines
	var sb strings.Builder

	// Calculate how many lines we can show (minus borders, title, and padding)
	// Height breakdown: top border(1) + title line(1) + blank line(1) + content + bottom border(1) = total
	// So content lines = height - 4
	availableLines := height - 4

	// If we have logs and they exceed available space, reserve 1 line for scroll indicator
	hasLogs := len(m.logPanel.lines) > 0
	logContentLines := availableLines
	if hasLogs && len(m.logPanel.lines) > availableLines {
		logContentLines = availableLines - 1 // Reserve 1 line for scroll indicator
	}

	// Determine which lines to show based on scroll offset
	// scrollOffset 0 = show newest (bottom), higher values scroll up (show older)
	startIdx := len(m.logPanel.lines) - logContentLines - m.logPanel.scrollOffset
	if startIdx < 0 {
		startIdx = 0
	}
	endIdx := startIdx + logContentLines
	if endIdx > len(m.logPanel.lines) {
		endIdx = len(m.logPanel.lines)
	}

	// Render visible log lines
	for i := startIdx; i < endIdx; i++ {
		sb.WriteString(m.logPanel.lines[i])
		sb.WriteString("\n")
	}

	// If no lines, show placeholder (uses 1 line)
	if !hasLogs {
		sb.WriteString(lipgloss.NewStyle().Faint(true).Render("Log messages will appear here..."))
	} else {
		// Add scroll indicator on its own line if content is scrollable
		if len(m.logPanel.lines) > availableLines {
			if m.logPanel.scrollOffset > 0 {
				sb.WriteString(lipgloss.NewStyle().Faint(true).Render("↑ (more above)"))
			} else {
				sb.WriteString(lipgloss.NewStyle().Faint(true).Render("↓ (scroll with ↑/↓ or j/k)"))
			}
		}
	}

	// Build panel with title and border using lipgloss
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("62"))
	content := titleStyle.Render("Action Log\n") + sb.String()

	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Width(width).
		Height(height)

	return style.Render(content)
}

// renderMapPanelFull renders the full map panel with system info, map, and legend
func (m *WatcherModel) renderMapPanelFull(width, height int) string {
	// Calculate grid dimensions based on available space
	const fixedLines = 9  // Title, header, legend, map borders

	// Calculate available grid rows (height)
	availableGridRows := height - fixedLines
	if availableGridRows < 5 {
		availableGridRows = 5
	}

	// Calculate available grid columns (width)
	// Subtract: lipgloss border (2) + ASCII map borders (5 = max width with connectors)
	// Add extra padding to ensure map fits comfortably
	const totalBorderWidth = 8 // lipgloss border (2) + ASCII borders max (5) + padding (1)
	availableGridCols := width - totalBorderWidth
	if availableGridCols < 10 {
		availableGridCols = 10
	}

	// Cap maximum sizes to avoid performance issues
	const maxGridRows = 60
	const maxGridCols = 120
	if availableGridRows > maxGridRows {
		availableGridRows = maxGridRows
	}
	if availableGridCols > maxGridCols {
		availableGridCols = maxGridCols
	}

	// Calculate half-sizes for renderMap (which creates halfSize*2+1 grid)
	halfGridRows := (availableGridRows - 1) / 2
	halfGridCols := (availableGridCols - 1) / 2

	if halfGridRows < 3 {
		halfGridRows = 3
	}
	if halfGridCols < 5 {
		halfGridCols = 5
	}

	// Build content with map data
	var sb strings.Builder

	// Get the map panel content
	var content string
	state := m.GetCurrentState()
	if state == nil {
		// No state available
		content = lipgloss.NewStyle().Faint(true).Render("No agent selected")
	} else {
		state.Mu.Lock()
		content = renderMapPanel(state.System, state, halfGridRows, halfGridCols)
		state.Mu.Unlock()
	}

	sb.WriteString(content)

	// Build panel with title and border using lipgloss
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("62"))
	panelContent := titleStyle.Render("System Map\n\n") + sb.String()

	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Width(width).
		Height(height)

	return style.Render(panelContent)
}

// renderStatusPanel renders the full status panel with player and ship stats
func (m *WatcherModel) renderStatusPanel(width, height int) string {
	// Build content with status data
	state := m.GetCurrentState()
	var content string

	if state == nil {
		content = lipgloss.NewStyle().Faint(true).Render("No agent selected")
	} else {
		state.Mu.Lock()
		content = m.buildStatusFull(state)
		state.Mu.Unlock()
	}

	// Build panel with title and border using lipgloss
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("62"))
	panelContent := titleStyle.Render("Status\n\n") + content

	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Width(width).
		Height(height)

	return style.Render(panelContent)
}

// buildStatusFull builds a three-column status layout for wide screens
func (m *WatcherModel) buildStatusFull(state *game.State) string {
	var leftCol, midCol, rightCol strings.Builder

	// Column 1: Player info (compact 4 lines)
	leftCol.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86")).Render("Player"))
	leftCol.WriteString("\n")
	leftCol.WriteString(fmt.Sprintf("Name: %s\n", state.Username))
	leftCol.WriteString(fmt.Sprintf("Credits: %.0f\n", state.Credits))

	// Column 2: Ship data (compact 4 lines)
	midCol.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86")).Render("Ship"))
	midCol.WriteString("\n")
	midCol.WriteString(fmt.Sprintf("Hull: %.0f/%.0f\n", state.Hull, state.MaxHull))
	midCol.WriteString(fmt.Sprintf("Fuel: %.0f/%.0f", state.Fuel, state.MaxFuel))

	// Column 3: Cargo (compact 1-2 lines)
	rightCol.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86")).Render("Cargo"))
	rightCol.WriteString("\n")
	if len(state.Cargo) > 0 {
		for _, item := range state.Cargo {
			if name, ok := item["name"].(string); ok {
				if quantity, ok := item["quantity"].(float64); ok {
					rightCol.WriteString(fmt.Sprintf("%s x%.0f", name, quantity))
					break // Only show first item
				}
			}
		}
	} else {
		rightCol.WriteString("Empty")
	}

	// Combine columns with spacing
	leftStyle := lipgloss.NewStyle().Width(25)
	midStyle := lipgloss.NewStyle().Width(25)
	rightStyle := lipgloss.NewStyle().Width(30)
	spacer := lipgloss.NewStyle().Width(2).Render("")

	return lipgloss.JoinHorizontal(lipgloss.Top,
		leftStyle.Render(leftCol.String()),
		spacer,
		midStyle.Render(midCol.String()),
		spacer,
		rightStyle.Render(rightCol.String()),
	)
}
