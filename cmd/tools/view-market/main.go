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

// historyEntry represents a distinct capture timestamp for a station.
type historyEntry struct {
	StationID   string
	StationName string
	SystemID    string
	SystemName  string
	CapturedAt  string
	OrderCount  int
}

// itemInfo represents an item seen in the market
type itemInfo struct {
	ItemID   string
	ItemName string
	Category string
}

// ohlcvEntry represents a price OHLCV record for an item
type ohlcvEntry struct {
	StationID  string  `json:"station_id"`
	StationName string `json:"station_name"`
	ItemID     string  `json:"item_id"`
	Side       string  `json:"side"`
	BucketUTC  string  `json:"bucket_utc"`
	OpenPrice  float64 `json:"open_price"`
	HighPrice  float64 `json:"high_price"`
	LowPrice   float64 `json:"low_price"`
	ClosePrice float64 `json:"close_price"`
	Volume     float64 `json:"volume"`
	TradeCount int     `json:"trade_count"`
	VWAP       float64 `json:"vwap"`
}

// arbitrageOpportunity represents a potential arbitrage opportunity
type arbitrageOpportunity struct {
	ItemID       string  `json:"item_id"`
	ItemName     string  `json:"item_name"`
	MinSellPrice float64 `json:"min_sell_price"`
	MaxBuyPrice  float64 `json:"max_buy_price"`
	Profit       float64 `json:"profit"`
	ProfitMargin float64 `json:"profit_margin"`
}

// listing is a local buy/sell order for display purposes.
type listing struct {
	ItemID   string
	ItemName string
	Category string
	Side     string
	Quantity float64
	PriceEach float64
	Source   string
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
		Description: "Show historical market capture timestamps",
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
		Description: "Show OHLCV price history for an item",
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
		DBPath: "data/market.db",
		Limit:  20,
		Format: formatTable,
	}

	flag.StringVar(&cfg.DBPath, "db-path", cfg.DBPath, "Path to market SQLite database file (default: data/market.db)")
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

// cmdLatest shows the most recent market snapshot for a station.
// Args: <system-id> [station-id]
// If station-id is omitted, picks the station with the newest captured_at in
// the given system (via stations JOIN market_orders).
func cmdLatest(ctx context.Context, db *sql.DB, cfg Config, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: view-market latest <system-id> [station-id]")
	}

	systemID := args[0]
	stationID := ""
	if len(args) >= 2 {
		stationID = args[1]
	}

	// Resolve stationID from system when not provided.
	if stationID == "" {
		query := `
			SELECT mo.station_id
			FROM market_orders mo
			JOIN stations s ON s.station_id = mo.station_id
			WHERE s.system_id = ?
			ORDER BY mo.captured_at DESC
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

	// Find the latest capture timestamp for this station.
	var latestCapturedAt string
	err := db.QueryRowContext(ctx,
		`SELECT MAX(captured_at) FROM market_orders WHERE station_id = ?`, stationID).
		Scan(&latestCapturedAt)
	if err == sql.ErrNoRows || latestCapturedAt == "" {
		return fmt.Errorf("no market data found for station: %s", stationID)
	}
	if err != nil {
		return fmt.Errorf("failed to query latest captured_at: %w", err)
	}

	// Fetch station metadata.
	var stationName, sysID, sysName string
	_ = db.QueryRowContext(ctx,
		`SELECT COALESCE(station_name,''), COALESCE(system_id,''), COALESCE(system_name,'') FROM stations WHERE station_id = ?`,
		stationID).Scan(&stationName, &sysID, &sysName)
	if stationName == "" {
		stationName = stationID
	}

	// Query all orders for this station at the latest capture time.
	rows, err := db.QueryContext(ctx, `
		SELECT mo.item_id, COALESCE(i.item_name, mo.item_id), COALESCE(i.category, ''),
		       mo.side, mo.quantity, mo.price_each, COALESCE(mo.source, '')
		FROM market_orders mo
		LEFT JOIN items i ON i.item_id = mo.item_id
		WHERE mo.station_id = ? AND mo.captured_at = ?
		ORDER BY mo.side, i.category, mo.item_id
	`, stationID, latestCapturedAt)
	if err != nil {
		return fmt.Errorf("failed to query latest snapshot: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var listings []listing
	for rows.Next() {
		var l listing
		if err := rows.Scan(&l.ItemID, &l.ItemName, &l.Category, &l.Side, &l.Quantity, &l.PriceEach, &l.Source); err != nil {
			return fmt.Errorf("failed to scan row: %w", err)
		}
		listings = append(listings, l)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating rows: %w", err)
	}

	if len(listings) == 0 {
		return fmt.Errorf("no market data found")
	}

	switch cfg.Format {
	case formatJSON:
		result := map[string]any{
			"system_name":  sysName,
			"station_name": stationName,
			"captured_at":  latestCapturedAt,
			"listings":     listings,
		}
		return outputJSON(result)
	default:
		outputLatestTable(sysName, stationName, latestCapturedAt, listings)
		return nil
	}
}

// outputLatestTable displays latest snapshot in table format
func outputLatestTable(systemName, stationName, capturedAt string, listings []listing) {
	fmt.Println(headerStyle.Render(fmt.Sprintf("Market: %s / %s", systemName, stationName)))
	fmt.Println(borderStyle.Render(strings.Repeat("─", 80)))

	fmt.Printf("\n  %s %s\n", labelStyle.Render("Captured:"), dimStyle.Render(formatTimestamp(capturedAt)))

	buyOrders := make(map[string][]listing)
	sellOrders := make(map[string][]listing)

	for _, l := range listings {
		cat := l.Category
		if cat == "" {
			cat = "(uncategorized)"
		}
		if l.Side == "buy" {
			buyOrders[cat] = append(buyOrders[cat], l)
		} else {
			sellOrders[cat] = append(sellOrders[cat], l)
		}
	}

	if len(sellOrders) > 0 {
		fmt.Println("\n" + subHeaderStyle.Render("Sell Orders (Available)"))
		fmt.Println(borderStyle.Render(strings.Repeat("─", 80)))

		cats := sortedKeys(sellOrders)
		for _, cat := range cats {
			fmt.Printf("\n%s\n", successStyle.Render(cat))
			for _, l := range sellOrders[cat] {
				fmt.Printf("  %s x %.2f @ %s\n",
					valueStyle.Render(l.ItemName),
					l.Quantity,
					successStyle.Render(fmt.Sprintf("%.2f credits", l.PriceEach)))
			}
		}
	}

	if len(buyOrders) > 0 {
		fmt.Println("\n" + subHeaderStyle.Render("Buy Orders (Wanted)"))
		fmt.Println(borderStyle.Render(strings.Repeat("─", 80)))

		cats := sortedKeys(buyOrders)
		for _, cat := range cats {
			fmt.Printf("\n%s\n", warningStyle.Render(cat))
			for _, l := range buyOrders[cat] {
				fmt.Printf("  %s x %.2f @ %s\n",
					valueStyle.Render(l.ItemName),
					l.Quantity,
					warningStyle.Render(fmt.Sprintf("%.2f credits", l.PriceEach)))
			}
		}
	}

	fmt.Printf("\n%s %s listing(s)\n",
		labelStyle.Render("Total:"),
		valueStyle.Render(fmt.Sprintf("%d", len(listings))))
}

// cmdHistory lists distinct capture timestamps for a station with per-capture
// order counts.
func cmdHistory(ctx context.Context, db *sql.DB, cfg Config, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: view-market history <system-id> [station-id]")
	}

	systemID := args[0]
	stationID := ""
	if len(args) >= 2 {
		stationID = args[1]
	}

	// Resolve stationID from system when not provided.
	if stationID == "" {
		query := `
			SELECT mo.station_id
			FROM market_orders mo
			JOIN stations s ON s.station_id = mo.station_id
			WHERE s.system_id = ?
			ORDER BY mo.captured_at DESC
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

	// Fetch station metadata once.
	var stationName, sysID, sysName string
	_ = db.QueryRowContext(ctx,
		`SELECT COALESCE(station_name,''), COALESCE(system_id,''), COALESCE(system_name,'') FROM stations WHERE station_id = ?`,
		stationID).Scan(&stationName, &sysID, &sysName)
	if stationName == "" {
		stationName = stationID
	}

	// List distinct capture timestamps with order counts.
	rows, err := db.QueryContext(ctx, `
		SELECT captured_at, COUNT(*) as order_count
		FROM market_orders
		WHERE station_id = ?
		GROUP BY captured_at
		ORDER BY captured_at DESC
		LIMIT ?
	`, stationID, cfg.Limit)
	if err != nil {
		return fmt.Errorf("failed to query history: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var entries []historyEntry
	for rows.Next() {
		e := historyEntry{
			StationID:   stationID,
			StationName: stationName,
			SystemID:    sysID,
			SystemName:  sysName,
		}
		if err := rows.Scan(&e.CapturedAt, &e.OrderCount); err != nil {
			return fmt.Errorf("failed to scan row: %w", err)
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating rows: %w", err)
	}

	switch cfg.Format {
	case formatJSON:
		return outputJSON(entries)
	default:
		outputHistoryTable(entries)
		return nil
	}
}

// outputHistoryTable displays history in table format
func outputHistoryTable(entries []historyEntry) {
	fmt.Println(headerStyle.Render("Market History"))
	fmt.Println(borderStyle.Render(strings.Repeat("─", 80)))

	if len(entries) == 0 {
		fmt.Println(dimStyle.Render("  No history found."))
		return
	}

	for _, e := range entries {
		fmt.Printf("\n%s %s / %s\n",
			dimStyle.Render("["+e.CapturedAt+"]"),
			subHeaderStyle.Render(e.SystemName),
			valueStyle.Render(e.StationName))
		fmt.Printf("  Orders: %d", e.OrderCount)
		fmt.Println()

		fmt.Printf("  %s%s%s\n",
			dimStyle.Render("→ run 'view-market latest "+e.SystemID+" "+e.StationID+"' for details"),
			"",
			"")

		if len(entries) > 1 {
			fmt.Println(dimStyle.Render(strings.Repeat("─", 40)))
		}
	}

	fmt.Printf("\n%s %s capture(s)\n",
		labelStyle.Render("Total:"),
		valueStyle.Render(fmt.Sprintf("%d", len(entries))))
}

// cmdItems lists distinct items from the items catalog, falling back to
// distinct item_ids from market_orders when the items table is empty.
func cmdItems(ctx context.Context, db *sql.DB, cfg Config, _ []string) error {
	rows, err := db.QueryContext(ctx, `
		SELECT item_id, COALESCE(item_name, item_id), COALESCE(category, '')
		FROM items
		ORDER BY category, item_id
		LIMIT ?
	`, cfg.Limit*10)
	if err != nil {
		return fmt.Errorf("failed to query items: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var items []itemInfo
	for rows.Next() {
		var item itemInfo
		if err := rows.Scan(&item.ItemID, &item.ItemName, &item.Category); err != nil {
			return fmt.Errorf("failed to scan row: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating rows: %w", err)
	}

	// Fallback: derive distinct items from market_orders if catalog is empty.
	if len(items) == 0 {
		rows2, err2 := db.QueryContext(ctx, `
			SELECT DISTINCT item_id, item_id, ''
			FROM market_orders
			ORDER BY item_id
			LIMIT ?
		`, cfg.Limit*10)
		if err2 != nil {
			return fmt.Errorf("failed to query items from orders: %w", err2)
		}
		defer func() { _ = rows2.Close() }()
		for rows2.Next() {
			var item itemInfo
			if err := rows2.Scan(&item.ItemID, &item.ItemName, &item.Category); err != nil {
				return fmt.Errorf("failed to scan row: %w", err)
			}
			items = append(items, item)
		}
		if err := rows2.Err(); err != nil {
			return fmt.Errorf("error iterating rows: %w", err)
		}
	}

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

	// Group by category
	byCategory := make(map[string][]itemInfo)
	for _, item := range items {
		cat := item.Category
		if cat == "" {
			cat = "(uncategorized)"
		}
		byCategory[cat] = append(byCategory[cat], item)
	}

	cats := sortedKeys(byCategory)
	for _, cat := range cats {
		fmt.Printf("\n%s\n", subHeaderStyle.Render(cat))
		for _, item := range byCategory[cat] {
			fmt.Printf("  • %s", valueStyle.Render(item.ItemName))
			if item.ItemName != item.ItemID {
				fmt.Printf(" %s", dimStyle.Render("("+item.ItemID+")"))
			}
			fmt.Println()
		}
	}

	fmt.Printf("\n%s %s unique item(s)\n",
		labelStyle.Render("Total:"),
		valueStyle.Render(fmt.Sprintf("%d", len(items))))
}

// cmdPrices shows OHLCV price history for an item from market_ohlcv.
func cmdPrices(ctx context.Context, db *sql.DB, cfg Config, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: view-market prices <item-id>")
	}

	itemID := args[0]

	rows, err := db.QueryContext(ctx, `
		SELECT mo.station_id, COALESCE(s.station_name, mo.station_id),
		       mo.item_id, mo.side, mo.bucket_utc,
		       mo.open_price, mo.high_price, mo.low_price, mo.close_price,
		       mo.volume, mo.trade_count, mo.vwap
		FROM market_ohlcv mo
		LEFT JOIN stations s ON s.station_id = mo.station_id
		WHERE mo.item_id = ?
		ORDER BY mo.bucket_utc DESC
		LIMIT ?
	`, itemID, cfg.Limit)
	if err != nil {
		return fmt.Errorf("failed to query prices: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var entries []ohlcvEntry
	for rows.Next() {
		var e ohlcvEntry
		if err := rows.Scan(&e.StationID, &e.StationName, &e.ItemID, &e.Side, &e.BucketUTC,
			&e.OpenPrice, &e.HighPrice, &e.LowPrice, &e.ClosePrice,
			&e.Volume, &e.TradeCount, &e.VWAP); err != nil {
			return fmt.Errorf("failed to scan row: %w", err)
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating rows: %w", err)
	}

	switch cfg.Format {
	case formatJSON:
		return outputJSON(entries)
	default:
		outputPricesTable(itemID, entries)
		return nil
	}
}

// outputPricesTable displays OHLCV price history in table format
func outputPricesTable(itemID string, entries []ohlcvEntry) {
	fmt.Println(headerStyle.Render(fmt.Sprintf("Price History (OHLCV): %s", itemID)))
	fmt.Println(borderStyle.Render(strings.Repeat("─", 80)))

	if len(entries) == 0 {
		fmt.Println(dimStyle.Render("  No OHLCV price data found for this item."))
		return
	}

	fmt.Printf("\n%s  %s  %s  %s  %s  %s  %s  %s\n",
		tableHeaderStyle.Render(padRight("Bucket (UTC)", 20)),
		tableHeaderStyle.Render(padRight("Station", 20)),
		tableHeaderStyle.Render(padRight("Side", 5)),
		tableHeaderStyle.Render(padRight("Open", 10)),
		tableHeaderStyle.Render(padRight("High", 10)),
		tableHeaderStyle.Render(padRight("Low", 10)),
		tableHeaderStyle.Render(padRight("Close", 10)),
		tableHeaderStyle.Render("VWAP"))
	fmt.Println(borderStyle.Render(strings.Repeat("─", 80)))

	for _, e := range entries {
		sideStyle := successStyle
		if e.Side == "buy" {
			sideStyle = warningStyle
		}
		fmt.Printf("%s  %s  %s  %s  %s  %s  %s  %s\n",
			dimStyle.Render(padRight(e.BucketUTC, 20)),
			valueStyle.Render(padRight(e.StationName, 20)),
			sideStyle.Render(padRight(e.Side, 5)),
			valueStyle.Render(padRight(fmt.Sprintf("%.2f", e.OpenPrice), 10)),
			successStyle.Render(padRight(fmt.Sprintf("%.2f", e.HighPrice), 10)),
			errorStyle.Render(padRight(fmt.Sprintf("%.2f", e.LowPrice), 10)),
			valueStyle.Render(padRight(fmt.Sprintf("%.2f", e.ClosePrice), 10)),
			valueStyle.Render(fmt.Sprintf("%.2f", e.VWAP)))
	}

	fmt.Printf("\n%s %s OHLCV record(s)\n",
		labelStyle.Render("Total:"),
		valueStyle.Render(fmt.Sprintf("%d", len(entries))))
}

// cmdArbitrage shows arbitrage opportunities computed on the fly from
// market_orders using each station's latest capture per item.
func cmdArbitrage(ctx context.Context, db *sql.DB, cfg Config, _ []string) error {
	// For each item, find min sell price and max buy price across stations,
	// using only the latest captured_at per station.
	query := `
		WITH latest_per_station AS (
			SELECT station_id, MAX(captured_at) AS latest_captured
			FROM market_orders
			GROUP BY station_id
		),
		latest_orders AS (
			SELECT mo.item_id, mo.side, mo.price_each
			FROM market_orders mo
			JOIN latest_per_station lps
				ON mo.station_id = lps.station_id AND mo.captured_at = lps.latest_captured
		)
		SELECT lo.item_id, COALESCE(i.item_name, lo.item_id),
		       MIN(CASE WHEN lo.side = 'sell' THEN lo.price_each END) AS min_sell_price,
		       MAX(CASE WHEN lo.side = 'buy'  THEN lo.price_each END) AS max_buy_price
		FROM latest_orders lo
		LEFT JOIN items i ON i.item_id = lo.item_id
		WHERE lo.side IN ('sell', 'buy')
		GROUP BY lo.item_id
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
		if err := rows.Scan(&opp.ItemID, &opp.ItemName, &opp.MinSellPrice, &opp.MaxBuyPrice); err != nil {
			return fmt.Errorf("failed to scan row: %w", err)
		}
		opp.Profit = opp.MaxBuyPrice - opp.MinSellPrice
		opp.ProfitMargin = (opp.Profit / opp.MinSellPrice) * 100
		opportunities = append(opportunities, opp)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating rows: %w", err)
	}

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
			valueStyle.Render(padRight(opp.ItemName, 25)),
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
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		t, err = time.Parse("2006-01-02 15:04:05", ts)
		if err != nil {
			return ts
		}
	}

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

// sortedKeys returns the sorted keys of any string-keyed map.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
