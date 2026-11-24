package worker

import (
	"maps"
	"strings"

	"github.com/rsned/spacemolt/pkg/customs"
	"github.com/rsned/spacemolt/pkg/navigation"
)

// contrabandBlocked is the set of systems a ship carrying contraband may not
// fly THROUGH: every customs-monitored system on the customs map plus the
// operator's avoid list. It mirrors what Autopilot will refuse at departure,
// so the mission runner can refuse the courier before accepting it instead of
// discovering at the undock that no route exists (pirate-7/9, 2026-11-24: an
// hour parked at the giver holding hot_cell for Ironhearth, which is reachable
// only through the_rampart or leviathan systems). A missing or unreadable map
// contributes nothing, matching customsAvoid.
func contrabandBlocked(customsMapPath, avoidSystemsPath string) map[string]string {
	blocked := map[string]string{}
	maps.Copy(blocked, loadAvoidSystems(avoidSystemsPath))
	if customsMapPath == "" {
		customsMapPath = customs.DefaultMapPath
	}
	m, err := customs.Load(customsMapPath)
	if err != nil {
		return blocked // absent or unreadable: Autopilot will not avoid it either
	}
	for id, why := range m.Monitored() {
		blocked[strings.ToLower(id)] = why
	}
	return blocked
}

// contrabandDist is navigation.BFSJumps for a contraband courier: a blocked
// system may be ARRIVED at (Autopilot exempts the route's endpoints) but never
// flown through. The origin is never blocked — the ship is already there.
func contrabandDist(graph navigation.JumpGraph, current string, targets []string, blocked map[string]string) map[string]int {
	if len(blocked) == 0 {
		return navigation.BFSJumps(graph, current, targets)
	}
	pruned := make(navigation.JumpGraph, len(graph))
	for sys, next := range graph {
		if _, stop := blocked[strings.ToLower(sys)]; stop && sys != current {
			continue // dead end: reachable as a destination, no way onward
		}
		pruned[sys] = next
	}
	return navigation.BFSJumps(pruned, current, targets)
}
