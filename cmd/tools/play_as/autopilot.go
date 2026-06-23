package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/worker"
)

// autopilot executes a multi-jump route to a target system via the shared
// worker engine, updating the KB (and writing intel files) at each waypoint.
// Usage: autopilot <system> [poi]
func autopilot(client game.GameClient, ctx context.Context, parts []string, format outputFormat) error {
	if len(parts) < 2 {
		return fmt.Errorf("usage: autopilot <system-name> [poi-name]")
	}
	targetSystem := parts[1]
	targetPOI := ""
	if len(parts) >= 3 {
		targetPOI = strings.Join(parts[2:], " ")
	}

	// Styled mode prints progress; raw mode suppresses it (the legacy per-jump
	// JSON dump is intentionally dropped — workers never used it).
	out := io.Writer(os.Stdout)
	if format != formatStyled {
		out = io.Discard
	}

	return worker.Autopilot(ctx, worker.AutopilotDeps{
		Client: client,
		Out:    out,
		// play_as preserves its per-waypoint intel-file writes via these wrappers.
		OnWaypoint: func(ctx context.Context) error {
			if globalKB == nil {
				return nil
			}
			if err := kbUpdateSystem(client, ctx); err != nil {
				return err
			}
			return kbUpdatePOI(client, ctx)
		},
	}, targetSystem, targetPOI)
}
