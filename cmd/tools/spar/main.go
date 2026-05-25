// Command spar runs a controlled PvP sparring match between two or more of our
// own agents in a non-empire arena, so combat mechanics are observable and
// testable. See docs/superpowers/specs/2026-05-25-sparring-testbed-design.md.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/spar"
)

func main() {
	var (
		mode       = flag.String("mode", "botvbot", "botvbot | partner")
		arena      = flag.String("arena", "", "arena system id, REQUIRED (e.g. ross_128); auto-discovery not yet supported")
		policyFlag = flag.String("policy", "", "per-agent policies, e.g. fighter-1=aggressor,fighter-2=dummy")
		aggressor  = flag.String("aggressor", "", "agent id that initiates (botvbot; default: first)")
		rendezvous = flag.String("rendezvous", "asteroid_belt", "POI type to gather at")
		maxTicks   = flag.Int("max-ticks", 60, "safety cap on match length in ticks")
		noEquip    = flag.Bool("no-equip", false, "skip auto-equip (verify only)")
		weaponID   = flag.String("weapon", "pulse_laser_i", "cheap weapon module id to fit if missing")
		shieldID   = flag.String("shield", "shield_booster_i", "cheap shield module id to fit if missing")
		debug      = flag.Bool("debug", false, "enable debug logging")
	)
	flag.Parse()

	agentIDs := flag.Args()
	minAgents := 2 // botvbot needs 2 bots; partner mode needs 1 (human is external)
	if *mode == "partner" {
		minAgents = 1
	}
	if len(agentIDs) < minAgents {
		fmt.Println("Usage: spar [flags] <agent-1> <agent-2> [agent-3 ...]")
		fmt.Println("Example: spar --arena ross_128 fighter-1 fighter-2")
		fmt.Println("         spar --mode partner --arena ross_128 --policy fighter-2=dummy fighter-2")
		flag.PrintDefaults()
		os.Exit(1)
	}

	logger := log.New(os.Stdout, "[spar] ", log.LstdFlags)
	ctx := context.Background()

	policies := parsePolicyFlag(*policyFlag)

	var (
		combatants []*spar.Combatant
		clients    []game.GameClient
		aggIdx     int
	)
	// Best-effort cleanup of opened clients on normal return. NOTE: log.Fatalf
	// below calls os.Exit, which skips this defer — acceptable for a dev CLI since
	// leaked WebSocket connections time out server-side.
	defer func() {
		for _, c := range clients {
			if err := c.Close(); err != nil {
				logger.Printf("close client: %v", err)
			}
		}
	}()

	for i, id := range agentIDs {
		client, creds, err := game.InitializeAgent(id, logger, ctx, *debug)
		if err != nil {
			log.Fatalf("login %s: %v", id, err)
		}
		clients = append(clients, client)

		// Default policy: first=aggressor, rest=skirmisher; override via --policy.
		name, ok := policies[id]
		if !ok {
			if i == 0 {
				name = "aggressor"
			} else {
				name = "skirmisher"
			}
		}
		pol, err := spar.PolicyByName(name)
		if err != nil {
			log.Fatalf("policy for %s: %v", id, err)
		}

		combatants = append(combatants, &spar.Combatant{
			AgentID:  id,
			Username: creds.Username,
			Client:   client,
			Policy:   pol,
			WeaponID: *weaponID,
			ShieldID: *shieldID,
			NoEquip:  *noEquip,
			Logger:   logger,
		})
		if *aggressor != "" && id == *aggressor {
			aggIdx = i
		}
	}

	if *aggressor != "" && agentIDs[aggIdx] != *aggressor {
		logger.Printf("WARNING: --aggressor %q not found in agent list; %s will initiate", *aggressor, agentIDs[aggIdx])
	}

	m := &spar.Match{
		Combatants:   combatants,
		Arena:        *arena,
		Rendezvous:   *rendezvous,
		AggressorIdx: aggIdx,
		Partner:      *mode == "partner",
		MaxTicks:     *maxTicks,
		Logger:       logger,
	}
	if err := m.Run(ctx); err != nil {
		log.Fatalf("match: %v", err)
	}
}

// parsePolicyFlag parses "a=aggressor,b=dummy" into a map.
func parsePolicyFlag(s string) map[string]string {
	out := map[string]string{}
	for pair := range strings.SplitSeq(s, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		k, v, ok := strings.Cut(pair, "=")
		if ok {
			out[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return out
}
