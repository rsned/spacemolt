// action-log-scan paginates through an agent's server-side action log
// (get_action_log) and reports the distinct event_types seen, with per-type
// counts, the owning category, and an example summary.
//
// It maintains a per-agent JSONL store of every entry it has pulled. On later
// runs it fetches only records newer than the highest stored id (entries come
// back newest-first, so it stops paging as soon as it reaches a known id),
// then reports the cumulative picture across the whole store. Pass --full to
// re-page the entire log (backfilling any gaps), or --no-store to skip
// persistence entirely.
//
// Usage:
//
//	action-log-scan --agent=craftsman-1                 # incremental pull + report
//	action-log-scan --agent=craftsman-1 --full          # full re-scan
//	action-log-scan --agent=craftsman-1 --category=combat
//	action-log-scan --agent=craftsman-1 --sort=name --json
//	action-log-scan --agent=craftsman-1 --faction-id=<id>   # faction log (separate store)
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/credentials"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

const gameServerURL = "wss://game.spacemolt.com/ws"

// eventStat is the per-event_type roll-up reported at the end of a run.
type eventStat struct {
	EventType string `json:"event_type"`
	Category  string `json:"category"`
	Count     int    `json:"count"`
	Example   string `json:"example,omitempty"`    // summary of the newest entry of this type
	FirstSeen string `json:"first_seen,omitempty"` // created_at of the oldest entry
	LastSeen  string `json:"last_seen,omitempty"`  // created_at of the newest entry
}

func main() {
	agentID := flag.String("agent", "", "Agent ID for authentication (required)")
	category := flag.String("category", "", "Optional category filter (combat, trading, ship, crafting, faction, mission, skill, salvage, storage, other)")
	eventType := flag.String("event-type", "", "Optional exact event_type filter (e.g. faction.production_cycle)")
	factionID := flag.String("faction-id", "", "Optional faction_id to scan a faction's log instead of personal")
	pageSize := flag.Int("page-size", 100, "Entries per page (max 100)")
	maxPages := flag.Int("max-pages", 0, "Stop after N pages (0 = all pages until has_more is false)")
	sortBy := flag.String("sort", "count", "Sort output by: count (desc), name (event_type), category")
	jsonOut := flag.Bool("json", false, "Emit the per-type stats as JSON instead of a table")
	storePath := flag.String("store", "", "Path to the JSONL entry store (default: data/agents/<agent>/action_log[.<faction>].jsonl)")
	full := flag.Bool("full", false, "Re-page the entire log instead of stopping at the newest stored id (backfills gaps)")
	noStore := flag.Bool("no-store", false, "Don't read or write the persistent store (one-off scan of this run only)")
	transport := flag.String("transport", "ws", "Transport: ws (WebSocket) or mcp (MCP HTTP)")
	debug := flag.Bool("debug", false, "Enable debug logging")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: action-log-scan --agent=<id> [flags]\n\n")
		fmt.Fprintf(os.Stderr, "Pull action-log entries (incrementally by default), persist them per agent,\n")
		fmt.Fprintf(os.Stderr, "and list the distinct event_types seen with counts, category, and an example.\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *agentID == "" {
		flag.Usage()
		os.Exit(1)
	}
	if *pageSize < 1 || *pageSize > 100 {
		fmt.Fprintf(os.Stderr, "Error: page-size must be between 1 and 100\n")
		os.Exit(1)
	}

	// Load the existing store so we know the newest id already captured.
	resolvedStore := *storePath
	if resolvedStore == "" {
		resolvedStore = defaultStorePath(*agentID, *factionID)
	}
	byID := map[int]serverapi.ActionLogEntry{}
	maxStoredID := 0
	if !*noStore {
		var err error
		byID, maxStoredID, err = loadStore(resolvedStore)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading store %s: %v\n", resolvedStore, err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "Store %s: %d entries, newest id %d\n", resolvedStore, len(byID), maxStoredID)
	}
	// The incremental early-stop only makes sense for an unfiltered personal
	// scan: a category/event-type filter narrows what the server returns, so
	// "newest stored id" no longer bounds the older matching records. Disable
	// the gate (do a bounded full pass) when a content filter is active.
	incremental := !*full && !*noStore && *category == "" && *eventType == ""
	if (*category != "" || *eventType != "") && !*full && !*noStore {
		fmt.Fprintln(os.Stderr, "Note: incremental stop disabled with a category/event-type filter; paging the full filtered result.")
	}

	ctx := context.Background()
	logger := log.New(os.Stderr, fmt.Sprintf("[%s] ", *agentID), log.LstdFlags)

	client, wsClient, err := connectClient(ctx, *transport, *agentID, logger, *debug)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	defer func() { _ = client.Close() }()

	// The game server has been observed to close the WS connection shortly
	// after a successful query (same behavior bulk-buy-order works around).
	// ensureConnected reconnects + re-logs in before each page; no-op for mcp.
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

	// Lazily-opened append writer for new entries (created on first new row).
	var (
		storeFile *os.File
		storeW    *bufio.Writer
	)
	defer func() {
		if storeW != nil {
			_ = storeW.Flush()
		}
		if storeFile != nil {
			_ = storeFile.Close()
		}
	}()
	persist := func(e serverapi.ActionLogEntry) error {
		if *noStore {
			return nil
		}
		if storeW == nil {
			if err := os.MkdirAll(filepath.Dir(resolvedStore), 0o755); err != nil {
				return fmt.Errorf("mkdir: %w", err)
			}
			f, err := os.OpenFile(resolvedStore, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
			if err != nil {
				return fmt.Errorf("open store: %w", err)
			}
			storeFile, storeW = f, bufio.NewWriter(f)
		}
		line, err := json.Marshal(e)
		if err != nil {
			return fmt.Errorf("marshal entry %d: %w", e.ID, err)
		}
		if _, err := storeW.Write(append(line, '\n')); err != nil {
			return fmt.Errorf("write entry %d: %w", e.ID, err)
		}
		return nil
	}

	newCount := 0
	caughtUp := false
	for page := 1; !caughtUp; page++ {
		if *maxPages > 0 && page > *maxPages {
			fmt.Fprintf(os.Stderr, "Reached --max-pages=%d, stopping.\n", *maxPages)
			break
		}

		payload := map[string]any{
			"page":      page,
			"page_size": *pageSize,
		}
		if *category != "" {
			payload["category"] = *category
		}
		if *eventType != "" {
			payload["event_type"] = *eventType
		}
		if *factionID != "" {
			payload["faction_id"] = *factionID
		}

		if err := ensureConnected(); err != nil {
			fmt.Fprintf(os.Stderr, "Error before page %d: %v\n", page, err)
			os.Exit(1)
		}
		if err := client.GetActionLog(ctx, payload); err != nil {
			// A mid-call disconnect surfaces here; reconnect and retry once.
			if wsClient != nil {
				logger.Printf("Page %d fetch failed (%v), reconnecting and retrying once", page, err)
				if rerr := wsClient.Reconnect(ctx); rerr != nil {
					fmt.Fprintf(os.Stderr, "Error fetching page %d: %v (reconnect failed: %v)\n", page, err, rerr)
					os.Exit(1)
				}
				time.Sleep(game.SleepQuick)
				err = client.GetActionLog(ctx, payload)
			}
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error fetching page %d: %v\n", page, err)
				os.Exit(1)
			}
		}

		// GetActionLog stores under "action_log" for ws; MCP stuffs the
		// synchronous result under "_last".
		rawKey := "action_log"
		if wsClient == nil {
			rawKey = "_last"
		}
		raw := client.GetRawJSON(rawKey)
		if len(raw) == 0 {
			fmt.Fprintf(os.Stderr, "Page %d: empty response (key=%s)\n", page, rawKey)
			os.Exit(1)
		}
		var resp serverapi.GetActionLogResponse
		if err := json.Unmarshal(raw, &resp); err != nil {
			fmt.Fprintf(os.Stderr, "Page %d: decode error: %v\n", page, err)
			os.Exit(1)
		}

		pageNew := 0
		for _, e := range resp.Entries {
			// Incremental: entries are newest-first, so the first id we've
			// already stored means everything below it is known too.
			if incremental && e.ID <= maxStoredID {
				caughtUp = true
				break
			}
			if _, seen := byID[e.ID]; seen {
				continue
			}
			byID[e.ID] = e
			if err := persist(e); err != nil {
				fmt.Fprintf(os.Stderr, "Error persisting entry %d: %v\n", e.ID, err)
				os.Exit(1)
			}
			newCount++
			pageNew++
		}

		if resp.Total > 0 {
			fmt.Fprintf(os.Stderr, "page %d/%d: %d entries (%d new, %d in store)\n",
				page, resp.TotalPages, len(resp.Entries), pageNew, len(byID))
		} else {
			fmt.Fprintf(os.Stderr, "page %d: %d entries (%d new, %d in store)\n",
				page, len(resp.Entries), pageNew, len(byID))
		}

		if caughtUp || !resp.HasMore || len(resp.Entries) == 0 {
			break
		}
		// Brief pause so a long-paged scan doesn't hammer the server.
		time.Sleep(game.SleepQuick)
	}

	if storeW != nil {
		if err := storeW.Flush(); err != nil {
			fmt.Fprintf(os.Stderr, "Error flushing store: %v\n", err)
			os.Exit(1)
		}
	}

	fmt.Fprintf(os.Stderr, "\nDone: %d new entries this run; %d total in store; %d distinct event_types.\n\n",
		newCount, len(byID), countTypes(byID))

	rows := sortStats(computeStats(byID), *sortBy)

	if *jsonOut {
		out, _ := json.MarshalIndent(rows, "", "  ")
		fmt.Println(string(out))
		return
	}
	fmt.Print(renderTable(rows, len(byID)))
}

// defaultStorePath returns the per-agent JSONL store path. A faction scan gets
// its own file so faction-log entries never mingle with the personal log.
func defaultStorePath(agentID, factionID string) string {
	name := "action_log.jsonl"
	if factionID != "" {
		name = "action_log." + factionID + ".jsonl"
	}
	return filepath.Join("data", "agents", agentID, name)
}

// loadStore reads a JSONL entry store into a map keyed by id and returns the
// highest id seen. A missing file is not an error (empty store).
func loadStore(path string) (map[int]serverapi.ActionLogEntry, int, error) {
	byID := map[int]serverapi.ActionLogEntry{}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return byID, 0, nil
		}
		return nil, 0, err
	}
	defer func() { _ = f.Close() }()

	maxID := 0
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var e serverapi.ActionLogEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			return nil, 0, fmt.Errorf("line %d: %w", lineNo, err)
		}
		byID[e.ID] = e
		if e.ID > maxID {
			maxID = e.ID
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, 0, fmt.Errorf("scan: %w", err)
	}
	return byID, maxID, nil
}

// countTypes returns the number of distinct event_types in the store.
func countTypes(byID map[int]serverapi.ActionLogEntry) int {
	seen := map[string]struct{}{}
	for _, e := range byID {
		seen[e.EventType] = struct{}{}
	}
	return len(seen)
}

// computeStats rolls the full store up per event_type. The example/last_seen
// come from the newest entry of each type (highest id), first_seen from the
// oldest, so the report reflects the cumulative store rather than one run.
func computeStats(byID map[int]serverapi.ActionLogEntry) map[string]*eventStat {
	type acc struct {
		stat         *eventStat
		minID, maxID int
	}
	m := map[string]*acc{}
	for _, e := range byID {
		a := m[e.EventType]
		if a == nil {
			a = &acc{stat: &eventStat{EventType: e.EventType, Category: e.Category}, minID: e.ID, maxID: e.ID}
			a.stat.Example = e.Summary
			a.stat.FirstSeen = e.CreatedAt
			a.stat.LastSeen = e.CreatedAt
			m[e.EventType] = a
		}
		a.stat.Count++
		if a.stat.Category == "" {
			a.stat.Category = e.Category
		}
		if e.ID >= a.maxID {
			a.maxID = e.ID
			a.stat.Example = e.Summary
			a.stat.LastSeen = e.CreatedAt
		}
		if e.ID <= a.minID {
			a.minID = e.ID
			a.stat.FirstSeen = e.CreatedAt
		}
	}
	stats := make(map[string]*eventStat, len(m))
	for et, a := range m {
		stats[et] = a.stat
	}
	return stats
}

// sortStats returns the stats as a slice ordered per the sort key. "count"
// sorts by descending count (event_type as tiebreaker), "name" by event_type,
// "category" by category then descending count; anything else falls back to
// event_type order for deterministic output.
func sortStats(stats map[string]*eventStat, sortBy string) []*eventStat {
	rows := make([]*eventStat, 0, len(stats))
	for _, s := range stats {
		rows = append(rows, s)
	}
	sort.Slice(rows, func(i, j int) bool {
		switch sortBy {
		case "name":
			return rows[i].EventType < rows[j].EventType
		case "category":
			if rows[i].Category != rows[j].Category {
				return rows[i].Category < rows[j].Category
			}
			if rows[i].Count != rows[j].Count {
				return rows[i].Count > rows[j].Count
			}
			return rows[i].EventType < rows[j].EventType
		case "count":
			if rows[i].Count != rows[j].Count {
				return rows[i].Count > rows[j].Count
			}
			return rows[i].EventType < rows[j].EventType
		default:
			return rows[i].EventType < rows[j].EventType
		}
	})
	return rows
}

// renderTable renders the per-type stats as an aligned table with a total row.
func renderTable(rows []*eventStat, total int) string {
	catW, typeW := len("CATEGORY"), len("EVENT_TYPE")
	for _, s := range rows {
		if len(s.Category) > catW {
			catW = len(s.Category)
		}
		if len(s.EventType) > typeW {
			typeW = len(s.EventType)
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%-*s  %-*s  %6s  %s\n", catW, "CATEGORY", typeW, "EVENT_TYPE", "COUNT", "EXAMPLE")
	for _, s := range rows {
		example := s.Example
		if len(example) > 60 {
			example = example[:57] + "..."
		}
		fmt.Fprintf(&b, "%-*s  %-*s  %6d  %s\n", catW, s.Category, typeW, s.EventType, s.Count, example)
	}
	fmt.Fprintf(&b, "%-*s  %-*s  %6d\n", catW, "TOTAL", typeW, "", total)
	return b.String()
}

// connectClient builds an authenticated GameClient for either transport.
// Returns the interface plus the concrete *game.Client when ws (nil for mcp).
func connectClient(ctx context.Context, transport, agentID string, logger *log.Logger, debug bool) (game.GameClient, *game.Client, error) {
	switch transport {
	case "mcp":
		logger.Printf("Using MCP transport")
		mcpClient, _, mcpErr := game.InitializeMCPAgent(agentID, logger, ctx, debug, false)
		if mcpErr != nil {
			return nil, nil, fmt.Errorf("initialize MCP agent: %w", mcpErr)
		}
		return mcpClient, nil, nil
	case "ws":
		logger.Printf("Using WebSocket transport")
		provider := credentials.NewFileProvider("data/agents")
		creds, err := provider.GetCredentials(ctx, agentID)
		if err != nil {
			return nil, nil, fmt.Errorf("load credentials for %q: %w", agentID, err)
		}

		wsClient := game.NewClient(gameServerURL, creds.Username, creds.Password, logger)
		wsClient.SetDebugLogging(debug)

		if err := wsClient.Connect(ctx); err != nil {
			return nil, nil, fmt.Errorf("connect: %w", err)
		}

		<-wsClient.Ready()
		time.Sleep(game.SleepRetry)

		if err := wsClient.Login(ctx); err != nil {
			return nil, nil, fmt.Errorf("login: %w", err)
		}
		time.Sleep(game.SleepQuick)
		return wsClient, wsClient, nil
	default:
		return nil, nil, fmt.Errorf("unknown transport: %s (must be: ws, mcp)", transport)
	}
}
