package sweep

import (
	"math"
	"testing"
)

// lattice is a system every 300 LY over ±7500, minus the points skip drops.
func lattice(skip func(x, y float64) bool) []Point {
	var pts []Point
	for x := -7500.0; x <= 7500; x += 300 {
		for y := -7500.0; y <= 7500; y += 300 {
			if !skip(x, y) {
				pts = append(pts, Point{x, y})
			}
		}
	}
	return pts
}

func in(v float64, set ...float64) bool {
	for _, s := range set {
		if v == s {
			return true
		}
	}
	return false
}

func TestComputeZonesFindsTheHoleAndSortsBySize(t *testing.T) {
	pts := lattice(func(x, y float64) bool {
		big := in(x, 1200, 1500, 1800) && in(y, 1200, 1500, 1800)
		small := in(x, -1500, -1200) && in(y, -1800, -1500, -1200)
		return big || small
	})
	o := DefaultZoneOpts()
	zs := ComputeZones(pts, o)
	if len(zs) != 2 {
		t.Fatalf("got %d zones, want 2: %+v", len(zs), zs)
	}
	if zs[0].ID != "V01" || zs[1].ID != "V02" || zs[0].Area <= zs[1].Area {
		t.Errorf("zones not sorted largest first: %s %.0f, %s %.0f", zs[0].ID, zs[0].Area, zs[1].ID, zs[1].Area)
	}
	if math.Hypot(zs[0].CX-1500, zs[0].CY-1500) > 100 {
		t.Errorf("V01 centre (%.0f,%.0f), want near (1500,1500)", zs[0].CX, zs[0].CY)
	}
	for _, z := range zs {
		for _, c := range z.Cells {
			x, y := (float64(c[0])+.5)*o.Grid, (float64(c[1])+.5)*o.Grid
			if math.Hypot(x, y) > o.MaxRadius {
				t.Fatalf("%s has a cell at (%.0f,%.0f), outside %.0f", z.ID, x, y, o.MaxRadius)
			}
		}
	}
}

func TestComputeZonesSkipsSmallZones(t *testing.T) {
	pts := lattice(func(x, y float64) bool { return x == 1500 && y == 1500 })
	if zs := ComputeZones(pts, DefaultZoneOpts()); len(zs) != 0 {
		t.Fatalf("got %d zones from a one-system gap, want 0", len(zs))
	}
}

// rect is a zone of grid-50 cells covering [x0,x1) x [y0,y1).
func cellsOf(keep func(x, y float64) bool, x0, y0, x1, y1 float64) [][2]int {
	var cs [][2]int
	for i := int(x0 / 50); i < int(x1/50); i++ {
		for j := int(y0 / 50); j < int(y1/50); j++ {
			if keep((float64(i)+.5)*50, (float64(j)+.5)*50) {
				cs = append(cs, [2]int{i, j})
			}
		}
	}
	return cs
}

func TestLanesRunAlongLongSideAndAlternate(t *testing.T) {
	z := Zone{ID: "V01", Grid: 50, Cells: cellsOf(func(x, y float64) bool { return true }, 0, 0, 1000, 400)}
	lanes := Lanes(z, 100)
	if len(lanes) != 4 {
		t.Fatalf("got %d lanes, want 4: %+v", len(lanes), lanes)
	}
	for k, l := range lanes {
		if want := 50 + 100*float64(k); l.AY != want || l.BY != want {
			t.Errorf("lane %d at y %.0f/%.0f, want %.0f", k, l.AY, l.BY, want)
		}
		lo, hi := math.Min(l.AX, l.BX), math.Max(l.AX, l.BX)
		if lo != 0 || hi != 1000 {
			t.Errorf("lane %d spans %.0f..%.0f, want 0..1000", k, lo, hi)
		}
	}
	if !(lanes[0].AX < lanes[0].BX) || !(lanes[1].AX > lanes[1].BX) {
		t.Errorf("lanes do not alternate: %+v %+v", lanes[0], lanes[1])
	}
}

func TestLanesSpanAGapInAUShapedZone(t *testing.T) {
	u := func(x, y float64) bool { return x < 200 || x > 800 || y < 200 }
	lanes := Lanes(Zone{ID: "V01", Grid: 50, Cells: cellsOf(u, 0, 0, 1000, 1000)}, 100)
	if len(lanes) != 10 {
		t.Fatalf("got %d lanes, want 10", len(lanes))
	}
	for k, l := range lanes {
		if lo, hi := math.Min(l.AX, l.BX), math.Max(l.AX, l.BX); lo != 0 || hi != 1000 {
			t.Errorf("lane %d spans %.0f..%.0f, want one 0..1000 span across the gap", k, lo, hi)
		}
	}
}

func TestWaypointsInterleaveLaneEnds(t *testing.T) {
	got := Waypoints([]Lane{{0, 50, 1000, 50}, {1000, 150, 0, 150}})
	want := []Point{{0, 50}, {1000, 50}, {1000, 150}, {0, 150}}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}
