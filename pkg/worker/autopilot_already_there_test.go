package worker

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

// TestTravelToPOIAlreadyThereIsANoOp is the fix for a strand that looked
// exactly like an out-of-fuel strand and was not one.
//
// Live 2026-08-13: explorer-1 sat docked at voss_redoubt in Alhena holding 420
// liquid_hydrogen for an opportunity whose sell station WAS voss_redoubt. The
// autopilot resolved "already at the target system", then issued travel to the
// POI it was already standing on. The server checks fuel before it checks
// distance, so at 0/120 that zero-distance move was rejected with "Insufficient
// fuel for travel" — and because the station's fuel desk was dry, the worker
// could never satisfy a precondition it never actually needed. It burned 84
// retries against a sale it could have made without moving.
func TestTravelToPOIAlreadyThereIsANoOp(t *testing.T) {
	fc := &fakeClient{
		fuelLow: true, // a dry tank must not matter: no move is required
		state: &game.State{
			Doc:    true,
			Player: game.Player{CurrentPOI: "voss_redoubt"},
		},
	}
	var out strings.Builder
	if err := autopilotTravelToPOI(context.Background(), fc, &out, "voss_redoubt"); err != nil {
		t.Fatalf("standing at the destination is arrival, not a failure: %v", err)
	}
	for _, c := range fc.calls {
		if strings.HasPrefix(c, "travel:") {
			t.Errorf("issued %q while already at the POI; the server prices this as a move", c)
		}
	}
}

// TestTravelToPOIStillTravelsWhenElsewhere guards the other direction: the
// early return must key on the destination, not swallow every travel.
func TestTravelToPOIStillTravelsWhenElsewhere(t *testing.T) {
	fc := &fakeClient{
		state: &game.State{
			Doc:    true,
			Player: game.Player{CurrentPOI: "ironhearth"},
		},
	}
	if err := autopilotTravelToPOI(context.Background(), fc, io.Discard, "voss_redoubt"); err != nil {
		t.Fatalf("travel: %v", err)
	}
	var traveled bool
	for _, c := range fc.calls {
		if c == "travel:voss_redoubt" {
			traveled = true
		}
	}
	if !traveled {
		t.Fatal("a different POI must still be traveled to")
	}
}

// TestTravelToPOIUnknownLocationStillTravels: an empty CurrentPOI means we do
// not know where we are, which is not the same as being there. Guessing
// "arrived" on missing data would strand a worker silently at the wrong POI.
func TestTravelToPOIUnknownLocationStillTravels(t *testing.T) {
	fc := &fakeClient{state: &game.State{}}
	if err := autopilotTravelToPOI(context.Background(), fc, io.Discard, "voss_redoubt"); err != nil {
		t.Fatalf("travel: %v", err)
	}
	if len(fc.calls) == 0 || !strings.HasPrefix(fc.calls[0], "travel:") {
		t.Fatalf("unknown location must still attempt travel, got %v", fc.calls)
	}
}
