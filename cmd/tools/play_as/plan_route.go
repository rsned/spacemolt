package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/navigation"
)

// planRoute computes the optimal order in which to visit a set of systems for
// the lowest total number of jumps, starting from the player's current system,
// and prints the autopilot commands that execute the journey.
//
// Usage: plan_route [--return] <system> [system...]   (comma- or space-separated)
//
// The optional --return flag, which must appear before any system names, adds a
// final leg back to the starting (current) system.
func planRoute(client game.GameClient, ctx context.Context, parts []string, format outputFormat) error {
	args := parts[1:]

	// Parse the optional --return flag. It is only valid as the first argument.
	returnToStart := false
	if len(args) > 0 && isReturnFlag(args[0]) {
		returnToStart = true
		args = args[1:]
	}
	if slices.ContainsFunc(args, isReturnFlag) {
		return fmt.Errorf("--return must appear before any system names")
	}

	// Split the remaining arguments into system tokens. If any comma is present,
	// split on commas so multi-word system names survive; otherwise treat each
	// whitespace-separated token as its own system.
	joined := strings.Join(args, " ")
	var tokens []string
	if strings.Contains(joined, ",") {
		for t := range strings.SplitSeq(joined, ",") {
			if t = strings.TrimSpace(t); t != "" {
				tokens = append(tokens, t)
			}
		}
	} else {
		tokens = strings.Fields(joined)
	}

	if len(tokens) == 0 {
		return fmt.Errorf("usage: plan_route [--return] <system> [system...]  (comma- or space-separated)")
	}

	if globalKB == nil {
		return fmt.Errorf("plan_route requires the knowledge base, which is not available")
	}

	// Determine the starting system from current game state.
	state := client.GetState()
	if state == nil || state.System.ID == "" {
		return fmt.Errorf("cannot determine current system; try get_status first")
	}
	startID := state.System.ID

	// Build name/id resolution maps from all known systems.
	systems, err := globalKB.GetSystems(ctx)
	if err != nil {
		return fmt.Errorf("load systems: %w", err)
	}
	byID := make(map[string]string, len(systems))   // lowercased id -> canonical id
	byName := make(map[string]string, len(systems)) // lowercased name -> canonical id
	nameOf := make(map[string]string, len(systems)) // canonical id -> display name
	for _, s := range systems {
		byID[strings.ToLower(s.ID)] = s.ID
		if s.Name != "" {
			byName[strings.ToLower(s.Name)] = s.ID
		}
		nameOf[s.ID] = s.Name
	}

	// Resolve each token to a canonical system id, deduplicating and dropping
	// the current system (we are already there).
	var waypoints []string
	seen := map[string]bool{startID: true}
	for _, tok := range tokens {
		id, ok := resolveSystemToken(tok, byID, byName)
		if !ok {
			return fmt.Errorf("unknown system: %q", tok)
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		waypoints = append(waypoints, id)
	}

	if len(waypoints) == 0 {
		if format == formatStyled {
			fmt.Println("All requested systems are the current system — nothing to plan.")
		}
		return nil
	}

	// Build the connection graph (undirected, one jump per edge).
	conns, err := globalKB.GetConnections(ctx)
	if err != nil {
		return fmt.Errorf("load connections: %w", err)
	}
	graph := navigation.JumpGraphFromConnections(conns)

	// Compute pairwise jump distances among the start and all waypoints.
	nodes := append([]string{startID}, waypoints...)
	dist := make(map[string]map[string]int, len(nodes))
	for _, n := range nodes {
		dist[n] = navigation.BFSJumps(graph, n, nodes)
	}

	// Verify every node is reachable from the start; report the first gap.
	for _, w := range waypoints {
		if dist[startID][w] >= navigation.RouteInf {
			return fmt.Errorf("no known jump route from %s to %s (knowledge base may be incomplete)",
				displayName(startID, nameOf), displayName(w, nameOf))
		}
	}

	// Solve for the optimal visiting order.
	order, total, ok := navigation.OptimalOrder(startID, waypoints, dist, returnToStart)
	if !ok {
		return fmt.Errorf("could not find a complete route through all systems (knowledge base may be incomplete)")
	}

	// Fuel per jump: prefer the server's authoritative figure (what autopilot /
	// find_route report), probed via any reachable waypoint; fall back to a
	// client-side estimate. This keeps plan_route consistent with autopilot.
	probe := startID
	if len(order) > 0 {
		probe = order[0]
	}
	fuelPerJump, haveFuel := currentJumpFuel(client, ctx, probe)

	// Emit the plan summary (styled only) followed by the autopilot commands.
	if format == formatStyled {
		header := fmt.Sprintf("\nRoute plan: %d system(s), %d total jump(s)", len(waypoints), total)
		if haveFuel {
			header += fmt.Sprintf(", ~%d fuel (%d/jump)", total*fuelPerJump, fuelPerJump)
		}
		fmt.Println(header)

		prev := startID
		printLeg := func(from, to string, tag string) {
			jumps := dist[from][to]
			line := fmt.Sprintf("  %s → %s (%d jump%s",
				displayName(from, nameOf), displayName(to, nameOf), jumps, plural(jumps))
			if haveFuel {
				line += fmt.Sprintf(", ~%d fuel", jumps*fuelPerJump)
			}
			line += ")" + tag
			fmt.Println(line)
		}
		for _, w := range order {
			printLeg(prev, w, "")
			prev = w
		}
		if returnToStart {
			printLeg(prev, startID, "  [return]")
		}

		if haveFuel {
			totalFuel := total * fuelPerJump
			if st := client.GetState(); st != nil && st.Ship.MaxFuel > 0 {
				avail := int(st.Ship.Fuel)
				fmt.Printf("\nFuel: ~%d needed, %d available\n", totalFuel, avail)
				if totalFuel > avail {
					fmt.Printf("  WARNING: short by ~%d fuel — refuel or carry fuel cells.\n", totalFuel-avail)
				}
			}
		}

		fmt.Println("\nAutopilot commands:")
	}

	for _, w := range order {
		fmt.Printf("autopilot %s\n", w)
	}
	if returnToStart {
		fmt.Printf("autopilot %s\n", startID)
	}

	return nil
}

// isReturnFlag reports whether the argument is the --return flag.
func isReturnFlag(s string) bool {
	return s == "--return" || s == "-return"
}

// currentJumpFuel returns the fuel consumed per jump by the current ship.
//
// It prefers the server's authoritative figure — the same fuel_per_jump that
// autopilot and find_route report — obtained via a single find_route probe to
// probeTarget (any reachable system; fuel_per_jump is ship-constant across a
// route). If the probe is unavailable it falls back to a client-side estimate
// derived from the ship class scale and base speed:
// ceil(scale^1.5 × speed × 10.0 × 0.10). The client formula can drift when the
// server changes its fuel model, so the server value always wins when present.
//
// It returns ok=false only when neither source yields a value.
func currentJumpFuel(client game.GameClient, ctx context.Context, probeTarget string) (int, bool) {
	// Server-authoritative path: a find_route query is tick-free and caches its
	// response under "_last" (mirrors worker.parseFuelEstimates).
	if probeTarget != "" {
		if _, err := client.FindRoute(ctx, probeTarget); err == nil {
			if fuel, ok := parseFuelPerJump(client.GetRawJSON("_last")); ok {
				return fuel, true
			}
		}
	}

	parse := func() (int, bool) {
		raw := client.GetRawJSON("ship")
		if len(raw) == 0 {
			return 0, false
		}
		var shipResp struct {
			Class *struct {
				Scale     int `json:"scale"`
				BaseSpeed int `json:"base_speed"`
			} `json:"class"`
		}
		if err := json.Unmarshal(raw, &shipResp); err != nil || shipResp.Class == nil {
			return 0, false
		}
		scale := float64(shipResp.Class.Scale)
		spd := float64(shipResp.Class.BaseSpeed)
		if scale <= 0 || spd <= 0 {
			return 0, false
		}
		return max(1, int(math.Ceil(math.Pow(scale, 1.5)*spd*10.0*0.10))), true
	}

	if fuel, ok := parse(); ok {
		return fuel, true
	}
	// The raw ship payload may be stale or unpopulated; refresh once and retry.
	if err := client.GetShip(ctx); err != nil {
		return 0, false
	}
	return parse()
}

// parseFuelPerJump extracts the server-authoritative fuel_per_jump from a
// find_route response body. Returns ok=false when the body is empty, malformed,
// or reports a non-positive fuel_per_jump.
func parseFuelPerJump(raw []byte) (int, bool) {
	if len(raw) == 0 {
		return 0, false
	}
	var routeResp struct {
		FuelPerJump int `json:"fuel_per_jump"`
	}
	if json.Unmarshal(raw, &routeResp) != nil || routeResp.FuelPerJump <= 0 {
		return 0, false
	}
	return routeResp.FuelPerJump, true
}

// plural returns "s" unless n == 1.
func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// displayName returns the system's display name, falling back to its id.
func displayName(id string, nameOf map[string]string) string {
	if n := nameOf[id]; n != "" {
		return n
	}
	return id
}

// resolveSystemToken maps a user-supplied token to a canonical system id,
// trying (in order) a direct id match, a display-name match, and a
// spaces-to-underscores id match.
func resolveSystemToken(tok string, byID, byName map[string]string) (string, bool) {
	lower := strings.ToLower(strings.TrimSpace(tok))
	if id, ok := byID[lower]; ok {
		return id, true
	}
	if id, ok := byName[lower]; ok {
		return id, true
	}
	if id, ok := byID[strings.ReplaceAll(lower, " ", "_")]; ok {
		return id, true
	}
	return "", false
}
