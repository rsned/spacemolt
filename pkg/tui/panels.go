package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
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

// panelLayout holds the calculated dimensions for each panel
type panelLayout struct {
	logWidth    int
	logHeight   int
	mapWidth    int
	mapHeight   int
	statusWidth int
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
	const borderWidth = 3  // Borders

	// Calculate available grid rows (height)
	availableGridRows := height - fixedLines
	if availableGridRows < 5 {
		availableGridRows = 5
	}

	// Calculate available grid columns (width)
	availableGridCols := width - borderWidth
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
	m.gameState.Mu.Lock()
	content := renderMapPanel(m.gameState.System, m.gameState, halfGridRows, halfGridCols)
	m.gameState.Mu.Unlock()

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
	m.gameState.Mu.Lock()
	content := m.buildStatusContent(width)
	m.gameState.Mu.Unlock()

	sb.WriteString(content)

	return style.Render(sb.String())
}

// buildStatusContent builds the appropriate status content based on mode
func (m *WatcherModel) buildStatusContent(width int) string {
	if m.statusPanel.compactMode {
		return m.buildStatusCompact()
	}
	return m.buildStatusFull()
}

// buildStatusFull builds a two-column status layout for wide screens
func (m *WatcherModel) buildStatusFull() string {
	var leftCol, rightCol strings.Builder

	// Left column: Player info and credits
	leftCol.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86")).Render("Player"))
	leftCol.WriteString("\n")
	leftCol.WriteString(fmt.Sprintf("Name: %s\n", m.gameState.Username))
	leftCol.WriteString("Empire: voidborn\n")
	leftCol.WriteString(fmt.Sprintf("Credits: %.0f\n", m.gameState.Credits))

	// Right column: Ship stats
	rightCol.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86")).Render("Ship"))
	rightCol.WriteString("\n")
	rightCol.WriteString("Type: Prospector (starter_mining)\n")
	rightCol.WriteString(fmt.Sprintf("Hull: %.0f/%.0f\n", m.gameState.Hull, m.gameState.MaxHull))
	rightCol.WriteString(fmt.Sprintf("Fuel: %.0f/%.0f\n", m.gameState.Fuel, m.gameState.MaxFuel))
	rightCol.WriteString(fmt.Sprintf("Cargo: %d/%d\n", len(m.gameState.Cargo), m.gameState.MaxCargo))

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
	if len(m.gameState.Cargo) > 0 {
		combined += "\n\n"
		combined += lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86")).Render("Cargo Items:")
		combined += "\n"
		for _, item := range m.gameState.Cargo {
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
func (m *WatcherModel) buildStatusCompact() string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("%s | Credits: %.0f | Hull: %.0f/%.0f | Fuel: %.0f/%.0f | Cargo: %d/%d",
		m.gameState.Username,
		m.gameState.Credits,
		m.gameState.Hull, m.gameState.MaxHull,
		m.gameState.Fuel, m.gameState.MaxFuel,
		len(m.gameState.Cargo), m.gameState.MaxCargo))

	return sb.String()
}
