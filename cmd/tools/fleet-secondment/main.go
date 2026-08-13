// Command fleet-secondment moves agents between fleets on a loan, one safe step
// per sweep.
//
// A hauler that finishes a delivery in nebula space nominates itself for the
// pirate-unlock chain (whose giver sits there), because running the chain from
// where it already stands is nearly free while a dedicated trip is a 20+ jump
// deadhead each way. This command is what acts on those nominations: it moves the
// agent out of haul, into unlock, and back again once it holds the unlock — so
// the haul fleet converts, one hull at a time, into a fully stronghold-capable
// fleet of the same size.
//
// The one invariant worth the whole program: an agent is never started in the
// second fleet until the first has confirmably stopped it. Running the same agent
// twice makes the game server close the older session (status 4001,
// session_replaced) and BOTH copies lose their connection.
//
// Usage:
//
//	fleet-secondment --once            # one sweep, print what moved
//	fleet-secondment --watch 5m        # sweep on an interval
//	fleet-secondment --status          # print the ledger, change nothing
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/rsned/spacemolt/pkg/assets"
	"github.com/rsned/spacemolt/pkg/overmind/supervisor"
)

func main() {
	var (
		ledger     = flag.String("ledger", "data/overmind/secondments.json", "Secondment ledger path")
		homeName   = flag.String("home", "haul", "Home fleet name")
		homeOv     = flag.String("home-overrides", "data/overmind/haul-overrides.json", "Home fleet membership sidecar")
		homeSock   = flag.String("home-socket", "data/overmind/haul.sock", "Home fleet control socket")
		awayName   = flag.String("away", "unlock", "Away fleet name")
		awayOv     = flag.String("away-overrides", "data/overmind/unlock-overrides.json", "Away fleet membership sidecar")
		awaySock   = flag.String("away-socket", "data/overmind/unlock.sock", "Away fleet control socket")
		maxFlight  = flag.Int("max-in-flight", 1, "How many agents may be away from home at once")
		stopWait   = flag.Duration("stop-timeout", 90*time.Second, "How long to wait for a worker to exit before failing the trip")
		watch      = flag.Duration("watch", 0, "Sweep repeatedly on this interval (0 = single sweep)")
		once       = flag.Bool("once", false, "Run a single sweep (default when --watch is unset)")
		showStatus = flag.Bool("status", false, "Print the ledger and exit without changing anything")
		assetsDB   = flag.String("assets-db-path", "data/assets.db", "Agent asset ledger, read to tell whether a seconded agent has earned the pirate unlock yet (empty = nobody is ever returned home)")
	)
	flag.Parse()

	if *showStatus {
		if err := printStatus(*ledger); err != nil {
			fmt.Fprintf(os.Stderr, "fleet-secondment: %v\n", err)
			os.Exit(1)
		}
		return
	}

	home := supervisor.ProcFleetSide(*homeName, *homeOv, *homeSock)
	away := supervisor.ProcFleetSide(*awayName, *awayOv, *awaySock)
	opts := supervisor.ReconcileOptions{MaxInFlight: *maxFlight, StopTimeout: *stopWait}

	// The graduation check reads the asset ledger's standings capture. Without a
	// ledger the trip is one-way by omission — seconded agents simply stay put,
	// which is visible in --status rather than silently wrong.
	if *assetsDB != "" {
		cfg := assets.DefaultConfig()
		cfg.DBPath = *assetsDB
		store, err := assets.Open(cfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "fleet-secondment: open assets %s: %v (no agent will be returned home)\n", *assetsDB, err)
		} else {
			defer store.Close() //nolint:errcheck
			opts.Graduated = func(agentID string) (bool, error) {
				return store.HoldsPirateUnlock(context.Background(), agentID)
			}
		}
	}

	sweep := func() {
		log, err := supervisor.ReconcileSecondments(*ledger, home, away, opts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s fleet-secondment: %v\n", time.Now().UTC().Format(time.RFC3339), err)
		}
		for _, line := range log {
			fmt.Printf("%s %s\n", time.Now().UTC().Format(time.RFC3339), line)
		}
	}

	if *watch <= 0 || *once {
		sweep()
		return
	}
	for {
		sweep()
		time.Sleep(*watch)
	}
}

func printStatus(ledgerPath string) error {
	led, err := supervisor.LoadSecondments(ledgerPath)
	if err != nil {
		return err
	}
	if len(led.Entries) == 0 {
		fmt.Println("no secondments recorded")
		return nil
	}
	fmt.Printf("%-24s %-10s %-8s %-8s %-20s %s\n", "AGENT", "PHASE", "HOME", "AWAY", "UPDATED", "NOTE")
	for _, e := range led.Entries {
		fmt.Printf("%-24s %-10s %-8s %-8s %-20s %s\n", e.AgentID, e.Phase, e.HomeFleet, e.AwayFleet, e.UpdatedAt, e.Note)
	}
	fmt.Printf("\n%d away from home, %d trip(s) open\n", led.Away(), led.InFlight())
	return nil
}
