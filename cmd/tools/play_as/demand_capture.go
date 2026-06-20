package main

import (
	"context"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/worker"
)

// captureMarket persists the full source-classified market data from the
// client's most recent (full, no-item_id) view_market response. Best-effort:
// silently no-ops when the KB is absent, there is no market data, or the player
// is not at a station. Delegates to worker.CaptureMarket.
func captureMarket(client game.GameClient, ctx context.Context) {
	worker.CaptureMarket(ctx, client, globalKB)
}
