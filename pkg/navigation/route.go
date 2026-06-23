package navigation

import "sort"

// RouteInf is the "unreachable" sentinel for jump distances. It is large enough
// that summing a handful of them never overflows an int.
const RouteInf = 1 << 30

// BFSJumps returns the jump distance from src to each system in targets.
// Unreachable targets map to RouteInf.
func BFSJumps(graph JumpGraph, src string, targets []string) map[string]int {
	want := make(map[string]bool, len(targets))
	for _, t := range targets {
		want[t] = true
	}

	out := make(map[string]int, len(targets))
	for _, t := range targets {
		out[t] = RouteInf
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

// OptimalOrder returns the waypoint visiting order that minimizes total jumps
// from start, optionally returning to start. It uses an exact Held-Karp DP for
// small inputs and a nearest-neighbor heuristic beyond that.
func OptimalOrder(start string, waypoints []string, dist map[string]map[string]int, returnToStart bool) ([]string, int, bool) {
	m := len(waypoints)
	if m == 1 {
		total := dist[start][waypoints[0]]
		if returnToStart {
			total += dist[waypoints[0]][start]
		}
		if total >= RouteInf {
			return nil, 0, false
		}
		return waypoints, total, true
	}

	// Beyond this size Held-Karp's 2^m table is too large; fall back to a
	// nearest-neighbor heuristic (no longer guaranteed optimal).
	const heldKarpMax = 15
	if m > heldKarpMax {
		return NearestNeighborOrder(start, waypoints, dist, returnToStart)
	}

	full := (1 << m) - 1
	// dp[mask][i] = min jumps to start at `start`, visit exactly `mask`, end at i.
	dp := make([][]int, 1<<m)
	parent := make([][]int, 1<<m)
	for mask := range dp {
		dp[mask] = make([]int, m)
		parent[mask] = make([]int, m)
		for i := range dp[mask] {
			dp[mask][i] = RouteInf
			parent[mask][i] = -1
		}
	}
	for i, w := range waypoints {
		dp[1<<i][i] = dist[start][w]
	}

	for mask := 1; mask <= full; mask++ {
		for i := range waypoints {
			if mask&(1<<i) == 0 || dp[mask][i] >= RouteInf {
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

	best, bestEnd := RouteInf, -1
	for i, w := range waypoints {
		total := dp[full][i]
		if returnToStart {
			total += dist[w][start]
		}
		if total < best {
			best, bestEnd = total, i
		}
	}
	if bestEnd < 0 || best >= RouteInf {
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

// NearestNeighborOrder is the heuristic fallback for large waypoint sets.
func NearestNeighborOrder(start string, waypoints []string, dist map[string]map[string]int, returnToStart bool) ([]string, int, bool) {
	remaining := make(map[string]bool, len(waypoints))
	for _, w := range waypoints {
		remaining[w] = true
	}
	order := make([]string, 0, len(waypoints))
	cur, total := start, 0
	for len(remaining) > 0 {
		next, nd := "", RouteInf
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
		if next == "" || nd >= RouteInf {
			return nil, 0, false
		}
		order = append(order, next)
		total += nd
		delete(remaining, next)
		cur = next
	}
	if returnToStart {
		if dist[cur][start] >= RouteInf {
			return nil, 0, false
		}
		total += dist[cur][start]
	}
	return order, total, true
}
