package worker

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

// A refuel alone leaves State.Fuel holding its pre-refuel value, and the
// standing loop never re-reads ship state. A worker that trusts that cache
// believes it is still empty and refuses to move — assist-nexus reported
// fuel 2/95 for minutes after refuelling to 95/95 (2026-07-29).
func TestRefuelAndSyncRefreshesTheFuelCache(t *testing.T) {
	client := &fakeClient{
		state:      &game.State{Fuel: 2, Ship: game.Ship{Fuel: 2, MaxFuel: 95}, MaxFuel: 95},
		refuelFuel: 95,
	}
	if err := RefuelAndSync(context.Background(), client, io.Discard, "assist"); err != nil {
		t.Fatalf("RefuelAndSync: %v", err)
	}
	if fuel, _ := client.state.GetFuel(); fuel != 95 {
		t.Errorf("cached fuel = %.0f, want 95 — a refuel without a re-read leaves the worker believing it is empty", fuel)
	}
	if !slices.Contains(client.calls, "get_status") {
		t.Errorf("calls = %v, want a state re-read after the refuel", client.calls)
	}
	if i, j := slices.Index(client.calls, "refuel"), slices.Index(client.calls, "get_status"); i < 0 || j < i {
		t.Errorf("calls = %v, want refuel before get_status", client.calls)
	}
}

// A failed refuel must surface as an error, and must not be papered over by a
// refresh that makes it look like something happened.
func TestRefuelAndSyncReturnsTheRefuelError(t *testing.T) {
	client := &fakeClient{
		state:     &game.State{Fuel: 2, MaxFuel: 95},
		refuelErr: errors.New("no_fuel_cells: No fuel cells found in cargo"),
	}
	err := RefuelAndSync(context.Background(), client, io.Discard, "assist")
	if err == nil {
		t.Fatal("want the refuel error, got nil")
	}
	if !strings.Contains(err.Error(), "no_fuel_cells") {
		t.Errorf("error = %v, want the server's reason preserved", err)
	}
	if slices.Contains(client.calls, "get_status") {
		t.Errorf("calls = %v, must not refresh after a failed refuel", client.calls)
	}
}

// The refresh is best-effort: the refuel already succeeded, so a failed
// re-read must not be reported as a failed refuel.
func TestRefuelAndSyncSurvivesAFailedRefresh(t *testing.T) {
	client := &failingStatusClient{fakeClient: fakeClient{state: &game.State{Fuel: 2, MaxFuel: 95}}}
	var out strings.Builder
	if err := RefuelAndSync(context.Background(), client, &out, "assist"); err != nil {
		t.Fatalf("a failed refresh must not fail the refuel: %v", err)
	}
	if !strings.Contains(out.String(), "stale") {
		t.Errorf("a failed refresh must warn that the cache may be stale, got %q", out.String())
	}
}

type failingStatusClient struct{ fakeClient }

func (f *failingStatusClient) GetStatus(ctx context.Context) error {
	return errors.New("not connected")
}
