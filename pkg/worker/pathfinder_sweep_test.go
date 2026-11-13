package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/sweep"
)

// sweepFakeClient scripts get_location replies and records the drive and
// cell calls a sweep pass makes.
type sweepFakeClient struct {
	game.GameClient
	state      *game.State
	locs       [][]byte // one per get_location; the last repeats
	locIdx     int
	launchCost float64
	calls      []string // "jump <bearing>", "use <item> <qty>", plus other verbs
	jumps      []float64
	raw        map[string][]byte
	// lag models the real client: UseItem and Jump change fuel server-side
	// but the cached state only catches up on GetShip.
	lag         bool
	pendingFuel float64
}

func (c *sweepFakeClient) GetState() *game.State { return c.state }

func (c *sweepFakeClient) RawCommand(_ context.Context, command string, _ map[string]any) error {
	if command == "get_location" {
		if c.raw == nil {
			c.raw = map[string][]byte{}
		}
		c.raw["location"] = c.locs[min(c.locIdx, len(c.locs)-1)]
		c.locIdx++
	}
	return nil
}

func (c *sweepFakeClient) GetRawJSON(key string) []byte { return c.raw[key] }

func (c *sweepFakeClient) Jump(_ context.Context, target string) (*game.JumpResult, error) {
	b, err := strconv.ParseFloat(target, 64)
	if err != nil {
		return nil, fmt.Errorf("fake: non-numeric jump %q", target)
	}
	c.calls = append(c.calls, "jump "+target)
	c.jumps = append(c.jumps, b)
	c.addFuel(-c.launchCost)
	return &game.JumpResult{Drifting: true}, nil
}

func (c *sweepFakeClient) UseItem(_ context.Context, itemID string, qty int) error {
	c.calls = append(c.calls, fmt.Sprintf("use %s %d", itemID, qty))
	per := map[string]float64{"fuel_cell": 20, "premium_fuel_cell": 50}[itemID]
	c.addFuel(per * float64(qty))
	for i := range c.state.Ship.Cargo {
		if c.state.Ship.Cargo[i].ItemID == itemID {
			c.state.Ship.Cargo[i].Quantity -= float64(qty)
		}
	}
	return nil
}

func (c *sweepFakeClient) addFuel(f float64) {
	if c.lag {
		c.pendingFuel += f
		return
	}
	c.state.Ship.Fuel += f
}

func (c *sweepFakeClient) GetShip(context.Context) error {
	c.state.Ship.Fuel += c.pendingFuel
	c.pendingFuel = 0
	return nil
}

func driftLoc(x, y, bearing float64) []byte {
	b, _ := json.Marshal(map[string]any{"location": map[string]any{
		"in_transit": true, "transit_type": "pathfinder",
		"transit_x": x, "transit_y": y, "transit_bearing": bearing,
		"nearby_repeaters": []any{},
	}})
	return b
}

func noSweepSleep(t *testing.T) {
	t.Helper()
	restore := sleepFunc
	sleepFunc = func(context.Context, time.Duration) error { return nil }
	t.Cleanup(func() { sleepFunc = restore })
}

// newSweepDispatch saves s for explorer-3 under a temp agents dir and
// returns a dispatch configured for the same ring.
func newSweepDispatch(t *testing.T, c *sweepFakeClient, kb knowledge.Base, s *sweep.State) (*WorkerDispatch, string) {
	t.Helper()
	noSweepSleep(t)
	d := NewWorkerDispatch(c, kb, nil, &bytes.Buffer{})
	d.AgentID = "explorer-3"
	d.AgentsDir = t.TempDir()
	d.SweepRing, d.SweepStartDeg, d.SweepLaunchCost = s.Ring, s.StartDeg, s.LaunchCost
	path := sweep.StatePath(d.AgentsDir, d.AgentID)
	if err := sweep.Save(path, s); err != nil {
		t.Fatal(err)
	}
	return d, path
}

func round1(b float64) float64 { return math.Round(b*10) / 10 }

// lapLeg is a ship on ring 7500 heading from the 148° ring point to 143°.
func lapLeg() (*sweep.State, float64) {
	s := sweep.NewState(7500, 148, 45)
	s.LaunchCostMeasured = true
	s.Launches = 1
	s.Drifted = true
	s.TargetX, s.TargetY = sweep.RingPoint(7500, 143)
	s.TargetDeg = 143
	fx, fy := sweep.RingPoint(7500, 148)
	s.PlannedBearing = round1(sweep.BearingTo(fx, fy, s.TargetX, s.TargetY))
	return s, s.PlannedBearing
}

func shipWith(fuel, maxFuel float64, cargo ...game.CargoItem) *game.State {
	return &game.State{Ship: game.Ship{Fuel: fuel, MaxFuel: maxFuel, Cargo: cargo}}
}

// back returns the point dist LY short of (x,y) along bearing b.
func back(x, y, b, dist float64) (float64, float64) {
	r := b * math.Pi / 180
	return x - dist*math.Cos(r), y - dist*math.Sin(r)
}

func TestSweepPassRedirectsAtWaypoint(t *testing.T) {
	s, b := lapLeg()
	x1, y1 := back(s.TargetX, s.TargetY, b, 300)
	x2, y2 := back(s.TargetX, s.TargetY, b, 5)
	c := &sweepFakeClient{state: shipWith(1000, 1020), launchCost: 45,
		locs: [][]byte{driftLoc(x1, y1, b), driftLoc(x2, y2, b)}}
	d, path := newSweepDispatch(t, c, knowledge.NewMemoryKB(), s)

	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(c.jumps) != 1 {
		t.Fatalf("jumps = %v, want exactly one redirect", c.calls)
	}
	nx, ny := sweep.RingPoint(7500, 138)
	if want := round1(sweep.BearingTo(x2, y2, nx, ny)); c.jumps[0] != want {
		t.Errorf("redirect bearing %v, want %v", c.jumps[0], want)
	}
	got, _, err := sweep.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.ProgressDeg != 143 || got.SweptDeg != 5 || got.TargetDeg != 138 {
		t.Errorf("progress %v swept %v target %v, want 143/5/138", got.ProgressDeg, got.SweptDeg, got.TargetDeg)
	}
}

func TestSweepPassStatePersisted(t *testing.T) {
	s, b := lapLeg()
	x, y := back(s.TargetX, s.TargetY, b, 5)
	c := &sweepFakeClient{state: shipWith(1000, 1020), launchCost: 45, locs: [][]byte{driftLoc(x, y, b)}}
	d, path := newSweepDispatch(t, c, knowledge.NewMemoryKB(), s)
	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, ok, err := sweep.Load(path)
	if err != nil || !ok {
		t.Fatalf("Load: %v %v", ok, err)
	}
	if got.Launches != 2 || got.ProgressDeg != 143 || got.LastX != x || got.LastY != y {
		t.Errorf("saved %+v", got)
	}
}

// A pass that wakes up already past its waypoint steps on; it never turns
// round to chase the point it overshot.
func TestSweepPassOvershootAdvances(t *testing.T) {
	s, b := lapLeg()
	x, y := back(s.TargetX, s.TargetY, b, -30)
	c := &sweepFakeClient{state: shipWith(1000, 1020), launchCost: 45, locs: [][]byte{driftLoc(x, y, b)}}
	d, _ := newSweepDispatch(t, c, knowledge.NewMemoryKB(), s)
	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(c.jumps) != 1 {
		t.Fatalf("calls = %v, want one redirect", c.calls)
	}
	diff := math.Abs(math.Mod(c.jumps[0]-(b+180)+540, 360) - 180)
	if diff <= 90 {
		t.Errorf("redirect %v turns back toward the passed waypoint (reverse of %v)", c.jumps[0], b)
	}
}

func TestSweepPassGoesHomeAtThreshold(t *testing.T) {
	s, b := lapLeg()
	x, y := back(s.TargetX, s.TargetY, b, 5)
	kb := knowledge.NewMemoryKB()
	addSweepStation(t, kb, "facing_home", -6000, 4000)
	c := &sweepFakeClient{state: shipWith(100, 1020), launchCost: 45, locs: [][]byte{driftLoc(x, y, b)}}
	d, path := newSweepDispatch(t, c, kb, s)
	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _, _ := sweep.Load(path)
	if got.Phase != sweep.PhaseHome || got.HomeSystem != "facing_home" {
		t.Fatalf("phase %q home %q, want home/facing_home", got.Phase, got.HomeSystem)
	}
	if len(c.jumps) != 1 || c.jumps[0] != round1(sweep.BearingTo(x, y, -6000, 4000)) {
		t.Errorf("jumps %v, want one toward the station", c.jumps)
	}
}

func TestSweepPassUsesCellsBeforeRedirect(t *testing.T) {
	s, b := lapLeg()
	x, y := back(s.TargetX, s.TargetY, b, 5)
	c := &sweepFakeClient{state: shipWith(40, 1020, game.CargoItem{ItemID: "fuel_cell", Quantity: 30}), launchCost: 45,
		locs: [][]byte{driftLoc(x, y, b)}}
	d, _ := newSweepDispatch(t, c, knowledge.NewMemoryKB(), s)
	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(c.calls) < 2 || c.calls[0] != "use fuel_cell 30" || c.jumps == nil {
		t.Fatalf("calls = %v, want use fuel_cell 30 then a jump", c.calls)
	}
}

func TestSweepPassStormLogsGap(t *testing.T) {
	s := sweep.NewState(7500, 148, 45)
	s.LaunchCostMeasured, s.Launches, s.Drifted = true, 1, true
	s.TargetX, s.TargetY = sweep.RingPoint(7500, 143)
	s.TargetDeg, s.PlannedBearing = 143, 226.5
	x, y := sweep.RingPoint(7500, 146)
	c := &sweepFakeClient{state: shipWith(1000, 1020), launchCost: 45, locs: [][]byte{driftLoc(x, y, 190)}}
	d, path := newSweepDispatch(t, c, knowledge.NewMemoryKB(), s)
	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _, _ := sweep.Load(path)
	if len(got.Gaps) != 1 || got.Gaps[0].Reason != "storm" || got.Gaps[0].FromDeg != 148 || got.Gaps[0].ToDeg != 143 {
		t.Fatalf("gaps = %+v", got.Gaps)
	}
	if len(c.jumps) != 1 {
		t.Errorf("calls = %v, want one re-aim", c.calls)
	}
}

func addSweepStation(t *testing.T, kb knowledge.Base, id string, x, y float64) {
	t.Helper()
	ctx := context.Background()
	if err := kb.RememberSystem(ctx, knowledge.System{ID: id, Name: id, Position: game.Position{X: x, Y: y}}); err != nil {
		t.Fatal(err)
	}
	if err := kb.RememberPOI(ctx, knowledge.POI{ID: id + "_station", SystemID: id, Type: "station"}); err != nil {
		t.Fatal(err)
	}
	if err := kb.RememberBase(ctx, knowledge.SpaceBase{ID: id + "_base", POIID: id + "_station", PublicAccess: true}); err != nil {
		t.Fatal(err)
	}
}
