package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/user/spacemolt/pkg/game"
)

// renderMap creates a dynamic-sized grid representation of the system
// Calculates extents from POI positions and scales them to fit the grid
// halfGridRows and halfGridCols are half-dimensions (resulting grid is 2*half+1 for each dimension)
func renderMap(sysData game.SystemData, halfGridRows, halfGridCols int) string {
	// Ensure minimum sizes
	if halfGridRows < 3 {
		halfGridRows = 3
	}
	if halfGridCols < 3 {
		halfGridCols = 3
	}

	mapRows := halfGridRows*2 + 1 // Total grid rows
	mapCols := halfGridCols*2 + 1 // Total grid cols

	// Use a slice instead of fixed array for dynamic sizing (rectangular grid)
	grid := make([][]rune, mapRows)
	for y := range mapRows {
		grid[y] = make([]rune, mapCols)
		for x := range mapCols {
			grid[y][x] = ' '
		}
	}

	// Calculate extents of all POIs in the system
	// Find the maximum absolute value for each axis
	var maxAbsX, maxAbsY float64
	firstPOI := true
	for _, poi := range sysData.POIs {
		absX := poi.Position.X
		if absX < 0 {
			absX = -absX
		}
		absY := poi.Position.Y
		if absY < 0 {
			absY = -absY
		}

		if firstPOI {
			maxAbsX, maxAbsY = absX, absY
			firstPOI = false
		} else {
			if absX > maxAbsX {
				maxAbsX = absX
			}
			if absY > maxAbsY {
				maxAbsY = absY
			}
		}
	}

	// If no POIs, use default bounds
	if firstPOI {
		maxAbsX, maxAbsY = 10.0, 10.0
	}

	// Ensure minimum size (all at origin or very close)
	if maxAbsX < 1.0 {
		maxAbsX = 10.0
	}
	if maxAbsY < 1.0 {
		maxAbsY = 10.0
	}

	// Add 25% padding
	paddedMaxX := maxAbsX * 1.25
	paddedMaxY := maxAbsY * 1.25

	// (0,0) is at the center, so bounds are:
	// X: [-paddedMaxX, +paddedMaxX]
	// Y: [-paddedMaxY, +paddedMaxY]

	// Scale factors for each dimension (world coords to grid coords)
	// Grid center is at (mapCols/2, mapRows/2)
	centerGridX := float64(mapCols) / 2
	centerGridY := float64(mapRows) / 2
	scaleX := centerGridX / paddedMaxX
	scaleY := centerGridY / paddedMaxY

	// Use the same scale for both to maintain aspect ratio
	scale := scaleX
	if scaleY < scale {
		scale = scaleY
	}

	// Helper function to convert world coordinates to grid coordinates
	// (0,0) maps to center of grid
	worldToGrid := func(worldX, worldY float64) (int, int) {
		gridX := int(centerGridX + worldX*scale)
		gridY := int(centerGridY - worldY*scale) // Flip Y so +Y is up
		// Clamp to grid bounds
		if gridX < 0 {
			gridX = 0
		}
		if gridX >= mapCols {
			gridX = mapCols - 1
		}
		if gridY < 0 {
			gridY = 0
		}
		if gridY >= mapRows {
			gridY = mapRows - 1
		}
		return gridX, gridY
	}

	// Build priority map for rendering
	priority := map[string]int{
		"ship":          6,
		"station":       5,
		"sun":           4,
		"planet":        3,
		"moon":          2,
		"asteroid_belt": 1,
		"asteroid":      1,
	}

	// Render POIs by priority
	for _, poi := range sysData.POIs {
		gridX, gridY := worldToGrid(poi.Position.X, poi.Position.Y)

		currentPriority := priority[poi.Type]
		existingAtPos := grid[gridY][gridX]

		// Get marker for this POI type
		marker := getEntityMarker(poi.Type)

		// Check if we should overwrite based on priority
		shouldPlace := true
		if existingAtPos != ' ' && existingAtPos != '@' {
			// Find what type is at this position
			existingType := getEntityTypeFromMarker(existingAtPos)
			if existingType != "" && priority[existingType] > currentPriority {
				shouldPlace = false
			}
		}

		if shouldPlace {
			grid[gridY][gridX] = marker
		}
	}

	// Find and render ship position
	var shipX, shipY float64
	shipFound := false
	for _, poi := range sysData.POIs {
		if poi.ID == sysData.ShipPOI {
			shipX, shipY = poi.Position.X, poi.Position.Y
			shipFound = true
			break
		}
	}

	// Render ship
	if shipFound {
		gridShipX, gridShipY := worldToGrid(shipX, shipY)
		grid[gridShipY][gridShipX] = '@'
	}

	// Calculate center for border rendering
	centerGridXInt := mapCols / 2

	// Build output string with borders and axis markers
	var sb strings.Builder
	// Top border: left border + axis space + map border
	sb.WriteString("┌─" + strings.Repeat("─", mapCols) + "┐\n")

	for y := 0; y < mapRows; y++ {
		// Simplified Y-axis marker - just " │" for all rows
		sb.WriteString(" │")

		for x := 0; x < mapCols; x++ {
			sb.WriteRune(grid[y][x])
		}
		sb.WriteString("│\n")
	}

	// Bottom border with connector at center
	sb.WriteString("└─" + strings.Repeat("─", centerGridXInt) + "┴" + strings.Repeat("─", mapCols-centerGridXInt-1) + "┘")

	// System name and range on same line
	if sysData.Name != "" {
		sb.WriteString(fmt.Sprintf("\n%s | X:[±%.1f] Y:[±%.1f]",
			sysData.Name, paddedMaxX, paddedMaxY))
	} else {
		sb.WriteString(fmt.Sprintf("\nX:[±%.1f] Y:[±%.1f]", paddedMaxX, paddedMaxY))
	}

	return sb.String()
}

func getEntityMarker(entityType string) rune {
	switch entityType {
	case "sun":
		return 'O'
	case "planet":
		return 'o'
	case "moon":
		return '*'
	case "station":
		return 'S'
	case "asteroid_belt", "asteroid":
		return '.'
	default:
		return '?'
	}
}

func getEntityTypeFromMarker(marker rune) string {
	switch marker {
	case 'O':
		return "sun"
	case 'o':
		return "planet"
	case '*':
		return "moon"
	case 'S':
		return "station"
	case '.':
		return "asteroid"
	default:
		return ""
	}
}

// renderMapHeader renders the map header with system name, location, and dock status
func renderMapHeader(sysData game.SystemData, gameState *game.State) string {
	var sb strings.Builder

	// System name
	if sysData.Name != "" {
		sb.WriteString(lipgloss.NewStyle().Bold(true).Render(fmt.Sprintf("System: %s", sysData.Name)))
		sb.WriteString(" | ")
	}

	// Current location
	if gameState.CurrentPOI != "" {
		// Find POI name
		poiName := gameState.CurrentPOI
		for _, poi := range sysData.POIs {
			if poi.ID == gameState.CurrentPOI {
				poiName = poi.Name
				break
			}
		}
		sb.WriteString(fmt.Sprintf("Location: %s", poiName))
		sb.WriteString(" | ")
	}

	// Dock status
	status := "IN SPACE"
	if gameState.Doc {
		status = "DOCKED"
	}
	statusStyle := lipgloss.NewStyle()
	if gameState.Doc {
		statusStyle = statusStyle.Foreground(lipgloss.Color("green"))
	} else {
		statusStyle = statusStyle.Foreground(lipgloss.Color("yellow"))
	}
	sb.WriteString(statusStyle.Render(status))

	return sb.String()
}

// renderMapLegendInline returns a single-line legend string
func renderMapLegendInline() string {
	return lipgloss.NewStyle().Faint(true).Render(
		"Legend: @=Ship O=Sun o=Planet *=Moon S=Station .=Asteroid",
	)
}

// renderMapPanel combines header, map, and legend into a single panel content
// halfGridRows and halfGridCols are the half-dimensions (resulting grid is 2*half+1 for each)
func renderMapPanel(sysData game.SystemData, gameState *game.State, halfGridRows, halfGridCols int) string {
	var sb strings.Builder

	// Header
	sb.WriteString(renderMapHeader(sysData, gameState))
	sb.WriteString("\n\n")

	// Map
	sb.WriteString(renderMap(sysData, halfGridRows, halfGridCols))
	sb.WriteString("\n\n")

	// Legend
	sb.WriteString(renderMapLegendInline())

	return sb.String()
}
