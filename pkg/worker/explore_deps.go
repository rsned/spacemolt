package worker

import (
	"context"
	"errors"
	"io"

	"github.com/rsned/spacemolt/pkg/explore"
	"github.com/rsned/spacemolt/pkg/game"
)

// workerExploreDeps wires pkg/explore to the worker's captures: the KB
// system, POI and station writers, autopilot travel and the settle seam.
func workerExploreDeps(deps ExploreDeps) explore.Deps {
	out := deps.Out
	if out == nil {
		out = io.Discard
	}
	return explore.Deps{
		Client:  deps.Client,
		KB:      deps.KB,
		AgentID: deps.AgentID,
		Report:  explore.WriterReporter{W: out},
		Settle:  settle,
		// The server decides whether the ship can survey (a module, an
		// integrated hull scanner or a consumable all count); its refusal is
		// logged and the tour goes on.
		HasScanner: func(*game.State) bool { return true },
		UpdateSystem: func(ctx context.Context) error {
			return KBUpdateSystem(ctx, deps.Client, deps.KB, deps.AgentID)
		},
		UpdatePOI: func(ctx context.Context) error {
			return KBUpdatePOI(ctx, deps.Client, deps.KB, deps.AgentID)
		},
		StationCapture: func(ctx context.Context) error {
			return errors.Join(
				KBUpdateStation(ctx, deps.Client, deps.KB, deps.Market, "worker"),
				KBUpdateFacilities(ctx, deps.Client, deps.KB),
				KBUpdateMissions(ctx, deps.Client, deps.KB),
			)
		},
		Travel: func(ctx context.Context, poiID string) (bool, error) {
			return false, autopilotTravelToPOI(ctx, deps.Client, out, poiID)
		},
	}
}
