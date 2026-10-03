package assets

import (
	"context"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// CaptureCargo records the manifest of the agent's ACTIVE hold.
//
// This closes the gap that hid the mission-grant bug: agent_hulls carries
// cargo_used, the scalar, so a hold reads "301/315" with no way to learn what
// the 301 units are. explorer-5 carried 200 starshine and 100 nerve_burn for ten
// days — supplied mission cargo the worker could not see — and no query against
// any database could show it. Fleetwide that blind spot covered roughly 1.9M
// credits of contraband (2026-10-02).
//
// GetCargo is issued first so the manifest is the SERVER's, not a cached clone.
// That matters here more than anywhere: the bug this capture exists to surface
// is precisely a client hold that disagrees with the real one, and capturing the
// stale copy would reproduce the blindness rather than cure it.
//
// Failure policy matches CaptureProfile and CaptureStorage: a source failure
// degrades to "less captured this pass" and returns nil; only a store write
// propagates. An empty hold is a legitimate observation and IS recorded, because
// "this agent is carrying nothing" is exactly as useful as a full manifest.
func CaptureCargo(ctx context.Context, client game.GameClient, st *Store, agentID string, now time.Time) error {
	if st == nil || client == nil {
		return nil
	}
	if err := client.GetCargo(ctx); err != nil {
		return nil //nolint:nilerr // a source failure must never fail the pass
	}
	state := client.GetState()
	if state == nil || state.Player.ID == "" {
		return nil
	}
	shipID := state.Ship.ID
	if shipID == "" {
		// Without a ship id the rows cannot be keyed, and guessing one would
		// attribute this hold to the wrong hull on the next capture.
		return nil
	}

	rows := make([]CargoRow, 0, len(state.Ship.Cargo))
	for _, c := range state.Ship.Cargo {
		rows = append(rows, CargoRow{
			ShipID:   shipID,
			ItemID:   c.ItemID,
			Quantity: c.Quantity,
		})
	}

	return st.ReplaceCargo(ctx, state.Player.ID, shipID, rows, now)
}
