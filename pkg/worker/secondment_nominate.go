package worker

import (
	"context"
	"fmt"
	"time"

	"github.com/rsned/spacemolt/pkg/overmind/supervisor"
)

// unlockAwayFleet is the fleet a nominated hauler is loaned to. It is the pool
// that runs the pirate-unlock mission chain.
const unlockAwayFleet = "unlock"

// nominateUnlockFn returns the HaulDeps.NominateForUnlock hook, or nil when this
// worker has no ledger configured (the default for every fleet but haul).
//
// The worker's entire role here is to APPEND a request. It never edits fleet
// membership and never moves itself: a worker that could start or stop itself in
// a fleet is a worker that can end up running twice, which takes its game session
// down (status 4001, session_replaced). Moving is the reconciler's job, done in a
// strict stop-then-start order — see pkg/overmind/supervisor/reconcile.go.
func (d *WorkerDispatch) nominateUnlockFn() func(ctx context.Context, stationID, systemID string) error {
	if d.SecondmentPath == "" {
		return nil
	}
	home := d.FleetName
	if home == "" {
		home = "haul"
	}
	return func(_ context.Context, stationID, systemID string) error {
		led, err := supervisor.LoadSecondments(d.SecondmentPath)
		if err != nil {
			// A corrupt ledger must not be overwritten by a nomination — that
			// would erase trips already in flight and could re-nominate an agent
			// mid-move. Report and leave the file alone.
			return fmt.Errorf("secondment ledger unreadable: %w", err)
		}
		if !led.Nominate(supervisor.Secondment{
			AgentID:     d.AgentID,
			HomeFleet:   home,
			AwayFleet:   unlockAwayFleet,
			Reason:      "sold in nebula space without the pirate unlock",
			NominatedAt: time.Now().UTC().Format(time.RFC3339),
			StationID:   stationID,
			SystemID:    systemID,
		}) {
			return errAlreadyNominated
		}
		return supervisor.SaveSecondments(d.SecondmentPath, led)
	}
}

// errAlreadyNominated is returned when this agent already has a trip open. It is
// the expected case on every pass after the first, so callers treat it as quiet.
var errAlreadyNominated = fmt.Errorf("already nominated")
