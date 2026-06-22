package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/market"
)

// ShouldCaptureMarket determines if market should be captured based on agent state
func ShouldCaptureMarket(state *game.State, agentStatus Status) bool {
	// Capture when idle OR docked
	if agentStatus.State == AgentStateIdle {
		return true
	}
	if state.IsDocked() {
		return true
	}
	return false
}

// CaptureMarketData fetches and stores market data via the market collector.
func CaptureMarketData(ctx context.Context, client *game.Client, mc *market.Collector, agentID string) error {
	// 1. Get listings from game
	if err := client.GetListings(ctx); err != nil {
		return fmt.Errorf("failed to get listings: %w", err)
	}

	// 2. Give server time to respond
	time.Sleep(500 * time.Millisecond)

	// 3. Get the current state
	state := client.GetState()

	// 4. Get listings
	listings := client.GetMarketListings()
	if len(listings) == 0 {
		return fmt.Errorf("no listings received")
	}

	// Find current station info
	var stationID, stationName string
	for _, poi := range state.System.POIs {
		if poi.ID == state.CurrentPOI {
			stationID = poi.ID
			stationName = poi.Name
			break
		}
	}

	if stationID == "" {
		// Use current POI as fallback
		stationID = state.CurrentPOI
		stationName = state.CurrentPOI
	}

	now := time.Now()
	snapshot := market.MarketSnapshot{
		StationID:   stationID,
		StationName: stationName,
		SystemID:    state.System.ID,
		SystemName:  state.System.Name,
		CapturedAt:  now,
		Orders:      market.OrdersFromListings(stationID, listings, "agent", now),
	}

	// 5. Store via market collector
	if err := mc.WriteSnapshot(ctx, snapshot); err != nil {
		return fmt.Errorf("failed to store market snapshot: %w", err)
	}

	return nil
}
