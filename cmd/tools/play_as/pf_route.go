package main

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/rsned/spacemolt/pkg/galaxy"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// pfCaptureRadius is how close to a drift line a known system may sit and
// still be flagged as able to catch the drift first. Observed captures
// ran 50–90 LY; the upper end is used so the warning errs on the safe side.
const pfCaptureRadius = 90.0

// pfLaunch is one place to fire the Pathfinder Drive from.
type pfLaunch struct {
	ID, Name     string
	Jumps        int     // lane jumps from the current system to here
	Station      bool    // has a public, non-outpost station
	Arrival      string  // system the drift aims at (the target or a lane neighbour of it)
	ArrivalName  string  // its display name
	ArrivalJumps int     // lane jumps from the arrival on to the target
	Distance     float64 // straight-line LY to the arrival
	Bearing      float64 // jump bearing, degrees CCW from +x
	Ticks        int     // drift ticks at pathfinderUnitsPerTick
	InTheWay     []driftTarget
}

// pfPlan is the output of planPathfinderRoute.
type pfPlan struct {
	Target    knowledge.System
	Source    string // position source: api, measured, estimate ("" unknown)
	From      string
	LaneJumps int // lane jumps from From to Target; -1 when unreachable by lanes
	Station   *pfLaunch
	Nearest   *pfLaunch
	Direct    *pfLaunch
	// Drive is the active ship's Pathfinder Drive state: "fitted",
	// "missing", or "" when the fitting is unknown.
	Drive string
}

func pfPositioned(s knowledge.System) bool {
	return s.ID == "sol" || s.Position.X != 0 || s.Position.Y != 0
}

// pfLaneHops is a breadth-first search over KB lanes from start. A
// stronghold is never passed through and only reached when allowed.
func pfLaneHops(byID map[string]knowledge.System, start string, strongholds bool) map[string]int {
	hops := map[string]int{start: 0}
	queue := []string{start}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur != start && byID[cur].IsStronghold {
			continue // a dead end: do not route through it
		}
		for _, c := range byID[cur].Connections {
			n, ok := byID[c.SystemID]
			if !ok {
				continue
			}
			if _, seen := hops[n.ID]; seen || (n.IsStronghold && !strongholds) {
				continue
			}
			hops[n.ID] = hops[cur] + 1
			queue = append(queue, n.ID)
		}
	}
	return hops
}

// resolvePFTarget finds a system by id or name, case-insensitively.
func resolvePFTarget(query string, systems []knowledge.System) *knowledge.System {
	q := strings.ToLower(strings.TrimSpace(query))
	for i := range systems {
		if strings.ToLower(systems[i].ID) == q {
			return &systems[i]
		}
	}
	for i := range systems {
		if strings.ToLower(systems[i].Name) == q {
			return &systems[i]
		}
	}
	return nil
}

// planPathfinderRoute plans how to reach target from from with the
// Pathfinder Drive, using only KB data (the server hides frontier systems
// from find_route). It picks the nearest lane-reachable station system and
// the nearest lane-reachable system of any kind to the target's lane
// cluster, plus a launch from where the ship is now. stations is the set of
// system ids with a public, non-outpost station; strongholds allows
// stronghold systems (pirate unlock).
func planPathfinderRoute(systems []knowledge.System, stations map[string]bool, from, target string, strongholds bool) (*pfPlan, error) {
	byID := make(map[string]knowledge.System, len(systems))
	for _, s := range systems {
		byID[s.ID] = s
	}
	tgt, ok := byID[target]
	if !ok {
		return nil, fmt.Errorf("system %q not in the knowledge base", target)
	}
	if !pfPositioned(tgt) {
		return nil, fmt.Errorf("%s has no known position in the knowledge base", target)
	}
	if _, ok := byID[from]; !ok {
		return nil, fmt.Errorf("current system %q not in the knowledge base", from)
	}

	p := &pfPlan{Target: tgt, From: from, LaneJumps: -1}
	reach := pfLaneHops(byID, from, strongholds)
	if j, ok := reach[target]; ok {
		p.LaneJumps = j
	}

	// Arrival candidates: the target and every positioned system that
	// reaches it by lanes, each with its lane jumps on to the target.
	// Strongholds are allowed here: the hops are only within the cluster.
	arrivals := map[string]int{}
	for id, j := range pfLaneHops(byID, target, true) {
		if a := byID[id]; pfPositioned(a) && (!a.IsStronghold || strongholds || id == target) {
			arrivals[id] = j
		}
	}

	best := func(l knowledge.System, jumps int) *pfLaunch {
		var out *pfLaunch
		for id, aj := range arrivals {
			if id == l.ID {
				continue
			}
			a := byID[id]
			d := math.Hypot(a.Position.X-l.Position.X, a.Position.Y-l.Position.Y)
			// Prefer the shorter drift; on a tie, fewer lanes after it.
			if out != nil && (d > out.Distance || (d == out.Distance && aj >= out.ArrivalJumps)) {
				continue
			}
			out = &pfLaunch{ID: l.ID, Name: l.Name, Jumps: jumps, Station: stations[l.ID],
				Arrival: id, ArrivalName: a.Name, ArrivalJumps: aj, Distance: d,
				Bearing: pathfinderBearing(l.Position.X, l.Position.Y, a.Position.X, a.Position.Y),
				Ticks:   int(math.Ceil(d / pathfinderUnitsPerTick))}
		}
		return out
	}

	ids := make([]string, 0, len(reach))
	for id := range reach {
		ids = append(ids, id)
	}
	slices.Sort(ids) // deterministic ties
	for _, id := range ids {
		l := byID[id]
		if _, inCluster := arrivals[id]; inCluster || !pfPositioned(l) {
			continue
		}
		c := best(l, reach[id])
		if c == nil {
			continue
		}
		if p.Nearest == nil || c.Distance < p.Nearest.Distance {
			p.Nearest = c
		}
		if stations[id] && (p.Station == nil || c.Distance < p.Station.Distance) {
			p.Station = c
		}
		if id == from {
			p.Direct = c
		}
	}

	for _, l := range []*pfLaunch{p.Station, p.Nearest, p.Direct} {
		if l == nil || l.InTheWay != nil {
			continue
		}
		src := byID[l.ID]
		ahead := driftTargetsAll(systems, l.Bearing, src.Position.X, src.Position.Y)
		for _, t := range ahead {
			if t.ID != l.Arrival && t.ID != l.ID && t.Off <= pfCaptureRadius && t.Along < l.Distance {
				l.InTheWay = append(l.InTheWay, t)
			}
		}
	}
	return p, nil
}

// driftTargetsAll is driftTargets without the "ahead" cap.
func driftTargetsAll(systems []knowledge.System, bearing, x, y float64) []driftTarget {
	rad := bearing * math.Pi / 180
	ux, uy := math.Cos(rad), math.Sin(rad)
	var ahead []driftTarget
	for _, s := range systems {
		if !pfPositioned(s) {
			continue
		}
		dx, dy := s.Position.X-x, s.Position.Y-y
		t := driftTarget{ID: s.ID, Name: s.Name, Along: dx*ux + dy*uy, Off: math.Abs(dy*ux - dx*uy),
			Distance: math.Hypot(dx, dy), BearingFrom: pathfinderBearing(x, y, s.Position.X, s.Position.Y)}
		if t.Along > 0 {
			ahead = append(ahead, t)
		}
	}
	slices.SortFunc(ahead, func(a, b driftTarget) int { return cmp.Compare(a.Along, b.Along) })
	return ahead
}

// pathfinderDriveState reports whether the ship has a Pathfinder Drive
// fitted: "fitted", "missing", or "" when the fitting cannot be read
// (module instance ids without definitions).
func pathfinderDriveState(ship game.Ship, defs map[string]game.ModuleDefinition) string {
	isDrive := func(s string) bool {
		s = strings.ToLower(s)
		return strings.Contains(s, "pathfinder_drive") || strings.Contains(s, "pathfinder drive")
	}
	unknown := false
	for _, m := range ship.Modules {
		if isDrive(m) {
			return "fitted"
		}
		d, ok := defs[m]
		if !ok {
			unknown = true
			continue
		}
		if isDrive(d.TypeID) || isDrive(d.ID) || isDrive(d.Name) {
			return "fitted"
		}
	}
	if unknown {
		return ""
	}
	return "missing"
}

func pfLabel(name, id string) string {
	if name == "" || name == id {
		return id
	}
	return fmt.Sprintf("%s (%s)", name, id)
}

func renderPFLaunch(b *strings.Builder, title string, l *pfLaunch) {
	if l == nil {
		fmt.Fprintf(b, "\n%s: none reachable by lanes\n", title)
		return
	}
	where := fmt.Sprintf("%d lane jump(s) away", l.Jumps)
	if l.Jumps == 0 {
		where = "you are here"
	}
	kind := ""
	if l.Station {
		kind = ", station"
	}
	fmt.Fprintf(b, "\n%s: %s — %s%s\n", title, pfLabel(l.Name, l.ID), where, kind)
	fmt.Fprintf(b, "  drift %.0f LY (~%d ticks) to %s on bearing %.1f°\n", l.Distance, l.Ticks, pfLabel(l.ArrivalName, l.Arrival), l.Bearing)
	if l.ArrivalJumps > 0 {
		fmt.Fprintf(b, "  then %d lane jump(s) on to the target\n", l.ArrivalJumps)
	}
	for _, t := range l.InTheWay {
		fmt.Fprintf(b, "  ⚠ %s lies %.0f LY off the line, %.0f LY out — it may catch the drift first\n", pfLabel(t.Name, t.ID), t.Off, t.Along)
	}
	if l.Jumps > 0 {
		fmt.Fprintf(b, "  > autopilot %s, then: jump %.0f\n", l.ID, l.Bearing)
	} else {
		fmt.Fprintf(b, "  > jump %.0f\n", l.Bearing)
	}
}

// renderPathfinderRoute renders a pfPlan.
func renderPathfinderRoute(p *pfPlan) string {
	var b strings.Builder
	t := p.Target
	fmt.Fprintf(&b, "Pathfinder route to %s at (%.0f, %.0f)", pfLabel(t.Name, t.ID), t.Position.X, t.Position.Y)
	if p.Source != "" {
		fmt.Fprintf(&b, " [position: %s]", p.Source)
	}
	b.WriteString("\n")
	switch p.Drive {
	case "missing":
		b.WriteString("⚠ WARNING: the active ship has no pathfinder_drive fitted\n")
	case "":
		b.WriteString("? Pathfinder Drive fitting unknown (run get_ship)\n")
	}
	if p.Source == "estimate" {
		b.WriteString("⚠ The target's position is an ESTIMATE — the drift may miss; re-aim mid-drift with get_location\n")
	}
	if p.LaneJumps >= 0 {
		fmt.Fprintf(&b, "Lane route exists: %d jump(s) from %s — find_route / autopilot works without the drive.\n", p.LaneJumps, p.From)
	} else {
		fmt.Fprintf(&b, "No lane route from %s: the target is off the lane network (frontier).\n", p.From)
	}
	renderPFLaunch(&b, "Nearest station", p.Station)
	renderPFLaunch(&b, "Nearest system", p.Nearest)
	renderPFLaunch(&b, "From here", p.Direct)
	return b.String()
}

// runPathfinderRoute implements the pf_route REPL command:
//
//	pf_route <system id or name> [from=<system>] [--strongholds]
func runPathfinderRoute(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: pf_route <system> [from=<system>] [--strongholds]")
	}
	if globalKB == nil {
		return fmt.Errorf("pf_route: knowledge base not available (run with --db-path)")
	}
	var query []string
	from, strongholds := "", false
	for _, a := range args {
		switch {
		case a == "--strongholds":
			strongholds = true
		case strings.HasPrefix(a, "from="):
			from = strings.TrimPrefix(a, "from=")
		default:
			query = append(query, a)
		}
	}
	var st *game.State
	if globalClient != nil {
		st = globalClient.GetState()
	}
	if from == "" && st != nil {
		from = st.System.ID
	}
	if from == "" {
		return fmt.Errorf("pf_route: current system unknown; pass from=<system>")
	}

	systems, err := globalKB.GetSystems(ctx)
	if err != nil {
		return fmt.Errorf("pf_route: %w", err)
	}
	tgt := resolvePFTarget(strings.Join(query, " "), systems)
	if tgt == nil {
		fmt.Printf("System %q not found in the knowledge base.\n", strings.Join(query, " "))
		if s := suggestSystems(strings.Join(query, " "), systems); len(s) > 0 {
			fmt.Printf("Did you mean: %s?\n", strings.Join(s, ", "))
		}
		return nil
	}
	ids, err := galaxy.AccessibleStationSystems(ctx, globalKB, strongholds)
	if err != nil {
		return fmt.Errorf("pf_route: %w", err)
	}
	stations := make(map[string]bool, len(ids))
	for _, id := range ids {
		stations[id] = true
	}

	p, err := planPathfinderRoute(systems, stations, from, tgt.ID, strongholds)
	if err != nil {
		return fmt.Errorf("pf_route: %w", err)
	}
	if src, ok := globalKB.(knowledge.SystemPositionSourcer); ok {
		p.Source, _ = src.SystemPositionSource(ctx, tgt.ID)
	}
	if st != nil {
		p.Drive = pathfinderDriveState(st.Ship, st.ModuleDefinitions)
	}
	fmt.Print(renderPathfinderRoute(p))
	return nil
}
