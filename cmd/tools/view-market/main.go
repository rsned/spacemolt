// Command view-market provides interactive access to market data.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/charmbracelet/lipgloss"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// Output format constants
const (
	formatTable = "table"
	formatJSON  = "json"
)

// Config holds the CLI configuration
type Config struct {
	DBPath string
	Limit  int
	Format string
}

// Command represents a CLI subcommand
type Command struct {
	Name        string
	Description string
	Handler     func(ctx context.Context, db *sql.DB, cfg Config, args []string) error
}

// historyEntry represents a market snapshot history entry
type historyEntry struct {
	ID          int
	SystemID    string
	SystemName  string
	StationID   string
	StationName string
	GameTick    int64
	CapturedAt  string
	AgentID     sql.NullString
}

// itemInfo represents an item seen in the market
type itemInfo struct {
	ItemID   string
	ItemType string
}

// priceEntry represents a price record for an item
type priceEntry struct {
	SystemName   string
	StationName  string
	CapturedAt   string
	ListingType  string
	PricePerUnit float64
	Quantity     float64
}

// arbitrageOpportunity represents a potential arbitrage opportunity
type arbitrageOpportunity struct {
	ItemID       string
	ItemType     string
	MinSellPrice float64
	MaxBuyPrice  float64
	Profit       float64
	ProfitMargin float64
}

// NOTE: Commands must be sorted alphabetically for BinarySearchFunc to work
var commands = []Command{
	{
		Name:        "arbitrage",
		Description: "Show arbitrage opportunities (buy low, sell high)",
		Handler:     cmdArbitrage,
	},
	{
		Name:        "history",
		Description: "Show historical market snapshots",
		Handler:     cmdHistory,
	},
	{
		Name:        "items",
		Description: "List all unique items seen across markets",
		Handler:     cmdItems,
	},
	{
		Name:        "latest",
		Description: "Show most recent market snapshot",
		Handler:     cmdLatest,
	},
	{
		Name:        "prices",
		Description: "Show price history for an item across systems",
		Handler:     cmdPrices,
	},
}

// Styles for terminal output
var (
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("cyan"))

	subHeaderStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("magenta"))

	labelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("blue"))

	valueStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("white"))

	successStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("green"))

	warningStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("yellow"))

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("red"))

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241"))

	borderStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241"))

	tableHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("cyan"))
)

func main() {
	// Define global flags
	cfg := Config{
		DBPath: "spacemolt-knowledge.db",
		Limit:  20,
		Format: formatTable,
	}

	flag.StringVar(&cfg.DBPath, "db-path", cfg.DBPath, "Path to SQLite database file")
	flag.IntVar(&cfg.Limit, "limit", cfg.Limit, "Limit number of records to show")
	flag.StringVar(&cfg.Format, "format", cfg.Format, "Output format: table, json")

	// Custom usage function
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "%s\n\n", headerStyle.Render("view-market - Interactive access to market data"))
		fmt.Fprintf(os.Stderr, "Usage:\n  view-market [global-flags] <command> [command-args]\n\n")
		fmt.Fprintf(os.Stderr, "Global Flags:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nCommands:\n")
		for _, cmd := range commands {
			fmt.Fprintf(os.Stderr, "  %-15s %s\n", cmd.Name, cmd.Description)
		}
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  view-market latest sol\n")
		fmt.Fprintf(os.Stderr, "  view-market latest sol station-1\n")
		fmt.Fprintf(os.Stderr, "  view-market history sol --limit 50\n")
		fmt.Fprintf(os.Stderr, "  view-market items\n")
		fmt.Fprintf(os.Stderr, "  view-market prices iron_ore\n")
		fmt.Fprintf(os.Stderr, "  view-market arbitrage\n")
	}

	flag.Parse()

	// Get remaining arguments
	args := flag.Args()

	if len(args) == 0 {
		flag.Usage()
		os.Exit(1)
	}

	// Find and execute command
	cmdName := args[0]
	cmdArgs := args[1:]

	cmd, found := slices.BinarySearchFunc(commands, cmdName, func(c Command, name string) int {
		return strings.Compare(c.Name, name)
	})

	if !found {
		fmt.Fprintf(os.Stderr, "%s Unknown command: %s\n\n", errorStyle.Render("Error:"), cmdName)
		flag.Usage()
		os.Exit(1)
	}

	// Open database connection
	db, err := openDatabase(cfg.DBPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s Failed to open database: %v\n", errorStyle.Render("Error:"), err)
		os.Exit(1)
	}
	defer func() { _ = db.Close() }()

	// Execute command
	ctx := context.Background()
	if err := commands[cmd].Handler(ctx, db, cfg, cmdArgs); err != nil {
		fmt.Fprintf(os.Stderr, "%s %v\n", errorStyle.Render("Error:"), err)
		os.Exit(1)
	}
}

// openDatabase opens the SQLite database connection
func openDatabase(dbPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Set busy timeout
	if _, err := db.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to set busy timeout: %w", err)
	}

	// Enable WAL mode for better concurrency
	if _, err := db.Exec("PRAGMA journal_mode = WAL"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to enable WAL mode: %w", err)
	}

	return db, nil
}

// cmdLatest shows the most recent market snapshot
func cmdLatest(ctx context.Context, db *sql.DB, cfg Config, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: view-market latest <system-id> [station-id]")
	}

	systemID := args[0]
	stationID := ""
	if len(args) >= 2 {
		stationID = args[1]
	}

	// If station ID not provided, try to find one
	if stationID == "" {
		query := `
			SELECT station_id
			FROM market_snapshots
			WHERE system_id = ?
			ORDER BY captured_at DESC
			LIMIT 1
		`
		err := db.QueryRowContext(ctx, query, systemID).Scan(&stationID)
		if err == sql.ErrNoRows {
			return fmt.Errorf("no market data found for system: %s", systemID)
		}
		if err != nil {
			return fmt.Errorf("failed to query latest station: %w", err)
		}
	}

	// Query latest snapshot
	query := `
		SELECT ms.id, ms.system_id, ms.system_name, ms.station_id, ms.station_name,
		       ms.game_tick, ms.captured_at, ms.agent_id,
		       ml.item_id, ml.item_type, ml.quantity, ml.price_per_unit,
		       ml.total_price, ml.listing_type, ml.listed_by
		FROM market_snapshots ms
		LEFT JOIN market_listings ml ON ms.id = ml.snapshot_id
		WHERE ms.system_id = ? AND ms.station_id = ?
		ORDER BY ms.captured_at DESC, ml.item_type, ml.item_id
		LIMIT 1
	`

	rows, err := db.QueryContext(ctx, query, systemID, stationID)
	if err != nil {
		return fmt.Errorf("failed to query latest snapshot: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var systemName, stationName, capturedAt string
	var gameTick int64
	var agentID sql.NullString
	listings := make([]knowledge.MarketListing, 0)

	for rows.Next() {
		var snapshotID int
		var itemID, itemType, listingType sql.NullString
		var listedBy sql.NullString
		var quantity, pricePerUnit, totalPrice sql.NullFloat64

		err := rows.Scan(
			&snapshotID, &systemID, &systemName, &stationID, &stationName,
			&gameTick, &capturedAt, &agentID,
			&itemID, &itemType, &quantity, &pricePerUnit, &totalPrice, &listingType, &listedBy,
		)
		if err != nil {
			return fmt.Errorf("failed to scan row: %w", err)
		}

		if itemID.Valid {
			listings = append(listings, knowledge.MarketListing{
				ItemID:       itemID.String,
				ItemType:     itemType.String,
				Quantity:     quantity.Float64,
				PricePerUnit: pricePerUnit.Float64,
				TotalPrice:   totalPrice.Float64,
				Type:         listingType.String,
				ListedBy:     listedBy.String,
			})
		}
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating rows: %w", err)
	}

	if len(listings) == 0 {
		return fmt.Errorf("no market data found")
	}

	// Output based on format
	switch cfg.Format {
	case formatJSON:
		result := map[string]any{
			"system_name":  systemName,
			"station_name": stationName,
			"captured_at":  capturedAt,
			"game_tick":    gameTick,
			"agent_id":     agentID.String,
			"listings":     listings,
		}
		return outputJSON(result)
	default:
		outputLatestTable(systemName, stationName, capturedAt, gameTick, agentID, listings)
		return nil
	}
}

// outputLatestTable displays latest snapshot in table format
func outputLatestTable(systemName, stationName, capturedAt string, gameTick int64, agentID sql.NullString, listings []knowledge.MarketListing) {
	fmt.Println(headerStyle.Render(fmt.Sprintf("Market: %s / %s", systemName, stationName)))
	fmt.Println(borderStyle.Render(strings.Repeat("─", 80)))

	// Metadata
	fmt.Printf("\n  %s %s\n", labelStyle.Render("Captured:"), dimStyle.Render(formatTimestamp(capturedAt)))
	fmt.Printf("  %s %s\n", labelStyle.Render("Game Tick:"), valueStyle.Render(fmt.Sprintf("%d", gameTick)))
	if agentID.Valid {
		fmt.Printf("  %s %s\n", labelStyle.Render("Agent:"), valueStyle.Render(agentID.String))
	}

	// Group listings by type and by buy/sell
	buyOrders := make(map[string][]knowledge.MarketListing)
	sellOrders := make(map[string][]knowledge.MarketListing)

	for _, listing := range listings {
		if listing.Type == "buy" {
			buyOrders[listing.ItemType] = append(buyOrders[listing.ItemType], listing)
		} else {
			sellOrders[listing.ItemType] = append(sellOrders[listing.ItemType], listing)
		}
	}

	// Display sell orders (what's available)
	if len(sellOrders) > 0 {
		fmt.Println("\n" + subHeaderStyle.Render("Sell Orders (Available)"))
		fmt.Println(borderStyle.Render(strings.Repeat("─", 80)))

		types := make([]string, 0, len(sellOrders))
		for t := range sellOrders {
			types = append(types, t)
		}
		sort.Strings(types)

		for _, itemType := range types {
			fmt.Printf("\n%s\n", successStyle.Render(itemType))
			for _, listing := range sellOrders[itemType] {
				fmt.Printf("  %s x %.2f @ %s\n",
					valueStyle.Render(listing.ItemID),
					listing.Quantity,
					successStyle.Render(fmt.Sprintf("%.2f credits", listing.PricePerUnit)))
			}
		}
	}

	// Display buy orders (what players want)
	if len(buyOrders) > 0 {
		fmt.Println("\n" + subHeaderStyle.Render("Buy Orders (Wanted)"))
		fmt.Println(borderStyle.Render(strings.Repeat("─", 80)))

		types := make([]string, 0, len(buyOrders))
		for t := range buyOrders {
			types = append(types, t)
		}
		sort.Strings(types)

		for _, itemType := range types {
			fmt.Printf("\n%s\n", warningStyle.Render(itemType))
			for _, listing := range buyOrders[itemType] {
				fmt.Printf("  %s x %.2f @ %s\n",
					valueStyle.Render(listing.ItemID),
					listing.Quantity,
					warningStyle.Render(fmt.Sprintf("%.2f credits", listing.PricePerUnit)))
			}
		}
	}

	fmt.Printf("\n%s %s listing(s)\n",
		labelStyle.Render("Total:"),
		valueStyle.Render(fmt.Sprintf("%d", len(listings))))
}

// cmdHistory shows historical market snapshots
func cmdHistory(ctx context.Context, db *sql.DB, cfg Config, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: view-market history <system-id> [station-id]")
	}

	systemID := args[0]
	stationID := ""
	if len(args) >= 2 {
		stationID = args[1]
	}

	// If station ID not provided, use latest
	if stationID == "" {
		query := `
			SELECT station_id
			FROM market_snapshots
			WHERE system_id = ?
			ORDER BY captured_at DESC
			LIMIT 1
		`
		err := db.QueryRowContext(ctx, query, systemID).Scan(&stationID)
		if err == sql.ErrNoRows {
			return fmt.Errorf("no market data found for system: %s", systemID)
		}
		if err != nil {
			return fmt.Errorf("failed to query latest station: %w", err)
		}
	}

	// Query snapshots
	query := `
		SELECT id, system_id, system_name, station_id, station_name,
		       game_tick, captured_at, agent_id
		FROM market_snapshots
		WHERE system_id = ? AND station_id = ?
		ORDER BY captured_at DESC
		LIMIT ?
	`

	rows, err := db.QueryContext(ctx, query, systemID, stationID, cfg.Limit)
	if err != nil {
		return fmt.Errorf("failed to query history: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var snapshots []historyEntry
	for rows.Next() {
		var s historyEntry
		if err := rows.Scan(&s.ID, &s.SystemID, &s.SystemName, &s.StationID, &s.StationName,
			&s.GameTick, &s.CapturedAt, &s.AgentID); err != nil {
			return fmt.Errorf("failed to scan row: %w", err)
		}
		snapshots = append(snapshots, s)
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating rows: %w", err)
	}

	// Output based on format
	switch cfg.Format {
	case formatJSON:
		return outputJSON(snapshots)
	default:
		outputHistoryTable(snapshots)
		return nil
	}
}

// outputHistoryTable displays history in table format
func outputHistoryTable(snapshots []historyEntry) {
	fmt.Println(headerStyle.Render("Market History"))
	fmt.Println(borderStyle.Render(strings.Repeat("─", 80)))

	if len(snapshots) == 0 {
		fmt.Println(dimStyle.Render("  No history found."))
		return
	}

	for _, s := range snapshots {
		fmt.Printf("\n%s %s / %s\n",
			dimStyle.Render("["+s.CapturedAt+"]"),
			subHeaderStyle.Render(s.SystemName),
			valueStyle.Render(s.StationName))
		fmt.Printf("  Tick: %d", s.GameTick)
		if s.AgentID.Valid {
			fmt.Printf(" | Agent: %s", s.AgentID.String)
		}
		fmt.Println()

		fmt.Printf("  %sSnapshot ID: %s%s\n",
			dimStyle.Render("→"),
			valueStyle.Render(fmt.Sprintf("%d", s.ID)),
			dimStyle.Render(" (run 'view-market latest "+s.SystemID+" "+s.StationID+"' for details)"))

		if len(snapshots) > 1 {
			fmt.Println(dimStyle.Render(strings.Repeat("─", 40)))
		}
	}

	fmt.Printf("\n%s %s snapshot(s)\n",
		labelStyle.Render("Total:"),
		valueStyle.Render(fmt.Sprintf("%d", len(snapshots))))
}

// cmdItems lists all unique items seen across markets
func cmdItems(ctx context.Context, db *sql.DB, cfg Config, args []string) error {
	query := `
		SELECT DISTINCT item_id, item_type
		FROM market_listings
		ORDER BY item_type, item_id
		LIMIT ?
	`

	rows, err := db.QueryContext(ctx, query, cfg.Limit*10) // Higher limit for items
	if err != nil {
		return fmt.Errorf("failed to query items: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var items []itemInfo
	for rows.Next() {
		var item itemInfo
		if err := rows.Scan(&item.ItemID, &item.ItemType); err != nil {
			return fmt.Errorf("failed to scan row: %w", err)
		}
		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating rows: %w", err)
	}

	// Output based on format
	switch cfg.Format {
	case formatJSON:
		return outputJSON(items)
	default:
		outputItemsTable(items)
		return nil
	}
}

// outputItemsTable displays items in table format
func outputItemsTable(items []itemInfo) {
	fmt.Println(headerStyle.Render("Market Items"))
	fmt.Println(borderStyle.Render(strings.Repeat("─", 80)))

	if len(items) == 0 {
		fmt.Println(dimStyle.Render("  No items found."))
		return
	}

	// Group by type
	byType := make(map[string][]string)
	for _, item := range items {
		byType[item.ItemType] = append(byType[item.ItemType], item.ItemID)
	}

	// Sort types
	types := make([]string, 0, len(byType))
	for t := range byType {
		types = append(types, t)
	}
	sort.Strings(types)

	for _, itemType := range types {
		fmt.Printf("\n%s\n", subHeaderStyle.Render(itemType))
		itemIDs := byType[itemType]
		sort.Strings(itemIDs)
		for _, itemID := range itemIDs {
			fmt.Printf("  • %s\n", valueStyle.Render(itemID))
		}
	}

	fmt.Printf("\n%s %s unique item(s)\n",
		labelStyle.Render("Total:"),
		valueStyle.Render(fmt.Sprintf("%d", len(items))))
}

// cmdPrices shows price history for an item across systems
func cmdPrices(ctx context.Context, db *sql.DB, cfg Config, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: view-market prices <item-id>")
	}

	itemID := args[0]

	// Query prices for this item across all systems
	query := `
		SELECT ms.system_name, ms.station_name, ms.captured_at,
		       ml.listing_type, ml.price_per_unit, ml.quantity
		FROM market_listings ml
		JOIN market_snapshots ms ON ml.snapshot_id = ms.id
		WHERE ml.item_id = ?
		ORDER BY ms.captured_at DESC
		LIMIT ?
	`

	rows, err := db.QueryContext(ctx, query, itemID, cfg.Limit)
	if err != nil {
		return fmt.Errorf("failed to query prices: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var prices []priceEntry
	for rows.Next() {
		var p priceEntry
		if err := rows.Scan(&p.SystemName, &p.StationName, &p.CapturedAt,
			&p.ListingType, &p.PricePerUnit, &p.Quantity); err != nil {
			return fmt.Errorf("failed to scan row: %w", err)
		}
		prices = append(prices, p)
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating rows: %w", err)
	}

	// Output based on format
	switch cfg.Format {
	case formatJSON:
		return outputJSON(prices)
	default:
		outputPricesTable(itemID, prices)
		return nil
	}
}

// outputPricesTable displays prices in table format
func outputPricesTable(itemID string, prices []priceEntry) {
	fmt.Println(headerStyle.Render(fmt.Sprintf("Price History: %s", itemID)))
	fmt.Println(borderStyle.Render(strings.Repeat("─", 80)))

	if len(prices) == 0 {
		fmt.Println(dimStyle.Render("  No price data found for this item."))
		return
	}

	for _, p := range prices {
		style := successStyle
		if p.ListingType == "buy" {
			style = warningStyle
		}

		fmt.Printf("\n%s %s / %s\n",
			dimStyle.Render("["+p.CapturedAt+"]"),
			subHeaderStyle.Render(p.SystemName),
			valueStyle.Render(p.StationName))

		fmt.Printf("  %s x %.2f @ %s\n",
			style.Render(p.ListingType),
			p.Quantity,
			style.Render(fmt.Sprintf("%.2f credits", p.PricePerUnit)))
	}

	fmt.Printf("\n%s %s price record(s)\n",
		labelStyle.Render("Total:"),
		valueStyle.Render(fmt.Sprintf("%d", len(prices))))
}

// cmdArbitrage shows arbitrage opportunities
func cmdArbitrage(ctx context.Context, db *sql.DB, cfg Config, args []string) error {
	// Find arbitrage opportunities by comparing sell prices across systems
	// For each item, find the lowest sell price and highest buy price
	query := `
		WITH LatestSnapshots AS (
			SELECT DISTINCT system_id, station_id, MAX(captured_at) as latest_captured
			FROM market_snapshots
			GROUP BY system_id, station_id
		),
		LatestListings AS (
			SELECT ml.item_id, ml.item_type, ml.listing_type, ml.price_per_unit,
			       ms.system_id, ms.system_name, ms.station_id, ms.station_name
			FROM market_listings ml
			JOIN market_snapshots ms ON ml.snapshot_id = ms.id
			JOIN LatestSnapshots ls ON ms.system_id = ls.system_id AND ms.station_id = ls.station_id
				AND ms.captured_at = ls.latest_captured
		)
		SELECT item_id, item_type,
		       MIN(CASE WHEN listing_type = 'sell' THEN price_per_unit END) as min_sell_price,
		       MAX(CASE WHEN listing_type = 'buy' THEN price_per_unit END) as max_buy_price
		FROM LatestListings
		WHERE listing_type IN ('sell', 'buy')
		GROUP BY item_id, item_type
		HAVING min_sell_price IS NOT NULL AND max_buy_price IS NOT NULL
		  AND max_buy_price > min_sell_price
		ORDER BY (max_buy_price - min_sell_price) DESC
		LIMIT ?
	`

	rows, err := db.QueryContext(ctx, query, cfg.Limit)
	if err != nil {
		return fmt.Errorf("failed to query arbitrage: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var opportunities []arbitrageOpportunity
	for rows.Next() {
		var opp arbitrageOpportunity
		if err := rows.Scan(&opp.ItemID, &opp.ItemType, &opp.MinSellPrice, &opp.MaxBuyPrice); err != nil {
			return fmt.Errorf("failed to scan row: %w", err)
		}
		opp.Profit = opp.MaxBuyPrice - opp.MinSellPrice
		opp.ProfitMargin = (opp.Profit / opp.MinSellPrice) * 100
		opportunities = append(opportunities, opp)
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating rows: %w", err)
	}

	// Output based on format
	switch cfg.Format {
	case formatJSON:
		return outputJSON(opportunities)
	default:
		outputArbitrageTable(opportunities)
		return nil
	}
}

// outputArbitrageTable displays arbitrage opportunities in table format
func outputArbitrageTable(opportunities []arbitrageOpportunity) {
	fmt.Println(headerStyle.Render("Arbitrage Opportunities"))
	fmt.Println(borderStyle.Render(strings.Repeat("─", 80)))

	if len(opportunities) == 0 {
		fmt.Println(dimStyle.Render("  No arbitrage opportunities found."))
		return
	}

	fmt.Printf("\n%s  %s  %s  %s  %s\n",
		tableHeaderStyle.Render(padRight("Item", 25)),
		tableHeaderStyle.Render(padRight("Buy At", 12)),
		tableHeaderStyle.Render(padRight("Sell At", 12)),
		tableHeaderStyle.Render(padRight("Profit", 12)),
		tableHeaderStyle.Render("Margin"))

	fmt.Println(borderStyle.Render(strings.Repeat("─", 80)))

	for _, opp := range opportunities {
		profitStyle := successStyle
		if opp.ProfitMargin < 10 {
			profitStyle = valueStyle
		} else if opp.ProfitMargin > 50 {
			profitStyle = warningStyle
		}

		fmt.Printf("%s  %s  %s  %s  %s\n",
			valueStyle.Render(padRight(opp.ItemID, 25)),
			successStyle.Render(padRight(fmt.Sprintf("%.2f", opp.MinSellPrice), 12)),
			warningStyle.Render(padRight(fmt.Sprintf("%.2f", opp.MaxBuyPrice), 12)),
			profitStyle.Render(padRight(fmt.Sprintf("%.2f", opp.Profit), 12)),
			profitStyle.Render(fmt.Sprintf("%.1f%%", opp.ProfitMargin)))
	}

	fmt.Printf("\n%s %s opportunity(ies)\n",
		labelStyle.Render("Total:"),
		valueStyle.Render(fmt.Sprintf("%d", len(opportunities))))
}

// outputJSON outputs data as JSON
func outputJSON(data any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(data); err != nil {
		return fmt.Errorf("failed to encode JSON: %w", err)
	}
	return nil
}

// formatTimestamp formats a timestamp string for display
func formatTimestamp(ts string) string {
	// Parse the timestamp
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		// Try parsing as SQLite datetime format
		t, err = time.Parse("2006-01-02 15:04:05", ts)
		if err != nil {
			return ts
		}
	}

	// Format as relative time if recent, otherwise as short date
	now := time.Now()
	diff := now.Sub(t)

	if diff < time.Minute {
		return "just now"
	}
	if diff < time.Hour {
		return fmt.Sprintf("%d min ago", int(diff.Minutes()))
	}
	if diff < 24*time.Hour {
		return fmt.Sprintf("%d hr ago", int(diff.Hours()))
	}
	if diff < 7*24*time.Hour {
		return fmt.Sprintf("%d days ago", int(diff.Hours()/24))
	}

	return t.Format("2006-01-02")
}

// padRight pads a string to the specified width
func padRight(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}
