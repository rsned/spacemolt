package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

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

func formatBar(current, max float64, width int) string {
	if max == 0 {
		filledBar := ""
		for i := 0; i < width; i++ {
			filledBar += "█"
		}
		return filledBar
	}

	percent := current / max
	if percent > 1 {
		percent = 1
	}
	if percent < 0 {
		percent = 0
	}

	filled := int(percent * float64(width))
	empty := width - filled

	bar := ""
	for i := 0; i < filled; i++ {
		bar += "█"
	}
	for i := 0; i < empty; i++ {
		bar += "░"
	}

	return fmt.Sprintf("%s %.1f%%", bar, percent*100)
}

func printStatus(state *game.State) {
	fmt.Println()
	fmt.Println("╔════════════════════════════════════════════════════════════════════╗")
	fmt.Println("║                           AGENT STATUS                             ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════════╝")

	// PILOT section
	fmt.Printf("┌─ PILOT ─────────────────────────┬──────────────────────────────────┐\n")
	fmt.Printf("│ Name:  %-28s │ Faction: %-24s │\n", state.Username, state.Player.FactionID)

	empireName := state.Player.Empire
	if empireName != "" {
		empireName = titleCase(empireName)
	}
	fmt.Printf("│ Empire: %28s │ Colors:  🟥🟥🟥🟥🟥 / 🟦🟦🟦🟦🟦 │\n", empireName)

	homeBase := state.Player.HomeBase
	if homeBase == "" {
		homeBase = "None"
	}
	statusMsg := state.Player.StatusMessage
	if statusMsg == "" {
		statusMsg = ""
	}
	fmt.Printf("│ Home Base: %24s │ Status: %-24s │\n", homeBase, statusMsg)
	fmt.Printf("│ Credits: %26.0f │                                  │\n", state.Credits)
	fmt.Printf("└─────────────────────────────────┴──────────────────────────────────┘\n")
	fmt.Println()

	// SKILLS section - always show if player has skills
	if len(state.Player.Skills) > 0 {
		fmt.Printf("┌─ SKILLS ────────────────────────┬──────────────────────────────────┐\n")
		skillCount := 0
		for skillID, skill := range state.Player.Skills {
			skillCount++
			xpToNext := float64((skill.Level + 1) * 100)
			if skill.Level == 0 {
				xpToNext = 100
			}
			skillName := titleCase(strings.ReplaceAll(skillID, "_", " "))
			fmt.Printf("│ %-32s │                                  │\n",
				fmt.Sprintf("%s: %5.0f / %5.0f XP", skillName, skill.XP, xpToNext))
			fmt.Printf("│ %32s │                                  │\n",
				fmt.Sprintf("Level %d", skill.Level))
			if skillCount < len(state.Player.Skills) {
				fmt.Printf("│                              │                                  │\n")
			}
		}
		fmt.Printf("└─────────────────────────────────┴──────────────────────────────────┘\n")
		fmt.Println()
	}

	// LOCATION section
	fmt.Printf("┌─ LOCATION ──────────────────────┬──────────────────────────────────┐\n")
	fmt.Printf("│ System: %26s │ POI: %-24s │\n", state.System.Name, state.CurrentPOI)

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
	fmt.Printf("│ Empire: %26s │             (%-20s) │\n", state.System.Empire, poiDisplayName)

	policePercent := 0
	if state.System.PoliceLevel > 0 {
		policePercent = 100
	}
	dockedStatus := "NO"
	if state.Doc {
		dockedStatus = "YES"
	}
	fmt.Printf("│ Police Level: %20d%% │ Docked: %-24s │\n", policePercent, dockedStatus)
	fmt.Printf("│                                │                                  │\n")
	fmt.Printf("└─────────────────────────────────┴──────────────────────────────────┘\n")
	fmt.Println()

	// SHIP section
	fmt.Printf("┌─ SHIP ──────────────────────────┬──────────────────────────────────┐\n")
	fmt.Printf("│ Name: %28s │ Cargo: %24s │\n",
		state.Ship.Name,
		fmt.Sprintf("(%s) %5.0f / %5.0f",
			percentBar(state.Ship.CargoUsed, state.Ship.CargoCapacity),
			state.Ship.CargoUsed, state.Ship.CargoCapacity))
	fmt.Printf("│                (%-20s) │                                  │\n", state.Ship.ClassID)
	fmt.Printf("│ Hull: %28s │ Items: %24d │\n",
		fmt.Sprintf("(%s) %5.0f / %5.0f",
			percentBar(state.Ship.Hull, state.Ship.MaxHull),
			state.Ship.Hull, state.Ship.MaxHull),
		len(state.Ship.Cargo))
	fmt.Printf("│ Shield: %26s │",
		fmt.Sprintf("(%s) %5.0f / %5.0f",
			percentBar(state.Ship.Shield, state.Ship.MaxShield),
			state.Ship.Shield, state.Ship.MaxShield))

	// List first 2 cargo items
	itemCount := 0
	for _, item := range state.Ship.Cargo {
		if itemCount >= 2 {
			break
		}
		if itemCount == 0 {
			fmt.Printf("   %-20s x%-5.0f │\n", item.ItemID, item.Quantity)
		} else {
			fmt.Printf("│   %-20s x%-5.0f │\n", item.ItemID, item.Quantity)
		}
		itemCount++
	}
	if len(state.Ship.Cargo) == 0 {
		fmt.Printf("                                  │\n")
	} else if itemCount == 1 {
		fmt.Printf("│                                  │\n")
	} else if len(state.Ship.Cargo) >= 2 {
		fmt.Printf("│                                  │                                  │\n")
	}

	fmt.Printf("│ Shield Recharge: %17.0f │                                  │\n", state.Ship.ShieldRecharge)
	fmt.Printf("│ Armor: %27.0f │                                  │\n", state.Ship.Armor)
	fmt.Printf("│ CPU: %28s │                                  │\n",
		fmt.Sprintf("%5.0f / %5.0f", state.Ship.CPUUsed, state.Ship.CPUCapacity))
	fmt.Printf("│ Power: %26s │                                  │\n",
		fmt.Sprintf("%5.0f / %5.0f", state.Ship.PowerUsed, state.Ship.PowerCapacity))
	fmt.Printf("│ Fuel: %27s │                                  │\n",
		fmt.Sprintf("%5.0f / %5.0f", state.Ship.Fuel, state.Ship.MaxFuel))
	fmt.Printf("│ Speed: %25.0f │                                  │\n", state.Ship.Speed)
	fmt.Printf("│ Insured: %23s │                                  │\n", "NO")
	fmt.Printf("│ Modules:                      │                                  │\n")

	// Show modules (simplified - just list them since we don't have slot type info)
	moduleCount := len(state.Ship.Modules)
	fmt.Printf("│   Total: %21d         │                                  │\n", moduleCount)

	// List up to 5 modules
	for i := 0; i < 5; i++ {
		if i < len(state.Ship.Modules) {
			fmt.Printf("│     %d) %-23s │                                  │\n", i+1, state.Ship.Modules[i])
		} else {
			fmt.Printf("│     %d) %-23s │                                  │\n", i+1, "")
		}
	}

	fmt.Printf("└─────────────────────────────────┴──────────────────────────────────┘\n")
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
	if len(os.Args) < 2 {
		fmt.Println("Usage: agent-status <agent-id>")
		fmt.Println("Example: agent-status miner-1")
		fmt.Println("Example: agent-status fighter-1")
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

	agentID := os.Args[1]
	agentDir := filepath.Join("data/agents", agentID)

	// Check if agent directory exists
	if _, err := os.Stat(agentDir); os.IsNotExist(err) {
		log.Fatalf("Agent directory not found: %s", agentDir)
	}

	// Load credentials
	creds, err := loadCredentials(agentDir)
	if err != nil {
		log.Fatalf("Failed to load credentials: %v", err)
	}

	fmt.Printf("Connecting to game as %s...\n", creds.Username)
	fmt.Println("(This may take 10-15 seconds...)")

	// Create context
	ctx := context.Background()

	// Create game client
	gameLogger := log.New(os.Stderr, "[GAME] ", log.LstdFlags)
	client := game.NewClient(gameServerURL, creds.Username, creds.Password, gameLogger)

	// Set up handler with automatic reconnection
	handler := &StatusHandler{logger: gameLogger}
	reconnectingHandler := game.NewReconnectingHandler(client, handler, ctx, gameLogger)
	client.SetHandler(reconnectingHandler)

	// Connect to game
	if err := client.Connect(ctx); err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer client.Close()

	// Wait for connection
	<-client.Ready()
	time.Sleep(1 * time.Second)

	// Login
	if err := client.Login(ctx); err != nil {
		log.Fatalf("Failed to login: %v", err)
	}

	// Wait for state to be populated after login
	time.Sleep(3 * time.Second)

	// Call get_ship to get detailed module information (if available)
	shipMsg := protocol.Message{
		Type: "get_ship",
	}
	if err := client.Send(ctx, shipMsg); err != nil {
		gameLogger.Printf("Warning: Could not get ship details: %v", err)
	}
	time.Sleep(2 * time.Second)

	// Call get_skills to get skill details (if available)
	skillsMsg := protocol.Message{
		Type: "get_skills",
	}
	if err := client.Send(ctx, skillsMsg); err != nil {
		gameLogger.Printf("Warning: Could not get skills: %v", err)
	}
	time.Sleep(2 * time.Second)

	// Get final state
	state := client.GetState()

	// Print formatted status
	printStatus(state)

	// Disconnect gracefully
	time.Sleep(500 * time.Millisecond)
}
