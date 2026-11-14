package sweep

import (
	"fmt"
	"math"
	"slices"
)

// Interior void sweep geometry.
//
// Spec: docs/superpowers/specs/2026-11-14-interior-void-sweep-design.md

// Point is a position in KB map coordinates (LY).
type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// ZoneOpts sets how void zones are found: a Grid-LY cell is void when its
// centre lies more than Threshold LY from every known system and within
// MaxRadius of the centre; connected void cells covering at least MinArea
// LY² form a zone.
type ZoneOpts struct {
	Grid, Threshold, MaxRadius, MinArea float64
}

// DefaultZoneOpts are the operator's 2026-11-14 settings.
func DefaultZoneOpts() ZoneOpts {
	return ZoneOpts{Grid: 50, Threshold: 350, MaxRadius: 7150, MinArea: 200 * 200}
}

// Zone is one connected void region. Cells are grid indices [i, j] whose
// cell spans [i*Grid, (i+1)*Grid) x [j*Grid, (j+1)*Grid).
type Zone struct {
	ID    string   `json:"id"`
	Cells [][2]int `json:"cells"`
	Grid  float64  `json:"grid"`
	Area  float64  `json:"area"`
	CX    float64  `json:"cx"`
	CY    float64  `json:"cy"`
	W     float64  `json:"w"`
	H     float64  `json:"h"`
}

// voidBucket is the spatial-bucket size for nearest-system tests; with a
// Threshold under it, the 5x5 buckets around a point hold every candidate.
const voidBucket = 500.0

// ComputeZones finds the void zones among systems, largest first, with IDs
// V01, V02, ... in that order.
func ComputeZones(systems []Point, o ZoneOpts) []Zone {
	buckets := map[[2]int][]Point{}
	for _, p := range systems {
		k := [2]int{int(math.Floor(p.X / voidBucket)), int(math.Floor(p.Y / voidBucket))}
		buckets[k] = append(buckets[k], p)
	}
	reach := int(math.Ceil(o.Threshold/voidBucket)) + 1
	clear := func(x, y float64) bool {
		bx, by := int(math.Floor(x/voidBucket)), int(math.Floor(y/voidBucket))
		for i := -reach; i <= reach; i++ {
			for j := -reach; j <= reach; j++ {
				for _, p := range buckets[[2]int{bx + i, by + j}] {
					if math.Hypot(x-p.X, y-p.Y) <= o.Threshold {
						return false
					}
				}
			}
		}
		return true
	}

	n := int(o.MaxRadius/o.Grid) + 1
	void := map[[2]int]bool{}
	for i := -n; i <= n; i++ {
		for j := -n; j <= n; j++ {
			x, y := (float64(i)+.5)*o.Grid, (float64(j)+.5)*o.Grid
			if math.Hypot(x, y) <= o.MaxRadius && clear(x, y) {
				void[[2]int{i, j}] = true
			}
		}
	}

	seen := map[[2]int]bool{}
	var zones []Zone
	for i := -n; i <= n; i++ { // fixed scan order keeps results deterministic
		for j := -n; j <= n; j++ {
			c := [2]int{i, j}
			if !void[c] || seen[c] {
				continue
			}
			var comp [][2]int
			stack := [][2]int{c}
			seen[c] = true
			for len(stack) > 0 {
				a := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				comp = append(comp, a)
				for _, d := range [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
					b := [2]int{a[0] + d[0], a[1] + d[1]}
					if void[b] && !seen[b] {
						seen[b] = true
						stack = append(stack, b)
					}
				}
			}
			if z := newZone(comp, o.Grid); z.Area >= o.MinArea {
				zones = append(zones, z)
			}
		}
	}
	slices.SortStableFunc(zones, func(a, b Zone) int {
		switch {
		case a.Area > b.Area:
			return -1
		case a.Area < b.Area:
			return 1
		}
		return 0
	})
	for k := range zones {
		zones[k].ID = fmt.Sprintf("V%02d", k+1)
	}
	return zones
}

// newZone measures a set of cells.
func newZone(cells [][2]int, g float64) Zone {
	z := Zone{Cells: cells, Grid: g, Area: float64(len(cells)) * g * g}
	x0, y0, x1, y1 := bbox(cells)
	z.W, z.H = float64(x1-x0+1)*g, float64(y1-y0+1)*g
	for _, c := range cells {
		z.CX += (float64(c[0]) + .5) * g
		z.CY += (float64(c[1]) + .5) * g
	}
	z.CX /= float64(len(cells))
	z.CY /= float64(len(cells))
	return z
}

// bbox is the inclusive index range of cells.
func bbox(cells [][2]int) (x0, y0, x1, y1 int) {
	x0, y0, x1, y1 = math.MaxInt, math.MaxInt, math.MinInt, math.MinInt
	for _, c := range cells {
		x0, x1 = min(x0, c[0]), max(x1, c[0])
		y0, y1 = min(y0, c[1]), max(y1, c[1])
	}
	return x0, y0, x1, y1
}

// Lane is one straight run of the square wave, flown from A to B.
type Lane struct {
	AX float64 `json:"ax"`
	AY float64 `json:"ay"`
	BX float64 `json:"bx"`
	BY float64 `json:"by"`
}

// Lanes cuts z into runs spacing apart, parallel to the long side of its
// bounding box and starting spacing/2 in from its edge. Each lane is one
// span from the first to the last cell on its line (straight over any gap),
// lanes with no cells are dropped, and directions alternate.
func Lanes(z Zone, spacing float64) []Lane {
	if len(z.Cells) == 0 {
		return nil
	}
	g := z.Grid
	x0, y0, x1, y1 := bbox(z.Cells)
	horizontal := x1-x0 >= y1-y0
	// across is the axis lanes are spaced along; along is the axis they run on.
	across := func(c [2]int) float64 { return (float64(c[1]) + .5) * g }
	along := func(c [2]int) int { return c[0] }
	lo, hi := float64(y0)*g, float64(y1+1)*g
	if !horizontal {
		across = func(c [2]int) float64 { return (float64(c[0]) + .5) * g }
		along = func(c [2]int) int { return c[1] }
		lo, hi = float64(x0)*g, float64(x1+1)*g
	}
	var lanes []Lane
	for v := lo + spacing/2; v < hi; v += spacing {
		a0, a1 := math.MaxInt, math.MinInt
		for _, c := range z.Cells {
			if math.Abs(across(c)-v) <= g/2 {
				a0, a1 = min(a0, along(c)), max(a1, along(c))
			}
		}
		if a0 > a1 {
			continue
		}
		s, e := float64(a0)*g, float64(a1+1)*g
		if len(lanes)%2 == 1 {
			s, e = e, s
		}
		if horizontal {
			lanes = append(lanes, Lane{AX: s, AY: v, BX: e, BY: v})
		} else {
			lanes = append(lanes, Lane{AX: v, AY: s, BX: v, BY: e})
		}
	}
	return lanes
}

// Waypoints is the square wave as the points a ship aims at in turn: each
// lane's start, then its end.
func Waypoints(lanes []Lane) []Point {
	pts := make([]Point, 0, 2*len(lanes))
	for _, l := range lanes {
		pts = append(pts, Point{l.AX, l.AY}, Point{l.BX, l.BY})
	}
	return pts
}
