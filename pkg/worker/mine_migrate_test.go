package worker

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// depletingMineClient answers "Resources depleted" to a mine at any POI in
// depleted, judged by the last POI the ship travelled to.
type depletingMineClient struct {
	*mineFakeClient
	depleted map[string]bool
}

func (f *depletingMineClient) Mine(ctx context.Context) error {
	here := ""
	for _, c := range slices.Backward(f.calls) {
		if p, ok := strings.CutPrefix(c, "travel:"); ok {
			here = p
			break
		}
	}
	if f.depleted[here] {
		f.calls = append(f.calls, "mine")
		return errors.New("mine: Resources depleted")
	}
	return f.mineFakeClient.Mine(ctx)
}

// newMigrateKB is newMineAtTestKB plus ice on the pinned belt and a second
// ice deposit in the same system.
func newMigrateKB(t *testing.T) *knowledge.SQLiteKB {
	t.Helper()
	kb := newMineAtTestKB(t, "", false)
	ctx := context.Background()
	for _, p := range []knowledge.POI{
		{ID: "glenhaven_ice_belt", SystemID: "glenhaven", Name: "Ice Belt", Type: "asteroid_belt",
			Resources: []game.POIResource{{ResourceID: "ice", Richness: 50, Remaining: 0}}, LastUpdatedTick: 2},
		{ID: "glenhaven_ice_field", SystemID: "glenhaven", Name: "Ice Field", Type: "ice_field",
			Resources: []game.POIResource{{ResourceID: "ice", Richness: 40, Remaining: 900}}, LastUpdatedTick: 2},
	} {
		if err := kb.RememberPOI(ctx, p); err != nil {
			t.Fatalf("RememberPOI(%s): %v", p.ID, err)
		}
	}
	return kb
}

// A depleted pinned belt sends the miner to the nearest other deposit of the
// item, and the haul is still handed off.
func TestMineQtyAtMigratesOffDepletedBelt(t *testing.T) {
	agentsDir := t.TempDir()
	writeDeliverCreds(t, agentsDir, "craftsman-3", "Artisan 'Ace' Anderson")
	kb := newMigrateKB(t)
	client := &depletingMineClient{mineFakeClient: newMineAtClient(130, 5), depleted: map[string]bool{"glenhaven_ice_belt": true}}
	d := noWaitMineDispatch(client, kb, agentsDir)
	var out bytes.Buffer
	d.Out = &out

	if err := d.MineQtyAt(context.Background(), "ice", 5, "glenhaven", "glenhaven_ice_belt", "to_base", "craftsman-3"); err != nil {
		t.Fatalf("MineQtyAt: %v\n%s", err, out.String())
	}
	if !slices.Contains(client.calls, "travel:glenhaven_ice_field") {
		t.Fatalf("calls %v: never migrated to the other deposit", client.calls)
	}
	if len(client.giftCalls) != 1 || client.giftCalls[0]["quantity"] != float64(5) {
		t.Fatalf("gift calls = %+v, want one gift of 5", client.giftCalls)
	}
	if !strings.Contains(out.String(), "glenhaven_ice_belt depleted") {
		t.Errorf("no migration line in output:\n%s", out.String())
	}
}

// Once found depleted, the pinned belt rests: the next pass goes straight to
// the alternate without trying it.
func TestMineQtyAtSkipsRestingBelt(t *testing.T) {
	agentsDir := t.TempDir()
	writeDeliverCreds(t, agentsDir, "craftsman-3", "Artisan 'Ace' Anderson")
	kb := newMigrateKB(t)
	client := &depletingMineClient{mineFakeClient: newMineAtClient(130, 5)}
	d := noWaitMineDispatch(client, kb, agentsDir)
	d.markResting("glenhaven_ice_belt", time.Now())

	if err := d.MineQtyAt(context.Background(), "ice", 5, "glenhaven", "glenhaven_ice_belt", "to_base", "craftsman-3"); err != nil {
		t.Fatalf("MineQtyAt: %v", err)
	}
	if slices.Contains(client.calls, "travel:glenhaven_ice_belt") {
		t.Fatalf("calls %v: visited the resting belt", client.calls)
	}
	if !slices.Contains(client.calls, "travel:glenhaven_ice_field") {
		t.Fatalf("calls %v: did not mine the alternate", client.calls)
	}
}

// After the rest period the pinned belt is tried again.
func TestRestingPOIExpires(t *testing.T) {
	d := &WorkerDispatch{}
	t0 := time.Now()
	d.markResting("belt", t0)
	if !d.isResting("belt", t0.Add(MineDepletedRest-time.Minute)) {
		t.Fatal("belt not resting inside the rest window")
	}
	if d.isResting("belt", t0.Add(MineDepletedRest)) {
		t.Fatal("belt still resting after the rest window")
	}
	if d.isResting("other", t0) {
		t.Fatal("unknown POI reported resting")
	}
}

// With nowhere else to go, the depletion is still an error (the overmind's
// stall handling owns it from there), and nothing is handed off.
func TestMineQtyAtDepletedWithNoAlternateErrors(t *testing.T) {
	kb := newMigrateKB(t)
	client := &depletingMineClient{mineFakeClient: newMineAtClient(130, 5),
		depleted: map[string]bool{"glenhaven_ice_belt": true, "glenhaven_ice_field": true}}
	d := noWaitMineDispatch(client, kb, t.TempDir())

	err := d.MineQtyAt(context.Background(), "ice", 5, "glenhaven", "glenhaven_ice_belt", "to_base", "craftsman-3")
	if !isDepletedErr(err) {
		t.Fatalf("err = %v, want the depletion error", err)
	}
	if len(client.giftCalls) != 0 {
		t.Fatalf("gift calls = %+v, want none", client.giftCalls)
	}
}
