// bulk-sell-order sells items from station storage by creating sell orders.
// For each item in storage matching the given category, it checks the station's
// market buy order prices. If a buy order exists with price > base_value, the
// item is sold at that buy price (instant fill). Remaining items are listed at
// 90% of base_value. Orders are batched (up to 50 per call).
//
// Usage:
//
//	bulk-sell-order --agent=craftsman-1                                  # all items
//	bulk-sell-order --agent=craftsman-1 --category=component             # one category
//	bulk-sell-order --agent=craftsman-1 --category=ore --dry-run
//	bulk-sell-order --agent=craftsman-1 --category=refined_material --min-value=10
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"time"

	"github.com/rsned/spacemolt/pkg/credentials"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"

	_ "modernc.org/sqlite"
)

const gameServerURL = "wss://game.spacemolt.com/ws"

type sellOrder struct {
	ItemID   string
	Quantity int
	Price    int
	Reason   string // "buy_order" or "base_value"
}

func main() {
	agentID := flag.String("agent", "", "Agent ID for authentication (required)")
	dbPath := flag.String("db", "spacemolt-knowledge.db", "Path to knowledge SQLite DB")
	category := flag.String("category", "", "Item category to sell (omit for all items)")
	batchSize := flag.Int("batch-size", 50, "Orders per API call (max 50)")
	discount := flag.Float64("discount", 0.90, "Fraction of base_value for items without buy orders (default 0.90)")
	minValue := flag.Int("min-value", 1, "Skip items with base_value below this threshold")
	dryRun := flag.Bool("dry-run", false, "Print orders without sending")
	debug := flag.Bool("debug", false, "Enable debug logging")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: bulk-sell-order --agent=<id> [--category=<category>] [flags]\n\n")
		fmt.Fprintf(os.Stderr, "Sell storage items using market buy orders or discounted base value.\n")
		fmt.Fprintf(os.Stderr, "Omit --category to sell all items in storage.\n\n")
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

	// Load item catalog from knowledge DB, optionally filtered by category.
	itemCatalog, err := loadItemCatalog(*dbPath, *category)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading item catalog: %v\n", err)
		os.Exit(1)
	}
	if *category != "" {
		fmt.Fprintf(os.Stderr, "Loaded %d items in category %q from %s\n", len(itemCatalog), *category, *dbPath)
	} else {
		fmt.Fprintf(os.Stderr, "Loaded %d items (all categories) from %s\n", len(itemCatalog), *dbPath)
	}

	if len(itemCatalog) == 0 {
		fmt.Fprintln(os.Stderr, "No items found in catalog.")
		return
	}

	// Connect to game server.
	provider := credentials.NewFileProvider("data/agents")
	ctx := context.Background()
	creds, err := provider.GetCredentials(ctx, *agentID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading credentials for %q: %v\n", *agentID, err)
		os.Exit(1)
	}

	logger := log.New(os.Stderr, fmt.Sprintf("[%s] ", *agentID), log.LstdFlags)
	client := game.NewClient(gameServerURL, creds.Username, creds.Password, logger)
	client.SetDebugLogging(*debug)

	if err := client.Connect(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Error connecting: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = client.Close() }()

	<-client.Ready()
	time.Sleep(game.SleepRetry)

	if err := client.Login(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Error logging in: %v\n", err)
		os.Exit(1)
	}
	time.Sleep(game.SleepQuick)

	// Step 1: View storage to get items.
	if err := client.ViewStorage(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Error viewing storage: %v\n", err)
		os.Exit(1)
	}
	time.Sleep(game.SleepQuick)

	storageItems := parseStorageItems(client)
	if len(storageItems) == 0 {
		fmt.Fprintln(os.Stderr, "Storage is empty, nothing to sell.")
		return
	}
	fmt.Fprintf(os.Stderr, "Storage contains %d item types\n", len(storageItems))

	// Filter storage to only items in our catalog (category-filtered or all).
	var matchedItems []storageEntry
	for itemID, qty := range storageItems {
		if _, ok := itemCatalog[itemID]; ok {
			matchedItems = append(matchedItems, storageEntry{ItemID: itemID, Quantity: qty})
		}
	}
	if *category != "" {
		fmt.Fprintf(os.Stderr, "Found %d item types in category %q in storage\n", len(matchedItems), *category)
	} else {
		fmt.Fprintf(os.Stderr, "Found %d item types in storage matching catalog\n", len(matchedItems))
	}

	if len(matchedItems) == 0 {
		fmt.Fprintln(os.Stderr, "No matching items found in storage.")
		return
	}

	// Step 2: View market to get buy order prices.
	if err := client.ViewMarket(ctx, ""); err != nil {
		fmt.Fprintf(os.Stderr, "Error viewing market: %v\n", err)
		os.Exit(1)
	}
	time.Sleep(game.SleepQuick)

	marketItems := parseMarketItems(client)
	fmt.Fprintf(os.Stderr, "Market has %d items listed\n", len(marketItems))

	// Step 3: Build sell orders.
	var orders []sellOrder
	var skippedLowValue, soldAtBuyPrice, soldAtDiscount int

	for _, entry := range matchedItems {
		catalogItem := itemCatalog[entry.ItemID]

		if catalogItem.BaseValue < *minValue {
			skippedLowValue++
			continue
		}

		qty := int(entry.Quantity)
		if qty <= 0 {
			continue
		}

		// Check if there's a buy order with price > base_value.
		if marketItem, ok := marketItems[entry.ItemID]; ok && marketItem.BestBuy > float64(catalogItem.BaseValue) {
			price := int(marketItem.BestBuy)
			orders = append(orders, sellOrder{
				ItemID:   entry.ItemID,
				Quantity: qty,
				Price:    price,
				Reason:   fmt.Sprintf("buy_order (buy=%d > base=%d)", price, catalogItem.BaseValue),
			})
			soldAtBuyPrice++
		} else {
			// List at discount of base_value.
			price := int(math.Round(float64(catalogItem.BaseValue) * *discount))
			if price < 1 {
				price = 1
			}
			orders = append(orders, sellOrder{
				ItemID:   entry.ItemID,
				Quantity: qty,
				Price:    price,
				Reason:   fmt.Sprintf("%.0f%% base_value (base=%d)", *discount*100, catalogItem.BaseValue),
			})
			soldAtDiscount++
		}
	}

	fmt.Fprintf(os.Stderr, "\nSell order summary:\n")
	fmt.Fprintf(os.Stderr, "  At buy order price: %d items\n", soldAtBuyPrice)
	fmt.Fprintf(os.Stderr, "  At %.0f%% base value: %d items\n", *discount*100, soldAtDiscount)
	fmt.Fprintf(os.Stderr, "  Skipped (low value): %d items\n", skippedLowValue)
	fmt.Fprintf(os.Stderr, "  Total orders: %d\n\n", len(orders))

	if len(orders) == 0 {
		fmt.Fprintln(os.Stderr, "No orders to place.")
		return
	}

	// Chunk into batches.
	batches := chunkOrders(orders, *batchSize)
	fmt.Fprintf(os.Stderr, "Will send %d batches of up to %d orders\n\n", len(batches), *batchSize)

	if *dryRun {
		for i, batch := range batches {
			fmt.Fprintf(os.Stderr, "Batch %d/%d: %d orders\n", i+1, len(batches), len(batch))
			for _, o := range batch {
				fmt.Fprintf(os.Stderr, "  %-40s qty=%-5d price=%-8d (%s)\n",
					o.ItemID, o.Quantity, o.Price, o.Reason)
			}
		}
		fmt.Fprintln(os.Stderr, "\nDry run complete, no orders sent.")
		return
	}

	// Step 4: Send batches.
	totalSent := 0
	for i, batch := range batches {
		payloadOrders := make([]map[string]any, len(batch))
		for j, o := range batch {
			payloadOrders[j] = map[string]any{
				"item_id":    o.ItemID,
				"price_each": o.Price,
				"quantity":   o.Quantity,
			}
		}
		payload := map[string]any{"orders": payloadOrders}

		fmt.Fprintf(os.Stderr, "Batch %d/%d: sending %d orders...\n", i+1, len(batches), len(batch))

		if err := client.CreateSellOrder(ctx, payload); err != nil {
			fmt.Fprintf(os.Stderr, "Error sending batch %d: %v\n", i+1, err)
			os.Exit(1)
		}
		totalSent += len(batch)

		if i < len(batches)-1 {
			time.Sleep(game.SleepTick)
		}
	}

	fmt.Fprintf(os.Stderr, "\nDone: %d sell orders placed across %d batches.\n", totalSent, len(batches))
}

type catalogEntry struct {
	BaseValue int
}

type storageEntry struct {
	ItemID   string
	Quantity float64
}

func loadItemCatalog(dbPath, category string) (map[string]catalogEntry, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	defer func() { _ = db.Close() }()

	query := "SELECT id, base_value FROM items"
	var args []any
	if category != "" {
		query += " WHERE category = ?"
		args = append(args, category)
	}
	query += " ORDER BY id"

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query items: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make(map[string]catalogEntry)
	for rows.Next() {
		var id string
		var baseValue int
		if err := rows.Scan(&id, &baseValue); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		items[id] = catalogEntry{BaseValue: baseValue}
	}
	return items, rows.Err()
}

func parseStorageItems(client *game.Client) map[string]float64 {
	raw := client.GetRawJSON("storage")
	if raw == nil {
		return nil
	}

	var resp struct {
		Items []game.CargoItem `json:"items"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil
	}

	items := make(map[string]float64, len(resp.Items))
	for _, item := range resp.Items {
		items[item.ItemID] = item.Quantity
	}
	return items
}

func parseMarketItems(client *game.Client) map[string]serverapi.ViewMarketItem {
	raw := client.GetRawJSON("market")
	if raw == nil {
		return nil
	}

	var resp struct {
		Items []serverapi.ViewMarketItem `json:"items"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil
	}

	items := make(map[string]serverapi.ViewMarketItem, len(resp.Items))
	for _, item := range resp.Items {
		items[item.ItemID] = item
	}
	return items
}

func chunkOrders(orders []sellOrder, size int) [][]sellOrder {
	var batches [][]sellOrder
	for i := 0; i < len(orders); i += size {
		end := i + size
		if end > len(orders) {
			end = len(orders)
		}
		batches = append(batches, orders[i:end])
	}
	return batches
}

// listCategories prints all distinct item categories from the knowledge DB.
func listCategories(dbPath string) error {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer func() { _ = db.Close() }()

	rows, err := db.Query("SELECT DISTINCT category FROM items WHERE category IS NOT NULL ORDER BY category")
	if err != nil {
		return fmt.Errorf("query categories: %w", err)
	}
	defer func() { _ = rows.Close() }()

	fmt.Fprintln(os.Stderr, "Available categories:")
	for rows.Next() {
		var cat string
		if err := rows.Scan(&cat); err != nil {
			return fmt.Errorf("scan: %w", err)
		}
		// Count items in category.
		var count int
		_ = db.QueryRow("SELECT COUNT(*) FROM items WHERE category = ?", cat).Scan(&count)
		fmt.Fprintf(os.Stderr, "  %-30s (%d items)\n", cat, count)
	}
	return rows.Err()
}

func init() {
	// Support --list-categories as a quick helper.
	if len(os.Args) > 1 && os.Args[1] == "--list-categories" {
		dbPath := "spacemolt-knowledge.db"
		if len(os.Args) > 3 && os.Args[2] == "--db" {
			dbPath = os.Args[3]
		}
		if err := listCategories(dbPath); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}
}
