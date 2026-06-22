package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/market"
)

const (
	// MarketAnalysisFreshnessThreshold is how long market analysis is considered fresh (2 hours)
	// Analysis data changes less frequently than raw listings, so we can cache it longer
	MarketAnalysisFreshnessThreshold = 720 * time.Second
)

// RefreshMarketAnalysis ensures fresh market analysis insights for the current station.
// It provides AI-generated trading insights that can help identify profitable opportunities.
//
// Returns the market analysis (either cached or freshly captured) and any error.
//
// The analysis includes:
// - Top insights (AI-identified trading opportunities)
// - Market analysis data
// - XP gained from scanning
// - Scanning range and stations in range
//
// Usage example:
//
//	analysis, err := agent.RefreshMarketAnalysis(ctx, client, mc, "trader-1")
//	if err != nil {
//	    return fmt.Errorf("failed to refresh market analysis: %w", err)
//	}
//	for _, insight := range analysis.TopInsights {
//	    fmt.Printf("Insight: %v\n", insight)
//	}
func RefreshMarketAnalysis(ctx context.Context, client *game.Client, mc *market.Collector, agentID string) (*market.MarketAnalysis, error) {
	state := client.GetState()

	// Get current station info
	stationID := state.CurrentPOI
	if stationID == "" {
		return nil, fmt.Errorf("not at a station")
	}

	// Try to get latest analysis from market collector
	analysis, err := mc.GetLatestAnalysis(ctx, stationID)
	if err != nil {
		return nil, fmt.Errorf("failed to query market analysis: %w", err)
	}

	// Check if analysis exists and is fresh
	if analysis != nil && isMarketAnalysisFresh(analysis.CapturedAt) {
		// Data is fresh, return it
		return analysis, nil
	}

	// Data is stale or doesn't exist, capture fresh analysis
	if err := CaptureMarketAnalysis(ctx, client, mc, agentID); err != nil {
		return nil, fmt.Errorf("failed to capture market analysis: %w", err)
	}

	// Retrieve the freshly captured analysis
	analysis, err = mc.GetLatestAnalysis(ctx, stationID)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve fresh market analysis: %w", err)
	}

	if analysis == nil {
		return nil, fmt.Errorf("market analysis not found after capture")
	}

	return analysis, nil
}

// CaptureMarketAnalysis fetches and stores market analysis data via the market collector.
func CaptureMarketAnalysis(ctx context.Context, client *game.Client, mc *market.Collector, agentID string) error {
	// 1. Request market analysis from game
	if err := client.AnalyzeMarket(ctx); err != nil {
		return fmt.Errorf("failed to analyze market: %w", err)
	}

	// 2. Give server time to respond
	time.Sleep(1 * time.Second)

	// 3. Get the response from client
	rawJSON := client.GetRawJSON("analyze_market")
	if rawJSON == nil {
		return fmt.Errorf("no analyze_market response received")
	}

	// 4. Parse the response into a map to handle dynamic structure
	var response map[string]any
	if err := json.Unmarshal(rawJSON, &response); err != nil {
		return fmt.Errorf("failed to parse analyze_market response: %w", err)
	}

	// 5. Get current state for metadata
	state := client.GetState()

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

	// Helper to safely extract string from response
	getString := func(key string) string {
		if val, ok := response[key].(string); ok {
			return val
		}
		return ""
	}

	// Helper to safely extract int from response
	getInt := func(key string) int {
		if val, ok := response[key].(float64); ok {
			return int(val)
		}
		if val, ok := response[key].(int); ok {
			return val
		}
		return 0
	}

	// Helper to safely extract map from response
	getMap := func(key string) map[string]any {
		if val, ok := response[key].(map[string]any); ok {
			return val
		}
		return nil
	}

	// Helper to safely extract slice from response
	getSlice := func(key string) []map[string]any {
		if val, ok := response[key].([]any); ok {
			result := make([]map[string]any, 0, len(val))
			for _, v := range val {
				if m, ok := v.(map[string]any); ok {
					result = append(result, m)
				}
			}
			return result
		}
		return nil
	}

	// 6. Create analysis record
	analysis := market.MarketAnalysis{
		SystemID:        state.System.ID,
		SystemName:      state.System.Name,
		StationID:       stationID,
		StationName:     stationName,
		GameTick:        state.CurrentTick,
		CapturedAt:      time.Now(),
		AgentID:         agentID,
		Mode:            getString("mode"),
		SkillLevel:      getInt("skill_level"),
		ScanningRange:   getString("scanning_range"),
		StationsInRange: getInt("stations_in_range"),
		ItemsScanned:    getInt("items_scanned"),
		TopInsights:     getSlice("top_insights"),
		TotalItems:      getInt("total_items"),
		TotalPages:      getInt("total_pages"),
		Page:            getInt("page"),
		Hint:            getString("hint"),
		XPGained:        getMap("xp_gained"),
		AnalysisData:    getMap("analysis"),
	}

	// 7. Store via market collector
	if err := mc.StoreAnalysis(ctx, analysis); err != nil {
		return fmt.Errorf("failed to store market analysis: %w", err)
	}

	return nil
}

// isMarketAnalysisFresh checks if market analysis is within the freshness threshold
func isMarketAnalysisFresh(capturedAt time.Time) bool {
	return time.Since(capturedAt) < MarketAnalysisFreshnessThreshold
}
