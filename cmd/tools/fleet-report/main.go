// Command fleet-report prints a per-agent hauler report: current location and
// credits, the agent's starting balance, total and since-midnight credit
// deltas, and realized arbitrage profit.
//
// It reads the live status file and the daily balance history written by the
// overmind (see pkg/overmind/balances) and joins realized profit from the
// market database's completed arbitrage opportunities. It is read-only and
// safe to run on a schedule or on demand while the fleet is live.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/market"
	"github.com/rsned/spacemolt/pkg/overmind/balances"
)

// errWriter accumulates the first write error so a long sequence of Fprintf
// calls needs a single error check at the end.
type errWriter struct {
	w   io.Writer
	err error
}

func (e *errWriter) printf(format string, a ...any) {
	if e.err != nil {
		return
	}
	_, e.err = fmt.Fprintf(e.w, format, a...)
}

func main() {
	statusPath := flag.String("status-file", "data/overmind/fleet-status.json", "Live fleet status snapshot file")
	historyPath := flag.String("history-file", "data/overmind/fleet-history.jsonl", "Daily balance history file")
	marketDB := flag.String("market-db", "data/market.db", "Market database for realized arbitrage profit")
	flag.Parse()

	if err := run(*statusPath, *historyPath, *marketDB, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "fleet-report:", err)
		os.Exit(1)
	}
}

func run(statusPath, historyPath, marketDB string, out io.Writer) error {
	sf, err := balances.ReadStatus(statusPath)
	if err != nil {
		return err
	}
	if len(sf.Workers) == 0 {
		_, err := fmt.Fprintf(out, "No fleet status found at %s — is the overmind running?\n", statusPath)
		return err
	}
	history, err := balances.ReadHistory(historyPath)
	if err != nil {
		return err
	}
	profit := loadProfit(marketDB, out)

	reports := balances.BuildReport(sf.Workers, history, profit)
	return printReport(out, sf, reports)
}

// loadProfit aggregates realized gross profit per agent from completed
// arbitrage opportunities. A market-db failure degrades to no profit data
// rather than failing the whole report.
func loadProfit(marketDB string, out io.Writer) map[string]balances.ProfitAgg {
	coll, err := market.Open(market.Config{DBPath: marketDB})
	if err != nil {
		fmt.Fprintf(out, "(realized profit unavailable: %v)\n", err) //nolint:errcheck // diagnostic line
		return nil
	}
	defer func() { _ = coll.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	opps, err := coll.GetOpportunities(ctx, "completed", 100000)
	if err != nil {
		fmt.Fprintf(out, "(realized profit unavailable: %v)\n", err) //nolint:errcheck // diagnostic line
		return nil
	}
	agg := make(map[string]balances.ProfitAgg)
	for _, o := range opps {
		p := agg[o.ClaimedBy]
		p.Hauls++
		p.Gross += o.GrossProfit
		agg[o.ClaimedBy] = p
	}
	return agg
}

func printReport(w io.Writer, sf balances.StatusFile, reports []balances.AgentReport) error {
	out := &errWriter{w: w}
	age := ""
	if t, err := time.Parse(time.RFC3339, sf.CapturedAt); err == nil {
		age = fmt.Sprintf(" (%s old)", time.Since(t).Round(time.Second))
	}
	out.printf("\nHauler Fleet Report — status captured %s%s\n\n", sf.CapturedAt, age)
	out.printf("%-10s %-9s %-26s %12s %12s %12s %12s %6s %12s\n",
		"AGENT", "ROLE", "LOCATION", "CREDITS", "START BAL", "TOTAL Δ", "SINCE 00:00", "HAULS", "REALIZED P")
	out.printf("%s\n", strings.Repeat("-", 122))

	var totCredits, totDelta, totRealized float64
	var totHauls int
	for _, r := range reports {
		loc := r.System
		if r.POI != "" {
			loc = r.System + "/" + r.POI
		}
		if !r.Seen {
			loc = "(no heartbeat)"
		}
		start, total, since := "—", "—", "—"
		if r.DaysTracked > 0 {
			start = comma(r.StartingBalance)
			total = signed(r.TotalDelta)
			since = signed(r.SinceSnapshot)
		}
		out.printf("%-10s %-9s %-26s %12s %12s %12s %12s %6d %12s\n",
			r.AgentID, trunc(r.Role, 9), trunc(loc, 26), comma(r.Credits),
			start, total, since, r.Hauls, comma(r.RealizedProfit))
		totCredits += r.Credits
		totDelta += r.TotalDelta
		totRealized += r.RealizedProfit
		totHauls += r.Hauls
	}
	out.printf("%s\n", strings.Repeat("-", 122))
	out.printf("%-10s %-9s %-26s %12s %12s %12s\n",
		"TOTALS", "", fmt.Sprintf("%d agents", len(reports)), comma(totCredits), "", signed(totDelta))
	out.printf("\nFleet realized profit: %s credits over %d completed hauls.\n", comma(totRealized), totHauls)
	if days := maxDays(reports); days > 0 {
		out.printf("Balance history: %d day(s) tracked. START BAL is each agent's first recorded balance; TOTAL Δ is vs. that; SINCE 00:00 is vs. the latest midnight snapshot.\n", days)
	} else {
		out.printf("No daily snapshots recorded yet — the first will land at the next overmind tick (establishing starting balances).\n")
	}
	return out.err
}

func maxDays(reports []balances.AgentReport) int {
	m := 0
	for _, r := range reports {
		if r.DaysTracked > m {
			m = r.DaysTracked
		}
	}
	return m
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// comma formats a float as a thousands-separated integer string.
func comma(v float64) string {
	neg := v < 0
	n := int64(v)
	if neg {
		n = -n
	}
	s := fmt.Sprintf("%d", n)
	var parts []string
	for len(s) > 3 {
		parts = append([]string{s[len(s)-3:]}, parts...)
		s = s[:len(s)-3]
	}
	parts = append([]string{s}, parts...)
	out := strings.Join(parts, ",")
	if neg {
		return "-" + out
	}
	return out
}

// signed formats a delta with an explicit + or - sign.
func signed(v float64) string {
	if v >= 0 {
		return "+" + comma(v)
	}
	return comma(v)
}
