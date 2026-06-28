// Command haul-distances reports, for each hauler in the live fleet-status
// file, the jump distance from its current system to the nearest AVAILABLE
// arbitrage opportunity's buy system — using the same jump graph and name
// resolution the hauler ranker uses. It quantifies how badly the distance cap
// starves each hauler (nearest opp = 6? 7? 15 jumps?).
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"

	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/market"
	"github.com/rsned/spacemolt/pkg/navigation"
	"github.com/rsned/spacemolt/pkg/overmind/balances"
)

func main() {
	kbPath := flag.String("kb-path", "data/spacemolt-knowledge.db", "Knowledge base path")
	marketDB := flag.String("market-db", "data/market.db", "Market database path")
	statusPath := flag.String("status-file", "data/overmind/fleet-status.json", "Live fleet status file")
	cap_ := flag.Int("cap", 5, "Distance cap the haulers currently use (for the within-cap count)")
	flag.Parse()

	if err := run(*kbPath, *marketDB, *statusPath, *cap_); err != nil {
		fmt.Fprintln(os.Stderr, "haul-distances:", err)
		os.Exit(1)
	}
}

func run(kbPath, marketDB, statusPath string, cap int) error {
	ctx := context.Background()

	kb, err := knowledge.NewSQLiteKB(knowledge.Config{DBPath: kbPath, WAL: true})
	if err != nil {
		return fmt.Errorf("open kb: %w", err)
	}
	defer func() { _ = kb.Close() }()
	systems, err := kb.GetSystems(ctx)
	if err != nil {
		return fmt.Errorf("get systems: %w", err)
	}
	conns, err := kb.GetConnections(ctx)
	if err != nil {
		return fmt.Errorf("get connections: %w", err)
	}
	nameToID := map[string]string{}
	for _, s := range systems {
		if s.ID != "" {
			nameToID[s.ID] = s.ID
		}
		if s.Name != "" {
			nameToID[s.Name] = s.ID
		}
	}
	graph := navigation.JumpGraphFromConnections(conns)

	coll, err := market.Open(market.Config{DBPath: marketDB})
	if err != nil {
		return fmt.Errorf("open market: %w", err)
	}
	defer func() { _ = coll.Close() }()
	opps, err := coll.GetOpportunities(ctx, "available", 100000)
	if err != nil {
		return fmt.Errorf("get opportunities: %w", err)
	}

	// Distinct reachable buy systems, with a representative opp for labelling.
	targetSet := map[string]bool{}
	var targets []string
	oppByBuy := map[string]market.ArbitrageOpportunity{}
	for _, o := range opps {
		id := nameToID[o.FromSystemName]
		if id == "" {
			continue
		}
		if !targetSet[id] {
			targetSet[id] = true
			targets = append(targets, id)
			oppByBuy[id] = o
		}
	}

	sf, err := balances.ReadStatus(statusPath)
	if err != nil {
		return fmt.Errorf("read status: %w", err)
	}
	if len(sf.Workers) == 0 {
		fmt.Printf("No fleet status at %s — overmind running?\n", statusPath)
		return nil
	}

	type row struct {
		agent, sys, nearestItem, nearestSys string
		nearest, withinCap                  int
		unresolved                          bool
	}
	var rows []row
	for _, w := range sf.Workers {
		r := row{agent: w.AgentID, sys: w.System, nearest: navigation.RouteInf}
		src := nameToID[w.System]
		if src == "" {
			r.unresolved = true
			rows = append(rows, r)
			continue
		}
		dist := navigation.BFSJumps(graph, src, targets)
		for id, d := range dist {
			// BFSJumps seeds out[src]=0 unconditionally; only count systems that
			// actually host an available opportunity's buy station.
			if !targetSet[id] || d >= navigation.RouteInf {
				continue
			}
			if d <= cap {
				r.withinCap++
			}
			if d < r.nearest {
				r.nearest = d
				o := oppByBuy[id]
				r.nearestItem, r.nearestSys = o.ItemID, o.FromSystemName
			}
		}
		rows = append(rows, r)
	}

	// Worst (farthest from any opp / fewest within cap) first.
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].nearest != rows[j].nearest {
			return rows[i].nearest > rows[j].nearest
		}
		return rows[i].withinCap < rows[j].withinCap
	})

	fmt.Printf("\nHauler distance-to-nearest-opportunity (%d available opps across %d buy systems, cap=%d)\n\n",
		len(opps), len(targets), cap)
	fmt.Printf("%-12s %-22s %8s %12s   %s\n", "AGENT", "CURRENT SYSTEM", "NEAREST", "OPPS<=CAP", "NEAREST OPP")
	fmt.Println("---------------------------------------------------------------------------------------------")
	starved := 0
	for _, r := range rows {
		if r.unresolved {
			fmt.Printf("%-12s %-22s %8s %12s   %s\n", r.agent, trunc(r.sys, 22), "?", "?", "(system unresolved in KB)")
			continue
		}
		nearest := fmt.Sprintf("%d", r.nearest)
		if r.nearest >= navigation.RouteInf {
			nearest = "∞"
		}
		if r.withinCap == 0 {
			starved++
		}
		fmt.Printf("%-12s %-22s %8s %12d   %s @ %s\n",
			r.agent, trunc(r.sys, 22), nearest, r.withinCap, r.nearestItem, r.nearestSys)
	}
	fmt.Printf("\n%d/%d haulers have ZERO opportunities within the cap of %d jumps (starved → idling).\n", starved, len(rows), cap)
	return nil
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
