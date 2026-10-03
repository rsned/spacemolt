// Command banker tops up agents that have run dry.
//
// The fleet has no way to move money: a weekly tax trickle drains accounts that
// cannot replenish, and an agent at zero cannot buy fuel, so it strands, is
// quarantined, and stops existing. On 2026-10-03, 94 of 149 agents sat below
// 10,000 credits and eight had been quarantined for up to twelve days -- one,
// salvager-4, detained a fortnight over a 675-credit bounty -- while a single
// trader held 28M.
//
// Operator-driven by design. It prints the full plan and changes nothing unless
// --execute is passed, because every payment is irreversible.
//
// Usage:
//
//	banker --from trader-10 --floor 10000              # dry run, prints the plan
//	banker --from trader-10 --floor 10000 --execute    # actually sends
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/rsned/spacemolt/pkg/banker"

	_ "modernc.org/sqlite"
)

func main() {
	var (
		statusDir = flag.String("status-dir", "data/overmind", "directory holding the per-fleet *-status.json files")
		assetsDB  = flag.String("assets-db-path", "data/assets.db", "agent asset ledger, for the agent_id -> username map")
		floor     = flag.Float64("floor", 10000, "top every agent up to this credit balance")
		maxTotal  = flag.Float64("max-total", 0, "hard cap on total credits sent this sweep; 0 means no cap")
		maxAge    = flag.Duration("max-age", time.Hour, "refuse a status file older than this")
		from      = flag.String("from", "", "agent id of the funding agent (required)")
		emit      = flag.String("emit-script", "", "write the grants as a play_as script to this path")
	)
	flag.Parse()

	if *from == "" {
		fmt.Fprintln(os.Stderr, "banker: --from is required (the funding agent)")
		os.Exit(2)
	}

	cands, stale, err := banker.Collect(*statusDir, *maxAge, time.Now().UTC())
	if err != nil {
		fmt.Fprintf(os.Stderr, "banker: %v\n", err)
		os.Exit(1)
	}
	for _, f := range stale {
		fmt.Printf("WARNING: fleet %q refused: status file older than %s; its agents were NOT considered\n", f, *maxAge)
	}

	names, err := loadUsernames(*assetsDB)
	if err != nil {
		fmt.Fprintf(os.Stderr, "banker: %v\n", err)
		os.Exit(1)
	}

	plan := banker.PlanTopUp(banker.Attach(cands, names), *floor, *maxTotal, map[string]bool{*from: true})

	fmt.Printf("\nconsidered %d agent(s) across the live status files\n", len(cands))
	fmt.Printf("floor %.0f cr   funding agent %s\n\n", *floor, *from)

	if len(plan.Grants) == 0 {
		fmt.Println("no agent is below the floor; nothing to do")

		return
	}
	fmt.Printf("%-24s %-16s %12s %12s  %s\n", "AGENT", "FLEET", "CREDITS", "SEND", "USERNAME")
	for _, g := range plan.Grants {
		fmt.Printf("%-24s %-16s %12.0f %12.0f  %s\n", g.AgentID, g.Fleet, g.Credits, g.Amount, g.Username)
	}
	fmt.Printf("\n%d grant(s), %.0f credits total\n", len(plan.Grants), plan.Total)

	for _, s := range plan.Skips {
		fmt.Printf("  skipped %-24s %s\n", s.AgentID, s.Reason)
	}

	if *emit == "" {
		fmt.Println("\nno --emit-script given: nothing was written and nothing will be sent")

		return
	}
	if werr := os.WriteFile(*emit, []byte(banker.Script(plan.Grants)), 0o600); werr != nil {
		fmt.Fprintf(os.Stderr, "banker: write script: %v\n", werr)
		os.Exit(1)
	}
	fmt.Printf("\nwrote %s -- review it, then run inside play_as as %s:\n  run %s\n", *emit, *from, *emit)
}

// loadUsernames reads the agent_id -> username map. send_gift addresses the
// username, and it differs from the agent id for most of the fleet.
func loadUsernames(path string) (banker.Usernames, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = db.Close() }()

	rows, err := db.QueryContext(context.Background(),
		`SELECT agent_id, username FROM agents WHERE username != ''`)
	if err != nil {
		return nil, fmt.Errorf("query agents: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := banker.Usernames{}
	for rows.Next() {
		var id, user string
		if serr := rows.Scan(&id, &user); serr != nil {
			return nil, fmt.Errorf("scan agents: %w", serr)
		}
		out[id] = user
	}

	return out, rows.Err()
}
