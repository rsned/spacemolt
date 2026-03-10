// bulk-buy-order places buy orders for every item in the crafting database
// to seed market data. Orders are sent in bulk batches (up to 50 per call).
//
// Usage:
//
//	bulk-buy-order --agent=trader-1
//	bulk-buy-order --agent=trader-1 --price=2 --quantity=1 --dry-run
//	bulk-buy-order --agent=trader-1 --db=path/to/crafting.db --batch-size=25
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/credentials"
	"github.com/rsned/spacemolt/pkg/game"

	_ "modernc.org/sqlite"
)

const gameServerURL = "wss://game.spacemolt.com/ws"

func main() {
	agentID := flag.String("agent", "", "Agent ID for authentication (required)")
	dbPath := flag.String("db", "", "Path to crafting SQLite DB (default: auto-detect, env: CRAFTING_DB)")
	price := flag.Int("price", 1, "Price per unit in credits")
	quantity := flag.Int("quantity", 1, "Quantity per item")
	batchSize := flag.Int("batch-size", 50, "Orders per API call (max 50)")
	offset := flag.Int("offset", 0, "Skip the first N items (start from item N)")
	limit := flag.Int("limit", 0, "Only send orders for N items (0 = all)")
	categories := flag.String("categories", "", "Comma-separated item categories to filter (e.g. defense,weapon,drone)")
	dryRun := flag.Bool("dry-run", false, "Print batches without sending")
	transport := flag.String("transport", "ws", "Transport: ws (WebSocket) or mcp (MCP HTTP)")
	debug := flag.Bool("debug", false, "Enable debug logging")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: bulk-buy-order --agent=<id> [flags]\n\n")
		fmt.Fprintf(os.Stderr, "Place buy orders for every item in the crafting DB to seed market data.\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *agentID == "" {
		flag.Usage()
		os.Exit(1)
	}

	if *batchSize < 1 || *batchSize > 50 {
		fmt.Fprintf(os.Stderr, "Error: batch-size must be between 1 and 50\n")
		os.Exit(1)
	}

	// Resolve DB path.
	resolvedDB := resolveDBPath(*dbPath)
	if resolvedDB == "" {
		fmt.Fprintf(os.Stderr, "Error: could not find crafting DB. Use --db or set CRAFTING_DB env var.\n")
		os.Exit(1)
	}

	// Parse category filter.
	var catFilter []string
	if *categories != "" {
		for _, c := range strings.Split(*categories, ",") {
			c = strings.TrimSpace(c)
			if c != "" {
				catFilter = append(catFilter, c)
			}
		}
	}

	// Load item IDs from crafting DB.
	itemIDs, err := loadItemIDs(resolvedDB, catFilter)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading items: %v\n", err)
		os.Exit(1)
	}
	if len(catFilter) > 0 {
		fmt.Fprintf(os.Stderr, "Loaded %d items from %s (categories: %s)\n", len(itemIDs), resolvedDB, strings.Join(catFilter, ", "))
	} else {
		fmt.Fprintf(os.Stderr, "Loaded %d items from %s\n", len(itemIDs), resolvedDB)
	}

	// Apply offset and limit.
	if *offset > 0 {
		if *offset >= len(itemIDs) {
			fmt.Fprintf(os.Stderr, "Offset %d exceeds item count %d, nothing to do.\n", *offset, len(itemIDs))
			return
		}
		itemIDs = itemIDs[*offset:]
		fmt.Fprintf(os.Stderr, "Skipped first %d items, %d remaining\n", *offset, len(itemIDs))
	}
	if *limit > 0 && *limit < len(itemIDs) {
		itemIDs = itemIDs[:*limit]
		fmt.Fprintf(os.Stderr, "Limited to %d items\n", *limit)
	}

	if len(itemIDs) == 0 {
		fmt.Fprintln(os.Stderr, "No items found, nothing to do.")
		return
	}

	// Chunk into batches.
	batches := chunk(itemIDs, *batchSize)
	fmt.Fprintf(os.Stderr, "Will send %d batches of up to %d orders (price=%d, quantity=%d)\n",
		len(batches), *batchSize, *price, *quantity)

	if *dryRun {
		for i, batch := range batches {
			fmt.Fprintf(os.Stderr, "Batch %d/%d: %d orders (items: %s ... %s)\n",
				i+1, len(batches), len(batch), batch[0], batch[len(batch)-1])
			if *debug {
				orders := make([]map[string]any, len(batch))
				for j, itemID := range batch {
					orders[j] = map[string]any{
						"item_id":  itemID,
						"price_each": *price,
						"quantity": *quantity,
					}
				}
				payload := map[string]any{"orders": orders}
				data, _ := json.MarshalIndent(payload, "  ", "  ")
				fmt.Fprintf(os.Stderr, "  Payload: %s\n", data)
			}
		}
		fmt.Fprintln(os.Stderr, "Dry run complete, no orders sent.")
		return
	}

	// Load credentials and connect.
	ctx := context.Background()
	logger := log.New(os.Stderr, fmt.Sprintf("[%s] ", *agentID), log.LstdFlags)

	var client game.GameClient
	switch *transport {
	case "mcp":
		logger.Printf("Using MCP transport")
		mcpClient, _, mcpErr := game.InitializeMCPAgent(*agentID, logger, ctx, *debug)
		if mcpErr != nil {
			fmt.Fprintf(os.Stderr, "Error initializing MCP agent: %v\n", mcpErr)
			os.Exit(1)
		}
		client = mcpClient
	case "ws":
		logger.Printf("Using WebSocket transport")
		provider := credentials.NewFileProvider("data/agents")
		creds, err := provider.GetCredentials(ctx, *agentID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading credentials for %q: %v\n", *agentID, err)
			os.Exit(1)
		}

		wsClient := game.NewClient(gameServerURL, creds.Username, creds.Password, logger)
		wsClient.SetDebugLogging(*debug)

		if err := wsClient.Connect(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "Error connecting: %v\n", err)
			os.Exit(1)
		}

		<-wsClient.Ready()
		time.Sleep(game.SleepRetry)

		if err := wsClient.Login(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "Error logging in: %v\n", err)
			os.Exit(1)
		}
		time.Sleep(game.SleepQuick)
		client = wsClient
	default:
		fmt.Fprintf(os.Stderr, "Unknown transport: %s (must be: ws, mcp)\n", *transport)
		os.Exit(1)
	}
	defer func() { _ = client.Close() }()

	// Send batches.
	totalSent := 0
	for i, batch := range batches {
		orders := make([]map[string]any, len(batch))
		for j, itemID := range batch {
			orders[j] = map[string]any{
				"item_id":  itemID,
				"price_each": *price,
				"quantity": *quantity,
			}
		}

		payload := map[string]any{
			"orders": orders,
		}

		fmt.Fprintf(os.Stderr, "Batch %d/%d: %d orders (items: %s ... %s)\n",
			i+1, len(batches), len(batch), batch[0], batch[len(batch)-1])

		if err := client.CreateBuyOrder(ctx, payload); err != nil {
			fmt.Fprintf(os.Stderr, "Error sending batch %d: %v\n", i+1, err)
			os.Exit(1)
		}
		totalSent += len(batch)

		// Wait a full tick between batches so the server finishes processing
		// the current action before we submit the next one.
		if i < len(batches)-1 {
			time.Sleep(game.SleepTick)
		}
	}

	fmt.Fprintf(os.Stderr, "Done: %d buy orders placed across %d batches.\n", totalSent, len(batches))
}

func resolveDBPath(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if envPath := os.Getenv("CRAFTING_DB"); envPath != "" {
		return envPath
	}
	defaultPath := "../../spacemolt-crafting-server/database/crafting.db"
	if _, err := os.Stat(defaultPath); err == nil {
		return defaultPath
	}
	return ""
}

func loadItemIDs(dbPath string, categories []string) ([]string, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	defer func() { _ = db.Close() }()

	query := "SELECT id FROM items"
	var args []any
	if len(categories) > 0 {
		placeholders := make([]string, len(categories))
		for i, c := range categories {
			placeholders[i] = "?"
			args = append(args, c)
		}
		query += " WHERE category IN (" + strings.Join(placeholders, ",") + ")"
	}
	query += " ORDER BY id"

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query items: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func chunk(items []string, size int) [][]string {
	var batches [][]string
	for i := 0; i < len(items); i += size {
		end := i + size
		if end > len(items) {
			end = len(items)
		}
		batches = append(batches, items[i:end])
	}
	return batches
}
