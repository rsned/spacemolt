package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mattn/go-runewidth"
	"github.com/rsned/spacemolt/internal/protocol"
	"github.com/rsned/spacemolt/pkg/game"
)

const gameServerURL = "wss://game.spacemolt.com/ws"

type Credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Empire   string `json:"empire"`
}

type StatusHandler struct {
	logger *log.Logger
}

func (h *StatusHandler) OnConnected(state *game.State) {
	h.logger.Printf("✓ Connected!")
}

func (h *StatusHandler) OnMessage(resp protocol.Response) {
	// Ignore messages
}

func (h *StatusHandler) OnDisconnected(err error) {
	h.logger.Printf("Disconnected: %v", err)
}

func loadCredentials(agentDir string) (*Credentials, error) {
	data, err := os.ReadFile(filepath.Join(agentDir, "credentials.json"))
	if err != nil {
		return nil, err
	}
	var creds Credentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, err
	}
	return &creds, nil
}

// truncateOrPadRight right-justifies a string within width (padding on left)
func truncateOrPadRight(s string, width int) string {
	currentWidth := runewidth.StringWidth(s)
	if currentWidth > width {
		// Truncate
		return runewidth.Truncate(s, width, "…")
	}
	// Pad on left
	return strings.Repeat(" ", width-currentWidth) + s
}

// formatCargoItemLine formats a single cargo line for the right column:
// "   item_id              xN" (item left-aligned, quantity right-aligned within width).
func formatCargoItemLine(itemID string, quantity float64, width int) string {
	prefix := "   "
	qtyStr := fmt.Sprintf("x%.0f", quantity)
	qtyW := runewidth.StringWidth(qtyStr)
	available := width - runewidth.StringWidth(prefix) - qtyW
	if available < 0 {
		available = 0
	}
	itemPart := prefix + runewidth.Truncate(itemID, available, "…")
	pad := width - runewidth.StringWidth(itemPart) - qtyW
	if pad < 0 {
		pad = 0
	}
	return itemPart + strings.Repeat(" ", pad) + qtyStr
}

// formatLabelValueCell renders one cell: label left-justified, value right-justified
// within width. Uses runewidth for display width. Value is truncated with "…" if too long.
// Non-empty labels get a trailing ":" before printing.
func formatLabelValueCell(label, value string, width int) string {
	displayLabel := label
	if label != "" {
		displayLabel = label + ":"
	}
	labelW := runewidth.StringWidth(displayLabel)
	valueZoneWidth := max(0, width-labelW)
	return displayLabel + truncateOrPadRight(value, valueZoneWidth)
}

// formatTwoColumnRow returns a full table row of two cells: "│ col1 │ col2 │\n".
// Each cell has col1Label/col2Label left-justified and col1Value/col2Value
// right-justified within colWidth. colWidth is the content width per column
// (e.g. 32 for a 71-char row: 1+1+32+1+1+1+32+1+1=71).
func formatTwoColumnRow(col1Label, col1Value, col2Label, col2Value string, colWidth int) string {
	c1 := formatLabelValueCell(col1Label, col1Value, colWidth)
	c2 := formatLabelValueCell(col2Label, col2Value, colWidth)
	return fmt.Sprintf("│ %s │ %s │\n", c1, c2)
}

// pilotColWidth is the content width per column for the PILOT/LOCATION-style rows (71-char row).
const pilotColWidth = 39

// sectionTitleBorder is the border line for section headers. The first cell's runes
// (after ┌) are overwritten with the section title (uppercase) by sectionTitleLine.
const sectionTitleBorder = "┌─────────────────────────────────────────┬─────────────────────────────────────────┐"
const sectionBaseBorder = "└─────────────────────────────────────────┴─────────────────────────────────────────┘"

// xpRequiredForNextLevel returns XP required to reach the next level from skills data.
// Uses state.SkillNextLevelXP (from get_skills player_skills) if set, else state.SkillDefinitions
// (xp_per_level[level]). Returns -1 if at max level (caller may display "MAX").
func xpRequiredForNextLevel(state *game.State, skillID string, level int) float64 {
	if nextXP, ok := state.SkillNextLevelXP[skillID]; ok && nextXP >= 0 {
		return nextXP
	}
	def, ok := state.SkillDefinitions[skillID]
	if !ok || len(def.XpPerLevel) == 0 {
		// Fallback: simple formula when no skills JSON data available
		if level == 0 {
			return 100
		}
		return float64((level + 1) * 100)
	}
	if level >= def.MaxLevel || level >= len(def.XpPerLevel) {
		return -1
	}
	return def.XpPerLevel[level]
}

// sectionTitleLine returns sectionTitleBorder with the first cell's runes (after ┌)
// replaced by title in uppercase. Works in runes so we never split multi-byte UTF-8.
func sectionTitleLine(title string) string {
	runes := []rune(sectionTitleBorder)
	// First rune is ┌; find end of first cell (┬).
	firstCellEnd := 1
	for firstCellEnd < len(runes) && runes[firstCellEnd] != '┬' {
		firstCellEnd++
	}
	cellRunes := firstCellEnd - 1 // rune count for title in first cell
	upper := []rune(strings.ToUpper(title))
	if len(upper) > cellRunes {
		upper = upper[:cellRunes]
	}
	copy(runes[1:1+len(upper)], upper)
	return string(runes)
}

// │ Home Base:                haven_base │ Status:                              │
func printStatus(state *game.State) {
	fmt.Println()
	fmt.Println("╔═══════════════════════════════════════════════════════════════════════════════╗")
	fmt.Println("║                                  AGENT STATUS                                 ║")
	fmt.Println("╚═══════════════════════════════════════════════════════════════════════════════╝")

	// ------------------------------------------------------------------

	// PILOT section
	fmt.Println(sectionTitleLine("Pilot"))
	fmt.Print(formatTwoColumnRow("Name", state.Username,
		"Faction", state.Player.FactionID, pilotColWidth))

	empireName := state.Player.Empire
	if empireName != "" {
		empireName = titleCase(empireName)
	}
	// TODO change hard coded colors to values from the "login" response
	// object.  The "player" struct has primary_color, secondary_color
	fmt.Print(formatTwoColumnRow("Empire", empireName, "Colors", "🟥🟥🟥🟥🟥/🟦🟦🟦🟦🟦", pilotColWidth))

	homeBase := state.Player.HomeBase
	if homeBase == "" {
		homeBase = "None"
	}
	statusMsg := state.Player.StatusMessage
	if statusMsg == "" {
		statusMsg = ""
	}
	fmt.Print(formatTwoColumnRow("Home Base", homeBase, "Status", statusMsg, pilotColWidth))
	fmt.Print(formatTwoColumnRow("Credits", fmt.Sprintf("%d", int(state.Credits)), "", "", pilotColWidth))
	fmt.Println(sectionBaseBorder)
	fmt.Println()

	// ------------------------------------------------------------------

	// SKILLS section - show if player has skills or skill XP
	if len(state.Player.Skills) > 0 || len(state.SkillXP) > 0 {
		fmt.Println(sectionTitleLine("Skills"))

		// Collect all skills into a slice for consistent ordering
		type skillInfo struct {
			id       string
			name     string
			xpValue  string
			levelStr string
		}
		var skills []skillInfo

		for skillID, skill := range state.Player.Skills {
			skillName := titleCase(strings.ReplaceAll(skillID, "_", " "))

			// Get XP from skill_xp if available, otherwise use skill.XP
			currentXP := skill.XP
			if xp, ok := state.SkillXP[skillID]; ok {
				currentXP = xp
			}

			// XP required for next level: from get_skills response, or from skill definition xp_per_level
			xpToNext := xpRequiredForNextLevel(state, skillID, skill.Level)
			xpToNextStr := fmt.Sprintf("%4d", int(xpToNext))
			if xpToNext < 0 {
				xpToNextStr = "MAX"
			}

			skills = append(skills, skillInfo{
				id:       skillID,
				name:     skillName,
				xpValue:  fmt.Sprintf("%4d / %4s XP", int(currentXP), xpToNextStr),
				levelStr: fmt.Sprintf("Level %d", skill.Level),
			})
		}

		// If more than 4 skills, split them between left and right columns
		if len(skills) > 4 {
			splitPoint := (len(skills) + 1) / 2 // Split in half, with odd number going to left
			leftSkills := skills[:splitPoint]
			rightSkills := skills[splitPoint:]

			// Build lines for each column
			var leftLines, rightLines []string

			for i, skill := range leftSkills {
				leftLines = append(leftLines, formatLabelValueCell(skill.name, skill.xpValue, pilotColWidth))
				leftLines = append(leftLines, formatLabelValueCell("", skill.levelStr, pilotColWidth))
				if i < len(leftSkills)-1 {
					leftLines = append(leftLines, strings.Repeat(" ", pilotColWidth))
				}
			}

			for i, skill := range rightSkills {
				rightLines = append(rightLines, formatLabelValueCell(skill.name, skill.xpValue, pilotColWidth))
				rightLines = append(rightLines, formatLabelValueCell("", skill.levelStr, pilotColWidth))
				if i < len(rightSkills)-1 {
					rightLines = append(rightLines, strings.Repeat(" ", pilotColWidth))
				}
			}

			// Print paired rows
			emptyCol := strings.Repeat(" ", pilotColWidth)
			nRows := max(len(leftLines), len(rightLines))
			for i := 0; i < nRows; i++ {
				left := emptyCol
				if i < len(leftLines) {
					left = leftLines[i]
				}
				right := emptyCol
				if i < len(rightLines) {
					right = rightLines[i]
				}
				fmt.Printf("│ %s │ %s │\n", left, right)
			}
		} else {
			// 4 or fewer skills: display in left column only (original behavior)
			for i, skill := range skills {
				fmt.Print(formatTwoColumnRow(skill.name, skill.xpValue, "", "", pilotColWidth))
				fmt.Print(formatTwoColumnRow("", skill.levelStr, "", "", pilotColWidth))
				if i < len(skills)-1 {
					fmt.Print(formatTwoColumnRow("", "", "", "", pilotColWidth))
				}
			}
		}

		fmt.Println(sectionBaseBorder)
	}

	// ------------------------------------------------------------------

	// LOCATION section
	fmt.Println(sectionTitleLine("Location"))
	fmt.Print(formatTwoColumnRow("System", state.System.Name, "POI", state.CurrentPOI, pilotColWidth))

	// Find POI display name
	poiDisplayName := state.CurrentPOI
	for _, poi := range state.System.POIs {
		if poi.ID == state.CurrentPOI {
			if poi.Name != "" && poi.Name != poi.ID {
				poiDisplayName = fmt.Sprintf("\"%s\"", poi.Name)
			}
			break
		}
	}
	fmt.Print(formatTwoColumnRow("Empire", state.System.Empire, "", fmt.Sprintf("(%s)", poiDisplayName), pilotColWidth))

	policePercent := 0
	if state.System.PoliceLevel > 0 {
		policePercent = 100
	}
	dockedStatus := "NO"
	if state.Doc {
		dockedStatus = "YES"
	}
	fmt.Print(formatTwoColumnRow("Police Level", fmt.Sprintf("%d%%", policePercent), "Docked", dockedStatus, pilotColWidth))
	fmt.Println(sectionBaseBorder)

	// ------------------------------------------------------------------

	// SHIP section: left column runs straight (no blank rows for cargo); right column
	// shows Cargo/Items and cargo list. Extra cargo lines extend past left column as empty-left rows.
	fmt.Println(sectionTitleLine("Ship"))
	cargoSummary := fmt.Sprintf("%.0f / %.0f (%s)",
		state.Ship.CargoUsed, state.Ship.CargoCapacity,
		percentBar(state.Ship.CargoUsed, state.Ship.CargoCapacity))
	hullStr := fmt.Sprintf("%.0f / %.0f (%s)",
		state.Ship.Hull, state.Ship.MaxHull,
		percentBar(state.Ship.Hull, state.Ship.MaxHull))
	shieldStr := fmt.Sprintf("%.0f / %.0f (%s)",
		state.Ship.Shield, state.Ship.MaxShield,
		percentBar(state.Ship.Shield, state.Ship.MaxShield))

	// Right column: Cargo summary, blank, blank, Items count, then one line per cargo item.
	rightLines := []string{
		formatLabelValueCell("Cargo", cargoSummary, pilotColWidth),
		"",
		"",
		formatLabelValueCell("Items", fmt.Sprintf("%d", len(state.Ship.Cargo)), pilotColWidth),
	}
	for _, item := range state.Ship.Cargo {
		rightLines = append(rightLines, formatCargoItemLine(item.ItemID, item.Quantity, pilotColWidth))
	}

	// Left column: continuous block (no blanks for cargo) — stats then module list.
	emptyLeft := strings.Repeat(" ", pilotColWidth)
	leftLines := []string{
		formatLabelValueCell("Name", state.Ship.Name, pilotColWidth),
		formatLabelValueCell("Class", state.Ship.ClassID, pilotColWidth),
		formatLabelValueCell("Hull", hullStr, pilotColWidth),
		formatLabelValueCell("Shield", shieldStr, pilotColWidth),
		formatLabelValueCell("Shield Recharge", fmt.Sprintf("+%.0f / tick", state.Ship.ShieldRecharge), pilotColWidth),
		formatLabelValueCell("Armor", fmt.Sprintf("%.0f", state.Ship.Armor), pilotColWidth),
		formatLabelValueCell("CPU", fmt.Sprintf("%.0f / %.0f", state.Ship.CPUUsed, state.Ship.CPUCapacity), pilotColWidth),
		formatLabelValueCell("Power", fmt.Sprintf("%.0f / %.0f", state.Ship.PowerUsed, state.Ship.PowerCapacity), pilotColWidth),
		formatLabelValueCell("Fuel", fmt.Sprintf("%.0f / %.0f", state.Ship.Fuel, state.Ship.MaxFuel), pilotColWidth),
		formatLabelValueCell("Speed", fmt.Sprintf("%.0f", state.Ship.Speed), pilotColWidth),
		formatLabelValueCell("Insured", "NO", pilotColWidth),
		formatLabelValueCell("Modules", fmt.Sprintf("%d", len(state.Ship.Modules)), pilotColWidth),
	}

	moduleSlots := state.Ship.WeaponSlots + state.Ship.DefenseSlots + state.Ship.UtilitySlots
	if moduleSlots <= 0 {
		moduleSlots = max(5, len(state.Ship.Modules))
	}
	for i := 0; i < moduleSlots; i++ {
		if i < len(state.Ship.Modules) {
			modID := state.Ship.Modules[i]
			modName := modID
			if modDef, ok := state.ModuleDefinitions[modID]; ok {
				modName = modDef.Name
			}
			leftLines = append(leftLines, formatLabelValueCell(fmt.Sprintf("%d)", i+1), modName, pilotColWidth))
		} else {
			leftLines = append(leftLines, formatLabelValueCell(fmt.Sprintf("%d)", i+1), "", pilotColWidth))
		}
	}

	// Print paired rows; if right has more lines, extra rows get empty left cell.
	nRows := max(len(rightLines), len(leftLines))
	for i := 0; i < nRows; i++ {
		left := emptyLeft
		if i < len(leftLines) {
			left = leftLines[i]
		}
		right := emptyLeft
		if i < len(rightLines) && rightLines[i] != "" {
			right = rightLines[i]
		}
		fmt.Printf("│ %s │ %s │\n", left, right)
	}

	fmt.Println(sectionBaseBorder)
	fmt.Println()
}

func titleCase(s string) string {
	if len(s) == 0 {
		return s
	}
	words := strings.Fields(s)
	for i, word := range words {
		if len(word) > 0 {
			words[i] = strings.ToUpper(string(word[0])) + strings.ToLower(word[1:])
		}
	}
	return strings.Join(words, " ")
}

func percentBar(current, max float64) string {
	if max == 0 {
		return "0%"
	}
	percent := int((current / max) * 100)
	if percent > 100 {
		percent = 100
	}
	if percent < 0 {
		percent = 0
	}
	return fmt.Sprintf("%d%%", percent)
}

func main() {
	debug := flag.Bool("debug", false, "Enable debug logging")
	transport := flag.String("transport", "ws", "Transport: ws (WebSocket) or mcp (MCP HTTP)")
	flag.Parse()

	if len(flag.Args()) < 1 {
		fmt.Println("Usage: agent-status [flags] <agent-id>")
		fmt.Println("Example: agent-status miner-1")
		fmt.Println("Example: agent-status -transport=mcp fighter-1")
		fmt.Println()
		fmt.Println("Flags:")
		flag.PrintDefaults()
		fmt.Println()
		fmt.Println("Available agents:")
		entries, err := os.ReadDir("data/agents")
		if err == nil {
			for _, e := range entries {
				if e.IsDir() && e.Name() != "README.md" {
					info, _ := e.Info()
					if info != nil && info.Name()[0] != '.' {
						fmt.Printf("  - %s\n", e.Name())
					}
				}
			}
		}
		os.Exit(1)
	}

	agentID := flag.Args()[0]

	fmt.Println("(This may take 10-15 seconds...)")

	ctx := context.Background()
	gameLogger := log.New(os.Stderr, "[GAME] ", log.LstdFlags)

	var client game.GameClient
	switch *transport {
	case "mcp":
		fmt.Printf("Connecting to game as %s (MCP)...\n", agentID)
		mcpClient, _, mcpErr := game.InitializeMCPAgent(agentID, gameLogger, ctx, *debug)
		if mcpErr != nil {
			log.Fatalf("Failed to initialize MCP agent: %v", mcpErr)
		}
		client = mcpClient
	case "ws":
		agentDir := filepath.Join("data/agents", agentID)
		if _, err := os.Stat(agentDir); os.IsNotExist(err) {
			log.Fatalf("Agent directory not found: %s", agentDir)
		}
		creds, err := loadCredentials(agentDir)
		if err != nil {
			log.Fatalf("Failed to load credentials: %v", err)
		}
		fmt.Printf("Connecting to game as %s...\n", creds.Username)

		wsClient := game.NewClient(gameServerURL, creds.Username, creds.Password, gameLogger)
		wsClient.SetDebugLogging(*debug)

		handler := &StatusHandler{logger: gameLogger}
		reconnectingHandler := game.NewReconnectingHandler(wsClient, handler, ctx, gameLogger)
		wsClient.SetHandler(reconnectingHandler)

		if err := wsClient.Connect(ctx); err != nil {
			log.Fatalf("Failed to connect: %v", err)
		}

		<-wsClient.Ready()
		time.Sleep(1 * time.Second)

		if err := wsClient.Login(ctx); err != nil {
			log.Fatalf("Failed to login: %v", err)
		}

		time.Sleep(3 * time.Second)

		// Call get_ship and get_skills for detailed info (WS-specific Send)
		shipMsg := protocol.Message{Type: "get_ship"}
		if err := wsClient.Send(ctx, shipMsg); err != nil {
			gameLogger.Printf("Warning: Could not get ship details: %v", err)
		}
		time.Sleep(2 * time.Second)

		skillsMsg := protocol.Message{Type: "get_skills"}
		if err := wsClient.Send(ctx, skillsMsg); err != nil {
			gameLogger.Printf("Warning: Could not get skills: %v", err)
		}
		time.Sleep(2 * time.Second)

		client = wsClient
	default:
		log.Fatalf("Unknown transport: %s (must be: ws, mcp)", *transport)
	}
	defer func() {
		if err := client.Close(); err != nil {
			log.Printf("Warning: Failed to close client: %v", err)
		}
	}()

	// For MCP transport, fetch ship and skills via interface methods
	if *transport == "mcp" {
		time.Sleep(game.SleepQuick)
		if err := client.GetShip(ctx); err != nil {
			gameLogger.Printf("Warning: Could not get ship details: %v", err)
		}
		time.Sleep(game.SleepQuick)
		if err := client.GetSkills(ctx); err != nil {
			gameLogger.Printf("Warning: Could not get skills: %v", err)
		}
		time.Sleep(game.SleepQuick)
	}

	// Get final state
	state := client.GetState()

	// Print formatted status
	printStatus(state)

	// Disconnect gracefully
	time.Sleep(500 * time.Millisecond)
}
