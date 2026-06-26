// Command arbitrage-scanner detects cross-station buy-low/sell-high spreads in
// data/market.db, persists them to arbitrage_opportunities, and lists/claims/
// completes them. See docs/superpowers/specs/2026-06-24-arbitrage-scanner-design.md.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/rsned/spacemolt/pkg/market"
)

const usage = `arbitrage-scanner — detect and manage cross-station arbitrage opportunities.

Usage:
  arbitrage-scanner scan     [flags]   detect spreads and write opportunities (default)
  arbitrage-scanner watch    [flags]   scan on a recurring, boundary-aligned schedule
  arbitrage-scanner list     [flags]   list opportunities
  arbitrage-scanner claim    --id N --agent X
  arbitrage-scanner complete --id N --agent X

Scan flags:
  --market-db-path PATH   path to market SQLite DB (default data/market.db)
  --min-profit FLOAT       gross-profit floor (default 1000)
  --min-price FLOAT        per-order price floor, filters basement orders (default 10)
  --min-quantity FLOAT     per-order depth floor (default 1)
  --expires DURATION       opportunity TTL (default 6h)
  --items a,b,c            item allowlist (default: all traded items)
  --limit N                cap on inserted rows (default 500)
  --json                   machine-readable output (scan only)

Watch flags (in addition to the scan flags above):
  --interval DURATION      scan cadence, aligned to UTC boundaries (default 30m → :00/:30)
  --offset DURATION        delay past each boundary for the capture to settle (default 5m)
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "arbitrage-scanner:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return runScan(defaultScanArgs())
	}
	switch args[0] {
	case "scan":
		cfg, err := parseScanArgs(args[1:])
		if err != nil {
			return err
		}
		return runScan(cfg)
	case "watch":
		return runWatch(args[1:])
	case "list":
		return runList(args[1:])
	case "claim":
		return runClaim(args[1:])
	case "complete":
		return runComplete(args[1:])
	case "-h", "--help", "help":
		fmt.Print(usage)
		return nil
	default:
		if strings.HasPrefix(args[0], "-") { // flags with no subcommand → default scan
			cfg, err := parseScanArgs(args)
			if err != nil {
				return err
			}
			return runScan(cfg)
		}
		return fmt.Errorf("unknown subcommand %q (want scan|watch|list|claim|complete)", args[0])
	}
}

// splitItems parses a comma-separated item allowlist, dropping blanks.
func splitItems(items string) []string {
	var out []string
	for s := range strings.SplitSeq(items, ",") {
		if t := strings.TrimSpace(s); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// scanConfig holds parsed scan flags.
type scanConfig struct {
	dbPath string
	opts   market.ScanOptions
	asJSON bool
}

func defaultScanArgs() scanConfig {
	return scanConfig{
		dbPath: "data/market.db",
		opts: market.ScanOptions{
			MinProfit: 1000, MinPrice: 10, MinQuantity: 1,
			ExpiresIn: 6 * time.Hour, Limit: 500,
		},
	}
}

func parseScanArgs(args []string) (scanConfig, error) {
	cfg := defaultScanArgs()
	var items string
	fs := flag.NewFlagSet("scan", flag.ContinueOnError)
	fs.StringVar(&cfg.dbPath, "market-db-path", cfg.dbPath, "path to market SQLite database")
	fs.Float64Var(&cfg.opts.MinProfit, "min-profit", cfg.opts.MinProfit, "gross-profit floor")
	fs.Float64Var(&cfg.opts.MinPrice, "min-price", cfg.opts.MinPrice, "per-order price floor (filters basement orders)")
	fs.Float64Var(&cfg.opts.MinQuantity, "min-quantity", cfg.opts.MinQuantity, "per-order depth floor")
	fs.DurationVar(&cfg.opts.ExpiresIn, "expires", cfg.opts.ExpiresIn, "opportunity TTL")
	fs.StringVar(&items, "items", "", "comma-separated item allowlist (default: all traded items)")
	fs.IntVar(&cfg.opts.Limit, "limit", cfg.opts.Limit, "cap on inserted rows")
	fs.BoolVar(&cfg.asJSON, "json", false, "machine-readable output")
	if err := fs.Parse(args); err != nil {
		return cfg, err
	}
	cfg.opts.Items = splitItems(items)
	return cfg, nil
}

func runScan(cfg scanConfig) error {
	c, err := market.Open(market.Config{DBPath: cfg.dbPath})
	if err != nil {
		return fmt.Errorf("open %s: %w", cfg.dbPath, err)
	}
	defer func() { _ = c.Close() }()

	res, err := c.ScanArbitrage(context.Background(), cfg.opts)
	if err != nil {
		return fmt.Errorf("scan: %w", err)
	}
	if cfg.asJSON {
		out := map[string]any{
			"expired":      res.Expired,
			"inserted":     res.Inserted,
			"generated_at": res.GeneratedAt,
		}
		if top, err := c.GetOpportunities(context.Background(), "available", 20); err == nil {
			out["top"] = top
		}
		return json.NewEncoder(os.Stdout).Encode(out)
	}
	fmt.Printf("scan complete: expired %d available, inserted %d opportunities (at %s)\n",
		res.Expired, res.Inserted, res.GeneratedAt.Format(time.RFC3339))
	return nil
}

func runList(args []string) error {
	dbPath := "data/market.db"
	status := "available"
	limit := 50
	asJSON := false
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.StringVar(&dbPath, "market-db-path", dbPath, "path to market SQLite database")
	fs.StringVar(&status, "status", status, "status filter (available|claimed|completed|expired|\"\")")
	fs.IntVar(&limit, "limit", limit, "row cap")
	fs.BoolVar(&asJSON, "json", false, "machine-readable output")
	if err := fs.Parse(args); err != nil {
		return err
	}

	c, err := market.Open(market.Config{DBPath: dbPath})
	if err != nil {
		return fmt.Errorf("open %s: %w", dbPath, err)
	}
	defer func() { _ = c.Close() }()

	opps, err := c.GetOpportunities(context.Background(), status, limit)
	if err != nil {
		return fmt.Errorf("list: %w", err)
	}
	if asJSON {
		return json.NewEncoder(os.Stdout).Encode(opps)
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(w, "ID\tITEM\tFROM→TO\tBUY\tSELL\tQTY\tGROSS\tSTATUS\tEXPIRES"); err != nil {
		return err
	}
	for _, o := range opps {
		if _, err := fmt.Fprintf(w, "%d\t%s\t%s→%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			o.ID, o.ItemID, o.FromStationID, o.ToStationID,
			fmtPrice(o.BuyPrice), fmtPrice(o.SellPrice), fmtPrice(o.Quantity), fmtPrice(o.GrossProfit),
			o.Status, o.ExpiresAt); err != nil {
			return err
		}
	}
	return w.Flush()
}

func runClaim(args []string) error {
	dbPath, id, agent, err := parseIDAgent("claim", args)
	if err != nil {
		return err
	}
	c, err := market.Open(market.Config{DBPath: dbPath})
	if err != nil {
		return fmt.Errorf("open %s: %w", dbPath, err)
	}
	defer func() { _ = c.Close() }()
	ok, err := c.ClaimOpportunity(context.Background(), id, agent)
	if err != nil {
		return fmt.Errorf("claim: %w", err)
	}
	if ok {
		fmt.Printf("claimed opportunity %d by %s\n", id, agent)
	} else {
		fmt.Printf("unavailable: opportunity %d is not claimable\n", id)
	}
	return nil
}

func runComplete(args []string) error {
	dbPath, id, agent, err := parseIDAgent("complete", args)
	if err != nil {
		return err
	}
	c, err := market.Open(market.Config{DBPath: dbPath})
	if err != nil {
		return fmt.Errorf("open %s: %w", dbPath, err)
	}
	defer func() { _ = c.Close() }()
	ok, err := c.CompleteOpportunity(context.Background(), id, agent)
	if err != nil {
		return fmt.Errorf("complete: %w", err)
	}
	if ok {
		fmt.Printf("completed opportunity %d\n", id)
	} else {
		fmt.Printf("not-owned: opportunity %d is not completable by %s\n", id, agent)
	}
	return nil
}

func parseIDAgent(name string, args []string) (dbPath string, id int, agent string, err error) {
	dbPath = "data/market.db"
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.StringVar(&dbPath, "market-db-path", dbPath, "path to market SQLite database")
	fs.IntVar(&id, "id", 0, "opportunity id")
	fs.StringVar(&agent, "agent", "", "agent id")
	if err = fs.Parse(args); err != nil {
		return
	}
	if id == 0 {
		err = fmt.Errorf("--id is required")
		return
	}
	if agent == "" {
		err = fmt.Errorf("--agent is required")
		return
	}
	return
}

func fmtPrice(n float64) string { return strconv.FormatFloat(n, 'f', -1, 64) }
