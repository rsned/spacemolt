package sweep

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)

// testRun writes a run whose zones need the given launch counts (one
// waypoint each), largest area first.
func testRun(t *testing.T, needs ...int) string {
	t.Helper()
	r := &VoidRun{Created: t0.Format(time.RFC3339)}
	for k, n := range needs {
		z := &VoidZone{Zone: Zone{ID: fmt.Sprintf("V%02d", k+1), Area: float64(1000 - k)}}
		for i := range n {
			z.Waypoints = append(z.Waypoints, Point{float64(i), 0})
		}
		r.Zones = append(r.Zones, z)
	}
	path := filepath.Join(t.TempDir(), "void-run.json")
	if err := SaveVoidRun(path, r); err != nil {
		t.Fatal(err)
	}
	return path
}

func claim(t *testing.T, path, agent string, perTank float64, now time.Time) string {
	t.Helper()
	z, err := ClaimZone(path, agent, perTank, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	if z == nil {
		return ""
	}
	return z.ID
}

func TestClaimZoneLargestFirstAndOncePerZone(t *testing.T) {
	path := testRun(t, 10, 10, 10)
	if a, b := claim(t, path, "a", 90, t0), claim(t, path, "b", 90, t0); a != "V01" || b != "V02" {
		t.Fatalf("claims %s %s, want V01 V02", a, b)
	}
}

func TestClaimZoneReturnsOwnClaimOnRestart(t *testing.T) {
	path := testRun(t, 10, 10)
	claim(t, path, "a", 90, t0)
	if got := claim(t, path, "a", 90, t0.Add(time.Minute)); got != "V01" {
		t.Fatalf("re-claim got %s, want own V01", got)
	}
}

func TestClaimZoneSmallTankTakesAFittingZone(t *testing.T) {
	path := testRun(t, 60, 20)
	if got := claim(t, path, "small", 15, t0); got != "V02" {
		t.Fatalf("small tank got %s, want V02 (needs 20 <= 2x15)", got)
	}
	if got := claim(t, path, "small2", 15, t0); got != "V01" {
		t.Fatalf("small tank with nothing fitting got %s, want the largest left (V01)", got)
	}
}

func TestClaimZoneTakesOverAnExpiredLease(t *testing.T) {
	path := testRun(t, 10)
	claim(t, path, "a", 90, t0)
	if err := SaveProgress(path, "a", "V01", 4, nil, t0); err != nil {
		t.Fatal(err)
	}
	if got := claim(t, path, "b", 90, t0.Add(time.Hour)); got != "" {
		t.Fatalf("b took a live lease: %s", got)
	}
	z, err := ClaimZone(path, "b", 90, nil, t0.Add(VoidLease+time.Minute))
	if err != nil || z == nil || z.ID != "V01" || z.Next != 4 || z.Claimant != "b" {
		t.Fatalf("takeover got %+v %v, want V01 resumed at 4 by b", z, err)
	}
}

func TestClaimZoneSkipsZonesOkRejects(t *testing.T) {
	path := testRun(t, 10, 10)
	z, err := ClaimZone(path, "a", 90, func(z *VoidZone) bool { return z.ID != "V01" }, t0)
	if err != nil || z == nil || z.ID != "V02" {
		t.Fatalf("got %+v %v, want V02", z, err)
	}
}

func TestSaveProgressMarksDoneAndRejectsNonClaimant(t *testing.T) {
	path := testRun(t, 4)
	claim(t, path, "a", 90, t0)
	if err := SaveProgress(path, "b", "V01", 2, nil, t0); err == nil {
		t.Fatal("non-claimant saved progress")
	}
	if err := SaveProgress(path, "a", "V01", 2, []string{"x", "x"}, t0); err != nil {
		t.Fatal(err)
	}
	if err := SaveProgress(path, "a", "V01", 4, []string{"x", "y"}, t0); err != nil {
		t.Fatal(err)
	}
	r, err := LoadVoidRun(path)
	if err != nil {
		t.Fatal(err)
	}
	z := r.Zones[0]
	if !z.Done || z.Claimant != "" || z.Next != 4 || len(z.Found) != 2 {
		t.Fatalf("zone %+v, want done, unclaimed, next 4, found [x y]", z)
	}
	if got := claim(t, path, "a", 90, t0); got != "" {
		t.Fatalf("claimed %s after every zone was done", got)
	}
}

func TestClaimZoneConcurrentClaimsNeverShare(t *testing.T) {
	path := testRun(t, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10)
	var mu sync.Mutex
	got := map[string]string{}
	var wg sync.WaitGroup
	for i := range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			agent := fmt.Sprintf("explorer-%d", i)
			z, err := ClaimZone(path, agent, 90, nil, t0)
			if err != nil || z == nil {
				t.Errorf("%s: %+v %v", agent, z, err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			if prev, dup := got[z.ID]; dup {
				t.Errorf("%s and %s both claimed %s", prev, agent, z.ID)
			}
			got[z.ID] = agent
		}()
	}
	wg.Wait()
}

func TestNewVoidRunBuildsWaypointsAndActive(t *testing.T) {
	z := Zone{ID: "V01", Grid: 50, Cells: [][2]int{{0, 0}, {1, 0}, {2, 0}, {3, 0}}}
	r := NewVoidRun([]Zone{z}, 100, t0)
	if len(r.Zones) != 1 || len(r.Zones[0].Waypoints) != 2 {
		t.Fatalf("run %+v, want one zone with one lane (2 waypoints)", r)
	}
	if r.Active() {
		t.Error("a fresh run reports active")
	}
	r.Zones[0].Next = 1
	if !r.Active() {
		t.Error("a run with progress reports inactive")
	}
}
