package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/rsned/spacemolt/pkg/game"
)

// RenderBorderedTitle creates a top border with embedded title (exported for use in model.go)
func RenderBorderedTitle(title string, width int) string {
	// Ensure minimum width
	if width < 8 {
		width = 8
	}

	borderColor := lipgloss.Color("62")

	// Calculate available space for title (excluding border chars and padding)
	maxTitleWidth := width - 6 // ╭─ (2) + spaces around title (2) + ─╮ (2)
	if maxTitleWidth < 1 {
		maxTitleWidth = 1
	}

	// Truncate title if necessary (using visual width, not string length)
	titleRunes := []rune(title)
	if len(titleRunes) > maxTitleWidth {
		title = string(titleRunes[:maxTitleWidth])
	}

	// Calculate remaining width for dashes after title and borders
	titleWidth := len(titleRunes)
	if titleWidth > maxTitleWidth {
		titleWidth = maxTitleWidth
	}

	// Total used: ╭─ (2) + space (1) + title + space (1) + ─╮ (2)
	usedWidth := 6 + titleWidth
	remainingWidth := width - usedWidth

	// Build border parts
	left := lipgloss.NewStyle().Foreground(borderColor).Render("╭─")
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("62"))
	titlePart := " " + titleStyle.Render(title) + " "
	right := lipgloss.NewStyle().Foreground(borderColor).Render("─╮")

	// Add remaining dashes (or none if title fills space)
	if remainingWidth > 0 {
		middle := lipgloss.NewStyle().Foreground(borderColor).Render(strings.Repeat("─", remainingWidth))
		return left + titlePart + middle + right
	}

	return left + titlePart + right
}

// RenderBorderBottom creates a bottom border (exported for use in model.go)
func RenderBorderBottom(width int) string {
	// Ensure minimum width
	if width < 4 {
		width = 4
	}

	borderColor := lipgloss.Color("62")
	dashes := width - 2
	if dashes < 2 {
		dashes = 2
	}
	return lipgloss.NewStyle().Foreground(borderColor).Render("╰" + strings.Repeat("─", dashes) + "╯")
}

// RenderBorderedContent creates the side borders with content (exported for use in model.go)
func RenderBorderedContent(content string, width int) string {
	// Ensure minimum width
	if width < 6 {
		width = 6
	}

	lines := strings.Split(content, "\n")
	borderColor := lipgloss.Color("62")
	left := lipgloss.NewStyle().Foreground(borderColor).Render("│")
	right := lipgloss.NewStyle().Foreground(borderColor).Render("│")

	var result strings.Builder
	for _, line := range lines {
		// Pad or truncate line to fit width
		lineWidth := width - 4 // 2 for borders, 2 for padding
		if lineWidth < 0 {
			lineWidth = 0
		}
		if len(line) < lineWidth {
			line = line + strings.Repeat(" ", lineWidth-len(line))
		} else if len(line) > lineWidth {
			line = line[:lineWidth]
		}
		result.WriteString(left + " " + line + " " + right + "\n")
	}
	return result.String()
}

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
	// Build content with log lines
	var sb strings.Builder

	// Calculate how many lines we can show (minus borders)
	availableLines := height - 3 // Account for top border, bottom border, and padding

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
	} else {
		// Add scroll indicator if content is scrollable
		if len(m.logPanel.lines) > availableLines {
			if m.logPanel.scrollOffset > 0 {
				sb.WriteString(lipgloss.NewStyle().Faint(true).Render("↑ (more above)"))
			} else {
				sb.WriteString(lipgloss.NewStyle().Faint(true).Render("↓ (scroll with ↑/↓ or j/k)"))
			}
		}
	}

	// Build bordered panel with title
	var result strings.Builder
	result.WriteString(RenderBorderedTitle("Action Log", width))
	result.WriteString(RenderBorderedContent(sb.String(), width))
	result.WriteString(RenderBorderBottom(width))

	return result.String()
}

// renderMapPanelFull renders the full map panel with system info, map, and legend
func (m *WatcherModel) renderMapPanelFull(width, height int) string {
	// Calculate grid dimensions based on available space
	const fixedLines = 8  // Title, header, legend, map borders

	// Calculate available grid rows (height)
	availableGridRows := height - fixedLines
	if availableGridRows < 5 {
		availableGridRows = 5
	}

	// Calculate available grid columns (width)
	// Subtract: lipgloss border (2) + ASCII map borders (3 = space + left pipe + right pipe)
	const totalBorderWidth = 5 // lipgloss border (2) + ASCII borders (3)
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

	// Build bordered panel with title
	var result strings.Builder
	result.WriteString(RenderBorderedTitle("System Map", width))
	result.WriteString(RenderBorderedContent(sb.String(), width))
	result.WriteString(RenderBorderBottom(width))

	return result.String()
}

// renderStatusPanel renders the full status panel with player and ship stats
func (m *WatcherModel) renderStatusPanel(width, height int) string {
	// Build content with status data
	var sb strings.Builder

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

	// Build bordered panel with title
	var result strings.Builder
	result.WriteString(RenderBorderedTitle("Status", width))
	result.WriteString(RenderBorderedContent(sb.String(), width))
	result.WriteString(RenderBorderBottom(width))

	return result.String()
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
