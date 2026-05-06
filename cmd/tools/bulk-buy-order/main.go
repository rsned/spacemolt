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
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"io"
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
	itemsFile := flag.String("items-file", "", "Path to a file with item_ids to order (one per line; '#' starts a comment; '-' reads stdin). When set, overrides --db/--categories.")
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
		fmt.Fprintf(os.Stderr, "Place buy orders to seed market data. By default item ids are pulled from\n")
		fmt.Fprintf(os.Stderr, "the local crafting DB; pass --items-file to drive the run from an explicit\n")
		fmt.Fprintf(os.Stderr, "list (one item_id per line) — useful for station-only items the catalog\n")
		fmt.Fprintf(os.Stderr, "DB doesn't know about.\n\n")
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

	// Parse category filter (only consulted for the DB path).
	var catFilter []string
	if *categories != "" {
		for _, c := range strings.Split(*categories, ",") {
			c = strings.TrimSpace(c)
			if c != "" {
				catFilter = append(catFilter, c)
			}
		}
	}

	// Load item IDs — from explicit file, or from the crafting DB.
	var (
		itemIDs []string
		err     error
	)
	if *itemsFile != "" {
		if *categories != "" {
			fmt.Fprintln(os.Stderr, "Note: --categories is ignored when --items-file is set.")
		}
		itemIDs, err = loadItemIDsFromFile(*itemsFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading items from %s: %v\n", *itemsFile, err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "Loaded %d items from %s\n", len(itemIDs), *itemsFile)
	} else {
		resolvedDB := resolveDBPath(*dbPath)
		if resolvedDB == "" {
			fmt.Fprintf(os.Stderr, "Error: could not find crafting DB. Use --db, set CRAFTING_DB, or pass --items-file.\n")
			os.Exit(1)
		}
		itemIDs, err = loadItemIDs(resolvedDB, catFilter)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading items: %v\n", err)
			os.Exit(1)
		}
		if len(catFilter) > 0 {
			fmt.Fprintf(os.Stderr, "Loaded %d items from %s (categories: %s)\n", len(itemIDs), resolvedDB, strings.Join(catFilter, ", "))
		} else {
			fmt.Fprintf(os.Stderr, "Loaded %d items from %s\n", len(itemIDs), resolvedDB)
		}
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

	var (
		client   game.GameClient
		wsClient *game.Client // non-nil only for ws transport; used for reconnect
	)
	switch *transport {
	case "mcp":
		logger.Printf("Using MCP transport")
		mcpClient, _, mcpErr := game.InitializeMCPAgent(*agentID, logger, ctx, *debug, false)
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

		wsClient = game.NewClient(gameServerURL, creds.Username, creds.Password, logger)
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

	// ensureConnected reconnects + re-logs in if the WS connection has dropped.
	// No-op for non-ws transports.
	ensureConnected := func() error {
		if wsClient == nil || wsClient.IsConnected() {
			return nil
		}
		logger.Printf("Connection dropped, reconnecting...")
		if err := wsClient.Reconnect(ctx); err != nil {
			return fmt.Errorf("reconnect: %w", err)
		}
		time.Sleep(game.SleepQuick)
		return nil
	}

	// Send batches. The server has been observed to close the WS connection
	// shortly after a successful 50-order create_buy_order. Reconnect on
	// demand between batches and on send failure.
	totalSent := 0
	for i, batch := range batches {
		orders := make([]map[string]any, len(batch))
		for j, itemID := range batch {
			orders[j] = map[string]any{
				"item_id":    itemID,
				"price_each": *price,
				"quantity":   *quantity,
			}
		}

		payload := map[string]any{
			"orders": orders,
		}

		fmt.Fprintf(os.Stderr, "Batch %d/%d: %d orders (items: %s ... %s)\n",
			i+1, len(batches), len(batch), batch[0], batch[len(batch)-1])

		if err := ensureConnected(); err != nil {
			fmt.Fprintf(os.Stderr, "Error before batch %d: %v\n", i+1, err)
			os.Exit(1)
		}

		err := client.CreateBuyOrder(ctx, payload)
		switch {
		case err == nil:
			// Happy path.
			totalSent += len(batch)
		case wsClient != nil && isConnectionDropError(err):
			// Send itself failed because socket was already dead — the server
			// never saw this batch. Reconnect and retry once.
			logger.Printf("Batch %d send failed (%v), reconnecting and retrying once", i+1, err)
			if rerr := wsClient.Reconnect(ctx); rerr != nil {
				fmt.Fprintf(os.Stderr, "Error sending batch %d: send failed and reconnect failed: %v (original: %v)\n",
					i+1, rerr, err)
				os.Exit(1)
			}
			time.Sleep(game.SleepQuick)
			if err = client.CreateBuyOrder(ctx, payload); err != nil {
				fmt.Fprintf(os.Stderr, "Error sending batch %d after reconnect: %v\n", i+1, err)
				os.Exit(1)
			}
			totalSent += len(batch)
		case wsClient != nil && isActionTimeoutError(err) && !wsClient.IsConnected():
			// Server replied "pending" but the WS dropped before the
			// action_result arrived. The orders were accepted server-side
			// (pending=true is a commit) and almost certainly processed —
			// retrying would just create duplicate orders. Log loudly,
			// reconnect for the next batch, and credit this one as sent.
			logger.Printf("Batch %d: connection dropped while awaiting action_result; "+
				"server already accepted via pending=true, assuming completed (%v)", i+1, err)
			totalSent += len(batch)
		default:
			fmt.Fprintf(os.Stderr, "Error sending batch %d: %v\n", i+1, err)
			os.Exit(1)
		}

		// Wait a full tick between batches so the server finishes processing
		// the current action before we submit the next one.
		if i < len(batches)-1 {
			time.Sleep(game.SleepTick)
		}
	}

	fmt.Fprintf(os.Stderr, "Done: %d buy orders placed across %d batches.\n", totalSent, len(batches))
}

// isConnectionDropError reports whether err is the WS-disconnected sentinel
// returned by Send when the underlying socket has gone away.
func isConnectionDropError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, needle := range []string{
		"not connected",
		"connection reset",
		"connection closed",
		"broken pipe",
		"websocket closed",
		"failed to send message",
	} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

// isActionTimeoutError reports whether err is the execMutation timeout that
// fires when we sent a command and got the initial OK but never received
// the matching action_result before the deadline. Combined with a dead
// socket this signals "server accepted; we just lost the confirmation."
func isActionTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "timeout waiting for")
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

// loadItemIDsFromFile reads item ids from a text file (one per line). Lines
// starting with "#" and blank lines are skipped; "-" reads from stdin.
// Trailing/leading whitespace is trimmed and duplicate ids are removed while
// preserving first-seen order.
func loadItemIDsFromFile(path string) ([]string, error) {
	var src io.ReadCloser
	if path == "-" {
		src = io.NopCloser(os.Stdin)
	} else {
		f, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("open: %w", err)
		}
		src = f
	}
	defer func() { _ = src.Close() }()

	seen := make(map[string]struct{})
	var ids []string
	scanner := bufio.NewScanner(src)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		// Strip inline comment.
		if hash := strings.IndexByte(line, '#'); hash >= 0 {
			line = line[:hash]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Item ids in this codebase are lowercase alphanumeric + underscore.
		// We don't enforce that strictly, but reject anything containing
		// whitespace as an obvious format error.
		if strings.ContainsAny(line, " \t") {
			return nil, fmt.Errorf("line %d: item id contains whitespace: %q", lineNo, line)
		}
		if _, dup := seen[line]; dup {
			continue
		}
		seen[line] = struct{}{}
		ids = append(ids, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan: %w", err)
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("no item ids found")
	}
	return ids, nil
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
