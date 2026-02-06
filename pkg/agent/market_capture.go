package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
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

// CaptureMarketData fetches and stores market data
func CaptureMarketData(ctx context.Context, client *game.Client, kb knowledge.Base, agentID string) error {
	// 1. Get listings from game
	if err := client.GetListings(ctx); err != nil {
		return fmt.Errorf("failed to get listings: %w", err)
	}

	// 2. Give server time to respond
	time.Sleep(500 * time.Millisecond)

	// 3. Get the current state
	state := client.GetState()

	// 4. Create snapshot
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

	snapshot := knowledge.MarketSnapshot{
		SystemID:    state.System.ID,
		SystemName:  state.System.Name,
		StationID:   stationID,
		StationName: stationName,
		GameTick:    state.CurrentTick,
		Listings:    make([]knowledge.MarketListing, len(listings)),
		CapturedAt:  time.Now(),
	}

	// Copy listings (convert from game.MarketListing to knowledge.MarketListing)
	for i, l := range listings {
		snapshot.Listings[i] = knowledge.MarketListing{
			ItemID:      l.ItemID,
			ItemType:    l.ItemType,
			Quantity:    l.Quantity,
			PricePerUnit: l.PricePerUnit,
			TotalPrice:  l.TotalPrice,
			Type:        l.Type,
			ListedBy:    l.ListedBy,
		}
	}

	// 5. Store in knowledge base
	if err := kb.StoreMarketSnapshot(ctx, snapshot, agentID); err != nil {
		return fmt.Errorf("failed to store market snapshot: %w", err)
	}

	return nil
}
