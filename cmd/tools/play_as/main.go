// Command: play_as
// Usage: play_as <agent-id>
//
// Interactive game terminal for playing as an agent using MCP transport.
// Provides a shell-like prompt for sending game commands and viewing responses.

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// Output format for server responses.
type outputFormat string

const (
	formatRaw    outputFormat = "raw"
	formatJSON   outputFormat = "json"
	formatStyled outputFormat = "styled"
)

func main() {
	debug := flag.Bool("debug", false, "Enable debug logging (show sent/received JSON)")
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

	// Show initial status
	fmt.Println("\n╔════════════════════════════════════════════════════════════════════╗")
	fmt.Println("║                    SPACE MOLT GAME TERMINAL                      ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════════╝")
	fmt.Printf("\nLogged in as: %s\n", creds.Username)
	fmt.Printf("Empire: %s\n", creds.Empire)
	fmt.Println("\nType 'help' for available commands, 'exit' or 'quit' to leave.")
	fmt.Println()

	// Run REPL loop
	runREPL(client, ctx)
}

func printUsage() {
	fmt.Println("Usage: play_as [flags] <agent-id>")
	fmt.Println("Example: play_as explorer-1")
	fmt.Println("  play_as --debug explorer-1")
	fmt.Println("\nFlags:")
	fmt.Println("  --debug    Enable debug logging (show sent/received JSON)")
	fmt.Println("\nThis tool provides an interactive terminal for playing Spacemolt.")
	fmt.Println("All commands are case-insensitive. Use 'help' to see available commands.")
}

func runREPL(client game.GameClient, ctx context.Context) {
	reader := bufio.NewReader(os.Stdin)
	format := formatJSON

	for {
		// Show prompt
		fmt.Print("$ ")

		// Read input
		line, err := reader.ReadString('\n')
		if err != nil {
			fmt.Printf("Error reading input: %v\n", err)
			continue
		}

		// Trim whitespace
		cmd := strings.TrimSpace(line)
		if cmd == "" {
			continue
		}

		// Parse command
		parts := strings.Fields(cmd)
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

		// Execute command
		startTime := time.Now()
		if err := executeCommand(client, ctx, parts); err != nil {
			fmt.Printf("❌ Error: %v\n", err)
		} else {
			duration := time.Since(startTime)
			// Print the last response from the server
			if raw := client.GetRawJSON("_last"); len(raw) > 0 {
				printResponse(raw, format, command)
			}
			fmt.Printf("✓ Completed in %v\n", duration)
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

// formatFloat formats a float64 nicely — as integer if whole, otherwise with decimals.
func formatFloat(f float64) string {
	if f == float64(int64(f)) {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'f', 2, 64)
}

func executeCommand(client game.GameClient, ctx context.Context, parts []string) error {
	cmd := strings.ToLower(parts[0])

	fmt.Printf("▶ Executing: %s %s\n", cmd, strings.Join(parts[1:], " "))

	switch cmd {
	// === NAVIGATION ===
	case "undock":
		return simpleCommand(client.Undock, ctx, 12*time.Second)

	case "dock":
		return simpleCommand(client.Dock, ctx, 3*time.Second)

	case "travel":
		if len(parts) < 2 {
			return fmt.Errorf("usage: travel <poi-id>")
		}
		target := strings.Join(parts[1:], " ")
		_, err := client.Travel(ctx, target)
		if err != nil {
			return err
		}
		time.Sleep(12 * time.Second)
		return nil

	case "jump":
		if len(parts) < 2 {
			return fmt.Errorf("usage: jump <system-id>")
		}
		_, err := client.Jump(ctx, parts[1])
		if err != nil {
			return err
		}
		time.Sleep(20 * time.Second)
		return nil

	// === MINING & SCANNING ===
	case "mine":
		return simpleCommand(client.Mine, ctx, 12*time.Second)

	case "scan":
		return simpleCommand(client.Scan, ctx, 3*time.Second)

	case "survey":
		return simpleCommand(client.SurveySystem, ctx, 15*time.Second)

	// === COMBAT ===
	case "attack":
		if len(parts) < 2 {
			return fmt.Errorf("usage: attack <target-id>")
		}
		return simpleCommand(func(ctx context.Context) error {
			return client.Attack(ctx, parts[1])
		}, ctx, 3*time.Second)

	case "cloak":
		enable := len(parts) >= 2 && (parts[1] == "on" || parts[1] == "true" || parts[1] == "1")
		return simpleCommand(func(ctx context.Context) error {
			return client.Cloak(ctx, enable)
		}, ctx, 2*time.Second)

	// === COMMERCE ===
	case "sell":
		if len(parts) < 3 {
			return fmt.Errorf("usage: sell <item-id> <quantity>")
		}
		qty, err := parseQuantity(parts[2])
		if err != nil {
			return fmt.Errorf("invalid quantity: %w", err)
		}
		return simpleCommand(func(ctx context.Context) error {
			return client.Sell(ctx, parts[1], qty)
		}, ctx, 3*time.Second)

	case "sell_all_bulk":
		return simpleCommand(func(ctx context.Context) error {
			return client.SellAllBulk(ctx, nil)
		}, ctx, 5*time.Second)

	case "buy":
		if len(parts) < 3 {
			return fmt.Errorf("usage: buy <item-id> <quantity>")
		}
		qty, err := parseQuantity(parts[2])
		if err != nil {
			return fmt.Errorf("invalid quantity: %w", err)
		}
		return simpleCommand(func(ctx context.Context) error {
			return client.Buy(ctx, parts[1], qty)
		}, ctx, 3*time.Second)

	case "listings", "get_listings":
		return simpleCommand(client.GetListings, ctx, 2*time.Second)

	case "trades", "get_trades":
		return simpleCommand(client.GetTrades, ctx, 2*time.Second)

	case "view_market":
		if len(parts) < 2 {
			return fmt.Errorf("usage: view_market <item-id>")
		}
		return simpleCommand(func(ctx context.Context) error {
			return client.ViewMarket(ctx, parts[1])
		}, ctx, 2*time.Second)

	case "view_orders":
		return simpleCommand(client.ViewOrders, ctx, 2*time.Second)

	case "create_sell_order":
		return fmt.Errorf("create_sell_order requires JSON payload: use 'help' for format")

	case "create_buy_order":
		return fmt.Errorf("create_buy_order requires JSON payload: use 'help' for format")

	// === CRAFTING ===
	case "craft":
		if len(parts) < 3 {
			return fmt.Errorf("usage: craft <recipe-id> <quantity>")
		}
		qty, err := strconv.Atoi(parts[2])
		if err != nil {
			return fmt.Errorf("invalid quantity: %w", err)
		}
		return simpleCommand(func(ctx context.Context) error {
			return client.CraftWithQuantity(ctx, parts[1], qty)
		}, ctx, 5*time.Second)

	case "recipes", "get_recipes":
		return simpleCommand(client.GetRecipes, ctx, 2*time.Second)

	// === SHIP MAINTENANCE ===
	case "refuel":
		return simpleCommand(client.Refuel, ctx, 3*time.Second)

	case "repair":
		return simpleCommand(client.Repair, ctx, 3*time.Second)

	case "install":
		if len(parts) < 2 {
			return fmt.Errorf("usage: install <item-id>")
		}
		return simpleCommand(func(ctx context.Context) error {
			return client.Install(ctx, parts[1])
		}, ctx, 3*time.Second)

	case "uninstall":
		if len(parts) < 2 {
			return fmt.Errorf("usage: uninstall <module-id>")
		}
		return simpleCommand(func(ctx context.Context) error {
			return client.UninstallMod(ctx, parts[1])
		}, ctx, 3*time.Second)

	case "buy_ship":
		if len(parts) < 2 {
			return fmt.Errorf("usage: buy_ship <ship-class>")
		}
		return simpleCommand(func(ctx context.Context) error {
			return client.BuyShip(ctx, parts[1])
		}, ctx, 5*time.Second)

	case "list_ships":
		return simpleCommand(client.ListShips, ctx, 2*time.Second)

	case "switch_ship":
		if len(parts) < 2 {
			return fmt.Errorf("usage: switch_ship <ship-id>")
		}
		return simpleCommand(func(ctx context.Context) error {
			return client.SwitchShip(ctx, parts[1])
		}, ctx, 5*time.Second)

	case "sell_ship":
		if len(parts) < 2 {
			return fmt.Errorf("usage: sell_ship <ship-id>")
		}
		return simpleCommand(func(ctx context.Context) error {
			return client.SellShip(ctx, parts[1])
		}, ctx, 3*time.Second)

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
		return simpleCommand(func(ctx context.Context) error {
			return client.BuyInsurance(ctx, ticks)
		}, ctx, 2*time.Second)

	case "claim_insurance":
		return simpleCommand(client.ClaimInsurance, ctx, 3*time.Second)

	// === CARGO & STORAGE ===
	case "cargo", "get_cargo":
		return simpleCommand(client.GetCargo, ctx, 2*time.Second)

	case "deposit":
		if len(parts) < 3 {
			return fmt.Errorf("usage: deposit <item-id> <quantity>")
		}
		qty, err := parseQuantity(parts[2])
		if err != nil {
			return fmt.Errorf("invalid quantity: %w", err)
		}
		return simpleCommand(func(ctx context.Context) error {
			return client.DepositItems(ctx, parts[1], qty)
		}, ctx, 3*time.Second)

	case "deposit_all":
		return simpleCommand(client.DepositAllItems, ctx, 5*time.Second)

	case "withdraw":
		if len(parts) < 3 {
			return fmt.Errorf("usage: withdraw <item-id> <quantity>")
		}
		qty, err := parseQuantity(parts[2])
		if err != nil {
			return fmt.Errorf("invalid quantity: %w", err)
		}
		return simpleCommand(func(ctx context.Context) error {
			return client.WithdrawItems(ctx, parts[1], qty)
		}, ctx, 3*time.Second)

	case "storage", "view_storage":
		return simpleCommand(client.ViewStorage, ctx, 2*time.Second)

	case "storage_at":
		if len(parts) < 2 {
			return fmt.Errorf("usage: storage_at <station-id>")
		}
		return simpleCommand(func(ctx context.Context) error {
			return client.ViewStorageAt(ctx, parts[1])
		}, ctx, 2*time.Second)

	case "jettison":
		if len(parts) < 3 {
			return fmt.Errorf("usage: jettison <item-id> <quantity>")
		}
		qty, err := parseQuantity(parts[2])
		if err != nil {
			return fmt.Errorf("invalid quantity: %w", err)
		}
		return simpleCommand(func(ctx context.Context) error {
			return client.Jettison(ctx, parts[1], qty)
		}, ctx, 2*time.Second)

	// === WRECKS ===
	case "wrecks", "get_wrecks":
		return simpleCommand(client.GetWrecks, ctx, 2*time.Second)

	case "loot":
		if len(parts) < 4 {
			return fmt.Errorf("usage: loot <wreck-id> <item-id> <quantity>")
		}
		qty, err := parseQuantity(parts[3])
		if err != nil {
			return fmt.Errorf("invalid quantity: %w", err)
		}
		return simpleCommand(func(ctx context.Context) error {
			return client.LootWreck(ctx, parts[1], parts[2], qty)
		}, ctx, 3*time.Second)

	case "salvage":
		if len(parts) < 2 {
			return fmt.Errorf("usage: salvage <wreck-id>")
		}
		return simpleCommand(func(ctx context.Context) error {
			return client.SalvageWreck(ctx, parts[1])
		}, ctx, 5*time.Second)

	// === QUERIES ===
	case "status", "get_status":
		return simpleCommand(client.GetStatus, ctx, 2*time.Second)

	case "system", "get_system":
		return simpleCommand(client.GetSystem, ctx, 2*time.Second)

	case "ship", "get_ship":
		return simpleCommand(client.GetShip, ctx, 2*time.Second)

	case "skills", "get_skills":
		return simpleCommand(client.GetSkills, ctx, 2*time.Second)

	case "poi", "get_poi":
		return simpleCommand(client.GetPOI, ctx, 2*time.Second)

	case "base", "get_base":
		return simpleCommand(client.GetBase, ctx, 2*time.Second)

	case "map", "get_map":
		force := len(parts) >= 2 && (parts[1] == "force" || parts[1] == "1")
		return simpleCommand(func(ctx context.Context) error {
			return client.GetMap(ctx, force)
		}, ctx, 5*time.Second)

	case "nearby", "get_nearby":
		return simpleCommand(client.GetNearby, ctx, 2*time.Second)

	case "version", "get_version":
		return simpleCommand(client.GetVersion, ctx, 2*time.Second)

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

	// === FACTIONS ===
	case "create_faction":
		return fmt.Errorf("create_faction requires JSON payload: use 'help' for format")

	case "join_faction":
		if len(parts) < 2 {
			return fmt.Errorf("usage: join_faction <faction-id>")
		}
		return simpleCommand(func(ctx context.Context) error {
			return client.JoinFaction(ctx, parts[1])
		}, ctx, 2*time.Second)

	case "leave_faction":
		return simpleCommand(client.LeaveFaction, ctx, 2*time.Second)

	// === COMMUNICATION ===
	case "chat":
		if len(parts) < 3 {
			return fmt.Errorf("usage: chat <channel> <message>")
		}
		msg := strings.Join(parts[2:], " ")
		return simpleCommand(func(ctx context.Context) error {
			return client.Chat(ctx, parts[1], msg, "")
		}, ctx, 2*time.Second)

	case "chat_history":
		if len(parts) < 2 {
			return fmt.Errorf("usage: chat_history <channel>")
		}
		return simpleCommand(func(ctx context.Context) error {
			return client.GetChatHistory(ctx, parts[1], nil)
		}, ctx, 2*time.Second)

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
		return simpleCommand(func(ctx context.Context) error {
			return client.ForumList(ctx, page)
		}, ctx, 2*time.Second)

	case "forum_thread":
		if len(parts) < 2 {
			return fmt.Errorf("usage: forum_thread <thread-id>")
		}
		return simpleCommand(func(ctx context.Context) error {
			return client.ForumGetThread(ctx, parts[1])
		}, ctx, 2*time.Second)

	// === NOTES ===
	case "notes", "get_notes":
		return simpleCommand(client.GetNotes, ctx, 2*time.Second)

	// === MISSIONS ===
	case "missions", "get_missions":
		return simpleCommand(client.GetMissions, ctx, 2*time.Second)

	case "accept_mission":
		if len(parts) < 2 {
			return fmt.Errorf("usage: accept_mission <mission-id>")
		}
		return simpleCommand(func(ctx context.Context) error {
			return client.AcceptMission(ctx, parts[1])
		}, ctx, 2*time.Second)

	// === CAPTAIN'S LOG ===
	case "log":
		if len(parts) < 2 {
			return fmt.Errorf("usage: log <entry>")
		}
		entry := strings.Join(parts[1:], " ")
		return simpleCommand(func(ctx context.Context) error {
			return client.CaptainsLogAdd(ctx, entry)
		}, ctx, 2*time.Second)

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

	default:
		return fmt.Errorf("unknown command: %s (type 'help' for available commands)", cmd)
	}
}

// simpleCommand executes a command and prints the resulting state
func simpleCommand(fn func(context.Context) error, ctx context.Context, wait time.Duration) error {
	if err := fn(ctx); err != nil {
		return err
	}
	if wait > 0 {
		time.Sleep(wait)
	}
	return nil
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

	fmt.Println("\n=== CRAFTING ===")
	fmt.Println("  craft <recipe> <qty>      - Craft items")
	fmt.Println("  recipes                   - Get available recipes")

	fmt.Println("\n=== SHIP ===")
	fmt.Println("  refuel, repair            - Refuel and repair ship")
	fmt.Println("  install <item>            - Install equipment")
	fmt.Println("  uninstall <module>        - Uninstall module")
	fmt.Println("  buy_ship <class>          - Buy a new ship")
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
	fmt.Println("  join_faction <id>         - Join a faction")
	fmt.Println("  leave_faction             - Leave current faction")

	fmt.Println("\n=== COMMUNICATION ===")
	fmt.Println("  chat <channel> <msg>      - Send chat message")
	fmt.Println("  chat_history <channel>    - Get chat history")

	fmt.Println("\n=== FORUM ===")
	fmt.Println("  forum_list [page]         - List forum threads")
	fmt.Println("  forum_thread <id>         - Get forum thread")

	fmt.Println("\n=== OTHER ===")
	fmt.Println("  log <entry>               - Add captain's log entry")
	fmt.Println("  notes                     - Get your notes")
	fmt.Println("  missions, accept_mission  - Mission commands")
	fmt.Println("  set_format <mode>         - Set output: raw, json, or styled")
	fmt.Println("  help                      - Show this help")
	fmt.Println("  exit, quit                - Exit terminal")
	fmt.Println()
	fmt.Println("📝 All commands are case-insensitive")
	fmt.Println()
}
