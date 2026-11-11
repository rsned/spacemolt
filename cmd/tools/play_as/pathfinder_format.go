package main

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

// Pathfinder bearings, measured 2026-11-11 (bearing 51 from Haven toward
// Market Prime): degrees counter-clockwise from +x in the galaxy map's own
// coordinates, the same frame as the KB's system positions. The drift
// covered 10 units per tick.
const (
	pathfinderUnitsPerTick = 10.0
	// pathfinderLaneWidth is how far off the bearing line a system may sit
	// and still be listed as "ahead". The server's capture radius is not
	// known yet; this is generous on purpose.
	pathfinderLaneWidth = 200.0
	pathfinderAheadMax  = 6
)

// driftTarget is a known system relative to the drift line.
type driftTarget struct {
	ID, Name    string
	Along, Off  float64 // distance ahead on the line, and off it
	Distance    float64 // straight-line distance from here
	BearingFrom float64 // bearing from here to the system
}

// pathfinderBearing is the bearing (degrees, 0..360) from (x,y) to (tx,ty).
func pathfinderBearing(x, y, tx, ty float64) float64 {
	b := math.Atan2(ty-y, tx-x) * 180 / math.Pi
	if b < 0 {
		b += 360
	}
	return b
}

// driftTargets places systems relative to the line through (x,y) on bearing:
// those ahead within the lane, nearest first along the line, plus the
// nearest system in any direction (for a course correction).
func driftTargets(systems []knowledge.System, bearing, x, y float64) (ahead []driftTarget, nearest *driftTarget) {
	rad := bearing * math.Pi / 180
	ux, uy := math.Cos(rad), math.Sin(rad)
	for _, s := range systems {
		if s.Position.X == 0 && s.Position.Y == 0 {
			continue
		}
		dx, dy := s.Position.X-x, s.Position.Y-y
		t := driftTarget{ID: s.ID, Name: s.Name,
			Along: dx*ux + dy*uy, Off: math.Abs(dy*ux - dx*uy),
			Distance: math.Hypot(dx, dy), BearingFrom: pathfinderBearing(x, y, s.Position.X, s.Position.Y)}
		if nearest == nil || t.Distance < nearest.Distance {
			n := t
			nearest = &n
		}
		if t.Along > 0 && t.Off <= pathfinderLaneWidth {
			ahead = append(ahead, t)
		}
	}
	slices.SortFunc(ahead, func(a, b driftTarget) int { return cmp.Compare(a.Along, b.Along) })
	if len(ahead) > pathfinderAheadMax {
		ahead = ahead[:pathfinderAheadMax]
	}
	return ahead, nearest
}

// formatPathfinderDrift renders a get_location reply taken mid-drift.
func formatPathfinderDrift(bearing float64, ticks int, x, y float64) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Adrift (Pathfinder Drive): bearing %.0f°, %d tick(s) out\n", bearing, ticks)
	fmt.Fprintf(&b, "Position: (%.1f, %.1f)\n", x, y)
	if globalKB == nil {
		return b.String()
	}
	systems, err := globalKB.GetSystems(context.Background())
	if err != nil || len(systems) == 0 {
		return b.String()
	}
	ahead, nearest := driftTargets(systems, bearing, x, y)
	if nearest != nil {
		fmt.Fprintf(&b, "Nearest:  %s — %.0f units away on bearing %.0f°\n", label(*nearest), nearest.Distance, nearest.BearingFrom)
	}
	if len(ahead) == 0 {
		fmt.Fprintf(&b, "\nNo known system within %.0f units of this line ahead.\n", pathfinderLaneWidth)
		return b.String()
	}
	fmt.Fprintf(&b, "\nKnown systems ahead (within %.0f units of the line):\n", pathfinderLaneWidth)
	fmt.Fprintf(&b, "  %-24s %9s %8s %7s %8s\n", "System", "Ahead", "Off-line", "~Ticks", "Bearing")
	for _, t := range ahead {
		fmt.Fprintf(&b, "  %-24s %9.0f %8.1f %7.0f %7.0f°\n", truncateName(label(t), 24), t.Along, t.Off,
			math.Ceil(t.Along/pathfinderUnitsPerTick), t.BearingFrom)
	}
	b.WriteString("  (ticks assume 10 units/tick, measured on the first drift)\n")
	return b.String()
}

func label(t driftTarget) string {
	if t.Name != "" {
		return t.Name
	}
	return t.ID
}
