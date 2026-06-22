package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/market"
)

const (
	// MarketFreshnessThreshold is how long market data is considered fresh (1 hour)
	// Since game ticks are 10 seconds, this is 360 ticks
	MarketFreshnessThreshold = 360 * time.Second
)

// RefreshMarketData ensures fresh market data for the current station.
// It checks if existing market data is less than an hour old, and if not,
// captures new market data from the game server.
//
// Returns the market snapshot (either cached or freshly captured) and any error.
//
// The agentID parameter is used for tracking who captured the market data.
//
// Usage example:
//
//	snapshot, err := agent.RefreshMarketData(ctx, client, mc, "craftsman-1")
//	if err != nil {
//	    return fmt.Errorf("failed to refresh market: %w", err)
//	}
//	// Use snapshot.Orders to make trading/crafting decisions
func RefreshMarketData(ctx context.Context, client *game.Client, mc *market.Collector, agentID string) (*market.MarketSnapshot, error) {
	state := client.GetState()

	// Get current station info
	stationID := state.CurrentPOI
	if stationID == "" {
		return nil, fmt.Errorf("not at a station")
	}

	// Try to get latest snapshot from market collector
	snapshot, err := mc.GetLatestSnapshot(ctx, stationID)
	if err != nil {
		return nil, fmt.Errorf("failed to query market snapshot: %w", err)
	}

	// Check if snapshot exists and is fresh
	if snapshot != nil && isMarketDataFresh(snapshot.CapturedAt) {
		// Data is fresh, return it
		return snapshot, nil
	}

	// Data is stale or doesn't exist, capture fresh data
	if err := CaptureMarketData(ctx, client, mc, agentID); err != nil {
		return nil, fmt.Errorf("failed to capture market data: %w", err)
	}

	// Retrieve the freshly captured snapshot
	snapshot, err = mc.GetLatestSnapshot(ctx, stationID)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve fresh market snapshot: %w", err)
	}

	if snapshot == nil {
		return nil, fmt.Errorf("market snapshot not found after capture")
	}

	return snapshot, nil
}

// isMarketDataFresh checks if market data is within the freshness threshold
func isMarketDataFresh(capturedAt time.Time) bool {
	return time.Since(capturedAt) < MarketFreshnessThreshold
}
