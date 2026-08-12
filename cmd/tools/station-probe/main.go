// Command station-probe surveys player-built stations to find out which admit
// an outsider and which sell fuel.
//
// Both facts are unreadable ahead of time and discoverable only by flying there
// and trying, and until they are known the shuttle must decline every fare bound
// for an unverified station. The survey answers them in one planned run and
// writes the verdicts into the fleet-wide access map, so one ship's trip spares
// every other worker the same discovery.
//
// The ship is deliberately abandoned wherever the run ends: GSA recovery tows a
// drifting ship back to empire space for a small fee, which is cheaper than
// reserving return fuel on every leg, so the whole tank buys forward progress.
// Undock and leave it -- do not fly it home.
//
// Usage:
//
//	station-probe --agent salvager-9 --fuel-per-jump 2 --dry-run
//	station-probe --agent salvager-9 --fuel-per-jump 2
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/rsned/spacemolt/pkg/credentials"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/navigation"
	"github.com/rsned/spacemolt/pkg/worker"
)

const gameServerURL = "wss://game.spacemolt.com/ws"

func main() {
	agentID := flag.String("agent", "", "agent to fly the survey (required)")
	fuelPerJump := flag.Float64("fuel-per-jump", 0, "the ship's fuel cost per jump (required; Cobble=2, Empirica=6)")
	approachFuel := flag.Float64("approach-fuel", 2, "in-system fuel to reserve for arrival -> station POI")
	targetList := flag.String("targets", "", "comma-separated station ids; default = every unverified player station")
	accessPath := flag.String("access-map", worker.DefaultStationAccessPath, "path to the shared station access map")
	kbPath := flag.String("kb", "data/spacemolt-knowledge.db", "knowledge database")
	start := flag.String("start", "", "system to plan the tour from (default: the ship's current system)")
	dryRun := flag.Bool("dry-run", false, "plan and print the survey without flying it")
	flag.Parse()

	if *agentID == "" || *fuelPerJump <= 0 {
		fmt.Fprintln(os.Stderr, "usage: station-probe --agent <id> --fuel-per-jump <n> [--targets ...] [--dry-run]")
		os.Exit(2)
	}

	// Ctrl-C must leave the ship adrift and recoverable rather than mid-jump, so
	// cancellation propagates into the survey instead of killing the process.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger := log.New(os.Stdout, fmt.Sprintf("[probe-%s] ", *agentID), log.LstdFlags)

	kbCfg := knowledge.DefaultConfig()
	kbCfg.DBPath = *kbPath
	kb, err := knowledge.NewSQLiteKB(kbCfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open knowledge base: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = kb.Close() }()

	access := worker.LoadStationAccess(*accessPath)

	targets, err := resolveTargets(ctx, kb, access, *targetList)
	if err != nil {
		fmt.Fprintf(os.Stderr, "resolve targets: %v\n", err)
		os.Exit(1)
	}
	if len(targets) == 0 {
		fmt.Println("nothing to survey: every known player station already has a verdict")

		return
	}

	// A dry run must not need credentials: the point is to review the plan
	// before spending a tank on it.
	if *dryRun {
		if *start == "" {
			fmt.Fprintln(os.Stderr, "--dry-run needs --start <system> to order the tour")
			os.Exit(2)
		}
		ordered, total, err := planTour(ctx, kb, *start, targets)
		if err != nil {
			fmt.Fprintf(os.Stderr, "plan tour: %v\n", err)
			os.Exit(1)
		}
		printPlan(ordered, total, *start, *fuelPerJump, *approachFuel)

		return
	}

	client, closer, err := connectWS(ctx, *agentID, logger)
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect as %s: %v\n", *agentID, err)
		os.Exit(1)
	}
	defer func() { _ = closer.Close() }()

	from := *start
	if from == "" {
		if s := client.GetState(); s != nil {
			from = s.System.ID
		}
	}
	if from == "" {
		fmt.Fprintln(os.Stderr, "cannot determine the starting system")
		os.Exit(1)
	}
	ordered, total, err := planTour(ctx, kb, from, targets)
	if err != nil {
		fmt.Fprintf(os.Stderr, "plan tour: %v\n", err)
		os.Exit(1)
	}
	printPlan(ordered, total, from, *fuelPerJump, *approachFuel)

	verdicts, err := worker.ProbeStations(ctx, worker.ProbeDeps{
		Client:       client,
		Out:          os.Stdout,
		Access:       access,
		FuelPerJump:  *fuelPerJump,
		ApproachFuel: *approachFuel,
		JumpsTo:      jumpCounter(kb, client),
	}, ordered)
	if err != nil {
		fmt.Fprintf(os.Stderr, "survey: %v\n", err)
		os.Exit(1)
	}

	report(verdicts, len(ordered))
	fmt.Println("\nLeave the ship where it is -- GSA recovery will tow it back to empire space.")
}

// planTour orders the targets for the fewest total jumps, the same optimisation
// play_as plan_route runs. No return leg: the survey is one-way by design and
// ends with a tow.
func planTour(ctx context.Context, kb knowledge.Base, start string, targets []worker.ProbeTarget) ([]worker.ProbeTarget, int, error) {
	conns, err := kb.GetConnections(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("connections: %w", err)
	}
	graph := navigation.JumpGraphFromConnections(conns)

	bySystem := map[string]worker.ProbeTarget{}
	systems := make([]string, 0, len(targets))
	for _, t := range targets {
		if _, dup := bySystem[t.SystemID]; dup {
			// Two stations in one system: the tour visits the system once, so
			// keep the first and survey the rest by name in a later run.
			continue
		}
		bySystem[t.SystemID] = t
		systems = append(systems, t.SystemID)
	}

	dist := map[string]map[string]int{start: navigation.BFSJumps(graph, start, systems)}
	for _, s := range systems {
		dist[s] = navigation.BFSJumps(graph, s, append(systems, start))
	}
	order, total, ok := navigation.OptimalOrder(start, systems, dist, false)
	if !ok {
		return nil, 0, fmt.Errorf("no tour visits all %d systems from %s", len(systems), start)
	}

	out := make([]worker.ProbeTarget, 0, len(order))
	for _, s := range order {
		out = append(out, bySystem[s])
	}

	return out, total, nil
}

// printPlan shows the route and its fuel budget, including the in-system
// approach reserve that a jumps-only estimate silently omits.
func printPlan(targets []worker.ProbeTarget, totalJumps int, start string, fuelPerJump, approach float64) {
	jumpFuel := float64(totalJumps) * fuelPerJump
	approachTotal := float64(len(targets)) * approach
	fmt.Printf("\nsurvey plan from %s: %d station(s), %d jump(s)\n", start, len(targets), totalJumps)
	fmt.Printf("  fuel: %.0f jumping + %.0f approaches = %.0f total\n", jumpFuel, approachTotal, jumpFuel+approachTotal)
	for i, t := range targets {
		fmt.Printf("  %2d. %-26s %-18s %s\n", i+1, t.Name, t.SystemID, t.StationID)
	}
	fmt.Println()
}

// report prints what the run learned, and what it did not get to.
func report(verdicts []worker.ProbeVerdict, planned int) {
	fmt.Printf("\n=== survey result: %d of %d planned stop(s) ===\n", len(verdicts), planned)
	var open, closed, dry int
	for _, v := range verdicts {
		status := "unreached"
		switch {
		case v.Docked && v.FuelKnown && v.SellsFuel:
			status, open = "OPEN + fuel", open+1
		case v.Docked && v.FuelKnown:
			status, open, dry = "OPEN, NO FUEL", open+1, dry+1
		case v.Docked:
			status, open = "OPEN, fuel unknown", open+1
		case v.Reached:
			status, closed = "CLOSED", closed+1
		}
		fmt.Printf("  %-26s %-20s %s\n", v.Target.Name, status, v.Note)
	}
	fmt.Printf("\n%d open, %d closed, %d with no fuel desk; %d station(s) still unverified\n",
		open, closed, dry, planned-len(verdicts))
}

// jumpCounter answers "how far is that from here" against the LIVE position,
// which is what the fuel gate needs before committing to each leg -- the planned
// order is not enough, because a skipped or failed stop moves the ship.
func jumpCounter(kb knowledge.Base, client game.GameClient) func(context.Context, string) (int, error) {
	return func(ctx context.Context, systemID string) (int, error) {
		state := client.GetState()
		if state == nil || state.System.ID == "" {
			return 0, fmt.Errorf("current system unknown")
		}
		conns, err := kb.GetConnections(ctx)
		if err != nil {
			return 0, fmt.Errorf("connections: %w", err)
		}
		graph := navigation.JumpGraphFromConnections(conns)
		d := navigation.BFSJumps(graph, state.System.ID, []string{systemID})
		j, ok := d[systemID]
		if !ok || j >= navigation.RouteInf {
			return 0, fmt.Errorf("no route from %s to %s", state.System.ID, systemID)
		}

		return j, nil
	}
}

// resolveTargets returns the stations to survey: an explicit list, or every
// player station without a verdict yet. A station already proven open or denied
// is skipped -- re-flying a known answer spends fuel to learn nothing.
func resolveTargets(ctx context.Context, kb knowledge.Base, access *worker.StationAccess, explicit string) ([]worker.ProbeTarget, error) {
	want := map[string]bool{}
	for _, id := range strings.Split(explicit, ",") {
		if id = strings.TrimSpace(id); id != "" {
			want[id] = true
		}
	}

	systems, err := kb.GetSystems(ctx)
	if err != nil {
		return nil, fmt.Errorf("load systems: %w", err)
	}

	var out []worker.ProbeTarget
	for _, s := range systems {
		pois, err := kb.GetPOIs(ctx, s.ID)
		if err != nil {
			continue // a system we cannot read is not a reason to abandon the survey
		}
		for _, p := range pois {
			if !worker.IsPlayerStationID(p.ID) {
				continue
			}
			// A station is dual-named: the access map and passenger boards key on
			// the BASE id, while the POI catalogue here yields the POI id. Checking
			// only the id in hand silently re-surveys stations whose verdict we
			// already have -- it put all three known-open stations back on the tour
			// the first time this ran.
			baseID := p.ID
			if b, err := kb.GetBaseByPOI(ctx, p.ID); err == nil && b != nil && b.ID != "" {
				baseID = b.ID
			}
			if len(want) > 0 && !want[p.ID] && !want[baseID] {
				continue
			}
			// Skip what is already known under EITHER id, unless explicitly named.
			known := access.Denied(p.ID) || access.Deliverable(p.ID) ||
				access.Denied(baseID) || access.Deliverable(baseID)
			if len(want) == 0 && known {
				continue
			}
			name := p.Name
			if name == "" {
				name = p.ID
			}
			out = append(out, worker.ProbeTarget{
				// Record under the base id when we have one, so a verdict learned
				// here is the id the shuttle will look up.
				StationID: baseID, SystemID: s.ID, POIID: p.ID, Name: name,
			})
		}
	}

	return out, nil
}

// connectWS logs the agent in over WebSocket.
func connectWS(ctx context.Context, agentID string, logger *log.Logger) (game.GameClient, *game.Client, error) {
	provider := credentials.NewFileProvider("data/agents")
	creds, err := provider.GetCredentials(ctx, agentID)
	if err != nil {
		return nil, nil, fmt.Errorf("load credentials: %w", err)
	}
	c := game.NewClient(gameServerURL, creds.Username, creds.Password, logger)
	if err := c.Connect(ctx); err != nil {
		return nil, nil, fmt.Errorf("connect: %w", err)
	}
	<-c.Ready()
	time.Sleep(game.SleepRetry)
	if err := c.Login(ctx); err != nil {
		return nil, nil, fmt.Errorf("login: %w", err)
	}
	time.Sleep(game.SleepQuick)

	return c, c, nil
}
