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
	agentHeight  int // May vary based on available space
	logWidth     int
	logHeight    int // Capped at maxLogPanelHeight (typically 12)
	mapWidth     int
	mapHeight    int // Gets priority for remaining space
	statusWidth  int
	statusHeight int
}

// renderLogPanel renders the full log panel with scrolling content
func (m *WatcherModel) renderLogPanel(width, height int) string {
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Width(width).
		Height(height)

	// Build content with title and log lines
	var sb strings.Builder
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("62"))
	sb.WriteString(titleStyle.Render("Action Log"))
	sb.WriteString("\n")

	// Calculate how many lines we can show (minus title and padding)
	availableLines := height - 3 // Account for title and borders

	// Determine which lines to show based on scroll offset
	// scrollOffset 0 = show newest (bottom), higher values scroll up (show older)
	startIdx := len(m.logPanel.lines) - availableLines - m.logPanel.scrollOffset
	if startIdx < 0 {
		startIdx = 0
	}
	endIdx := startIdx + availableLines
	if endIdx > len(m.logPanel.lines) {
		endIdx = len(m.logPanel.lines)
	}

	// Render visible lines
	for i := startIdx; i < endIdx; i++ {
		sb.WriteString(m.logPanel.lines[i])
		sb.WriteString("\n")
	}

	// If no lines, show placeholder
	if len(m.logPanel.lines) == 0 {
		sb.WriteString(lipgloss.NewStyle().Faint(true).Render("Log messages will appear here..."))
	}

	// Add scroll indicator if content is scrollable
	if len(m.logPanel.lines) > availableLines {
		if m.logPanel.scrollOffset > 0 {
			sb.WriteString(lipgloss.NewStyle().Faint(true).Render("↑ (more above)"))
		} else {
			sb.WriteString(lipgloss.NewStyle().Faint(true).Render("↓ (scroll with ↑/↓ or j/k)"))
		}
	}

	return style.Render(sb.String())
}

// renderMapPanelFull renders the full map panel with system info, map, and legend
func (m *WatcherModel) renderMapPanelFull(width, height int) string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("62"))

	// Calculate grid dimensions based on available space
	const fixedLines = 8  // Title, header, legend, map borders

	// Calculate available grid rows (height)
	availableGridRows := height - fixedLines
	if availableGridRows < 5 {
		availableGridRows = 5
	}

	// Calculate available grid columns (width)
	// Subtract: lipgloss border (2) + ASCII map borders (4 = left pipe + space + space + right pipe)
	const totalBorderWidth = 6 // lipgloss border (2) + ASCII borders (4)
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

	// Build content with title and map data
	var sb strings.Builder
	sb.WriteString(titleStyle.Render("System Map"))
	sb.WriteString("\n\n")

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

	// Apply border style with constraints
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Width(width).
		Height(height)

	return style.Render(sb.String())
}

// renderStatusPanel renders the full status panel with player and ship stats
func (m *WatcherModel) renderStatusPanel(width, height int) string {
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Width(width).
		Height(height)

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("62"))

	// Build content with title and status data
	var sb strings.Builder
	sb.WriteString(titleStyle.Render("Status"))
	sb.WriteString("\n\n")

	// Determine compact mode based on width
	m.statusPanel.compactMode = width < 80

	// Get status content
	state := m.GetCurrentState()
	if state == nil {
		sb.WriteString(lipgloss.NewStyle().Faint(true).Render("No agent selected"))
	} else {
		state.Mu.Lock()
		content := m.buildStatusContent(state, width)
		state.Mu.Unlock()
		sb.WriteString(content)
	}

	return style.Render(sb.String())
}

// buildStatusContent builds the appropriate status content based on mode
func (m *WatcherModel) buildStatusContent(state *game.State, width int) string {
	if m.statusPanel.compactMode {
		return m.buildStatusCompact(state)
	}
	return m.buildStatusFull(state)
}

// buildStatusFull builds a two-column status layout for wide screens
func (m *WatcherModel) buildStatusFull(state *game.State) string {
	var leftCol, rightCol strings.Builder

	// Left column: Player info and credits
	leftCol.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86")).Render("Player"))
	leftCol.WriteString("\n")
	leftCol.WriteString(fmt.Sprintf("Name: %s\n", state.Username))
	leftCol.WriteString("Empire: voidborn\n")
	leftCol.WriteString(fmt.Sprintf("Credits: %.0f\n", state.Credits))

	// Right column: Ship stats
	rightCol.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86")).Render("Ship"))
	rightCol.WriteString("\n")
	rightCol.WriteString("Type: Prospector (starter_mining)\n")
	rightCol.WriteString(fmt.Sprintf("Hull: %.0f/%.0f\n", state.Hull, state.MaxHull))
	rightCol.WriteString(fmt.Sprintf("Fuel: %.0f/%.0f\n", state.Fuel, state.MaxFuel))
	rightCol.WriteString(fmt.Sprintf("Cargo: %d/%d\n", len(state.Cargo), state.MaxCargo))

	// Combine columns with spacing
	leftStyle := lipgloss.NewStyle().Width(30)
	rightStyle := lipgloss.NewStyle().Width(30)
	spacer := lipgloss.NewStyle().Width(4).Render("") // 4 spaces between columns
	combined := lipgloss.JoinHorizontal(lipgloss.Top,
		leftStyle.Render(leftCol.String()),
		spacer,
		rightStyle.Render(rightCol.String()),
	)

	// Add cargo details if any
	if len(state.Cargo) > 0 {
		combined += "\n\n"
		combined += lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86")).Render("Cargo Items:")
		combined += "\n"
		for _, item := range state.Cargo {
			if name, ok := item["name"].(string); ok {
				if quantity, ok := item["quantity"].(float64); ok {
					combined += fmt.Sprintf("  - %s x%.0f\n", name, quantity)
				} else {
					combined += fmt.Sprintf("  - %s\n", name)
				}
			}
		}
	}

	return combined
}

// buildStatusCompact builds a single-line status layout for narrow screens
func (m *WatcherModel) buildStatusCompact(state *game.State) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("%s | Credits: %.0f | Hull: %.0f/%.0f | Fuel: %.0f/%.0f | Cargo: %d/%d",
		state.Username,
		state.Credits,
		state.Hull, state.MaxHull,
		state.Fuel, state.MaxFuel,
		len(state.Cargo), state.MaxCargo))

	return sb.String()
}
