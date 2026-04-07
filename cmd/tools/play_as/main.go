// Command: play_as
// Usage: play_as <agent-id>
//
// Interactive game terminal for playing as an agent using MCP transport.
// Provides a shell-like prompt for sending game commands and viewing responses.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"cmp"

	"github.com/peterh/liner"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/registry"
)

// Package-level knowledge base, initialized if --db-path is provided.
var globalKB knowledge.Base

// processStartTime records when this play_as session started, used to filter
// old chat messages (show at most 1 per sender for messages before this time).
var processStartTime = time.Now()

// globalClient is set during initialization so formatters can access game state.
var globalClient game.GameClient

// Output format for server responses.
type outputFormat string

const (
	formatRaw    outputFormat = "raw"
	formatStyled outputFormat = "styled"
)

func main() {
	debug := flag.Bool("debug", false, "Enable debug logging (show sent/received JSON)")
	configPath := flag.String("config", defaultConfigPath(), "Path to config file")
	registryURL := flag.String("registry-url", "", "Status registry URL (e.g., http://localhost:8081)")
	dbPath := flag.String("db-path", "data/spacemolt-knowledge.db", "Path to SQLite knowledge base (enables update_* commands)")
	flag.Parse()

	args := flag.Args()
	if len(args) < 1 {
		printUsage()
		os.Exit(1)
	}

	agentID := args[0]
	logger := log.New(os.Stdout, fmt.Sprintf("[PLAY_AS-%s] ", agentID), log.LstdFlags)

	ctx := context.Background()

	// Initialize MCP agent (with polling disabled for interactive use)
	logger.Printf("Initializing agent %s...", agentID)
	client, creds, err := game.InitializeMCPAgent(agentID, logger, ctx, *debug, true) // disablePolling=true
	if err != nil {
		log.Fatalf("Failed to initialize agent: %v", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			logger.Printf("Error closing client: %v", err)
		}
	}()

	logger.Printf("Connected as: %s (Empire: %s)", creds.Username, creds.Empire)
	globalClient = client

	// Register with status registry if configured
	if *registryURL != "" {
		toolID := fmt.Sprintf("play-as-%s", agentID)
		regClient := registry.NewClient(*registryURL, toolID)

		reg := registry.ToolRegistration{
			ToolID:    toolID,
			ToolType:  registry.ToolTypePlayAs,
			PID:       os.Getpid(),
			AgentID:   agentID,
			AgentName: creds.Username,
			AgentRole: "Interactive",
			Status:    "active",
			Capabilities: map[string]any{
				"interactive": true,
			},
			Metadata: map[string]any{
				"empire": creds.Empire,
			},
		}

		if err := regClient.Register(reg); err != nil {
			logger.Printf("⚠ Warning: Failed to register with status registry: %v", err)
		} else {
			logger.Printf("✓ Registered with status registry")
			regClient.StartHeartbeat(ctx, 5*time.Second, func() (status, action string) {
				state := client.GetState()
				if state == nil {
					return "active", "Interactive session"
				}
				return "active", fmt.Sprintf("In %s (%.0f credits)", state.System.Name, state.Credits)
			})
			defer func() {
				if err := regClient.Deregister(); err != nil {
					logger.Printf("Warning: Failed to deregister: %v", err)
				}
			}()
		}
	}

	// Initialize knowledge base for update_* commands.
	if *dbPath != "" {
		sqliteKB, err := knowledge.NewSQLiteKB(knowledge.Config{DBPath: *dbPath})
		if err != nil {
			logger.Printf("Warning: Failed to open knowledge base at %s: %v", *dbPath, err)
			logger.Printf("  update_* commands will be unavailable")
		} else {
			globalKB = sqliteKB
			logger.Printf("Knowledge base loaded: %s", *dbPath)
			defer func() { _ = sqliteKB.Close() }()
		}
	}

	// Cache ship and system data on startup for travel estimation and statusline.
	_ = client.GetShip(ctx)
	_ = client.GetSystem(ctx)

	// Show initial status
	fmt.Println("\n╔════════════════════════════════════════════════════════════════════╗")
	fmt.Println("║                    SPACE MOLT GAME TERMINAL                        ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════════╝")
	fmt.Printf("\nLogged in as: %s\n", creds.Username)
	fmt.Printf("Empire: %s\n", creds.Empire)
	fmt.Println("\nType 'help' for available commands, 'exit' or 'quit' to leave.")

	// Load config
	cfg := loadConfig(*configPath)

	// Set client for lazy ship catalog lookup (used by browse_ships styled output).
	setShipCatalogClient(client)

	// Run REPL loop
	runREPL(client, ctx, cfg, agentID, creds.Username)
}

func printUsage() {
	fmt.Println("Usage: play_as [flags] <agent-id>")
	fmt.Println("Example: play_as explorer-1")
	fmt.Println("  play_as --debug explorer-1")
	fmt.Println("\nFlags:")
	fmt.Println("  --debug                Enable debug logging (show sent/received JSON)")
	fmt.Println("  --config <path>        Path to config file (default: ~/.config/spacemolt/play_as.yaml)")
	fmt.Println("  --registry-url <url>   Status registry URL (e.g., http://localhost:8081)")
	fmt.Println("\nThis tool provides an interactive terminal for playing Spacemolt.")
	fmt.Println("All commands are case-insensitive. Use 'help' to see available commands.")
}

const maxHistoryLines = 25

func runREPL(client game.GameClient, ctx context.Context, cfg PlayAsConfig, agentID, username string) {
	line := liner.NewLiner()
	defer func() { _ = line.Close() }()

	line.SetCtrlCAborts(true)

	// Load persistent command history from agent directory.
	historyPath := filepath.Join("data", "agents", agentID, "play_as_history.txt")
	if f, err := os.Open(historyPath); err == nil {
		_, _ = line.ReadHistory(f)
		_ = f.Close()
	}
	saveHistory := func() {
		if f, err := os.Create(historyPath); err == nil {
			_, _ = line.WriteHistory(f)
			_ = f.Close()
		}
	}
	defer saveHistory()

	// Start background chat poller to display incoming messages.
	poller := newChatPoller(client, ctx, username)
	poller.start()
	defer poller.stop()

	format := outputFormat(cfg.OutputFormat)

	for {
		// Read input with history support (up/down arrows)
		input, err := line.Prompt("$ ")
		if err != nil {
			if err == liner.ErrPromptAborted {
				fmt.Println("Goodbye!")
				return
			}
			fmt.Printf("Error reading input: %v\n", err)
			continue
		}

		// Trim whitespace
		cmd := strings.TrimSpace(input)
		if cmd == "" {
			continue
		}

		// Add to history for up/down arrow cycling and persist
		line.AppendHistory(cmd)
		saveHistory()

		// Parse command (supports quoted strings)
		parts := splitArgs(cmd)
		if len(parts) == 0 {
			continue
		}

		command := strings.ToLower(parts[0])

		// Handle exit/quit
		if command == "exit" || command == "quit" {
			fmt.Println("Goodbye!")
			return
		}

		// Handle help
		if command == "help" {
			printHelp()
			continue
		}

		// Handle history
		if command == "history" {
			// Read history file and show last N entries
			data, err := os.ReadFile(historyPath)
			if err != nil {
				fmt.Println("No command history yet.")
			} else {
				lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
				start := 0
				if len(lines) > maxHistoryLines {
					start = len(lines) - maxHistoryLines
				}
				for i, l := range lines[start:] {
					fmt.Printf("  %3d  %s\n", start+i+1, l)
				}
			}
			fmt.Println()
			continue
		}

		// Handle set_format
		if command == "set_format" {
			if len(parts) < 2 {
				fmt.Printf("Current format: %s\n", format)
				fmt.Println("Usage: set_format <raw|json|styled>")
				continue
			}
			switch strings.ToLower(parts[1]) {
			case "raw", "json":
				format = outputFormat(strings.ToLower(parts[1]))
				fmt.Printf("Output format set to: %s\n", format)
			case "styled":
				format = formatStyled
				fmt.Printf("Output format set to: styled\n")
			default:
				fmt.Printf("Unknown format %q. Use: raw, json, or styled\n", parts[1])
			}
			fmt.Println()
			continue
		}

		// Handle loop meta-command: loop [-f] <count> <command...>
		if command == "loop" {
			if len(parts) < 3 {
				fmt.Println("Usage: loop [-f] <count> <command...>")
				fmt.Println("  -f  Force: continue on errors instead of stopping")
				fmt.Println("Example: loop 5 mine")
				fmt.Println("         loop -f 20 mine")
				fmt.Println("         loop 10 sell iron_ore 5")
				fmt.Println()
				continue
			}
			// Check for -f flag
			forceLoop := false
			argIdx := 1
			if parts[argIdx] == "-f" {
				forceLoop = true
				argIdx++
				if argIdx >= len(parts)-1 {
					fmt.Println("Usage: loop [-f] <count> <command...>")
					fmt.Println()
					continue
				}
			}
			count, err := strconv.Atoi(parts[argIdx])
			if err != nil || count < 1 {
				fmt.Printf("❌ Invalid count: %s (must be a positive integer)\n\n", parts[argIdx])
				continue
			}
			loopParts := parts[argIdx+1:]
			loopCmd := strings.Join(loopParts, " ")
			if forceLoop {
				fmt.Printf("🔁 Repeating %q %d time(s) (force mode)...\n", loopCmd, count)
			} else {
				fmt.Printf("🔁 Repeating %q %d time(s)...\n", loopCmd, count)
			}
			errors := 0
			for i := range count {
				fmt.Printf("── [%d/%d] %s\n", i+1, count, loopCmd)
				startTime := time.Now()
				if err := executeCommand(client, ctx, loopParts, format); err != nil {
					errors++
					fmt.Printf("❌ %s\n", formatError(err, loopParts[0], format))
					if !forceLoop {
						fmt.Printf("Stopping loop after %d/%d iterations\n", i+1, count)
						break
					}
					fmt.Printf("⚠️  Error %d (continuing due to -f)...\n", errors)
					continue
				}
				duration := time.Since(startTime)
				fmt.Printf("✓ [%d/%d] Completed in %v\n", i+1, count, duration)
			}
			if forceLoop && errors > 0 {
				fmt.Printf("🔁 Loop finished with %d error(s) out of %d iterations\n", errors, count)
			}
			// Render statusline after loop
			if sl := renderStatusline(client, cfg, agentID); sl != "" {
				fmt.Println(sl)
			}
			fmt.Println()
			continue
		}

		// Execute command
		startTime := time.Now()
		if err := executeCommand(client, ctx, parts, format); err != nil {
			fmt.Printf("❌ %s\n", formatError(err, command, format))
		} else {
			duration := time.Since(startTime)
			fmt.Printf("✓ Completed in %v\n", duration)
		}

		// Render statusline before next prompt
		if sl := renderStatusline(client, cfg, agentID); sl != "" {
			fmt.Println(sl)
		}

		fmt.Println()
	}
}

// printResponse formats and prints the server response based on the current format.
func printResponse(raw []byte, format outputFormat, command string) {
	if format == formatRaw {
		fmt.Printf("\n%s\n", string(raw))
		return
	}

	// For styled format, try command-specific formatters first
	if format == formatStyled {
		if styled := formatStyledResponse(raw, command); styled != "" {
			fmt.Printf("\n%s\n", styled)
			return
		}
		// Fall through to JSON if no styled formatter exists
	}

	// Default: pretty-printed JSON
	var pretty any
	if err := json.Unmarshal(raw, &pretty); err == nil {
		formatted, _ := json.MarshalIndent(pretty, "", "  ")
		fmt.Printf("\n%s\n", string(formatted))
	} else {
		fmt.Printf("\n%s\n", string(raw))
	}
}

// formatStyledResponse returns a styled string for the given command, or "" if no formatter exists.
func formatStyledResponse(raw []byte, command string) string {
	switch command {
	case "storage", "view_storage":
		return formatStorage(raw)
	case "cargo", "get_cargo":
		return formatCargo(raw)
	case "browse_ships":
		return formatBrowseShips(raw)
	case "nearby", "get_nearby":
		return formatNearby(raw)
	case "travel":
		return formatTravel(raw)
	case "mine":
		return formatMine(raw)
	case "jump":
		return formatJump(raw)
	case "dock":
		return formatDock(raw)
	case "wrecks", "get_wrecks":
		return formatWrecks(raw)
	case "loot", "loot_wreck":
		return formatLootWreck(raw)
	case "jettison":
		return formatJettison(raw)
	case "refuel":
		return formatRefuel(raw)
	case "undock":
		return "Undocked"
	case "system", "get_system":
		return formatSystem(raw)
	case "withdraw", "withdraw_items":
		return formatWithdraw(raw)
	case "create_faction":
		return formatCreateFaction(raw)
	case "faction_info":
		return formatFactionInfo(raw)
	case "deposit", "deposit_items":
		return formatDeposit(raw)
	case "skills", "get_skills":
		return formatSkills(raw)
	case "view_market":
		return formatMarket(raw)
	case "chat_history", "get_chat_history":
		return formatChatHistory(raw)
	default:
		return ""
	}
}

// storageItem is a parsed item from a view_storage response.
type storageItem struct {
	ItemID   string  `json:"item_id"`
	Name     string  `json:"name"`
	Quantity float64 `json:"quantity"`
	Size     int     `json:"size"`
}

// storageShip is a parsed ship from a view_storage response.
type storageShip struct {
	ShipID    string `json:"ship_id"`
	ClassID   string `json:"class_id"`
	ClassName string `json:"class_name,omitempty"`
	CargoUsed int    `json:"cargo_used"`
	Modules   int    `json:"modules"`
}

// formatStorage formats a view_storage response as sorted tables.
func formatStorage(raw []byte) string {
	var resp struct {
		BaseID  string        `json:"base_id"`
		Credits int           `json:"credits"`
		Items   []storageItem `json:"items"`
		Ships   []storageShip `json:"ships"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Storage at %s (credits: %d)\n", resp.BaseID, resp.Credits)

	// Items table
	if len(resp.Items) == 0 {
		b.WriteString("  (no items)\n")
	} else {
		slices.SortFunc(resp.Items, func(a, b storageItem) int {
			return strings.Compare(a.ItemID, b.ItemID)
		})

		idW, nameW, qtyW, sizeW := len("ID"), len("Name"), len("Qty"), len("Unit Size")
		for _, item := range resp.Items {
			idW = max(idW, len(item.ItemID))
			nameW = max(nameW, len(item.Name))
			qtyW = max(qtyW, len(formatFloat(item.Quantity)))
			sizeW = max(sizeW, len(strconv.Itoa(item.Size)))
		}

		fmt.Fprintf(&b, "  %-*s | %-*s | %*s | %*s\n", idW, "ID", nameW, "Name", qtyW, "Qty", sizeW, "Unit Size")
		fmt.Fprintf(&b, "  %s-+-%s-+-%s-+-%s\n",
			strings.Repeat("-", idW), strings.Repeat("-", nameW),
			strings.Repeat("-", qtyW), strings.Repeat("-", sizeW))

		for _, item := range resp.Items {
			fmt.Fprintf(&b, "  %-*s | %-*s | %*s | %*d\n",
				idW, item.ItemID, nameW, item.Name,
				qtyW, formatFloat(item.Quantity), sizeW, item.Size)
		}
		fmt.Fprintf(&b, "  (%d items)\n", len(resp.Items))
	}

	// Ships table
	if len(resp.Ships) > 0 {
		b.WriteString("\n  SHIPS\n")

		slices.SortFunc(resp.Ships, func(a, b storageShip) int {
			return strings.Compare(a.ShipID, b.ShipID)
		})

		idW, nameW, classW, cargoW, modW := len("ID"), len("Ship Name"), len("Class"), len("Cargo Used"), len("Modules")
		for _, ship := range resp.Ships {
			idW = max(idW, len(ship.ShipID))
			nameW = max(nameW, len(ship.ClassName))
			classW = max(classW, len(ship.ClassID))
			cargoW = max(cargoW, len(strconv.Itoa(ship.CargoUsed)))
			modW = max(modW, len(strconv.Itoa(ship.Modules)))
		}

		fmt.Fprintf(&b, "  %-*s | %-*s | %-*s | %*s | %*s\n",
			idW, "ID", nameW, "Ship Name", classW, "Class", cargoW, "Cargo Used", modW, "Modules")
		fmt.Fprintf(&b, "  %s-+-%s-+-%s-+-%s-+-%s\n",
			strings.Repeat("-", idW), strings.Repeat("-", nameW),
			strings.Repeat("-", classW), strings.Repeat("-", cargoW),
			strings.Repeat("-", modW))

		for _, ship := range resp.Ships {
			fmt.Fprintf(&b, "  %-*s | %-*s | %-*s | %*d | %*d\n",
				idW, ship.ShipID, nameW, ship.ClassName,
				classW, ship.ClassID, cargoW, ship.CargoUsed,
				modW, ship.Modules)
		}
		fmt.Fprintf(&b, "  (%d ships)\n", len(resp.Ships))
	}

	return b.String()
}

// formatBuyOrders formats up to 2 buy orders with station prefix.
// Returns slice of formatted strings (price with prefix, quantity).
func formatBuyOrders(orders []struct {
	PriceEach float64 `json:"price_each"`
	Quantity  float64 `json:"quantity"`
	Source    string  `json:"source,omitempty"`
}) []struct {
	price string
	qty   string
} {
	// Sort by price ascending (lowest first)
	sorted := make([]struct {
		PriceEach float64 `json:"price_each"`
		Quantity  float64 `json:"quantity"`
		Source    string  `json:"source,omitempty"`
	}, len(orders))
	copy(sorted, orders)

	slices.SortFunc(sorted, func(a, b struct {
		PriceEach float64 `json:"price_each"`
		Quantity  float64 `json:"quantity"`
		Source    string  `json:"source,omitempty"`
	}) int {
		return cmp.Compare(a.PriceEach, b.PriceEach)
	})

	// Take top 2
	result := make([]struct {
		price string
		qty   string
	}, 0, 2)
	for i := 0; i < len(sorted) && i < 2; i++ {
		prefix := ""
		if sorted[i].Source == "station" {
			prefix = "* "
		}
		result = append(result, struct {
			price string
			qty   string
		}{
			price: prefix + fmt.Sprintf("%.0f", sorted[i].PriceEach),
			qty:   fmt.Sprintf("%.0f", sorted[i].Quantity),
		})
	}

	return result
}

// formatSellOrders formats up to 2 sell orders with station prefix.
// Returns slice of formatted strings (price with prefix, quantity).
func formatSellOrders(orders []struct {
	PriceEach float64 `json:"price_each"`
	Quantity  float64 `json:"quantity"`
	Source    string  `json:"source,omitempty"`
}) []struct {
	price string
	qty   string
} {
	// Sort by price descending (highest first)
	sorted := make([]struct {
		PriceEach float64 `json:"price_each"`
		Quantity  float64 `json:"quantity"`
		Source    string  `json:"source,omitempty"`
	}, len(orders))
	copy(sorted, orders)

	slices.SortFunc(sorted, func(a, b struct {
		PriceEach float64 `json:"price_each"`
		Quantity  float64 `json:"quantity"`
		Source    string  `json:"source,omitempty"`
	}) int {
		return cmp.Compare(b.PriceEach, a.PriceEach) // Descending
	})

	// Take top 2
	result := make([]struct {
		price string
		qty   string
	}, 0, 2)
	for i := 0; i < len(sorted) && i < 2; i++ {
		prefix := ""
		if sorted[i].Source == "station" {
			prefix = "* "
		}
		result = append(result, struct {
			price string
			qty   string
		}{
			price: prefix + fmt.Sprintf("%.0f", sorted[i].PriceEach),
			qty:   fmt.Sprintf("%.0f", sorted[i].Quantity),
		})
	}

	return result
}

// formatMarket formats a view_market response as a multi-row table grouped by category.
func formatChatHistory(raw []byte) string {
	type chatMsg struct {
		Channel      string `json:"channel"`
		Sender       string `json:"sender"`
		SenderID     string `json:"sender_id"`
		Content      string `json:"content"`
		TimestampUTC string `json:"timestamp_utc"`
		Timestamp    string `json:"timestamp"`
		TargetID     string `json:"target_id,omitempty"`
	}
	var resp struct {
		Messages []chatMsg `json:"messages"`
		Channel  string    `json:"channel"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil || len(resp.Messages) == 0 {
		return ""
	}

	// Get current system ID for filtering system chat messages.
	currentSystemID := ""
	if globalClient != nil {
		if state := globalClient.GetState(); state != nil {
			currentSystemID = state.System.ID
		}
	}

	// Phase 1: Filter messages.
	// - System chat: only show messages targeting the current system.
	// - Old messages (before session start): keep only the most recent per sender.
	//   We do a reverse pass to find which old message per sender to keep.
	keepOldSender := make(map[string]int) // sender -> index of most recent old msg to keep
	for i := len(resp.Messages) - 1; i >= 0; i-- {
		msg := resp.Messages[i]
		msgTime, err := time.Parse(time.RFC3339Nano, msg.TimestampUTC)
		isOld := err == nil && msgTime.Before(processStartTime)
		if isOld {
			key := msg.SenderID
			if key == "" {
				key = msg.Sender
			}
			if _, seen := keepOldSender[key]; !seen {
				keepOldSender[key] = i
			}
		}
	}

	var filtered []chatMsg
	skipped := 0
	for i, msg := range resp.Messages {
		// Filter system chat by target system.
		if resp.Channel == "system" && msg.TargetID != "" && currentSystemID != "" {
			if !strings.EqualFold(msg.TargetID, currentSystemID) {
				skipped++
				continue
			}
		}

		msgTime, err := time.Parse(time.RFC3339Nano, msg.TimestampUTC)
		isOld := err == nil && msgTime.Before(processStartTime)

		if isOld {
			key := msg.SenderID
			if key == "" {
				key = msg.Sender
			}
			if keepIdx, ok := keepOldSender[key]; ok && keepIdx != i {
				skipped++
				continue
			}
		}
		filtered = append(filtered, msg)
	}

	// Phase 2: Collapse consecutive duplicate messages (same sender + content).
	type entry struct {
		sender    string
		content   string
		timestamp string
		count     int
	}
	var collapsed []entry
	for _, msg := range filtered {
		ts := msg.Timestamp
		if ts == "" {
			ts = msg.TimestampUTC
		}
		if len(collapsed) > 0 {
			last := &collapsed[len(collapsed)-1]
			if last.sender == msg.Sender && last.content == msg.Content {
				last.count++
				continue
			}
		}
		collapsed = append(collapsed, entry{
			sender:    msg.Sender,
			content:   msg.Content,
			timestamp: ts,
			count:     1,
		})
	}

	var b strings.Builder
	fmt.Fprintf(&b, "\nChat history (%s) — %d messages", resp.Channel, len(resp.Messages))
	if skipped > 0 {
		fmt.Fprintf(&b, " (%d old duplicates hidden)", skipped)
	}
	b.WriteString(":\n\n")

	// Debug: known senders to dump full JSON for investigation.
	debugSenders := map[string]bool{
		"Chrisjen Avasarala": true,
		"WaterFixer":         true,
		"N Nagata":           true,
		"GunnyDraper":        true,
	}

	for _, e := range collapsed {
		repeat := ""
		if e.count > 1 {
			repeat = fmt.Sprintf(" (x%d)", e.count)
		}
		fmt.Fprintf(&b, "  [%s] %s: %s%s\n", e.timestamp, e.sender, e.content, repeat)
	}

	// Dump full JSON for debug senders — show ALL messages (including skipped)
	// to understand what fields are available for filtering.
	for _, msg := range resp.Messages {
		if debugSenders[msg.Sender] {
			raw, _ := json.MarshalIndent(msg, "    ", "  ")
			fmt.Fprintf(&b, "\n  DEBUG [%s]:\n    %s\n", msg.Sender, string(raw))
			break // Just show one example per run
		}
	}

	fmt.Fprintf(&b, "\n  %d shown (%d after dedup)\n", len(filtered), len(collapsed))
	return b.String()
}

func formatMarket(raw []byte) string {
	type MarketItem struct {
		ItemID    string `json:"item_id"`
		ItemName  string `json:"item_name"`
		Category  string `json:"category,omitempty"`
		BuyOrders []struct {
			PriceEach float64 `json:"price_each"`
			Quantity  float64 `json:"quantity"`
			Source    string `json:"source,omitempty"`
		} `json:"buy_orders"`
		SellOrders []struct {
			PriceEach float64 `json:"price_each"`
			Quantity  float64 `json:"quantity"`
			Source    string `json:"source,omitempty"`
		} `json:"sell_orders"`
	}

	var resp struct {
		Items []MarketItem `json:"items"`
	}

	if err := json.Unmarshal(raw, &resp); err != nil {
		return fmt.Sprintf("Error parsing market data: %v", err)
	}

	if len(resp.Items) == 0 {
		return "No market data available"
	}

	var buf bytes.Buffer

	// Group items by category
	categories := make(map[string][]MarketItem)
	categoryOrder := make([]string, 0, len(resp.Items))

	for _, item := range resp.Items {
		cat := item.Category
		if cat == "" {
			cat = "Uncategorized"
		}
		if _, exists := categories[cat]; !exists {
			categoryOrder = append(categoryOrder, cat)
		}
		categories[cat] = append(categories[cat], item)
	}

	// Sort categories alphabetically, but ensure "Uncategorized" comes first
	slices.SortFunc(categoryOrder, func(a, b string) int {
		if a == "Uncategorized" && b != "Uncategorized" {
			return -1
		}
		if a != "Uncategorized" && b == "Uncategorized" {
			return 1
		}
		return cmp.Compare(a, b)
	})

	// Sort items within each category by ItemID
	for cat := range categories {
		slices.SortFunc(categories[cat], func(a, b MarketItem) int {
			return cmp.Compare(a.ItemID, b.ItemID)
		})
	}

	// Calculate max widths for Name and ID columns across all items
	maxNameWidth := len("Name") // Header is minimum
	maxIDWidth := len("ID")     // Header is minimum
	for _, item := range resp.Items {
		if len(item.ItemName) > maxNameWidth {
			maxNameWidth = len(item.ItemName)
		}
		if len(item.ItemID) > maxIDWidth {
			maxIDWidth = len(item.ItemID)
		}
	}

	// Use a single tabwriter for all sections to ensure consistent column widths
	w := tabwriter.NewWriter(&buf, 0, 0, 1, ' ', 0)

	// Print each category section
	for idx, cat := range categoryOrder {
		items := categories[cat]

		// Add blank line before category (except first)
		if idx > 0 {
			_, _ = fmt.Fprintln(w)
		}

		// Category heading - write directly with padding to match max widths
		// We need to pad the category name to align with the table structure
		fmt.Fprintf(&buf, "%s\n", cat)
		fmt.Fprintf(&buf, "%s\n", strings.Repeat("-", len(cat)))

		// Pad Name header to max width
		nameHeader := "Name"
		for len(nameHeader) < maxNameWidth {
			nameHeader += " "
		}
		
		// Pad ID header to max width
		idHeader := "ID"
		for len(idHeader) < maxIDWidth {
			idHeader += " "
		}

		// Header row (numeric columns right-aligned with leading tabs)
		_, _ = fmt.Fprintf(w, "%s\t| %s\t|\tBuy\t|\tQty\t|\tSell\t|\tQty\t|\n",
			nameHeader, idHeader)
		
		// Separator row
		nameSep := strings.Repeat("-", maxNameWidth)
		idSep := strings.Repeat("-", maxIDWidth)
		_, _ = fmt.Fprintf(w, "%s\t| %s\t|\t-----\t|\t---\t|\t-----\t|\t---\t|\n",
			nameSep, idSep)

		for _, item := range items {
			buys := formatBuyOrders(item.BuyOrders)
			sells := formatSellOrders(item.SellOrders)

			// Row 1: Best buy and sell
			buyPrice1, buyQty1 := "-", "-"
			if len(buys) > 0 {
				buyPrice1 = buys[0].price
				buyQty1 = buys[0].qty
			}

			sellPrice1, sellQty1 := "-", "-"
			if len(sells) > 0 {
				sellPrice1 = sells[0].price
				sellQty1 = sells[0].qty
			}

			// Pad Name and ID to max widths
			name := item.ItemName
			for len(name) < maxNameWidth {
				name += " "
			}
			id := item.ItemID
			for len(id) < maxIDWidth {
				id += " "
			}

			_, _ = fmt.Fprintf(w, "%s\t| %s\t|\t%s\t|\t%s\t|\t%s\t|\t%s\t|\n",
				name, id,
				buyPrice1, buyQty1,
				sellPrice1, sellQty1,
			)

			// Row 2: Second best buy and sell (if exists)
			if len(buys) > 1 || len(sells) > 1 {
				buyPrice2, buyQty2 := "-", "-"
				if len(buys) > 1 {
					buyPrice2 = buys[1].price
					buyQty2 = buys[1].qty
				}

				sellPrice2, sellQty2 := "-", "-"
				if len(sells) > 1 {
					sellPrice2 = sells[1].price
					sellQty2 = sells[1].qty
				}

				// Empty Name and ID for second row (still padded)
				emptyName := ""
				emptyID := ""
				for len(emptyName) < maxNameWidth {
					emptyName += " "
				}
				for len(emptyID) < maxIDWidth {
					emptyID += " "
				}
				
				_, _ = fmt.Fprintf(w, "%s\t| %s\t|\t%s\t|\t%s\t|\t%s\t|\t%s\t|\n",
					emptyName, emptyID,
					buyPrice2, buyQty2,
					sellPrice2, sellQty2,
				)
			}
		}
	}

	_ = w.Flush()
	return buf.String()
}

// formatCargo formats a get_cargo response as a sorted table.
func formatCargo(raw []byte) string {
	var resp struct {
		Cargo     []storageItem `json:"cargo"`
		Used      int           `json:"used"`
		Capacity  int           `json:"capacity"`
		Available int           `json:"available"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Cargo (%d/%d used, %d available)\n", resp.Used, resp.Capacity, resp.Available)

	if len(resp.Cargo) == 0 {
		b.WriteString("  (empty)\n")
		return b.String()
	}

	slices.SortFunc(resp.Cargo, func(a, b storageItem) int {
		return strings.Compare(a.ItemID, b.ItemID)
	})

	idW, nameW, qtyW, sizeW := len("ID"), len("Name"), len("Qty"), len("Unit Size")
	for _, item := range resp.Cargo {
		idW = max(idW, len(item.ItemID))
		nameW = max(nameW, len(item.Name))
		qtyW = max(qtyW, len(formatFloat(item.Quantity)))
		sizeW = max(sizeW, len(strconv.Itoa(item.Size)))
	}

	fmt.Fprintf(&b, "  %-*s | %-*s | %*s | %*s\n", idW, "ID", nameW, "Name", qtyW, "Qty", sizeW, "Unit Size")
	fmt.Fprintf(&b, "  %s-+-%s-+-%s-+-%s\n",
		strings.Repeat("-", idW), strings.Repeat("-", nameW),
		strings.Repeat("-", qtyW), strings.Repeat("-", sizeW))

	for _, item := range resp.Cargo {
		fmt.Fprintf(&b, "  %-*s | %-*s | %*s | %*d\n",
			idW, item.ItemID, nameW, item.Name,
			qtyW, formatFloat(item.Quantity), sizeW, item.Size)
	}
	fmt.Fprintf(&b, "  (%d items)\n", len(resp.Cargo))
	return b.String()
}

// shipListing is a parsed listing from a browse_ships response.
type shipListing struct {
	ShipName  string  `json:"ship_name"`
	ClassID   string  `json:"class_id"`
	Category  string  `json:"category"`
	Price     float64 `json:"price"`
	Seller    string  `json:"seller"`
	ListingID string  `json:"listing_id"`
}

// shipCatalogEntry holds class and tier info from the ship catalog.
type shipCatalogEntry struct {
	Class string `json:"class"`
	Tier  int    `json:"tier"`
}

// shipCatalogCache is lazily populated from the ship catalog on first browse_ships.
var (
	shipCatalogCache map[string]shipCatalogEntry // keyed by ship class ID
	shipCatalogOnce  sync.Once
	shipCatalogClient game.GameClient // set before first use
)

// setShipCatalogClient stores the client reference for lazy catalog loading.
func setShipCatalogClient(c game.GameClient) {
	shipCatalogClient = c
}

// loadShipCatalog fetches the ship catalog and builds a lookup map.
func loadShipCatalog() {
	shipCatalogCache = make(map[string]shipCatalogEntry)
	if shipCatalogClient == nil {
		return
	}
	ctx := context.Background()
	if err := shipCatalogClient.Catalog(ctx, "ships", 1, 100); err != nil {
		return
	}
	raw := shipCatalogClient.GetRawJSON("_last")
	if raw == nil {
		raw = shipCatalogClient.GetRawJSON("catalog")
	}
	if raw == nil {
		return
	}
	var resp struct {
		Items []struct {
			ID    string `json:"id"`
			Class string `json:"class"`
			Tier  int    `json:"tier"`
		} `json:"items"`
		TotalPages int `json:"total_pages"`
	}
	if json.Unmarshal(raw, &resp) != nil {
		return
	}
	for _, item := range resp.Items {
		shipCatalogCache[item.ID] = shipCatalogEntry{Class: item.Class, Tier: item.Tier}
	}
	// Fetch remaining pages if needed
	for page := 2; page <= resp.TotalPages; page++ {
		if err := shipCatalogClient.Catalog(ctx, "ships", page, 100); err != nil {
			break
		}
		pageRaw := shipCatalogClient.GetRawJSON("_last")
		if pageRaw == nil {
			pageRaw = shipCatalogClient.GetRawJSON("catalog")
		}
		if pageRaw == nil {
			break
		}
		var pageResp struct {
			Items []struct {
				ID    string `json:"id"`
				Class string `json:"class"`
				Tier  int    `json:"tier"`
			} `json:"items"`
		}
		if json.Unmarshal(pageRaw, &pageResp) != nil {
			break
		}
		for _, item := range pageResp.Items {
			shipCatalogCache[item.ID] = shipCatalogEntry{Class: item.Class, Tier: item.Tier}
		}
	}
}

// formatBrowseShips formats a browse_ships response as a table.
func formatBrowseShips(raw []byte) string {
	var resp struct {
		BaseName string        `json:"base_name"`
		Count    int           `json:"count"`
		Listings []shipListing `json:"listings"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Station:  %q\n\nListings:\n", resp.BaseName)

	if len(resp.Listings) == 0 {
		b.WriteString("  (no ships for sale)\n")
		return b.String()
	}

	// Lazily load ship catalog for class/tier lookup.
	shipCatalogOnce.Do(loadShipCatalog)

	slices.SortFunc(resp.Listings, func(a, c shipListing) int {
		if a.Price != c.Price {
			if a.Price < c.Price {
				return -1
			}
			return 1
		}
		return strings.Compare(strings.ToLower(a.ShipName), strings.ToLower(c.ShipName))
	})

	// Build display rows with catalog data.
	type row struct {
		shipListing
		class   string
		tierStr string
	}
	rows := make([]row, len(resp.Listings))
	for i, l := range resp.Listings {
		r := row{shipListing: l}
		if entry, ok := shipCatalogCache[l.ClassID]; ok {
			r.class = entry.Class
			r.tierStr = fmt.Sprintf("T%d", entry.Tier)
		}
		rows[i] = r
	}

	shipW, catW, classW, tierW := len("Ship"), len("Category"), len("Class"), len("Tier")
	priceW, sellerW, idW := len("Price"), len("Seller"), len("Listing ID")
	for _, r := range rows {
		shipW = max(shipW, len(r.ShipName))
		catW = max(catW, len(r.Category))
		classW = max(classW, len(r.class))
		tierW = max(tierW, len(r.tierStr))
		priceW = max(priceW, len(formatCredits(r.Price)))
		sellerW = max(sellerW, len(r.Seller))
		idW = max(idW, len(r.ListingID))
	}

	fmt.Fprintf(&b, "%-*s | %-*s | %-*s | %-*s | %*s | %-*s | %-*s\n",
		shipW, "Ship", catW, "Category", classW, "Class", tierW, "Tier",
		priceW, "Price", sellerW, "Seller", idW, "Listing ID")
	b.WriteString(strings.Repeat("-", shipW+catW+classW+tierW+priceW+sellerW+idW+18) + "\n")

	for _, r := range rows {
		fmt.Fprintf(&b, "%-*s | %-*s | %-*s | %-*s | %*s | %-*s | %-*s\n",
			shipW, r.ShipName, catW, r.Category, classW, r.class, tierW, r.tierStr,
			priceW, formatCredits(r.Price), sellerW, r.Seller, idW, r.ListingID)
	}

	return b.String()
}

// nearbyPlayer is a parsed player from a get_nearby response.
type nearbyPlayer struct {
	Username       string `json:"username"`
	FactionTag     string `json:"faction_tag,omitempty"`
	ShipClass      string `json:"ship_class"`
	InCombat       bool   `json:"in_combat"`
	PrimaryColor   string `json:"primary_color,omitempty"`
	SecondaryColor string `json:"secondary_color,omitempty"`
}

// nearbyPirate is a parsed pirate from a get_nearby response.
type nearbyPirate struct {
	Name string `json:"name"`
	Tier string `json:"tier,omitempty"`
}

// formatNearby formats a get_nearby response as a table.
func formatNearby(raw []byte) string {
	var resp struct {
		POIID       string         `json:"poi_id"`
		Count       int            `json:"count"`
		PirateCount int            `json:"pirate_count"`
		Nearby      []nearbyPlayer `json:"nearby"`
		Pirates     []nearbyPirate `json:"pirates"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "POI ID:  %s\n", resp.POIID)
	fmt.Fprintf(&b, "Count: %d\n\n", resp.Count)

	writePlayerTable(&b, resp.Nearby)

	fmt.Fprintf(&b, "\nPirates:  %d\n", resp.PirateCount)
	for _, p := range resp.Pirates {
		tier := p.Tier
		if tier == "" {
			tier = "normal"
		}
		fmt.Fprintf(&b, "  %s (%s)\n", p.Name, tier)
	}

	return b.String()
}

// formatTravel formats a travel response with online players at the destination.
func formatTravel(raw []byte) string {
	var resp struct {
		POI           string         `json:"poi"`
		POIID         string         `json:"poi_id"`
		ArrivalTick   int64          `json:"arrival_tick"`
		OnlinePlayers []nearbyPlayer `json:"online_players"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}

	var b strings.Builder
	poi := resp.POI
	if poi == "" {
		poi = resp.POIID
	}
	fmt.Fprintf(&b, "Arrived at %q\n\n", poi)

	writePlayerTable(&b, resp.OnlinePlayers)

	return b.String()
}

// styledErrors maps (command, error substring) pairs to friendly messages.
var styledErrors = map[[2]string]string{
	{"mine", "depleted"}: "Ore depleted.",
}

// formatError returns a friendly error message in styled mode, or the raw error otherwise.
func formatError(err error, command string, format outputFormat) string {
	if format == formatStyled {
		msg := err.Error()
		for key, friendly := range styledErrors {
			if key[0] == command && strings.Contains(msg, key[1]) {
				return friendly
			}
		}
	}
	return "Error: " + err.Error()
}

// formatMine formats a mine response as a one-line summary.
func formatMine(raw []byte) string {
	var resp struct {
		Quantity         float64 `json:"quantity"`
		ResourceName     string  `json:"resource_name"`
		RemainingDisplay string  `json:"remaining_display"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}
	return fmt.Sprintf("Mined %s %s ( %s remaining )", formatFloat(resp.Quantity), resp.ResourceName, resp.RemainingDisplay)
}

// formatDock formats a dock response with station condition and truncated story.
func formatDock(raw []byte) string {
	var resp struct {
		Base             string `json:"base"`
		StationCondition struct {
			Condition         string `json:"condition"`
			ConditionText     string `json:"condition_text"`
			SatisfactionPct   int    `json:"satisfaction_pct"`
			SatisfiedCount    int    `json:"satisfied_count"`
			TotalServiceInfra int    `json:"total_service_infra"`
		} `json:"station_condition"`
		Story string `json:"story"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Docked at %q\n\n", resp.Base)

	sc := resp.StationCondition
	fmt.Fprintf(&b, "Station is in %q condition.  %s\n\n", sc.Condition, sc.ConditionText)
	fmt.Fprintf(&b, "Services satisfied: %d / %d (%d%%)\n", sc.SatisfiedCount, sc.TotalServiceInfra, sc.SatisfactionPct)

	if resp.Story != "" {
		story := resp.Story
		if len(story) > 200 {
			story = story[:200] + "..."
		}
		// Collapse newlines for compact display.
		story = strings.ReplaceAll(story, "\n", " ")
		fmt.Fprintf(&b, "\nStation Lore: %q\n", story)
	}

	return b.String()
}

// formatJump formats a jump response as a one-line summary.
func formatJump(raw []byte) string {
	var resp struct {
		FromSystem string `json:"from_system"`
		System     string `json:"system"`
		POI        string `json:"poi"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}
	return fmt.Sprintf("Successfully jumped from %s to %s.  Now located at %s", resp.FromSystem, resp.System, resp.POI)
}

// wreckCargo is a parsed cargo item from a wreck.
type wreckCargo struct {
	ItemID   string  `json:"item_id"`
	Quantity float64 `json:"quantity"`
}

// wreckEntry is a parsed wreck from a get_wrecks response.
type wreckEntry struct {
	ID           string       `json:"id"`
	Type         string       `json:"type"`
	VictimName   string       `json:"victim_name"`
	ShipClass    string       `json:"ship_class"`
	Cargo        []wreckCargo `json:"cargo"`
	Modules      []string     `json:"modules"`
	SalvageValue int          `json:"salvage_value"`
}

// formatWrecks formats a get_wrecks response.
func formatWrecks(raw []byte) string {
	var resp struct {
		Count  int          `json:"count"`
		Wrecks []wreckEntry `json:"wrecks"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}

	if resp.Count == 0 || len(resp.Wrecks) == 0 {
		return "No wrecks found."
	}

	// Separate by type.
	var jettisons, ships []wreckEntry
	for _, w := range resp.Wrecks {
		if w.Type == "jettison" {
			jettisons = append(jettisons, w)
		} else {
			ships = append(ships, w)
		}
	}

	var b strings.Builder

	if len(jettisons) > 0 {
		fmt.Fprintf(&b, "Jettison Cannisters: %d\n", len(jettisons))
		for _, w := range jettisons {
			fmt.Fprintf(&b, "\nCanister: %s\n", w.ID)
			fmt.Fprintf(&b, "Owner: %q\n", w.VictimName)
			b.WriteString("Contents:\n")

			// Calculate column widths for alignment.
			idW := 0
			for _, c := range w.Cargo {
				idW = max(idW, len(c.ItemID))
			}

			for _, c := range w.Cargo {
				fmt.Fprintf(&b, "  %*s | %s\n", idW, c.ItemID, formatFloat(c.Quantity))
			}

			b.WriteString("\nTo loot:\n")
			for _, c := range w.Cargo {
				fmt.Fprintf(&b, "loot_wreck %s %s %s\n", w.ID, c.ItemID, formatFloat(c.Quantity))
			}
		}
	}

	if len(ships) > 0 {
		if len(jettisons) > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "Ship Wrecks: %d\n", len(ships))
		for _, w := range ships {
			fmt.Fprintf(&b, "\nShip: %s\n", w.ID)
			fmt.Fprintf(&b, "Owner: %q\n", w.VictimName)
			fmt.Fprintf(&b, "Class: %s\n", w.ShipClass)
			fmt.Fprintf(&b, "Salvage Value: %d\n", w.SalvageValue)
			fmt.Fprintf(&b, "Modules:  %d\n", len(w.Modules))
			if len(w.Cargo) == 0 {
				b.WriteString("Cargo:   None\n")
			} else {
				b.WriteString("Cargo:\n")
				idW := 0
				for _, c := range w.Cargo {
					idW = max(idW, len(c.ItemID))
				}
				for _, c := range w.Cargo {
					fmt.Fprintf(&b, "  %*s | %s\n", idW, c.ItemID, formatFloat(c.Quantity))
				}
			}
			b.WriteString("To salvage:\n")
			fmt.Fprintf(&b, "tow_ship %s\n", w.ID)
		}
	}

	return b.String()
}

// systemPOI is a parsed POI from a get_system response.
type systemPOI struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Online   int    `json:"online,omitempty"`
	Position struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
	} `json:"position"`
	HasBase  bool   `json:"has_base,omitempty"`
	BaseID   string `json:"base_id,omitempty"`
	BaseName string `json:"base_name,omitempty"`
}

// systemConnection is a parsed connection from a get_system response.
type systemConnection struct {
	SystemID string `json:"system_id"`
	Name     string `json:"name"`
	Distance int    `json:"distance"`
}

// formatSystem formats a get_system response with system details, connections, and POIs.
func formatSystem(raw []byte) string {
	var resp struct {
		System struct {
			ID             string             `json:"id"`
			Name           string             `json:"name"`
			Description    string             `json:"description"`
			Empire         string             `json:"empire"`
			PoliceLevel    int                `json:"police_level"`
			SecurityStatus string             `json:"security_status"`
			Connections    []systemConnection `json:"connections"`
			POIs           []systemPOI        `json:"pois"`
		} `json:"system"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}

	sys := resp.System
	var b strings.Builder

	// Header
	empire := sys.Empire
	if empire != "" {
		empire = strings.ToUpper(empire[:1]) + empire[1:]
	}
	fmt.Fprintf(&b, "%s (%s)   | %s\n", sys.Name, sys.ID, empire)
	fmt.Fprintf(&b, "Security Status: %d - %s\n", sys.PoliceLevel, sys.SecurityStatus)
	if sys.Description != "" {
		fmt.Fprintf(&b, "%s\n", sys.Description)
	}

	// Connections
	b.WriteString("\nConnections:\n")
	if len(sys.Connections) == 0 {
		b.WriteString("  (none)\n")
	} else {
		nameW, idW := 0, 0
		for _, c := range sys.Connections {
			nameW = max(nameW, len(c.Name))
			idW = max(idW, len(c.SystemID))
		}
		for _, c := range sys.Connections {
			fmt.Fprintf(&b, "    %-*s | %-*s | %d LY\n", nameW, c.Name, idW, c.SystemID, c.Distance)
		}
	}

	// POIs
	b.WriteString("\nPOIs:\n")
	if len(sys.POIs) == 0 {
		b.WriteString("  (none)\n")
	} else {
		nameW, idW, typeW := len("Name"), len("ID"), len("Type")
		for _, p := range sys.POIs {
			nameW = max(nameW, len(p.Name))
			idW = max(idW, len(p.ID))
			typeW = max(typeW, len(p.Type))
		}

		fmt.Fprintf(&b, "%-*s | %-*s | %-*s | Position\n",
			nameW, "Name", idW, "ID", typeW, "Type")
		b.WriteString(strings.Repeat("-", nameW+idW+typeW+18) + "\n")

		for _, p := range sys.POIs {
			pos := fmt.Sprintf("(%.1f, %.1f)", p.Position.X, p.Position.Y)
			fmt.Fprintf(&b, "%-*s | %-*s | %-*s | %s\n",
				nameW, p.Name, idW, p.ID, typeW, p.Type, pos)
		}
	}

	return b.String()
}

// formatCreateFaction formats a create_faction response as a one-line summary.
func formatCreateFaction(raw []byte) string {
	var resp struct {
		FactionID string `json:"faction_id"`
		Name      string `json:"name"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}
	return fmt.Sprintf("Faction created:  %q (%s)", resp.Name, resp.FactionID)
}

// formatFactionInfo formats a faction_info response with colored names.
func formatFactionInfo(raw []byte) string {
	var resp struct {
		ID             string `json:"id"`
		Name           string `json:"name"`
		Tag            string `json:"tag"`
		Description    string `json:"description"`
		Charter        string `json:"charter"`
		LeaderUsername string `json:"leader_username"`
		MemberCount    int    `json:"member_count"`
		OwnedBases     int    `json:"owned_bases"`
		Treasury       int    `json:"treasury"`
		PrimaryColor   string `json:"primary_color"`
		SecondaryColor string `json:"secondary_color"`
		AtWar          bool   `json:"at_war"`
		IsMember       bool   `json:"is_member"`
		IsAlly         bool   `json:"is_ally"`
		IsEnemy        bool   `json:"is_enemy"`
		Members        []struct {
			Username string `json:"username"`
			Role     string `json:"role"`
			IsOnline bool   `json:"is_online"`
		} `json:"members"`
		Allies []struct {
			Name string `json:"name"`
			Tag  string `json:"tag"`
		} `json:"allies"`
		Enemies []struct {
			Name string `json:"name"`
			Tag  string `json:"tag"`
		} `json:"enemies"`
		Wars []struct {
			FactionName string `json:"faction_name"`
			FactionTag  string `json:"faction_tag"`
			DeclaredBy  string `json:"declared_by"`
		} `json:"wars"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}

	var b strings.Builder
	colorName := colorizeHex(resp.Name, resp.PrimaryColor, resp.SecondaryColor)
	colorTag := colorizeHex("["+resp.Tag+"]", resp.PrimaryColor, resp.SecondaryColor)
	fmt.Fprintf(&b, "%s %s\n", colorName, colorTag)
	fmt.Fprintf(&b, "ID: %s\n", resp.ID)
	fmt.Fprintf(&b, "Leader: %s | Members: %d | Bases: %d\n", resp.LeaderUsername, resp.MemberCount, resp.OwnedBases)

	if resp.IsMember && resp.Treasury > 0 {
		fmt.Fprintf(&b, "Treasury: %d credits\n", resp.Treasury)
	}

	if resp.Description != "" {
		fmt.Fprintf(&b, "\n%s\n", resp.Description)
	}
	if resp.Charter != "" {
		fmt.Fprintf(&b, "\nCharter: %s\n", resp.Charter)
	}

	// Relationship indicator
	switch {
	case resp.IsMember:
		fmt.Fprintf(&b, "\nYou are a member of this faction.\n")
	case resp.IsAlly:
		fmt.Fprintf(&b, "\nThis faction is an ally.\n")
	case resp.IsEnemy:
		fmt.Fprintf(&b, "\nThis faction is an enemy.\n")
	}

	// Members (only shown for own faction)
	if len(resp.Members) > 0 {
		fmt.Fprintf(&b, "\nMembers:\n")
		nameW, roleW := len("Username"), len("Role")
		for _, m := range resp.Members {
			nameW = max(nameW, len(m.Username))
			roleW = max(roleW, len(m.Role))
		}
		fmt.Fprintf(&b, "  %-*s | %-*s | Status\n", nameW, "Username", roleW, "Role")
		fmt.Fprintf(&b, "  %s-+-%s-+--------\n", strings.Repeat("-", nameW), strings.Repeat("-", roleW))
		for _, m := range resp.Members {
			status := "offline"
			if m.IsOnline {
				status = "online"
			}
			name := colorizeHex(m.Username, resp.PrimaryColor, resp.SecondaryColor)
			pad := nameW - len(m.Username)
			if pad > 0 {
				name += strings.Repeat(" ", pad)
			}
			fmt.Fprintf(&b, "  %s | %-*s | %s\n", name, roleW, m.Role, status)
		}
	}

	// Allies
	if len(resp.Allies) > 0 {
		fmt.Fprintf(&b, "\nAllies:\n")
		for _, a := range resp.Allies {
			fmt.Fprintf(&b, "  %s [%s]\n", a.Name, a.Tag)
		}
	}

	// Enemies
	if len(resp.Enemies) > 0 {
		fmt.Fprintf(&b, "\nEnemies:\n")
		for _, e := range resp.Enemies {
			fmt.Fprintf(&b, "  %s [%s]\n", e.Name, e.Tag)
		}
	}

	// Wars
	if len(resp.Wars) > 0 {
		fmt.Fprintf(&b, "\nActive Wars:\n")
		for _, w := range resp.Wars {
			fmt.Fprintf(&b, "  vs %s [%s] (declared by: %s)\n", w.FactionName, w.FactionTag, w.DeclaredBy)
		}
	}

	return b.String()
}

// formatDeposit formats a deposit_items response as a one-line summary.
func formatDeposit(raw []byte) string {
	var resp struct {
		ItemID       string `json:"item_id"`
		Quantity     int    `json:"quantity"`
		StorageTotal int    `json:"storage_total"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}
	return fmt.Sprintf("Deposited %d %s from cargo into storage. %d %s now in storage.",
		resp.Quantity, resp.ItemID, resp.StorageTotal, resp.ItemID)
}

// formatSkills formats a get_skills response as a table.
func formatSkills(raw []byte) string {
	var resp struct {
		Skills map[string]struct {
			Name       string `json:"name"`
			Category   string `json:"category"`
			Level      int    `json:"level"`
			MaxLevel   int    `json:"max_level"`
			XP         int    `json:"xp"`
			NextLvlXP  int    `json:"next_level_xp"`
		} `json:"skills"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}
	if len(resp.Skills) == 0 {
		return "No skills"
	}

	type skillRow struct {
		Name     string
		Category string
		Level    int
		XP       int
		NextXP   int
		Pct      int
	}

	rows := make([]skillRow, 0, len(resp.Skills))
	for _, s := range resp.Skills {
		pct := 0
		if s.NextLvlXP > 0 {
			pct = s.XP * 100 / s.NextLvlXP
		}
		rows = append(rows, skillRow{
			Name:     s.Name,
			Category: s.Category,
			Level:    s.Level,
			XP:       s.XP,
			NextXP:   s.NextLvlXP,
			Pct:      pct,
		})
	}
	slices.SortFunc(rows, func(a, b skillRow) int {
		if c := strings.Compare(a.Category, b.Category); c != 0 {
			return c
		}
		return strings.Compare(a.Name, b.Name)
	})

	nameW, catW, lvlW, progW := len("Skill"), len("Category"), len("Level"), len("Progress to Next")
	for _, r := range rows {
		nameW = max(nameW, len(r.Name))
		catW = max(catW, len(r.Category))
		ls := strconv.Itoa(r.Level)
		lvlW = max(lvlW, len(ls))
		ps := fmt.Sprintf("%d / %d (%d%%)", r.XP, r.NextXP, r.Pct)
		progW = max(progW, len(ps))
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Skills (%d)\n", len(rows))
	fmt.Fprintf(&b, "  %-*s | %-*s | %*s | %*s\n", nameW, "Skill", catW, "Category", lvlW, "Level", progW, "Progress to Next")
	fmt.Fprintf(&b, "  %s-+-%s-+-%s-+-%s\n",
		strings.Repeat("-", nameW), strings.Repeat("-", catW),
		strings.Repeat("-", lvlW), strings.Repeat("-", progW))

	for _, r := range rows {
		prog := fmt.Sprintf("%d / %d (%d%%)", r.XP, r.NextXP, r.Pct)
		fmt.Fprintf(&b, "  %-*s | %-*s | %*d | %*s\n",
			nameW, r.Name, catW, r.Category, lvlW, r.Level, progW, prog)
	}
	return b.String()
}

// formatWithdraw formats a withdraw_items response as a one-line summary.
func formatWithdraw(raw []byte) string {
	var resp struct {
		ItemID           string `json:"item_id"`
		Quantity         int    `json:"quantity"`
		StorageRemaining int    `json:"storage_remaining"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}
	return fmt.Sprintf("Withdraw %d %s from storage into cargo. %d %s remaining in storage.",
		resp.Quantity, resp.ItemID, resp.StorageRemaining, resp.ItemID)
}

// formatRefuel formats a refuel response as a one-line summary.
func formatRefuel(raw []byte) string {
	var resp struct {
		Source string `json:"source"`
		Fuel   int    `json:"fuel"`
		Cost   int    `json:"cost"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}
	return fmt.Sprintf("Refueled at %s.  %d units for %d credits.", resp.Source, resp.Fuel, resp.Cost)
}

// formatJettison formats a jettison response as a one-line summary.
func formatJettison(raw []byte) string {
	var resp struct {
		ContainerID string  `json:"container_id"`
		ItemID      string  `json:"item_id"`
		Quantity    float64 `json:"quantity"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}
	return fmt.Sprintf("Jettisoned %s %s into cannister %q.", formatFloat(resp.Quantity), resp.ItemID, resp.ContainerID)
}

// formatLootWreck formats a loot_wreck response as a one-line summary.
func formatLootWreck(raw []byte) string {
	var resp struct {
		ItemID     string  `json:"item_id"`
		Quantity   float64 `json:"quantity"`
		WreckEmpty bool    `json:"wreck_empty"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}
	msg := fmt.Sprintf("Looted %s %s from cannister.", formatFloat(resp.Quantity), resp.ItemID)
	if !resp.WreckEmpty {
		msg += " There are still more items in the cannister."
	}
	return msg
}

// writePlayerTable writes a sorted player table to b.
func writePlayerTable(b *strings.Builder, players []nearbyPlayer) {
	if len(players) == 0 {
		b.WriteString("  (no players nearby)\n")
		return
	}

	slices.SortFunc(players, func(a, c nearbyPlayer) int {
		return strings.Compare(strings.ToLower(a.Username), strings.ToLower(c.Username))
	})

	nameW, tagW, shipW, combatW := len("Username"), len("Faction"), len("Ship"), len("Combat")
	for _, p := range players {
		nameW = max(nameW, len(p.Username))
		tagW = max(tagW, len(p.FactionTag))
		shipW = max(shipW, len(p.ShipClass))
	}

	fmt.Fprintf(b, "%-*s | %-*s | %-*s | %-*s\n",
		nameW, "Username", tagW, "Faction", shipW, "Ship", combatW, "Combat")
	b.WriteString(strings.Repeat("-", nameW+tagW+shipW+combatW+9) + "\n")

	for _, p := range players {
		combat := "no combat"
		if p.InCombat {
			combat = "COMBAT"
		}
		// Colorize name at natural length, then pad with spaces for alignment.
		name := colorizeHex(p.Username, p.PrimaryColor, p.SecondaryColor)
		pad := nameW - len(p.Username)
		if pad > 0 {
			name += strings.Repeat(" ", pad)
		}
		fmt.Fprintf(b, "%s | %-*s | %-*s | %s |\n",
			name, tagW, p.FactionTag,
			shipW, p.ShipClass, combat)
	}
}

// formatFloat formats a float64 nicely — as integer if whole, otherwise with decimals.
func formatFloat(f float64) string {
	if f == float64(int64(f)) {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'f', 2, 64)
}

func executeCommand(client game.GameClient, ctx context.Context, parts []string, format outputFormat) error {
	cmd := strings.ToLower(parts[0])

	fmt.Printf("▶ Executing: %s %s\n", cmd, strings.Join(parts[1:], " "))

	switch cmd {
	// === NAVIGATION ===
	case "undock":
		return simpleCommand(client, client.Undock, ctx, 12*time.Second, cmd, format)

	case "dock":
		return simpleCommand(client, client.Dock, ctx, 3*time.Second, cmd, format)

	case "travel":
		if len(parts) < 2 {
			return fmt.Errorf("usage: travel <poi-id>")
		}
		target := strings.Join(parts[1:], " ")

		// Estimate travel time before sending the command.
		est := estimateTravel(client, target)
		if est.valid {
			fmt.Printf("⏱ Distance: %.1f AU | Speed: %.1f | Est. %d tick(s) (~%ds) | Est. fuel: %d\n",
				est.distance, est.speed, est.ticks, est.ticks*10, est.fuel)
		}

		// Server blocks until travel completes.
		_, err := client.Travel(ctx, target)
		if err != nil {
			return err
		}
		showLastResponse(client, format, cmd)
		return nil

	case "jump":
		if len(parts) < 2 {
			return fmt.Errorf("usage: jump <system-id>")
		}
		// Show jump distance, time, and fuel estimate from connection and ship data.
		if state := client.GetState(); state != nil {
			for _, conn := range state.System.Connections {
				if strings.EqualFold(conn.SystemID, parts[1]) || strings.EqualFold(conn.Name, parts[1]) {
					jumpTicks := max(1, 7-int(state.Ship.Speed))
					// Fuel: ceil(scale^1.5 × speed × 10.0 × 0.10)
					jumpFuel := 1
					if raw := client.GetRawJSON("ship"); len(raw) > 0 {
						var shipResp struct {
							Class *struct {
								Scale     int `json:"scale"`
								BaseSpeed int `json:"base_speed"`
							} `json:"class"`
						}
						if err := json.Unmarshal(raw, &shipResp); err == nil && shipResp.Class != nil {
							scale := float64(shipResp.Class.Scale)
							spd := float64(shipResp.Class.BaseSpeed)
							if scale > 0 && spd > 0 {
								jumpFuel = max(1, int(math.Ceil(math.Pow(scale, 1.5)*spd*10.0*0.10)))
							}
						}
					}
					fmt.Printf("⏱ Jump distance: %d ly | Est. %d tick(s) (~%ds) | Est. fuel: %d\n",
						conn.Distance, jumpTicks, jumpTicks*10, jumpFuel)
					break
				}
			}
		}
		// Server blocks until jump completes.
		_, err := client.Jump(ctx, parts[1])
		if err != nil {
			return err
		}
		showLastResponse(client, format, cmd)
		_ = client.GetSystem(ctx) // Refresh system data (POIs, connections) for the new system.
		return nil

	// === MINING & SCANNING ===
	case "mine":
		return simpleCommand(client, client.Mine, ctx, 12*time.Second, cmd, format)

	case "scan":
		return simpleCommand(client, client.Scan, ctx, 3*time.Second, cmd, format)

	case "survey":
		return simpleCommand(client, client.SurveySystem, ctx, 15*time.Second, cmd, format)

	// === COMBAT ===
	case "attack":
		if len(parts) < 2 {
			return fmt.Errorf("usage: attack <target-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.Attack(ctx, parts[1])
		}, ctx, 3*time.Second, cmd, format)

	case "cloak":
		enable := len(parts) >= 2 && (parts[1] == "on" || parts[1] == "true" || parts[1] == "1")
		return simpleCommand(client, func(ctx context.Context) error {
			return client.Cloak(ctx, enable)
		}, ctx, 2*time.Second, cmd, format)

	case "battle":
		if len(parts) < 2 {
			return fmt.Errorf("usage: battle <action> [--stance <stance>] [--target_id <id>] [--side_id <id>]")
		}
		payload := map[string]any{"action": parts[1]}
		flags := parseFlagArgs(parts[2:], "stance", "target_id", "side_id")
		for k, v := range flags {
			if k == "side_id" {
				if n, err := strconv.Atoi(v.(string)); err == nil {
					payload[k] = n
				}
			} else {
				payload[k] = v
			}
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "battle", payload)
		}, ctx, 3*time.Second, cmd, format)

	case "reload":
		if len(parts) < 3 {
			return fmt.Errorf("usage: reload <weapon-instance-id> <ammo-item-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "reload", map[string]any{
				"weapon_instance_id": parts[1],
				"ammo_item_id":      parts[2],
			})
		}, ctx, 3*time.Second, cmd, format)

	case "distress_signal":
		var distressType string
		if len(parts) >= 2 {
			distressType = parts[1]
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.DistressSignal(ctx, distressType)
		}, ctx, 2*time.Second, cmd, format)

	// === COMMERCE ===
	case "sell":
		if len(parts) < 3 {
			return fmt.Errorf("usage: sell <item-id> <quantity>")
		}
		qty, err := parseQuantity(parts[2])
		if err != nil {
			return fmt.Errorf("invalid quantity: %w", err)
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.Sell(ctx, parts[1], qty)
		}, ctx, 3*time.Second, cmd, format)

	case "sell_all_bulk":
		return simpleCommand(client, func(ctx context.Context) error {
			return client.SellAllBulk(ctx, nil)
		}, ctx, 5*time.Second, cmd, format)

	case "buy":
		if len(parts) < 3 {
			return fmt.Errorf("usage: buy <item-id> <quantity>")
		}
		qty, err := parseQuantity(parts[2])
		if err != nil {
			return fmt.Errorf("invalid quantity: %w", err)
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.Buy(ctx, parts[1], qty)
		}, ctx, 3*time.Second, cmd, format)

	case "listings", "get_listings":
		return simpleCommand(client, client.GetListings, ctx, 2*time.Second, cmd, format)

	case "trades", "get_trades":
		return simpleCommand(client, client.GetTrades, ctx, 2*time.Second, cmd, format)

	case "view_market":
		if len(parts) < 2 {
			return simpleCommand(client, client.GetListings, ctx, 2*time.Second, cmd, format)
		}
		// First non-flag arg is item_id; also accept --item_id and --category flags
		payload := make(map[string]any)
		for i := 1; i < len(parts); i++ {
			arg := parts[i]
			if key, ok := strings.CutPrefix(arg, "--"); ok {
				if k, v, ok2 := strings.Cut(key, "="); ok2 {
					payload[k] = v
				} else if i+1 < len(parts) {
					i++
					payload[key] = parts[i]
				}
			} else if payload["item_id"] == nil {
				payload["item_id"] = arg
			}
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "view_market", payload)
		}, ctx, 2*time.Second, cmd, format)

	case "view_orders":
		if len(parts) > 1 {
			payload := parseFlagArgs(parts[1:], "item_id", "order_type", "page", "page_size", "scope", "search", "sort_by", "station_id")
			for _, k := range []string{"page", "page_size"} {
				if v, ok := payload[k]; ok {
					if n, err := strconv.Atoi(v.(string)); err == nil {
						payload[k] = n
					}
				}
			}
			return simpleCommand(client, func(ctx context.Context) error {
				return client.RawCommand(ctx, "view_orders", payload)
			}, ctx, 2*time.Second, cmd, format)
		}
		return simpleCommand(client, client.ViewOrders, ctx, 2*time.Second, cmd, format)

	case "create_sell_order":
		if len(parts) < 4 {
			return fmt.Errorf("usage: create_sell_order <item-id> <quantity> <price-each>")
		}
		qty, err := strconv.Atoi(parts[2])
		if err != nil {
			return fmt.Errorf("invalid quantity: %w", err)
		}
		price, err := strconv.Atoi(parts[3])
		if err != nil {
			return fmt.Errorf("invalid price: %w", err)
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.CreateSellOrder(ctx, map[string]any{
				"item_id":    parts[1],
				"quantity":   qty,
				"price_each": price,
			})
		}, ctx, 3*time.Second, cmd, format)

	case "create_buy_order":
		if len(parts) < 4 {
			return fmt.Errorf("usage: create_buy_order <item-id> <quantity> <price-each>")
		}
		qty, err := strconv.Atoi(parts[2])
		if err != nil {
			return fmt.Errorf("invalid quantity: %w", err)
		}
		price, err := strconv.Atoi(parts[3])
		if err != nil {
			return fmt.Errorf("invalid price: %w", err)
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.CreateBuyOrder(ctx, map[string]any{
				"item_id":    parts[1],
				"quantity":   qty,
				"price_each": price,
			})
		}, ctx, 3*time.Second, cmd, format)

	case "cancel_order":
		if len(parts) < 2 {
			return fmt.Errorf("usage: cancel_order <order-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "cancel_order", map[string]any{"order_id": parts[1]})
		}, ctx, 3*time.Second, cmd, format)

	case "modify_order":
		if len(parts) < 2 {
			return fmt.Errorf("usage: modify_order <order-id> --new_price <price>")
		}
		payload := map[string]any{"order_id": parts[1]}
		flags := parseFlagArgs(parts[2:], "new_price")
		if v, ok := flags["new_price"]; ok {
			if n, err := strconv.Atoi(v.(string)); err == nil {
				payload["new_price"] = n
			}
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "modify_order", payload)
		}, ctx, 3*time.Second, cmd, format)

	case "estimate_purchase":
		if len(parts) < 3 {
			return fmt.Errorf("usage: estimate_purchase <item-id> <quantity>")
		}
		qty, err := strconv.Atoi(parts[2])
		if err != nil {
			return fmt.Errorf("invalid quantity: %w", err)
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "estimate_purchase", map[string]any{
				"item_id": parts[1], "quantity": qty,
			})
		}, ctx, 2*time.Second, cmd, format)

	case "list_ship_for_sale":
		if len(parts) < 3 {
			return fmt.Errorf("usage: list_ship_for_sale <ship-id> <price>")
		}
		price, err := strconv.Atoi(parts[2])
		if err != nil {
			return fmt.Errorf("invalid price: %w", err)
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "list_ship_for_sale", map[string]any{
				"ship_id": parts[1], "price": price,
			})
		}, ctx, 3*time.Second, cmd, format)

	case "name_ship":
		if len(parts) < 2 {
			return fmt.Errorf("usage: name_ship <name>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "name_ship", map[string]any{
				"name": strings.Join(parts[1:], " "),
			})
		}, ctx, 2*time.Second, cmd, format)

	case "commission_quote":
		if len(parts) < 2 {
			return fmt.Errorf("usage: commission_quote <ship-class>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "commission_quote", map[string]any{"ship_class": parts[1]})
		}, ctx, 2*time.Second, cmd, format)

	case "commission_status":
		var payload map[string]any
		if len(parts) >= 2 {
			payload = map[string]any{"base_id": parts[1]}
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "commission_status", payload)
		}, ctx, 2*time.Second, cmd, format)

	case "cancel_commission":
		if len(parts) < 2 {
			return fmt.Errorf("usage: cancel_commission <commission-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "cancel_commission", map[string]any{"commission_id": parts[1]})
		}, ctx, 3*time.Second, cmd, format)

	case "claim_commission":
		if len(parts) < 2 {
			return fmt.Errorf("usage: claim_commission <commission-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "claim_commission", map[string]any{"commission_id": parts[1]})
		}, ctx, 3*time.Second, cmd, format)

	case "commission_ship":
		if len(parts) < 2 {
			return fmt.Errorf("usage: commission_ship <ship-class> [--provide_materials true|false]")
		}
		payload := map[string]any{}
		// Parse positional and flags
		for i := 1; i < len(parts); i++ {
			arg := parts[i]
			if key, ok := strings.CutPrefix(arg, "--"); ok {
				if k, v, ok2 := strings.Cut(key, "="); ok2 {
					payload[k] = v
				} else if i+1 < len(parts) {
					i++
					payload[key] = parts[i]
				}
			} else if payload["ship_class"] == nil {
				payload["ship_class"] = arg
			}
		}
		// Convert provide_materials to bool
		if v, ok := payload["provide_materials"]; ok {
			s, _ := v.(string)
			payload["provide_materials"] = strings.EqualFold(s, "true") || s == "1"
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "commission_ship", payload)
		}, ctx, 5*time.Second, cmd, format)

	case "supply_commission":
		if len(parts) < 4 {
			return fmt.Errorf("usage: supply_commission <commission-id> <item-id> <quantity>")
		}
		qty, err := strconv.Atoi(parts[3])
		if err != nil {
			return fmt.Errorf("invalid quantity: %w", err)
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "supply_commission", map[string]any{
				"commission_id": parts[1], "item_id": parts[2], "quantity": qty,
			})
		}, ctx, 3*time.Second, cmd, format)

	case "trade_offer":
		if len(parts) < 2 {
			return fmt.Errorf("usage: trade_offer <target-id> [--offer_credits <n>] [--request_credits <n>]")
		}
		payload := map[string]any{"target_id": parts[1]}
		flags := parseFlagArgs(parts[2:], "offer_credits", "request_credits")
		for _, k := range []string{"offer_credits", "request_credits"} {
			if v, ok := flags[k]; ok {
				if n, err := strconv.Atoi(v.(string)); err == nil {
					payload[k] = n
				}
			}
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "trade_offer", payload)
		}, ctx, 3*time.Second, cmd, format)

	case "trade_accept":
		if len(parts) < 2 {
			return fmt.Errorf("usage: trade_accept <trade-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "trade_accept", map[string]any{"trade_id": parts[1]})
		}, ctx, 3*time.Second, cmd, format)

	case "trade_cancel":
		if len(parts) < 2 {
			return fmt.Errorf("usage: trade_cancel <trade-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "trade_cancel", map[string]any{"trade_id": parts[1]})
		}, ctx, 3*time.Second, cmd, format)

	case "trade_decline":
		if len(parts) < 2 {
			return fmt.Errorf("usage: trade_decline <trade-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "trade_decline", map[string]any{"trade_id": parts[1]})
		}, ctx, 3*time.Second, cmd, format)

	// === CRAFTING ===
	case "craft":
		if len(parts) < 3 {
			return fmt.Errorf("usage: craft <recipe-id> <quantity>")
		}
		qty, err := strconv.Atoi(parts[2])
		if err != nil {
			return fmt.Errorf("invalid quantity: %w", err)
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.CraftWithQuantity(ctx, parts[1], qty)
		}, ctx, 5*time.Second, cmd, format)

	case "recipes", "get_recipes":
		return simpleCommand(client, client.GetRecipes, ctx, 2*time.Second, cmd, format)

	// === SHIP MAINTENANCE ===
	case "refuel":
		if len(parts) > 1 {
			payload := parseFlagArgs(parts[1:], "item_id", "quantity", "target")
			if v, ok := payload["quantity"]; ok {
				if n, err := strconv.Atoi(v.(string)); err == nil {
					payload["quantity"] = n
				}
			}
			return simpleCommand(client, func(ctx context.Context) error {
				return client.RawCommand(ctx, "refuel", payload)
			}, ctx, 3*time.Second, cmd, format)
		}
		return simpleCommand(client, client.Refuel, ctx, 3*time.Second, cmd, format)

	case "repair":
		if len(parts) > 1 {
			payload := parseFlagArgs(parts[1:], "item_id", "quantity", "target")
			if v, ok := payload["quantity"]; ok {
				if n, err := strconv.Atoi(v.(string)); err == nil {
					payload["quantity"] = n
				}
			}
			return simpleCommand(client, func(ctx context.Context) error {
				return client.RawCommand(ctx, "repair", payload)
			}, ctx, 3*time.Second, cmd, format)
		}
		return simpleCommand(client, client.Repair, ctx, 3*time.Second, cmd, format)

	case "install", "install_mod":
		if len(parts) < 2 {
			return fmt.Errorf("usage: install <item-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.Install(ctx, parts[1])
		}, ctx, 3*time.Second, cmd, format)

	case "uninstall", "uninstall_mod":
		if len(parts) < 2 {
			return fmt.Errorf("usage: uninstall <module-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.UninstallMod(ctx, parts[1])
		}, ctx, 3*time.Second, cmd, format)

	case "buy_ship":
		if len(parts) < 2 {
			return fmt.Errorf("usage: buy_ship <ship-class>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.BuyShip(ctx, parts[1])
		}, ctx, 5*time.Second, cmd, format)

	case "buy_listed_ship":
		if len(parts) < 2 {
			return fmt.Errorf("usage: buy_listed_ship <listing-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "buy_listed_ship", map[string]any{"listing_id": parts[1]})
		}, ctx, 5*time.Second, cmd, format)

	case "browse_ships":
		var payload map[string]any
		if len(parts) > 1 {
			payload = make(map[string]any)
			for i := 1; i < len(parts); i++ {
				arg := parts[i]
				if key, ok := strings.CutPrefix(arg, "--"); ok {
					if k, v, ok2 := strings.Cut(key, "="); ok2 {
						payload[k] = v
					} else if i+1 < len(parts) {
						i++
						payload[key] = parts[i]
					}
				}
			}
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.BrowseShips(ctx, payload)
		}, ctx, 2*time.Second, cmd, format)

	case "list_ships":
		return simpleCommand(client, client.ListShips, ctx, 2*time.Second, cmd, format)

	case "switch_ship":
		if len(parts) < 2 {
			return fmt.Errorf("usage: switch_ship <ship-id>")
		}
		err := simpleCommand(client, func(ctx context.Context) error {
			return client.SwitchShip(ctx, parts[1])
		}, ctx, 5*time.Second, cmd, format)
		if err == nil {
			_ = client.GetShip(ctx) // Refresh ship data for new ship.
		}
		return err

	case "sell_ship":
		if len(parts) < 2 {
			return fmt.Errorf("usage: sell_ship <ship-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.SellShip(ctx, parts[1])
		}, ctx, 3*time.Second, cmd, format)

	// === INSURANCE ===
	case "buy_insurance":
		ticks := 100
		if len(parts) >= 2 {
			var err error
			ticks, err = strconv.Atoi(parts[1])
			if err != nil {
				return fmt.Errorf("invalid ticks: %w", err)
			}
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.BuyInsurance(ctx, ticks)
		}, ctx, 2*time.Second, cmd, format)

	case "claim_insurance":
		return simpleCommand(client, client.ClaimInsurance, ctx, 3*time.Second, cmd, format)

	// === CARGO & STORAGE ===
	case "cargo", "get_cargo":
		return simpleCommand(client, client.GetCargo, ctx, 2*time.Second, cmd, format)

	case "deposit", "deposit_items":
		if len(parts) < 3 {
			return fmt.Errorf("usage: deposit <item-id> <quantity>")
		}
		qty, err := parseQuantity(parts[2])
		if err != nil {
			return fmt.Errorf("invalid quantity: %w", err)
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.DepositItems(ctx, parts[1], qty)
		}, ctx, 3*time.Second, cmd, format)

	case "deposit_all":
		return simpleCommand(client, client.DepositAllItems, ctx, 5*time.Second, cmd, format)

	case "withdraw", "withdraw_items":
		if len(parts) < 3 {
			return fmt.Errorf("usage: withdraw <item-id> <quantity>")
		}
		qty, err := parseQuantity(parts[2])
		if err != nil {
			return fmt.Errorf("invalid quantity: %w", err)
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.WithdrawItems(ctx, parts[1], qty)
		}, ctx, 3*time.Second, cmd, format)

	case "storage", "view_storage":
		return simpleCommand(client, client.ViewStorage, ctx, 2*time.Second, cmd, format)

	case "storage_at":
		if len(parts) < 2 {
			return fmt.Errorf("usage: storage_at <station-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.ViewStorageAt(ctx, parts[1])
		}, ctx, 2*time.Second, cmd, format)

	case "jettison":
		if len(parts) < 3 {
			return fmt.Errorf("usage: jettison <item-id> <quantity>")
		}
		qty, err := parseQuantity(parts[2])
		if err != nil {
			return fmt.Errorf("invalid quantity: %w", err)
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.Jettison(ctx, parts[1], qty)
		}, ctx, 2*time.Second, cmd, format)

	// === WRECKS ===
	case "wrecks", "get_wrecks":
		return simpleCommand(client, client.GetWrecks, ctx, 2*time.Second, cmd, format)

	case "loot", "loot_wreck":
		if len(parts) < 4 {
			return fmt.Errorf("usage: loot <wreck-id> <item-id> <quantity>")
		}
		qty, err := parseQuantity(parts[3])
		if err != nil {
			return fmt.Errorf("invalid quantity: %w", err)
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.LootWreck(ctx, parts[1], parts[2], qty)
		}, ctx, 3*time.Second, cmd, format)

	case "salvage", "salvage_wreck":
		if len(parts) < 2 {
			return fmt.Errorf("usage: salvage <wreck-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.SalvageWreck(ctx, parts[1])
		}, ctx, 5*time.Second, cmd, format)

	case "tow", "tow_wreck":
		if len(parts) < 2 {
			return fmt.Errorf("usage: tow <wreck-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "tow_wreck", map[string]any{"wreck_id": parts[1]})
		}, ctx, 3*time.Second, cmd, format)

	case "use_item":
		if len(parts) < 2 {
			return fmt.Errorf("usage: use_item <item-id> [quantity]")
		}
		payload := map[string]any{"item_id": parts[1]}
		if len(parts) >= 3 {
			if n, err := strconv.Atoi(parts[2]); err == nil {
				payload["quantity"] = n
			}
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "use_item", payload)
		}, ctx, 3*time.Second, cmd, format)

	case "repair_module":
		if len(parts) < 2 {
			return fmt.Errorf("usage: repair_module <module-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "repair_module", map[string]any{"module_id": parts[1]})
		}, ctx, 3*time.Second, cmd, format)

	// === QUERIES ===
	case "status", "get_status":
		return simpleCommand(client, client.GetStatus, ctx, 2*time.Second, cmd, format)

	case "system", "get_system":
		return simpleCommand(client, client.GetSystem, ctx, 2*time.Second, cmd, format)

	case "ship", "get_ship":
		return simpleCommand(client, client.GetShip, ctx, 2*time.Second, cmd, format)

	case "skills", "get_skills":
		return simpleCommand(client, client.GetSkills, ctx, 2*time.Second, cmd, format)

	case "poi", "get_poi":
		return simpleCommand(client, client.GetPOI, ctx, 2*time.Second, cmd, format)

	case "base", "get_base":
		return simpleCommand(client, client.GetBase, ctx, 2*time.Second, cmd, format)

	case "map", "get_map":
		// Check for --system_id flag or bare force arg
		mapFlags := parseFlagArgs(parts[1:], "system_id")
		if sysID, ok := mapFlags["system_id"]; ok {
			return simpleCommand(client, func(ctx context.Context) error {
				return client.RawCommand(ctx, "get_map", map[string]any{"system_id": sysID})
			}, ctx, 5*time.Second, cmd, format)
		}
		force := len(parts) >= 2 && (parts[1] == "force" || parts[1] == "1")
		return simpleCommand(client, func(ctx context.Context) error {
			return client.GetMap(ctx, force)
		}, ctx, 5*time.Second, cmd, format)

	case "nearby", "get_nearby":
		return simpleCommand(client, client.GetNearby, ctx, 2*time.Second, cmd, format)

	case "version", "get_version":
		return simpleCommand(client, client.GetVersion, ctx, 2*time.Second, cmd, format)

	case "find_route":
		if len(parts) < 2 {
			return fmt.Errorf("usage: find_route <system-id>")
		}
		route, err := client.FindRoute(ctx, parts[1])
		if err != nil {
			return err
		}
		fmt.Println("\n📍 Route:")
		for i, step := range route {
			fmt.Printf("  %d. %s (%d jumps)\n", i+1, step.Name, step.Jumps)
		}
		return nil

	case "catalog":
		if len(parts) < 2 {
			return fmt.Errorf("usage: catalog <type> [--page N] [--page_size N] [--search text] [--category cat] [--class cls] [--empire emp] [--id id] [--tier N]")
		}
		payload := map[string]any{"type": parts[1]}
		flags := parseFlagArgs(parts[2:], "page", "page_size", "search", "category", "class", "empire", "id", "tier", "commissionable")
		for k, v := range flags {
			switch k {
			case "page", "page_size", "tier":
				if n, err := strconv.Atoi(v.(string)); err == nil {
					payload[k] = n
				}
			case "commissionable":
				payload[k] = strings.EqualFold(v.(string), "true") || v.(string) == "1"
			default:
				payload[k] = v
			}
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "catalog", payload)
		}, ctx, 2*time.Second, cmd, format)

	case "search_systems":
		if len(parts) < 2 {
			return fmt.Errorf("usage: search_systems <query>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "search_systems", map[string]any{
				"query": strings.Join(parts[1:], " "),
			})
		}, ctx, 2*time.Second, cmd, format)

	case "get_guide":
		var payload map[string]any
		if len(parts) >= 2 {
			payload = map[string]any{"guide": parts[1]}
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "get_guide", payload)
		}, ctx, 2*time.Second, cmd, format)

	case "server_help":
		payload := parseFlagArgs(parts[1:], "command", "category")
		return simpleCommand(client, func(ctx context.Context) error {
			return client.Help(ctx, payload)
		}, ctx, 2*time.Second, "help", format)

	case "get_notifications":
		payload := parseFlagArgs(parts[1:], "clear", "limit")
		if v, ok := payload["clear"]; ok {
			payload["clear"] = strings.EqualFold(v.(string), "true") || v.(string) == "1"
		}
		if v, ok := payload["limit"]; ok {
			if n, err := strconv.Atoi(v.(string)); err == nil {
				payload["limit"] = n
			}
		}
		if len(payload) == 0 {
			return simpleCommand(client, client.GetNotifications, ctx, 2*time.Second, cmd, format)
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "get_notifications", payload)
		}, ctx, 2*time.Second, cmd, format)

	case "fleet":
		if len(parts) < 2 {
			return fmt.Errorf("usage: fleet <action> [--player_id <id>]")
		}
		playerID := ""
		fleetFlags := parseFlagArgs(parts[2:], "player_id")
		if v, ok := fleetFlags["player_id"]; ok {
			playerID = v.(string)
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.Fleet(ctx, parts[1], playerID)
		}, ctx, 2*time.Second, cmd, format)

	case "set_home_base":
		if len(parts) < 2 {
			return fmt.Errorf("usage: set_home_base <base-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.SetHomeBase(ctx, parts[1])
		}, ctx, 2*time.Second, cmd, format)

	// === FACTIONS ===
	case "create_faction":
		if len(parts) < 3 {
			return fmt.Errorf("usage: create_faction <name> <tag>  (tag must be 4 chars, quote the name if it has spaces)")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.CreateFaction(ctx, map[string]any{"name": parts[1], "tag": parts[2]})
		}, ctx, 3*time.Second, cmd, format)

	case "join_faction":
		if len(parts) < 2 {
			return fmt.Errorf("usage: join_faction <faction-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.JoinFaction(ctx, parts[1])
		}, ctx, 2*time.Second, cmd, format)

	case "leave_faction":
		return simpleCommand(client, client.LeaveFaction, ctx, 2*time.Second, cmd, format)

	case "faction_info":
		// Accepts faction UUID or tag. If a short string is given, try tag lookup via faction_list.
		var payload map[string]any
		if len(parts) >= 2 {
			factionRef := parts[1]
			if len(factionRef) <= 6 {
				// Looks like a tag — normalize to uppercase and resolve to ID via faction_list.
				factionRef = strings.ToUpper(factionRef)
				if id := resolveFactionTag(client, ctx, factionRef); id != "" {
					factionRef = id
				}
			}
			payload = map[string]any{"faction_id": factionRef}
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "faction_info", payload)
		}, ctx, 2*time.Second, cmd, format)

	case "faction_list":
		payload := parseFlagArgs(parts[1:], "limit", "offset")
		for _, k := range []string{"limit", "offset"} {
			if v, ok := payload[k]; ok {
				if n, err := strconv.Atoi(v.(string)); err == nil {
					payload[k] = n
				}
			}
		}
		if len(payload) == 0 {
			payload = nil
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "faction_list", payload)
		}, ctx, 2*time.Second, cmd, format)

	case "faction_edit":
		// Usage: faction_edit [--charter "text"] [--description "text"] [--primary_color "#hex"] [--secondary_color "#hex"]
		payload := parseFlagArgs(parts[1:], "charter", "description", "primary_color", "secondary_color")
		if len(payload) == 0 {
			return fmt.Errorf("usage: faction_edit --charter \"text\" --description \"text\" --primary_color \"#hex\" --secondary_color \"#hex\"")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "faction_edit", payload)
		}, ctx, 2*time.Second, cmd, format)

	case "faction_invite":
		if len(parts) < 2 {
			return fmt.Errorf("usage: faction_invite <player-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.FactionInvite(ctx, parts[1])
		}, ctx, 2*time.Second, cmd, format)

	case "faction_kick":
		if len(parts) < 2 {
			return fmt.Errorf("usage: faction_kick <player-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.FactionKick(ctx, parts[1])
		}, ctx, 2*time.Second, cmd, format)

	case "faction_promote":
		if len(parts) < 3 {
			return fmt.Errorf("usage: faction_promote <player-id> <role-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.FactionPromote(ctx, parts[1], parts[2])
		}, ctx, 2*time.Second, cmd, format)

	case "faction_get_invites":
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "faction_get_invites", nil)
		}, ctx, 2*time.Second, cmd, format)

	case "faction_decline_invite":
		if len(parts) < 2 {
			return fmt.Errorf("usage: faction_decline_invite <faction-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "faction_decline_invite", map[string]any{"faction_id": parts[1]})
		}, ctx, 2*time.Second, cmd, format)

	case "faction_declare_war":
		if len(parts) < 2 {
			return fmt.Errorf("usage: faction_declare_war <target-faction-id> [reason]")
		}
		payload := map[string]any{"target_faction_id": parts[1]}
		if len(parts) >= 3 {
			payload["reason"] = strings.Join(parts[2:], " ")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "faction_declare_war", payload)
		}, ctx, 3*time.Second, cmd, format)

	case "faction_propose_peace":
		if len(parts) < 2 {
			return fmt.Errorf("usage: faction_propose_peace <target-faction-id> [terms]")
		}
		payload := map[string]any{"target_faction_id": parts[1]}
		if len(parts) >= 3 {
			payload["terms"] = strings.Join(parts[2:], " ")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "faction_propose_peace", payload)
		}, ctx, 3*time.Second, cmd, format)

	case "faction_accept_peace":
		if len(parts) < 2 {
			return fmt.Errorf("usage: faction_accept_peace <target-faction-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "faction_accept_peace", map[string]any{"target_faction_id": parts[1]})
		}, ctx, 3*time.Second, cmd, format)

	case "faction_set_ally":
		if len(parts) < 2 {
			return fmt.Errorf("usage: faction_set_ally <target-faction-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "faction_set_ally", map[string]any{"target_faction_id": parts[1]})
		}, ctx, 2*time.Second, cmd, format)

	case "faction_set_enemy":
		if len(parts) < 2 {
			return fmt.Errorf("usage: faction_set_enemy <target-faction-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "faction_set_enemy", map[string]any{"target_faction_id": parts[1]})
		}, ctx, 2*time.Second, cmd, format)

	case "faction_deposit_credits":
		if len(parts) < 2 {
			return fmt.Errorf("usage: faction_deposit_credits <amount>")
		}
		amount, err := strconv.Atoi(parts[1])
		if err != nil {
			return fmt.Errorf("invalid amount: %w", err)
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "faction_deposit_credits", map[string]any{"amount": amount})
		}, ctx, 3*time.Second, cmd, format)

	case "faction_withdraw_credits":
		if len(parts) < 2 {
			return fmt.Errorf("usage: faction_withdraw_credits <amount>")
		}
		amount, err := strconv.Atoi(parts[1])
		if err != nil {
			return fmt.Errorf("invalid amount: %w", err)
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "faction_withdraw_credits", map[string]any{"amount": amount})
		}, ctx, 3*time.Second, cmd, format)

	case "faction_deposit_items":
		if len(parts) < 3 {
			return fmt.Errorf("usage: faction_deposit_items <item-id> <quantity>")
		}
		qty, err := parseQuantity(parts[2])
		if err != nil {
			return fmt.Errorf("invalid quantity: %w", err)
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "faction_deposit_items", map[string]any{"item_id": parts[1], "quantity": qty})
		}, ctx, 3*time.Second, cmd, format)

	case "faction_withdraw_items":
		if len(parts) < 3 {
			return fmt.Errorf("usage: faction_withdraw_items <item-id> <quantity>")
		}
		qty, err := parseQuantity(parts[2])
		if err != nil {
			return fmt.Errorf("invalid quantity: %w", err)
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "faction_withdraw_items", map[string]any{"item_id": parts[1], "quantity": qty})
		}, ctx, 3*time.Second, cmd, format)

	case "view_faction_storage":
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "view_faction_storage", nil)
		}, ctx, 2*time.Second, cmd, format)

	case "faction_create_buy_order":
		if len(parts) < 4 {
			return fmt.Errorf("usage: faction_create_buy_order <item-id> <quantity> <price-each>")
		}
		qty, err := strconv.Atoi(parts[2])
		if err != nil {
			return fmt.Errorf("invalid quantity: %w", err)
		}
		price, err := strconv.Atoi(parts[3])
		if err != nil {
			return fmt.Errorf("invalid price: %w", err)
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "faction_create_buy_order", map[string]any{
				"item_id": parts[1], "quantity": qty, "price_each": price,
			})
		}, ctx, 3*time.Second, cmd, format)

	case "faction_create_sell_order":
		if len(parts) < 4 {
			return fmt.Errorf("usage: faction_create_sell_order <item-id> <quantity> <price-each>")
		}
		qty, err := strconv.Atoi(parts[2])
		if err != nil {
			return fmt.Errorf("invalid quantity: %w", err)
		}
		price, err := strconv.Atoi(parts[3])
		if err != nil {
			return fmt.Errorf("invalid price: %w", err)
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "faction_create_sell_order", map[string]any{
				"item_id": parts[1], "quantity": qty, "price_each": price,
			})
		}, ctx, 3*time.Second, cmd, format)

	case "faction_create_role":
		if len(parts) < 3 {
			return fmt.Errorf("usage: faction_create_role <name> <priority>")
		}
		priority, err := strconv.Atoi(parts[2])
		if err != nil {
			return fmt.Errorf("invalid priority: %w", err)
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "faction_create_role", map[string]any{"name": parts[1], "priority": priority})
		}, ctx, 2*time.Second, cmd, format)

	case "faction_edit_role":
		if len(parts) < 2 {
			return fmt.Errorf("usage: faction_edit_role <role-id> [--name \"name\"]")
		}
		payload := parseFlagArgs(parts[2:], "name")
		payload["role_id"] = parts[1]
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "faction_edit_role", payload)
		}, ctx, 2*time.Second, cmd, format)

	case "faction_delete_role":
		if len(parts) < 2 {
			return fmt.Errorf("usage: faction_delete_role <role-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "faction_delete_role", map[string]any{"role_id": parts[1]})
		}, ctx, 2*time.Second, cmd, format)

	case "faction_submit_intel":
		// Complex payload — pass remaining args as JSON literal.
		return fmt.Errorf("faction_submit_intel requires complex payload; use the generic passthrough or MCP directly")

	case "faction_submit_trade_intel":
		return fmt.Errorf("faction_submit_trade_intel requires complex payload; use the generic passthrough or MCP directly")

	case "faction_query_intel":
		payload := parseFlagArgs(parts[1:], "poi_type", "resource_type", "system_id", "system_name")
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "faction_query_intel", payload)
		}, ctx, 2*time.Second, cmd, format)

	case "faction_query_trade_intel":
		payload := parseFlagArgs(parts[1:], "base_id", "item_id", "station_name")
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "faction_query_trade_intel", payload)
		}, ctx, 2*time.Second, cmd, format)

	case "faction_intel_status":
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "faction_intel_status", nil)
		}, ctx, 2*time.Second, cmd, format)

	case "faction_trade_intel_status":
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "faction_trade_intel_status", nil)
		}, ctx, 2*time.Second, cmd, format)

	case "faction_rooms":
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "faction_rooms", nil)
		}, ctx, 2*time.Second, cmd, format)

	case "faction_visit_room":
		if len(parts) < 2 {
			return fmt.Errorf("usage: faction_visit_room <room-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "faction_visit_room", map[string]any{"room_id": parts[1]})
		}, ctx, 2*time.Second, cmd, format)

	case "faction_write_room":
		payload := parseFlagArgs(parts[1:], "room_id", "name", "description", "access")
		if len(payload) == 0 {
			return fmt.Errorf("usage: faction_write_room [--room_id id] --name \"name\" --description \"text\" [--access public|faction]")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "faction_write_room", payload)
		}, ctx, 2*time.Second, cmd, format)

	case "faction_delete_room":
		if len(parts) < 2 {
			return fmt.Errorf("usage: faction_delete_room <room-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "faction_delete_room", map[string]any{"room_id": parts[1]})
		}, ctx, 2*time.Second, cmd, format)

	case "faction_post_mission":
		return fmt.Errorf("faction_post_mission requires complex payload; use the generic passthrough or MCP directly")

	case "faction_list_missions":
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "faction_list_missions", nil)
		}, ctx, 2*time.Second, cmd, format)

	case "faction_cancel_mission":
		if len(parts) < 2 {
			return fmt.Errorf("usage: faction_cancel_mission <template-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "faction_cancel_mission", map[string]any{"template_id": parts[1]})
		}, ctx, 3*time.Second, cmd, format)

	// === COMMUNICATION ===
	case "chat":
		if len(parts) < 3 {
			return fmt.Errorf("usage: chat <channel> <message>")
		}
		channel := parts[1]
		var msg string
		var target string

		// Private messages require a target username: chat private <target> <message>
		if strings.EqualFold(channel, "private") {
			if len(parts) < 4 {
				return fmt.Errorf("usage: chat private <target> <message>")
			}
			target = parts[2]
			msg = strings.Join(parts[3:], " ")
		} else {
			// Public channels: chat local|system|faction <message>
			msg = strings.Join(parts[2:], " ")
		}

		return simpleCommand(client, func(ctx context.Context) error {
			return client.Chat(ctx, channel, msg, target)
		}, ctx, 2*time.Second, cmd, format)

	case "chat_history", "get_chat_history":
		if len(parts) < 2 {
			return fmt.Errorf("usage: chat_history <channel> [--target_id <username>]")
		}
		channel := parts[1]
		// Parse optional --target_id flag (required for private channel)
		flagArgs := parseFlagArgs(parts[2:], "target_id", "before", "limit")
		payload := make(map[string]any)
		if targetID, ok := flagArgs["target_id"]; ok {
			payload["target_id"] = targetID
		}
		if before, ok := flagArgs["before"]; ok {
			payload["before"] = before
		}
		if limitStr, ok := flagArgs["limit"]; ok {
			if n, err := strconv.Atoi(limitStr.(string)); err == nil {
				payload["limit"] = n
			}
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.GetChatHistory(ctx, channel, payload)
		}, ctx, 2*time.Second, cmd, format)

	case "send_gift":
		// Usage: send_gift <recipient> <item_id> <quantity> [--message "text"]
		//        send_gift <recipient> credits <amount> [--message "text"]
		//        send_gift <recipient> ship <ship_id> [--message "text"]
		if len(parts) < 4 {
			return fmt.Errorf("usage: send_gift <recipient> <item_id> <quantity> [--message \"text\"]\n" +
				"       send_gift <recipient> credits <amount>\n" +
				"       send_gift <recipient> ship <ship_id>")
		}
		payload := map[string]any{"recipient": parts[1]}
		switch parts[2] {
		case "credits":
			amount, err := parseQuantity(parts[3])
			if err != nil {
				return fmt.Errorf("invalid credits amount: %w", err)
			}
			payload["credits"] = amount
		case "ship":
			payload["ship_id"] = parts[3]
		default:
			qty, err := parseQuantity(parts[3])
			if err != nil {
				return fmt.Errorf("invalid quantity: %w", err)
			}
			payload["item_id"] = parts[2]
			payload["quantity"] = qty
		}
		// Parse optional --message flag
		msgArgs := parseFlagArgs(parts[4:], "message")
		if msg, ok := msgArgs["message"]; ok {
			payload["message"] = msg
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "send_gift", payload)
		}, ctx, 3*time.Second, cmd, format)

	// === FORUM ===
	case "forum_list":
		page := 1
		if len(parts) >= 2 {
			var err error
			page, err = strconv.Atoi(parts[1])
			if err != nil {
				return fmt.Errorf("invalid page number: %w", err)
			}
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.ForumList(ctx, page)
		}, ctx, 2*time.Second, cmd, format)

	case "forum_thread":
		if len(parts) < 2 {
			return fmt.Errorf("usage: forum_thread <thread-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.ForumGetThread(ctx, parts[1])
		}, ctx, 2*time.Second, cmd, format)

	case "forum_create_thread":
		if len(parts) < 3 {
			return fmt.Errorf("usage: forum_create_thread <title> <content> [--category <cat>]")
		}
		// Find --category flag; everything else after title is content
		title := parts[1]
		category := ""
		var contentParts []string
		for i := 2; i < len(parts); i++ {
			if parts[i] == "--category" && i+1 < len(parts) {
				i++
				category = parts[i]
			} else {
				contentParts = append(contentParts, parts[i])
			}
		}
		content := strings.Join(contentParts, " ")
		return simpleCommand(client, func(ctx context.Context) error {
			return client.ForumCreateThread(ctx, title, content, category)
		}, ctx, 3*time.Second, cmd, format)

	case "forum_reply":
		if len(parts) < 3 {
			return fmt.Errorf("usage: forum_reply <thread-id> <content>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.ForumReply(ctx, parts[1], strings.Join(parts[2:], " "))
		}, ctx, 3*time.Second, cmd, format)

	case "forum_upvote":
		if len(parts) < 2 {
			return fmt.Errorf("usage: forum_upvote <thread-id> [--reply_id <id>]")
		}
		replyID := ""
		upvoteFlags := parseFlagArgs(parts[2:], "reply_id")
		if v, ok := upvoteFlags["reply_id"]; ok {
			replyID = v.(string)
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.ForumUpvote(ctx, parts[1], replyID)
		}, ctx, 2*time.Second, cmd, format)

	case "forum_delete_thread":
		if len(parts) < 2 {
			return fmt.Errorf("usage: forum_delete_thread <thread-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.ForumDeleteThread(ctx, parts[1])
		}, ctx, 3*time.Second, cmd, format)

	case "forum_delete_reply":
		if len(parts) < 2 {
			return fmt.Errorf("usage: forum_delete_reply <reply-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.ForumDeleteReply(ctx, parts[1])
		}, ctx, 3*time.Second, cmd, format)

	// === NOTES ===
	case "notes", "get_notes":
		return simpleCommand(client, client.GetNotes, ctx, 2*time.Second, cmd, format)

	case "create_note":
		if len(parts) < 3 {
			return fmt.Errorf("usage: create_note <title> <content>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.CreateNote(ctx, parts[1], strings.Join(parts[2:], " "))
		}, ctx, 2*time.Second, cmd, format)

	case "read_note":
		if len(parts) < 2 {
			return fmt.Errorf("usage: read_note <note-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "read_note", map[string]any{"note_id": parts[1]})
		}, ctx, 2*time.Second, cmd, format)

	case "write_note":
		if len(parts) < 3 {
			return fmt.Errorf("usage: write_note <note-id> <content>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.WriteNote(ctx, parts[1], strings.Join(parts[2:], " "))
		}, ctx, 2*time.Second, cmd, format)

	// === MISSIONS ===
	case "missions", "get_missions":
		return simpleCommand(client, client.GetMissions, ctx, 2*time.Second, cmd, format)

	case "accept_mission":
		if len(parts) < 2 {
			return fmt.Errorf("usage: accept_mission <mission-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.AcceptMission(ctx, parts[1])
		}, ctx, 2*time.Second, cmd, format)

	case "complete_mission":
		if len(parts) < 2 {
			return fmt.Errorf("usage: complete_mission <mission-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "complete_mission", map[string]any{"mission_id": parts[1]})
		}, ctx, 3*time.Second, cmd, format)

	case "abandon_mission":
		if len(parts) < 2 {
			return fmt.Errorf("usage: abandon_mission <mission-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "abandon_mission", map[string]any{"mission_id": parts[1]})
		}, ctx, 3*time.Second, cmd, format)

	case "decline_mission":
		if len(parts) < 2 {
			return fmt.Errorf("usage: decline_mission <template-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "decline_mission", map[string]any{"template_id": parts[1]})
		}, ctx, 3*time.Second, cmd, format)

	case "view_completed_mission":
		if len(parts) < 2 {
			return fmt.Errorf("usage: view_completed_mission <template-id>")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "view_completed_mission", map[string]any{"template_id": parts[1]})
		}, ctx, 2*time.Second, cmd, format)

	// === ACTION LOG ===
	case "get_action_log", "action_log":
		payload := parseFlagArgs(parts[1:], "category", "faction_id", "page", "page_size")
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "get_action_log", payload)
		}, ctx, 2*time.Second, cmd, format)

	// === CAPTAIN'S LOG ===
	case "log":
		if len(parts) < 2 {
			return fmt.Errorf("usage: log <entry>")
		}
		entry := strings.Join(parts[1:], " ")
		return simpleCommand(client, func(ctx context.Context) error {
			return client.CaptainsLogAdd(ctx, entry)
		}, ctx, 2*time.Second, cmd, format)

	case "captains_log_get":
		if len(parts) < 2 {
			return fmt.Errorf("usage: captains_log_get <index>")
		}
		idx, err := strconv.Atoi(parts[1])
		if err != nil {
			return fmt.Errorf("invalid index: %w", err)
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "captains_log_get", map[string]any{"index": idx})
		}, ctx, 2*time.Second, cmd, format)

	case "captains_log_list":
		var payload map[string]any
		if len(parts) >= 2 {
			if idx, err := strconv.Atoi(parts[1]); err == nil {
				payload = map[string]any{"index": idx}
			}
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "captains_log_list", payload)
		}, ctx, 2*time.Second, cmd, format)

	// === STATE ===
	case "state":
		printState(client)
		return nil

	case "raw":
		if len(parts) < 2 {
			return fmt.Errorf("usage: raw <key>")
		}
		data := client.GetRawJSON(parts[1])
		if len(data) == 0 {
			return fmt.Errorf("no data found for key: %s", parts[1])
		}
		fmt.Printf("\n📄 Raw JSON [%s]:\n", parts[1])
		var prettyJSON map[string]any
		if err := json.Unmarshal(data, &prettyJSON); err == nil {
			pretty, _ := json.MarshalIndent(prettyJSON, "", "  ")
			fmt.Println(string(pretty))
		} else {
			fmt.Println(string(data))
		}
		return nil

	// === STATION FACILITIES ===
	case "facility":
		if len(parts) < 2 {
			return fmt.Errorf("usage: facility <action> [facility_type] [--flag value...]\n" +
				"  actions: types, build, list, toggle, upgrades, upgrade,\n" +
				"           faction_build, faction_upgrade, faction_list, faction_toggle,\n" +
				"           transfer, personal_build, personal_decorate, personal_visit, help")
		}
		payload := map[string]any{"action": parts[1]}
		// Parse remaining args: bare positional is facility_type,
		// --key value flags, or key=value pairs.
		facilityTypeSet := false
		for i := 2; i < len(parts); i++ {
			arg := parts[i]
			if key, ok := strings.CutPrefix(arg, "--"); ok {
				// --key value flag
				if i+1 < len(parts) {
					i++
					payload[key] = parts[i]
				}
			} else if k, v, ok := strings.Cut(arg, "="); ok {
				// key=value pair
				payload[k] = v
			} else if !facilityTypeSet {
				// bare positional = facility_type
				payload["facility_type"] = arg
				facilityTypeSet = true
			}
		}
		// Convert numeric string fields
		for _, numKey := range []string{"level", "page", "per_page"} {
			if v, ok := payload[numKey].(string); ok {
				if n, err := strconv.Atoi(v); err == nil {
					payload[numKey] = n
				}
			}
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "facility", payload)
		}, ctx, 5*time.Second, cmd, format)

	// === APPEARANCE ===
	case "set_colors":
		if len(parts) < 3 {
			return fmt.Errorf("usage: set_colors <primary-hex> <secondary-hex>  (e.g. set_colors #FF0000 #00FFFF)")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "set_colors", map[string]any{
				"primary_color":   parts[1],
				"secondary_color": parts[2],
			})
		}, ctx, 2*time.Second, cmd, format)

	case "set_anonymous":
		if len(parts) < 2 {
			return fmt.Errorf("usage: set_anonymous <true|false>")
		}
		anon := strings.EqualFold(parts[1], "true") || parts[1] == "1"
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "set_anonymous", map[string]any{
				"anonymous": anon,
			})
		}, ctx, 2*time.Second, cmd, format)

	case "set_status":
		// Usage: set_status --status_message "text" [--clan_tag "TAG"]
		payload := parseFlagArgs(parts[1:], "status_message", "clan_tag")
		if len(payload) == 0 {
			return fmt.Errorf("usage: set_status --status_message \"text\" [--clan_tag \"TAG\"]")
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, "set_status", payload)
		}, ctx, 2*time.Second, cmd, format)

	// === AUTOPILOT & EXPLORE ===
	case "autopilot", "ap":
		return autopilot(client, ctx, parts)
	case "explore":
		return explore(client, ctx)

	// === KNOWLEDGE BASE UPDATE COMMANDS ===
	case "update_system":
		return kbUpdateSystem(client, ctx)
	case "update_poi":
		return kbUpdatePOI(client, ctx)
	case "update_station", "update_base":
		return kbUpdateStation(client, ctx)
	case "update_facilities":
		return kbUpdateFacilities(client, ctx)
	case "update_all":
		return kbUpdateAll(client, ctx)

	default:
		// Generic passthrough: send any unrecognized command directly to the server.
		// Parse --key value, --key=value flags, and bare positional args.
		args := make(map[string]any)
		positional := 0
		for i := 1; i < len(parts); i++ {
			arg := parts[i]
			if key, ok := strings.CutPrefix(arg, "--"); ok {
				if k, v, ok2 := strings.Cut(key, "="); ok2 {
					args[k] = v
				} else if i+1 < len(parts) {
					i++
					args[key] = parts[i]
				}
			} else {
				positional++
				args[fmt.Sprintf("arg%d", positional)] = arg
			}
		}
		if len(args) == 0 {
			args = nil
		}
		return simpleCommand(client, func(ctx context.Context) error {
			return client.RawCommand(ctx, cmd, args)
		}, ctx, 2*time.Second, cmd, format)
	}
}

// showLastResponse prints the most recent server response.
func showLastResponse(client game.GameClient, format outputFormat, command string) {
	if raw := client.GetRawJSON("_last"); len(raw) > 0 {
		printResponse(raw, format, command)
	}
}

// simpleCommand executes a command, prints the server response, then waits.
func simpleCommand(client game.GameClient, fn func(context.Context) error, ctx context.Context, wait time.Duration, command string, format outputFormat) error {
	if err := fn(ctx); err != nil {
		// Even on error, show the server's response for debugging/JSON mode
		// The response contains: action, code, message, command, tick
		if raw := client.GetRawJSON("_last"); len(raw) > 0 {
			printResponse(raw, format, command)
		}
		return err
	}
	showLastResponse(client, format, command)
	if wait > 0 {
		time.Sleep(wait)
	}
	return nil
}

// travelEstimate holds pre-travel distance, tick, and fuel estimates.
type travelEstimate struct {
	valid    bool
	distance float64
	speed    float64
	ticks    int
	fuel     int
}

// estimateTravel estimates distance, ticks, and fuel cost for traveling to a target POI.
func estimateTravel(client game.GameClient, targetPOI string) travelEstimate {
	state := client.GetState()
	if state == nil {
		return travelEstimate{}
	}

	speed := state.Ship.Speed
	if speed <= 0 {
		return travelEstimate{}
	}

	// Find current and target POI positions.
	var curPos, targetPos *game.Position
	for i := range state.System.POIs {
		poi := &state.System.POIs[i]
		if poi.ID == state.CurrentPOI {
			curPos = &poi.Position
		}
		if poi.ID == targetPOI {
			targetPos = &poi.Position
		}
	}
	if curPos == nil || targetPos == nil {
		return travelEstimate{}
	}

	dx := targetPos.X - curPos.X
	dy := targetPos.Y - curPos.Y
	distance := math.Sqrt(dx*dx + dy*dy)
	if distance <= 0 {
		return travelEstimate{}
	}

	ticks := max(int(math.Ceil(distance/speed)), 1)

	// Estimate fuel cost from ship class data: ceil(scale^1.5 × speed × distance × 0.07)
	fuel := 0
	if raw := client.GetRawJSON("ship"); len(raw) > 0 {
		var shipResp struct {
			Class *struct {
				Scale     int `json:"scale"`
				BaseSpeed int `json:"base_speed"`
			} `json:"class"`
		}
		if err := json.Unmarshal(raw, &shipResp); err == nil && shipResp.Class != nil {
			scale := float64(shipResp.Class.Scale)
			spd := float64(shipResp.Class.BaseSpeed)
			if scale > 0 && spd > 0 {
				fuel = int(math.Ceil(math.Pow(scale, 1.5) * spd * distance * 0.07))
			}
		}
	}

	return travelEstimate{
		valid:    true,
		speed:    speed,
		distance: distance,
		ticks:    ticks,
		fuel:     fuel,
	}
}

// colorizeHex wraps text with ANSI 24-bit color escape sequences.
// primary sets the foreground, secondary sets the background.
// Either can be "" to skip. Colors are "#RRGGBB" hex strings.
func colorizeHex(text, primary, secondary string) string {
	if primary == "" && secondary == "" {
		return text
	}
	var prefix string
	if r, g, b, ok := parseHexColor(primary); ok {
		prefix += fmt.Sprintf("\033[38;2;%d;%d;%dm", r, g, b)
	}
	if r, g, b, ok := parseHexColor(secondary); ok {
		prefix += fmt.Sprintf("\033[48;2;%d;%d;%dm", r, g, b)
	}
	if prefix == "" {
		return text
	}
	return prefix + text + "\033[0m"
}

// parseHexColor parses a "#RRGGBB" string into RGB components.
func parseHexColor(s string) (r, g, b uint8, ok bool) {
	s = strings.TrimPrefix(s, "#")
	if len(s) != 6 {
		return 0, 0, 0, false
	}
	val, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return 0, 0, 0, false
	}
	return uint8(val >> 16), uint8(val >> 8), uint8(val), true
}

// splitArgs splits a command string into arguments, respecting double and single quotes.
// e.g. `create_faction "Covenant of the Eternal Spark" SPRK` → ["create_faction", "Covenant of the Eternal Spark", "SPRK"]
func splitArgs(s string) []string {
	var args []string
	var current strings.Builder
	var inQuote rune

	for _, r := range s {
		switch {
		case inQuote != 0:
			if r == inQuote {
				inQuote = 0
			} else {
				current.WriteRune(r)
			}
		case r == '"' || r == '\'':
			inQuote = r
		case r == ' ' || r == '\t':
			if current.Len() > 0 {
				args = append(args, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(r)
		}
	}
	if current.Len() > 0 {
		args = append(args, current.String())
	}
	return args
}

// resolveFactionTag looks up a faction tag via faction_list and returns the faction ID, or "" if not found.
func resolveFactionTag(client game.GameClient, ctx context.Context, tag string) string {
	tag = strings.ToUpper(tag)
	if err := client.RawCommand(ctx, "faction_list", nil); err != nil {
		return ""
	}
	raw := client.GetRawJSON("_last")
	if len(raw) == 0 {
		return ""
	}
	var resp struct {
		Factions []struct {
			ID  string `json:"id"`
			Tag string `json:"tag"`
		} `json:"factions"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}
	for _, f := range resp.Factions {
		if strings.EqualFold(f.Tag, tag) {
			return f.ID
		}
	}
	return ""
}

// parseFlagArgs parses --key value pairs from args, accepting only the specified keys.
// Returns a map of key→value for all matched flags.
// Attempts to convert values to integers when possible.
func parseFlagArgs(args []string, keys ...string) map[string]any {
	allowed := make(map[string]bool, len(keys))
	for _, k := range keys {
		allowed[k] = true
	}
	result := make(map[string]any)
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "--") {
			continue
		}
		key := strings.TrimPrefix(arg, "--")
		if !allowed[key] || i+1 >= len(args) {
			continue
		}
		i++
		value := args[i]

		// Try to parse as integer first
		if intVal, err := strconv.Atoi(value); err == nil {
			result[key] = intVal
		} else {
			// Keep as string if not a number
			result[key] = value
		}
	}
	return result
}

func parseQuantity(s string) (float64, error) {
	// Try parsing as float first
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		// Try parsing as int
		i, err2 := strconv.Atoi(s)
		if err2 != nil {
			return 0, fmt.Errorf("cannot parse %q as number", s)
		}
		return float64(i), nil
	}
	return f, nil
}

func printState(client game.GameClient) {
	state := client.GetState()

	// Print summary
	fmt.Printf("\n📊 State Summary:\n")
	fmt.Printf("  Player: %s\n", state.Player.Username)
	fmt.Printf("  Credits: %.0f\n", state.Credits)
	fmt.Printf("  Location: %s / %s\n", state.System.Name, state.CurrentPOI)
	if state.Doc {
		fmt.Printf("  Status: Docked\n")
	} else {
		fmt.Printf("  Status: In space\n")
	}
	fmt.Printf("  Hull: %.0f/%.0f\n", state.Ship.Hull, state.Ship.MaxHull)
	fmt.Printf("  Fuel: %.0f/%.0f\n", state.Ship.Fuel, state.Ship.MaxFuel)
	fmt.Printf("  Cargo: %.0f/%.0f\n", state.Ship.CargoUsed, state.Ship.CargoCapacity)

	// For full JSON, user can use 'raw' command with specific keys
	fmt.Println("\n💡 Tip: Use 'raw <key>' to see full JSON for specific data")
	fmt.Println("   Available keys: player, ship, system, poi, etc.")
}

func printHelp() {
	fmt.Println("\n📖 Available Commands:")
	fmt.Println("\n=== NAVIGATION ===")
	fmt.Println("  dock, undock              - Dock/undock from current POI")
	fmt.Println("  travel <poi>              - Travel to a POI")
	fmt.Println("  jump <system>             - Jump to another system")
	fmt.Println("  find_route <system>       - Find route to system")
	fmt.Println("  autopilot <system> [poi]  - Auto-navigate to system (and optional POI)")
	fmt.Println("  explore                   - Visit all POIs in current system (nearest-first)")

	fmt.Println("\n=== MINING & COMBAT ===")
	fmt.Println("  mine, scan, survey        - Mining and scanning operations")
	fmt.Println("  attack <target-id>        - Attack a target")
	fmt.Println("  cloak [on|off]           - Toggle cloaking device")

	fmt.Println("\n=== COMMERCE ===")
	fmt.Println("  sell <item> <qty>         - Sell items")
	fmt.Println("  buy <item> <qty>          - Buy items")
	fmt.Println("  listings, trades          - View market listings/trades")
	fmt.Println("  view_market <item>        - View market for item")
	fmt.Println("  view_orders               - View your orders")
	fmt.Println("  create_sell_order <item> <qty> <price>  - Create sell order")
	fmt.Println("  create_buy_order <item> <qty> <price>   - Create buy order")

	fmt.Println("\n=== CRAFTING ===")
	fmt.Println("  craft <recipe> <qty>      - Craft items")
	fmt.Println("  recipes                   - Get available recipes")

	fmt.Println("\n=== SHIP ===")
	fmt.Println("  refuel, repair            - Refuel and repair ship")
	fmt.Println("  install, install_mod <item>  - Install equipment")
	fmt.Println("  uninstall, uninstall_mod <module> - Uninstall module")
	fmt.Println("  buy_ship <class>          - Buy a new ship")
	fmt.Println("  browse_ships [--base_id X] - Browse ships for sale (at current or specified base)")
	fmt.Println("  list_ships                - List your ships")
	fmt.Println("  switch_ship <ship-id>     - Switch to another ship")
	fmt.Println("  sell_ship <ship-id>       - Sell a ship")

	fmt.Println("\n=== CARGO & STORAGE ===")
	fmt.Println("  cargo                     - View ship cargo")
	fmt.Println("  deposit <item> <qty>      - Deposit items to storage")
	fmt.Println("  deposit_all               - Deposit all items")
	fmt.Println("  withdraw <item> <qty>     - Withdraw items from storage")
	fmt.Println("  storage, storage_at <id>  - View storage")
	fmt.Println("  jettison <item> <qty>     - Jettison cargo")

	fmt.Println("\n=== WRECKS ===")
	fmt.Println("  wrecks                    - List nearby wrecks")
	fmt.Println("  loot <wreck> <item> <qty> - Loot from wreck")
	fmt.Println("  salvage <wreck>           - Salvage entire wreck")

	fmt.Println("\n=== QUERIES ===")
	fmt.Println("  status, system, ship      - Get current status/system/ship")
	fmt.Println("  skills, poi, base         - Get skills/POI/base info")
	fmt.Println("  map, nearby, version      - Get map/nearby/version")
	fmt.Println("  state                     - Show state summary")
	fmt.Println("  raw <key>                 - Show raw JSON for key")

	fmt.Println("\n=== FACTIONS ===")
	fmt.Println("  create_faction <name> <tag>  - Create a faction (tag = 4 chars)")
	fmt.Println("  join_faction <id>            - Join a faction")
	fmt.Println("  leave_faction                - Leave current faction")
	fmt.Println("  faction_info [faction-id]     - View faction details")
	fmt.Println("  faction_list                  - List all factions")
	fmt.Println("  faction_edit --description \"text\" --charter \"text\"")
	fmt.Println("  faction_invite <player-id>    - Invite a player")
	fmt.Println("  faction_kick <player-id>      - Kick a member")
	fmt.Println("  faction_promote <player> <role> - Promote/demote member")
	fmt.Println("  faction_get_invites           - View pending invitations")
	fmt.Println("  faction_decline_invite <id>   - Decline invitation")
	fmt.Println("  faction_declare_war <id> [reason]  - Declare war")
	fmt.Println("  faction_propose_peace <id> [terms] - Propose peace")
	fmt.Println("  faction_accept_peace <id>     - Accept peace proposal")
	fmt.Println("  faction_set_ally <id>         - Mark faction as ally")
	fmt.Println("  faction_set_enemy <id>        - Mark faction as enemy")
	fmt.Println("  faction_deposit_credits <amt> - Deposit credits to treasury")
	fmt.Println("  faction_withdraw_credits <amt> - Withdraw from treasury")
	fmt.Println("  faction_deposit_items <item> <qty>  - Deposit to faction storage")
	fmt.Println("  faction_withdraw_items <item> <qty> - Withdraw from faction storage")
	fmt.Println("  view_faction_storage          - View faction storage")
	fmt.Println("  faction_create_buy_order <item> <qty> <price>  - Faction buy order")
	fmt.Println("  faction_create_sell_order <item> <qty> <price> - Faction sell order")
	fmt.Println("  faction_rooms                 - List faction rooms")
	fmt.Println("  faction_visit_room <id>       - Visit a room")
	fmt.Println("  faction_write_room --name \"n\" --description \"d\" [--room_id id]")
	fmt.Println("  faction_delete_room <id>      - Delete a room")
	fmt.Println("  faction_query_intel --system_name \"name\"  - Query intel DB")
	fmt.Println("  faction_query_trade_intel --item_id \"id\"  - Query trade intel")
	fmt.Println("  faction_intel_status           - Intel coverage stats")
	fmt.Println("  faction_trade_intel_status      - Trade intel coverage")
	fmt.Println("  faction_create_role <name> <priority> - Create role")
	fmt.Println("  faction_edit_role <id> [--name \"n\"]    - Edit role")
	fmt.Println("  faction_delete_role <id>       - Delete role")
	fmt.Println("  faction_list_missions          - List faction missions")
	fmt.Println("  faction_cancel_mission <id>    - Cancel a faction mission")

	fmt.Println("\n=== COMMUNICATION ===")
	fmt.Println("  chat <channel> <msg>                    - Send chat message")
	fmt.Println("  chat private <target> <msg>            - Send private message")
	fmt.Println("  chat_history <channel>                  - Get chat history")
	fmt.Println("  chat_history private --target_id <name> - Get private messages")
	fmt.Println("  send_gift <recipient> <item_id> <qty>  - Send items")
	fmt.Println("  send_gift <recipient> credits <amount> - Send credits")
	fmt.Println("  send_gift <recipient> ship <ship_id>   - Send ship")

	fmt.Println("\n=== FORUM ===")
	fmt.Println("  forum_list [page]         - List forum threads")
	fmt.Println("  forum_thread <id>         - Get forum thread")

	fmt.Println("\n=== KNOWLEDGE BASE ===")
	fmt.Println("  update_system             - Save current system data to KB")
	fmt.Println("  update_poi                - Save current POI data to KB")
	fmt.Println("  update_station            - Save base, market, ships to KB (must be docked)")
	fmt.Println("  update_facilities         - Save facility details to KB (must be docked)")
	fmt.Println("  update_all                - Run all update commands for current location")

	fmt.Println("\n=== OTHER ===")
	fmt.Println("  log <entry>               - Add captain's log entry")
	fmt.Println("  notes                     - Get your notes")
	fmt.Println("  missions, accept_mission  - Mission commands")
	fmt.Println("  action_log [--category X] [--page N] - Action history")
	fmt.Println("  loop [-f] <count> <command> - Repeat a command N times (-f to continue on errors)")
	fmt.Println("  history                   - Show last 25 commands (persisted across sessions)")
	fmt.Println("  set_format <mode>         - Set output: raw, json, or styled")
	fmt.Println("  help                      - Show this help")
	fmt.Println("  exit, quit                - Exit terminal")
	fmt.Println()
	fmt.Println("📝 All commands are case-insensitive")
	fmt.Println()
}

// chatPoller periodically polls chat channels and prints new messages.
type chatPoller struct {
	client   game.GameClient
	ctx      context.Context
	cancel   context.CancelFunc
	seen     map[string]bool // Message IDs already displayed.
	mu       sync.Mutex
	username string // Our own username, to skip own messages.
}

// chatChannels are the channels to poll. Private is omitted since it requires a target_id.
var chatChannels = []string{"system", "local", "faction"}

// channelColors maps channel names to ANSI color codes for display.
var channelColors = map[string]string{
	"system":  "\033[36m",  // cyan
	"local":   "\033[33m",  // yellow
	"faction": "\033[35m",  // magenta
	"private": "\033[32m",  // green
}

func newChatPoller(client game.GameClient, ctx context.Context, username string) *chatPoller {
	pollCtx, cancel := context.WithCancel(ctx)
	return &chatPoller{
		client:   client,
		ctx:      pollCtx,
		cancel:   cancel,
		seen:     make(map[string]bool),
		username: username,
	}
}

func (cp *chatPoller) start() {
	// Seed seen messages so we don't replay history on startup.
	cp.seedSeen()

	go func() {
		ticker := time.NewTicker(game.SleepLong) // Poll chat once per minute
		defer ticker.Stop()
		for {
			select {
			case <-cp.ctx.Done():
				return
			case <-ticker.C:
				cp.poll()
			}
		}
	}()
}

func (cp *chatPoller) stop() {
	cp.cancel()
}

// seedSeen fetches current history for each channel and marks all messages as seen.
func (cp *chatPoller) seedSeen() {
	for _, ch := range chatChannels {
		msgs := cp.fetchMessages(ch)
		cp.mu.Lock()
		for _, m := range msgs {
			cp.seen[m.ID] = true
		}
		cp.mu.Unlock()
	}
}

// poll fetches new messages from all channels and prints them.
func (cp *chatPoller) poll() {
	for _, ch := range chatChannels {
		msgs := cp.fetchMessages(ch)
		if len(msgs) == 0 {
			continue
		}

		// Messages come newest-first; reverse to print chronologically.
		slices.Reverse(msgs)

		// Get current system ID for filtering system/local chat.
		currentSystemID := ""
		if globalClient != nil {
			if state := globalClient.GetState(); state != nil {
				currentSystemID = state.System.ID
			}
		}

		cp.mu.Lock()
		for _, m := range msgs {
			if cp.seen[m.ID] {
				continue
			}
			cp.seen[m.ID] = true

			// Skip our own messages.
			if strings.EqualFold(m.Sender, cp.username) {
				continue
			}

			// Filter system/local messages by target system.
			if (ch == "system" || ch == "local") && m.TargetID != "" && currentSystemID != "" {
				if !strings.EqualFold(m.TargetID, currentSystemID) {
					continue
				}
			}

			// Debug: dump full JSON for specific senders to investigate filtering.
			if m.Sender == "N Nagata" || m.Sender == "GunnyDraper" || m.Sender == "Chrisjen Avasarala" {
				raw, _ := json.MarshalIndent(m, "  ", "  ")
				fmt.Printf("\r  DEBUG POLLER [%s]:\n  %s\n", m.Sender, string(raw))
			}

			color := channelColors[ch]
			reset := "\033[0m"
			fmt.Printf("\r%s[%s]%s %s: %s\n", color, ch, reset, m.Sender, m.Content)
		}
		cp.mu.Unlock()
	}
}

func (cp *chatPoller) fetchMessages(channel string) []serverapi.ChatMessage {
	if err := cp.client.GetChatHistory(cp.ctx, channel, map[string]any{"limit": 20}); err != nil {
		return nil
	}
	raw := cp.client.GetRawJSON("_last")
	if len(raw) == 0 {
		return nil
	}
	var resp struct {
		Messages []serverapi.ChatMessage `json:"messages"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil
	}
	return resp.Messages
}
