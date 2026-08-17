// Command fleet-deaths reports how often each agent has died, from the asset
// ledger's agent_stats table.
//
// It opens only the ledger — no game login — so it works while all 160 workers
// hold their sessions. That is the whole reason it is a separate tool rather
// than a play_as command: the agents whose numbers you want are exactly the ones
// you cannot log in as.
//
// The counters are lifetime totals the server keeps, captured hourly by every
// worker. An agent missing from the output has not been captured since the
// stats capture shipped, which is not the same as never having died.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"text/tabwriter"

	"github.com/rsned/spacemolt/pkg/assets"
)

func main() {
	dbPath := flag.String("assets-db-path", "data/assets.db", "Path to the agent asset ledger")
	onlyDead := flag.Bool("only-dead", false, "Show only agents with at least one death")
	flag.Parse()

	cfg := assets.DefaultConfig()
	cfg.DBPath = *dbPath
	store, err := assets.Open(cfg)
	if err != nil {
		log.Fatalf("open assets db %s: %v", *dbPath, err)
	}
	defer func() { _ = store.Close() }()

	rows, err := store.FleetDeaths(context.Background())
	if err != nil {
		log.Fatalf("read fleet deaths: %v", err)
	}
	if len(rows) == 0 {
		fmt.Println("No agent_stats rows yet. Every worker writes one on its next hourly")
		fmt.Println("capture, but only once it is running a binary that includes the stats capture.")

		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "AGENT\tDEATHS\tPIRATE\tPLAYER\tWILDLIFE\tSELF\tSHIPS LOST\tCLAIMS\tPAYOUTS\tAS OF")
	var totals assets.Stats
	shown := 0
	for _, r := range rows {
		s := r.Stats
		totals.DeathsByPirate += s.DeathsByPirate
		totals.DeathsByPlayer += s.DeathsByPlayer
		totals.DeathsByWildlife += s.DeathsByWildlife
		totals.DeathsBySelfDestruct += s.DeathsBySelfDestruct
		totals.ShipsLost += s.ShipsLost
		totals.InsuranceClaimsMade += s.InsuranceClaimsMade
		totals.InsurancePayoutsReceived += s.InsurancePayoutsReceived
		if *onlyDead && s.Deaths() == 0 {
			continue
		}
		shown++
		name := r.AgentID
		if name == "" {
			name = r.PlayerID
		}
		_, _ = fmt.Fprintf(w, "%s\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%s\n",
			name, s.Deaths(), s.DeathsByPirate, s.DeathsByPlayer,
			s.DeathsByWildlife, s.DeathsBySelfDestruct, s.ShipsLost,
			s.InsuranceClaimsMade, s.InsurancePayoutsReceived, s.CapturedAt)
	}
	_, _ = fmt.Fprintf(w, "\nTOTAL (%d agents)\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t\n",
		len(rows), totals.Deaths(), totals.DeathsByPirate, totals.DeathsByPlayer,
		totals.DeathsByWildlife, totals.DeathsBySelfDestruct, totals.ShipsLost,
		totals.InsuranceClaimsMade, totals.InsurancePayoutsReceived)
	_ = w.Flush()

	// ShipsLost is not the death count and the two disagree in live data, so the
	// footer states both rather than letting a reader infer one from the other.
	fmt.Printf("\n%d agent(s) listed. Deaths are per-cause lifetime counters; SHIPS LOST is a\n", shown)
	fmt.Println("separate counter and does not equal their sum — a death does not always cost a hull.")
}
