package navigation

import (
	"slices"
	"sort"
)

// ShortestPath returns the fewest-jumps path from src to dst (both ends
// included) that never enters a system in exclude, or nil when there is none.
// src itself may be excluded: a ship already inside an avoided system must
// still be able to fly out. Neighbors are visited in sorted order so ties
// break deterministically.
func ShortestPath(graph JumpGraph, src, dst string, exclude map[string]bool) []string {
	if src == dst {
		return []string{src}
	}
	if exclude[dst] {
		return nil
	}
	prev := map[string]string{src: ""}
	queue := []string{src}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		neighbors := append([]string(nil), graph[cur]...)
		sort.Strings(neighbors)
		for _, nb := range neighbors {
			if _, seen := prev[nb]; seen || exclude[nb] {
				continue
			}
			prev[nb] = cur
			if nb == dst {
				path := []string{dst}
				for p := cur; p != ""; p = prev[p] {
					path = append(path, p)
				}
				slices.Reverse(path)
				return path
			}
			queue = append(queue, nb)
		}
	}
	return nil
}
