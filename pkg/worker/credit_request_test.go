package worker

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/creditreq"
	"github.com/rsned/spacemolt/pkg/game"
)

type fakeFiler struct {
	recs []creditreq.Record
	err  error
}

func (f *fakeFiler) File(r creditreq.Record) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	f.recs = append(f.recs, r)
	return true, nil
}

const brokeErr = "no_fuel_source: No fuel cells in cargo and insufficient credits for station refueling"

func newCreditClient(refuelErr error, docked bool, credits float64) (*CreditRequestingClient, *fakeFiler, *bytes.Buffer) {
	st := &game.State{Doc: docked, CurrentSystem: "sys_a", Fuel: 3, MaxFuel: 100}
	st.Player.DockedAtBase = "base_a"
	fc := &fakeClient{state: st, refuelErr: refuelErr}
	fc.onGetStatus = func() { st.Credits = credits }
	filer := &fakeFiler{}
	out := &bytes.Buffer{}
	return &CreditRequestingClient{
		GameClient: fc, Queue: filer, AgentID: "miner-1", Username: "Miner One", Out: out,
	}, filer, out
}

func TestRefuel_InsufficientCreditsFiles(t *testing.T) {
	orig := errors.New(brokeErr)
	c, filer, out := newCreditClient(orig, true, 0)
	if err := c.Refuel(context.Background()); !errors.Is(err, orig) {
		t.Fatalf("err = %v, want original", err)
	}
	if len(filer.recs) != 1 {
		t.Fatalf("records = %d, want 1", len(filer.recs))
	}
	r := filer.recs[0]
	if r.AgentID != "miner-1" || r.Username != "Miner One" || r.Credits != 0 ||
		r.BaseID != "base_a" || r.SystemID != "sys_a" || r.Fuel != 3 || r.MaxFuel != 100 ||
		r.Status != creditreq.StatusPending || r.RequestedAt.IsZero() ||
		!strings.Contains(r.Reason, "no_fuel_source") {
		t.Errorf("bad record: %+v", r)
	}
	if !strings.Contains(out.String(), "credit request filed (credits 0)") {
		t.Errorf("log = %q", out.String())
	}
}

func TestRefuel_DryDeskDoesNotFile(t *testing.T) {
	c, filer, _ := newCreditClient(errors.New("no_fuel_source: station has no fuel"), true, 0)
	_ = c.Refuel(context.Background())
	if len(filer.recs) != 0 {
		t.Fatalf("filed %d, want 0", len(filer.recs))
	}
}

func TestRefuel_RichAgentDoesNotFile(t *testing.T) {
	c, filer, _ := newCreditClient(errors.New(brokeErr), true, 12000)
	_ = c.Refuel(context.Background())
	if len(filer.recs) != 0 {
		t.Fatalf("filed %d, want 0", len(filer.recs))
	}
}

func TestRefuel_UndockedDoesNotFile(t *testing.T) {
	c, filer, _ := newCreditClient(errors.New(brokeErr), false, 0)
	_ = c.Refuel(context.Background())
	if len(filer.recs) != 0 {
		t.Fatalf("filed %d, want 0", len(filer.recs))
	}
}

func TestRefuel_SuccessDoesNotFile(t *testing.T) {
	c, filer, _ := newCreditClient(nil, true, 0)
	if err := c.Refuel(context.Background()); err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(filer.recs) != 0 {
		t.Fatalf("filed %d, want 0", len(filer.recs))
	}
}

func TestRefuel_FilerErrorIsSwallowed(t *testing.T) {
	orig := errors.New(brokeErr)
	c, filer, out := newCreditClient(orig, true, 0)
	filer.err = errors.New("disk on fire")
	if err := c.Refuel(context.Background()); !errors.Is(err, orig) {
		t.Fatalf("err = %v, want original", err)
	}
	if !strings.Contains(out.String(), "disk on fire") {
		t.Errorf("filer error not logged: %q", out.String())
	}
}
