package worker

import (
	"context"
	"slices"
	"testing"

	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/sweep"
)

// GetNearby serves c.nearby (one reply per call; the last repeats) as the
// raw "nearby" payload; no reply means an empty location.
func (c *sweepFakeClient) GetNearby(context.Context) error {
	c.calls = append(c.calls, "get_nearby")
	if c.raw == nil {
		c.raw = map[string][]byte{}
	}
	if len(c.nearby) == 0 {
		c.raw["nearby"] = []byte(`{}`)
		return nil
	}
	c.raw["nearby"] = []byte(c.nearby[min(c.nearbyIdx, len(c.nearby)-1)])
	c.nearbyIdx++
	return nil
}

const nearbyLeviathan = `{"creatures":[{"creature_id":"c1","species":"molt_leviathan","name":"Molt Leviathan","role":"predator","hull":4000,"max_hull":4000}]}`
const nearbyGrazers = `{"creatures":[{"creature_id":"c2","species":"void_grazer","name":"Void Grazer","role":"grazer","hull":200,"max_hull":200}]}`

func leviathanLanding(t *testing.T, nearby ...string) (*arrivalFakeClient, *WorkerDispatch, string, *sweepSeams) {
	t.Helper()
	kb := knowledge.NewMemoryKB()
	placeSystem(t, kb, "maw", -4170, -7769, 1, 1, "sallow")
	s := sweep.NewState(7900, -120, 45)
	s.ProgressDeg = -120
	s.Drifted, s.LastX, s.LastY = true, -4170, -7769
	c := inSystem("maw", 900)
	c.nearby = nearby
	d, path, seams := newArrivalDispatch(t, c, kb, s)
	return c, d, path, seams
}

// v0.711.2: wildlife cannot strike on the arrival tick, so the first command
// after landing must be the way out when a leviathan is there.
func TestSweepArrivalLeavesOnLeviathan(t *testing.T) {
	c, d, path, seams := leviathanLanding(t, nearbyLeviathan)
	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(seams.tours)+len(seams.steps) != 0 {
		t.Errorf("toured/explored beside a leviathan: %v %v", seams.tours, seams.steps)
	}
	got, _, _ := sweep.Load(path)
	if len(c.jumps) != 1 || !slices.Contains(got.Unfinished, "maw") || got.Phase != sweep.PhaseLap {
		t.Errorf("jumps %v unfinished %v phase %q, want one rejoin launch", c.jumps, got.Unfinished, got.Phase)
	}
}

// The arrival race: a get_nearby in the arrival tick can read empty; the
// re-read finds the leviathan.
func TestSweepArrivalLeviathanOnReRead(t *testing.T) {
	c, d, _, seams := leviathanLanding(t, `{}`, nearbyLeviathan)
	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(seams.tours) != 0 || len(c.jumps) != 1 {
		t.Errorf("tours %v jumps %v, want the re-read to send it away", seams.tours, c.jumps)
	}
}

func TestSweepArrivalToursBesideGrazers(t *testing.T) {
	c, d, _, seams := leviathanLanding(t, nearbyGrazers)
	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(seams.tours) != 1 || len(c.jumps) != 0 {
		t.Errorf("tours %v jumps %v, want a normal tour", seams.tours, c.jumps)
	}
}
