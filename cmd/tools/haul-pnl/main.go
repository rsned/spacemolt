// haul-pnl reconciles each hauler's TRUE realized profit-and-loss from the
// server-side action log against the numbers recorded in market.db.
//
// It reads the per-agent JSONL store that action-log-scan persists
// (data/agents/<agent>/action_log.jsonl) and folds every trading.exchange_fill
// into real spent/earned totals — buyer fills are money out, seller fills are
// money in — with ship.refuel total_cost as the fuel deduction. Because it works
// off the persisted log it makes NO network calls and never collides with a live
// agent session. To refresh an agent's log first, run:
//
//	action-log-scan --agent=<id> --full
//
// When market.db is readable it also pulls the recorded haul_results realized
// profit per agent and shows the delta, exposing how far the recorded numbers
// drift from the real fills.
//
// Usage:
//
//	haul-pnl                                  # all salvager-*/trader-* with a log
//	haul-pnl --agents=salvager-6,trader-2     # specific agents
//	haul-pnl --no-fuel                        # gross of fuel
//	haul-pnl --json                           # machine-readable
//	haul-pnl --market-db=                     # skip the recorded comparison
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/market"
)

// AgentPnL is one hauler's reconstructed trading outcome over the stored log.
type AgentPnL struct {
	AgentID   string `json:"agent_id"`
	Buys      int    `json:"buys"`
	Sells     int    `json:"sells"`
	Bought    int    `json:"bought"`     // Σ total of buyer fills (credits spent)
	Sold      int    `json:"sold"`       // Σ total of seller fills (credits earned)
	FuelCost  int    `json:"fuel_cost"`  // Σ ship.refuel total_cost
	FirstFill string `json:"first_fill"` // created_at of the earliest exchange_fill
	LastFill  string `json:"last_fill"`  // created_at of the latest exchange_fill

	// Recorded comparison (populated only when market.db is read).
	RecordedNet   int  `json:"recorded_net"`
	RecordedHauls int  `json:"recorded_hauls"`
	HasRecorded   bool `json:"has_recorded"`
}

// TrueNet is the real round-trip profit: earned minus spent, optionally net of
// the fuel the agent burned over the same window.
func (p AgentPnL) TrueNet(includeFuel bool) int {
	n := p.Sold - p.Bought
	if includeFuel {
		n -= p.FuelCost
	}
	return n
}

// numData reads a numeric action-log data field. JSON numbers decode to float64
// through the generic map, so this narrows them to int credits.
func numData(data map[string]any, key string) int {
	if v, ok := data[key].(float64); ok {
		return int(v)
	}
	return 0
}

// computePnL folds an agent's action-log entries into its trading P&L. Only
// trading.exchange_fill (split by role) and ship.refuel (fuel) contribute; every
// other event_type is ignored. The fill window is bounded by exchange_fills only.
func computePnL(agentID string, entries []serverapi.ActionLogEntry) AgentPnL {
	p := AgentPnL{AgentID: agentID}
	for _, e := range entries {
		switch e.EventType {
		case "trading.exchange_fill":
			total := numData(e.Data, "total")
			switch role, _ := e.Data["role"].(string); role {
			case "buyer":
				p.Bought += total
				p.Buys++
			case "seller":
				p.Sold += total
				p.Sells++
			}
			if p.FirstFill == "" || e.CreatedAt < p.FirstFill {
				p.FirstFill = e.CreatedAt
			}
			if e.CreatedAt > p.LastFill {
				p.LastFill = e.CreatedAt
			}
		case "ship.refuel":
			p.FuelCost += numData(e.Data, "total_cost")
		}
	}
	return p
}

// loadEntries reads a JSONL action-log store into a slice. A missing file yields
// an empty slice (the agent has no pulled log yet), not an error.
func loadEntries(path string) ([]serverapi.ActionLogEntry, error) {
	f, err := os.Open(path) //nolint:gosec // operator-supplied agent log path
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var entries []serverapi.ActionLogEntry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	line := 0
	for sc.Scan() {
		line++
		text := strings.TrimSpace(sc.Text())
		if text == "" {
			continue
		}
		var e serverapi.ActionLogEntry
		if err := json.Unmarshal([]byte(text), &e); err != nil {
			return nil, fmt.Errorf("%s line %d: %w", path, line, err)
		}
		entries = append(entries, e)
	}
	return entries, sc.Err()
}

// discoverAgents lists salvager-*/trader-* agent ids under dir that have a
// persisted action_log.jsonl, sorted for stable output.
func discoverAgents(dir string) []string {
	var ids []string
	for _, prefix := range []string{"salvager-", "trader-"} {
		matches, _ := filepath.Glob(filepath.Join(dir, prefix+"*"))
		for _, m := range matches {
			if _, err := os.Stat(filepath.Join(m, "action_log.jsonl")); err == nil {
				ids = append(ids, filepath.Base(m))
			}
		}
	}
	sort.Strings(ids)
	return ids
}

// recordedByAgent sums recorded haul_results realized profit and haul count per
// agent from market.db. Returns an empty map (not an error) when the db cannot be
// opened, so the tool still reports true P&L offline.
func recordedByAgent(ctx context.Context, dbPath string, agents []string) map[string]AgentPnL {
	out := map[string]AgentPnL{}
	if dbPath == "" {
		return out
	}
	col, err := market.Open(market.Config{DBPath: dbPath, WAL: true})
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: cannot open market.db %s (%v); skipping recorded comparison\n", dbPath, err)
		return out
	}
	defer func() { _ = col.Close() }()

	for _, id := range agents {
		results, err := col.GetHaulResults(ctx, id, 1_000_000)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: GetHaulResults(%s): %v\n", id, err)
			continue
		}
		var net float64
		for _, r := range results {
			net += r.RealizedProfit
		}
		out[id] = AgentPnL{RecordedNet: int(net), RecordedHauls: len(results), HasRecorded: true}
	}
	return out
}

func main() {
	agentsFlag := flag.String("agents", "", "Comma-separated agent ids (default: all salvager-*/trader-* with a log)")
	agentsDir := flag.String("agents-dir", "data/agents", "Directory holding per-agent dirs + action_log.jsonl")
	marketDB := flag.String("market-db", "data/market.db", "market.db for the recorded comparison (empty = skip)")
	includeFuel := flag.Bool("fuel", true, "Deduct ship.refuel total_cost from net (use --fuel=false for gross)")
	jsonOut := flag.Bool("json", false, "Emit JSON instead of a table")
	flag.Parse()

	var agents []string
	if *agentsFlag != "" {
		for a := range strings.SplitSeq(*agentsFlag, ",") {
			if a = strings.TrimSpace(a); a != "" {
				agents = append(agents, a)
			}
		}
	} else {
		agents = discoverAgents(*agentsDir)
	}
	if len(agents) == 0 {
		fmt.Fprintln(os.Stderr, "No agents found. Pull logs first: action-log-scan --agent=<id> --full")
		os.Exit(1)
	}

	ctx := context.Background()
	recorded := recordedByAgent(ctx, *marketDB, agents)

	rows := make([]AgentPnL, 0, len(agents))
	for _, id := range agents {
		entries, err := loadEntries(filepath.Join(*agentsDir, id, "action_log.jsonl"))
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: %v\n", err)
			continue
		}
		p := computePnL(id, entries)
		if rec, ok := recorded[id]; ok {
			p.RecordedNet, p.RecordedHauls, p.HasRecorded = rec.RecordedNet, rec.RecordedHauls, rec.HasRecorded
		}
		rows = append(rows, p)
	}

	if *jsonOut {
		out, _ := json.MarshalIndent(rows, "", "  ")
		fmt.Println(string(out))
		return
	}
	fmt.Print(renderTable(rows, *includeFuel))
}

// renderTable renders the per-agent reconciliation with a fleet totals row.
func renderTable(rows []AgentPnL, includeFuel bool) string {
	var b strings.Builder
	header := fmt.Sprintf("%-12s  %5s  %5s  %12s  %12s  %10s  %12s  %12s  %12s\n",
		"AGENT", "BUYS", "SELLS", "BOUGHT", "SOLD", "FUEL", "TRUE_NET", "RECORDED", "Δ(TRUE-REC)")
	b.WriteString(header)
	b.WriteString(strings.Repeat("-", len(header)) + "\n")

	var tBuys, tSells, tBought, tSold, tFuel, tTrue, tRec int
	anyRecorded := false
	for _, p := range rows {
		trueNet := p.TrueNet(includeFuel)
		rec, delta := "n/a", "n/a"
		if p.HasRecorded {
			anyRecorded = true
			rec = fmt.Sprintf("%d (%d)", p.RecordedNet, p.RecordedHauls)
			delta = fmt.Sprintf("%+d", trueNet-p.RecordedNet)
			tRec += p.RecordedNet
		}
		fmt.Fprintf(&b, "%-12s  %5d  %5d  %12d  %12d  %10d  %12d  %12s  %12s\n",
			p.AgentID, p.Buys, p.Sells, p.Bought, p.Sold, p.FuelCost, trueNet, rec, delta)
		tBuys += p.Buys
		tSells += p.Sells
		tBought += p.Bought
		tSold += p.Sold
		tFuel += p.FuelCost
		tTrue += trueNet
	}

	b.WriteString(strings.Repeat("-", len(header)) + "\n")
	totalRec, totalDelta := "n/a", "n/a"
	if anyRecorded {
		totalRec = fmt.Sprintf("%d", tRec)
		totalDelta = fmt.Sprintf("%+d", tTrue-tRec)
	}
	fmt.Fprintf(&b, "%-12s  %5d  %5d  %12d  %12d  %10d  %12d  %12s  %12s\n",
		"FLEET", tBuys, tSells, tBought, tSold, tFuel, tTrue, totalRec, totalDelta)
	if includeFuel {
		b.WriteString("\nTRUE_NET = sold - bought - fuel  (per real exchange_fill + ship.refuel totals)\n")
	} else {
		b.WriteString("\nTRUE_NET = sold - bought  (gross of fuel; per real exchange_fill totals)\n")
	}
	return b.String()
}
