package worker

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/navigation"
)

// DefaultAvoidSystemsPath is the operator's list of systems workers keep out
// of (a roaming Molt Leviathan made Izar Belt a kill zone for miners on
// 2026-11-13). It is re-read on every check, so an edit takes effect without
// restarting any worker.
const DefaultAvoidSystemsPath = "data/overmind/avoid-systems.json"

// ErrRouteThroughAvoided is returned by Autopilot when the route enters a
// system on the avoid list and no detour around it exists (or the
// destination itself is avoided). The ship is left where it was.
var ErrRouteThroughAvoided = errors.New("route enters an avoided system")

// avoidSystemsFile is {"systems": {"<system_id>": "<reason>"}}.
type avoidSystemsFile struct {
	Systems map[string]string `json:"systems"`
}

// loadAvoidSystems reads the avoid list keyed by lowercased system id. A
// missing or unreadable file avoids nothing.
func loadAvoidSystems(path string) map[string]string {
	if path == "" {
		path = DefaultAvoidSystemsPath
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var f avoidSystemsFile
	if json.Unmarshal(data, &f) != nil {
		return nil
	}
	out := make(map[string]string, len(f.Systems))
	for id, why := range f.Systems {
		out[strings.ToLower(id)] = why
	}
	return out
}

// avoidedSystem reports whether systemID is on the avoid list.
func (d *WorkerDispatch) avoidedSystem(systemID string) bool {
	_, ok := loadAvoidSystems(d.AvoidSystemsPath)[strings.ToLower(systemID)]
	return ok
}

// stepAvoided matches a route step by id or name: route steps are dual-named.
func stepAvoided(step game.RouteStep, avoid map[string]string) bool {
	if _, ok := avoid[strings.ToLower(step.SystemID)]; ok && step.SystemID != "" {
		return true
	}
	_, ok := avoid[strings.ToLower(step.Name)]
	return ok && step.Name != ""
}

// avoidedOnRoute lists the route steps on the avoid list, by display name.
func avoidedOnRoute(route []game.RouteStep, avoid map[string]string) []string {
	var hits []string
	for _, step := range route {
		if stepAvoided(step, avoid) {
			hits = append(hits, cmp.Or(step.Name, step.SystemID))
		}
	}
	return hits
}

// avoidDetour plans the fewest-jumps route from src to dst on the KB map that
// enters no avoided system and no system in blocked (strongholds the agent
// may not enter). It returns the hops after src, or nil when none exists.
func avoidDetour(ctx context.Context, kb knowledge.Base, src, dst string, avoid map[string]string, blocked map[string]bool) ([]game.RouteStep, error) {
	if kb == nil {
		return nil, errors.New("no KB to plan a detour")
	}
	conns, err := kb.GetConnections(ctx)
	if err != nil {
		return nil, fmt.Errorf("connections: %w", err)
	}
	systems, err := kb.GetSystems(ctx)
	if err != nil {
		return nil, fmt.Errorf("systems: %w", err)
	}
	names := make(map[string]string, len(systems))
	exclude := map[string]bool{}
	for _, s := range systems {
		names[s.ID] = s.Name
		if _, ok := avoid[strings.ToLower(s.ID)]; ok {
			exclude[s.ID] = true
		}
		if blocked[s.ID] || (s.Name != "" && blocked[s.Name]) {
			exclude[s.ID] = true
		}
	}
	for id := range avoid {
		exclude[id] = true
	}
	path := navigation.ShortestPath(navigation.JumpGraphFromConnections(conns), src, dst, exclude)
	if len(path) < 2 {
		return nil, nil
	}
	steps := make([]game.RouteStep, 0, len(path)-1)
	for _, id := range path[1:] {
		steps = append(steps, game.RouteStep{SystemID: id, Name: cmp.Or(names[id], id)})
	}
	return steps, nil
}
