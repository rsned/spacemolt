package main

import (
	"context"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

// stubDistressClient records whether a distress signal went out and with what
// need, and lets a test set the fuel/cargo/credits state that should trigger it.
type stubDistressClient struct {
	game.GameClient
	state     *game.State
	distress  []string
	refuels   int
	statusHit int
}

func (s *stubDistressClient) GetState() *game.State { return s.state }

func (s *stubDistressClient) DistressSignal(_ context.Context, distressType string) error {
	s.distress = append(s.distress, distressType)

	return nil
}

func (s *stubDistressClient) Refuel(_ context.Context) error {
	s.refuels++

	return nil
}

func (s *stubDistressClient) GetStatus(_ context.Context) error {
	s.statusHit++

	return nil
}

// resetDistress isolates each test from the package-level latch. Without it
// the first test to raise a signal silences every later one, which is exactly
// the cross-test leak that hid the missing reset on recovery.
func resetDistress(t *testing.T) {
	t.Helper()
	saved := distressRaised
	distressRaised = false
	t.Cleanup(func() { distressRaised = saved })
}

func dryState(fuel, maxFuel float64, cargo []game.CargoItem) *game.State {
	st := &game.State{Fuel: fuel, MaxFuel: maxFuel}
	st.Ship.Cargo = cargo

	return st
}

// A fuel-starved explorer with no fuel_cell used to print
// "Fuel low (5%) but no fuel_cell in cargo — continuing" and keep jumping,
// which is how craftsman-1 spent its last three jumps' worth of fuel and
// stranded at groombridge_1618 -- a system with no station, five jumps from
// the nearest refuel and ten from the nearest tanker.
//
// A GSA tow costs 500 credits and arrives in ~15 minutes, and
// DistressSignal has been on the GameClient interface the whole time with no
// fleet caller. Raise it and STOP, rather than continuing on the last of the
// tank.
func TestRefuelFromCargoIfLow_RaisesDistressWhenDry(t *testing.T) {
	resetDistress(t)
	c := &stubDistressClient{state: dryState(8, 1020, nil)}

	stranded := refuelFromCargoIfLow(c, context.Background(), formatStyled)

	if !stranded {
		t.Error("want stranded=true so the explore loop stops burning the last of the tank")
	}
	if len(c.distress) != 1 || c.distress[0] != "fuel" {
		t.Errorf("distress signals = %v, want exactly one \"fuel\"", c.distress)
	}
}

// The signal must not be re-raised on every pass: it is a public broadcast
// that generates rescue missions, and 170 workers each shouting per tick would
// flood the emergency channel.
func TestRefuelFromCargoIfLow_DistressFiresOncePerStrand(t *testing.T) {
	resetDistress(t)
	c := &stubDistressClient{state: dryState(8, 1020, nil)}
	ctx := context.Background()

	for range 4 {
		refuelFromCargoIfLow(c, ctx, formatStyled)
	}

	if len(c.distress) != 1 {
		t.Errorf("distress raised %d times, want 1", len(c.distress))
	}
}

// Cargo cells are still the first resort: no signal when the agent can fix
// itself, and no stop either.
func TestRefuelFromCargoIfLow_BurnsCargoCellBeforeSignalling(t *testing.T) {
	resetDistress(t)
	c := &stubDistressClient{state: dryState(8, 1020, []game.CargoItem{
		{ItemID: "fuel_cell", Quantity: 3},
	})}

	stranded := refuelFromCargoIfLow(c, context.Background(), formatStyled)

	if stranded {
		t.Error("want stranded=false: a cargo fuel_cell is a self-fix")
	}
	if len(c.distress) != 0 {
		t.Errorf("distress raised %v while cargo held a fuel_cell", c.distress)
	}
	if c.refuels != 1 {
		t.Errorf("refuels = %d, want 1", c.refuels)
	}
}

// Above the threshold nothing happens at all.
func TestRefuelFromCargoIfLow_QuietWhenFuelIsFine(t *testing.T) {
	resetDistress(t)
	c := &stubDistressClient{state: dryState(900, 1020, nil)}

	if refuelFromCargoIfLow(c, context.Background(), formatStyled) {
		t.Error("want stranded=false at 88% fuel")
	}
	if len(c.distress) != 0 {
		t.Errorf("distress raised at 88%% fuel: %v", c.distress)
	}
}

// Docked is someone else's problem: explore() already refuels at stations, and
// a docked agent is not stranded.
func TestRefuelFromCargoIfLow_NoSignalWhileDocked(t *testing.T) {
	resetDistress(t)
	st := dryState(8, 1020, nil)
	st.Doc = true
	c := &stubDistressClient{state: st}

	if refuelFromCargoIfLow(c, context.Background(), formatStyled) {
		t.Error("want stranded=false while docked")
	}
	if len(c.distress) != 0 {
		t.Errorf("distress raised while docked: %v", c.distress)
	}
}

// A strand that ends must re-arm the signal. The latch used to be
// process-lifetime, so an agent rescued once would then run dry in silence.
func TestRefuelFromCargoIfLow_DistressRearmsAfterRecovery(t *testing.T) {
	resetDistress(t)
	ctx := context.Background()

	dry := &stubDistressClient{state: dryState(8, 1020, nil)}
	if !refuelFromCargoIfLow(dry, ctx, formatStyled) {
		t.Fatal("want stranded on the first strand")
	}

	// Rescued: fuel healthy again.
	full := &stubDistressClient{state: dryState(900, 1020, nil)}
	refuelFromCargoIfLow(full, ctx, formatStyled)

	// Dry a second time -- this must shout again.
	dry2 := &stubDistressClient{state: dryState(8, 1020, nil)}
	if !refuelFromCargoIfLow(dry2, ctx, formatStyled) {
		t.Fatal("want stranded on the second strand")
	}
	if len(dry2.distress) != 1 {
		t.Errorf("second strand raised %d signals, want 1", len(dry2.distress))
	}
}
