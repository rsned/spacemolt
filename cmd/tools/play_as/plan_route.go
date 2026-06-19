package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"

	"github.com/rsned/spacemolt/pkg/game"
)

// routeInf is the "unreachable" sentinel for jump distances. It is large enough
// that summing a handful of them never overflows an int.
const routeInf = 1 << 30

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
	graph, err := buildJumpGraph(ctx)
	if err != nil {
		return err
	}

	// Compute pairwise jump distances among the start and all waypoints.
	nodes := append([]string{startID}, waypoints...)
	dist := make(map[string]map[string]int, len(nodes))
	for _, n := range nodes {
		dist[n] = bfsJumps(graph, n, nodes)
	}

	// Verify every node is reachable from the start; report the first gap.
	for _, w := range waypoints {
		if dist[startID][w] >= routeInf {
			return fmt.Errorf("no known jump route from %s to %s (knowledge base may be incomplete)",
				displayName(startID, nameOf), displayName(w, nameOf))
		}
	}

	// Solve for the optimal visiting order.
	order, total, ok := optimalOrder(startID, waypoints, dist, returnToStart)
	if !ok {
		return fmt.Errorf("could not find a complete route through all systems (knowledge base may be incomplete)")
	}

	// Estimate fuel from the current ship's class scale/speed (best-effort).
	fuelPerJump, haveFuel := currentJumpFuel(client, ctx)

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

// currentJumpFuel returns the estimated fuel consumed per jump by the current
// ship, derived from its class scale and base speed. This mirrors the formula
// used by the `jump` command: ceil(scale^1.5 × speed × 10.0 × 0.10).
//
// It returns ok=false when the ship class data (scale/speed) is unavailable.
func currentJumpFuel(client game.GameClient, ctx context.Context) (int, bool) {
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

// buildJumpGraph builds an undirected adjacency map from the KB connections,
// where every edge counts as a single jump.
func buildJumpGraph(ctx context.Context) (map[string][]string, error) {
	conns, err := globalKB.GetConnections(ctx)
	if err != nil {
		return nil, fmt.Errorf("load connections: %w", err)
	}
	graph := make(map[string][]string)
	add := func(a, b string) {
		if slices.Contains(graph[a], b) {
			return
		}
		graph[a] = append(graph[a], b)
	}
	for _, c := range conns {
		if c.FromSystem == "" || c.ToSystem == "" {
			continue
		}
		add(c.FromSystem, c.ToSystem)
		add(c.ToSystem, c.FromSystem)
	}
	return graph, nil
}

// bfsJumps returns the jump distance from src to each system in targets.
// Unreachable targets map to routeInf.
func bfsJumps(graph map[string][]string, src string, targets []string) map[string]int {
	want := make(map[string]bool, len(targets))
	for _, t := range targets {
		want[t] = true
	}

	out := make(map[string]int, len(targets))
	for _, t := range targets {
		out[t] = routeInf
	}
	out[src] = 0

	visited := map[string]bool{src: true}
	queue := []string{src}
	found := 0
	if want[src] {
		found++
	}
	for len(queue) > 0 && found < len(want) {
		cur := queue[0]
		queue = queue[1:]
		d := out[cur]
		neighbors := graph[cur]
		// Stable iteration for deterministic tie-breaking.
		sorted := append([]string(nil), neighbors...)
		sort.Strings(sorted)
		for _, nb := range sorted {
			if visited[nb] {
				continue
			}
			visited[nb] = true
			if _, ok := out[nb]; !ok || out[nb] > d+1 {
				out[nb] = d + 1
			}
			if want[nb] {
				found++
			}
			queue = append(queue, nb)
		}
	}
	return out
}

// optimalOrder returns the waypoint visiting order that minimizes total jumps
// from start, optionally returning to start. It uses an exact Held-Karp DP for
// small inputs and a nearest-neighbor heuristic beyond that.
func optimalOrder(start string, waypoints []string, dist map[string]map[string]int, returnToStart bool) ([]string, int, bool) {
	m := len(waypoints)
	if m == 1 {
		total := dist[start][waypoints[0]]
		if returnToStart {
			total += dist[waypoints[0]][start]
		}
		if total >= routeInf {
			return nil, 0, false
		}
		return waypoints, total, true
	}

	// Beyond this size Held-Karp's 2^m table is too large; fall back to a
	// nearest-neighbor heuristic (no longer guaranteed optimal).
	const heldKarpMax = 15
	if m > heldKarpMax {
		return nearestNeighborOrder(start, waypoints, dist, returnToStart)
	}

	full := (1 << m) - 1
	// dp[mask][i] = min jumps to start at `start`, visit exactly `mask`, end at i.
	dp := make([][]int, 1<<m)
	parent := make([][]int, 1<<m)
	for mask := range dp {
		dp[mask] = make([]int, m)
		parent[mask] = make([]int, m)
		for i := range dp[mask] {
			dp[mask][i] = routeInf
			parent[mask][i] = -1
		}
	}
	for i, w := range waypoints {
		dp[1<<i][i] = dist[start][w]
	}

	for mask := 1; mask <= full; mask++ {
		for i := range waypoints {
			if mask&(1<<i) == 0 || dp[mask][i] >= routeInf {
				continue
			}
			base := dp[mask][i]
			for j := range waypoints {
				if mask&(1<<j) != 0 {
					continue
				}
				cost := base + dist[waypoints[i]][waypoints[j]]
				next := mask | (1 << j)
				if cost < dp[next][j] {
					dp[next][j] = cost
					parent[next][j] = i
				}
			}
		}
	}

	best, bestEnd := routeInf, -1
	for i, w := range waypoints {
		total := dp[full][i]
		if returnToStart {
			total += dist[w][start]
		}
		if total < best {
			best, bestEnd = total, i
		}
	}
	if bestEnd < 0 || best >= routeInf {
		return nil, 0, false
	}

	// Reconstruct the order by walking parent pointers backward.
	order := make([]string, 0, m)
	mask, cur := full, bestEnd
	for cur != -1 {
		order = append(order, waypoints[cur])
		prev := parent[mask][cur]
		mask ^= 1 << cur
		cur = prev
	}
	for l, r := 0, len(order)-1; l < r; l, r = l+1, r-1 {
		order[l], order[r] = order[r], order[l]
	}
	return order, best, true
}

// nearestNeighborOrder is the heuristic fallback for large waypoint sets.
func nearestNeighborOrder(start string, waypoints []string, dist map[string]map[string]int, returnToStart bool) ([]string, int, bool) {
	remaining := make(map[string]bool, len(waypoints))
	for _, w := range waypoints {
		remaining[w] = true
	}
	order := make([]string, 0, len(waypoints))
	cur, total := start, 0
	for len(remaining) > 0 {
		next, nd := "", routeInf
		// Deterministic: break ties by system id.
		keys := make([]string, 0, len(remaining))
		for w := range remaining {
			keys = append(keys, w)
		}
		sort.Strings(keys)
		for _, w := range keys {
			if dist[cur][w] < nd {
				next, nd = w, dist[cur][w]
			}
		}
		if next == "" || nd >= routeInf {
			return nil, 0, false
		}
		order = append(order, next)
		total += nd
		delete(remaining, next)
		cur = next
	}
	if returnToStart {
		if dist[cur][start] >= routeInf {
			return nil, 0, false
		}
		total += dist[cur][start]
	}
	return order, total, true
}
