package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// explore visits all POIs in the current system in distance-optimized order,
// running update_poi at each and update_all at stations. Equivalent to
// exploreSystem(client, ctx, false) — i.e., no refuel on station docking.
func explore(client game.GameClient, ctx context.Context) error {
	return exploreSystem(client, ctx, false)
}

// exploreSystem runs the explore loop. When refuelAtStations is true, every
// station dock is followed by a refuel command (which uses station credits
// if a refuel service is available, or cargo fuel cells otherwise). Used
// by auto_explore so long exploration runs can replenish opportunistically.
func exploreSystem(client game.GameClient, ctx context.Context, refuelAtStations bool) error {
	if err := client.GetSystem(ctx); err != nil {
		return fmt.Errorf("get_system failed: %w", err)
	}
	time.Sleep(game.SleepQuick)

	state := client.GetState()
	if state.System.ID == "" {
		return fmt.Errorf("no system data available")
	}

	pois := state.System.POIs
	if len(pois) == 0 {
		return fmt.Errorf("no POIs in system %s", state.System.Name)
	}

	// Plan route starting from current POI.
	route := planExploreRoute(pois, state.CurrentPOI)

	// Get ship speed for tick estimates.
	speed := state.Ship.Speed
	if speed <= 0 {
		speed = 1
	}

	// Print route table.
	fmt.Printf("\nExploring %d POIs in %s:\n\n", len(route), state.System.Name)
	fmt.Printf("  %-3s %-28s %-16s %10s %10s\n", "#", "POI", "Type", "Dist (AU)", "Est. Ticks")
	fmt.Printf("  %s\n", strings.Repeat("-", 71))

	var totalDist float64
	var totalTicks int
	prevPos := currentPOIPosition(pois, state.CurrentPOI)
	for i, poi := range route {
		dist := poiDistance(prevPos, poi.Position)
		ticks := max(int(math.Ceil(dist/speed)), 1)
		if i == 0 && poi.ID == state.CurrentPOI {
			dist = 0
			ticks = 0
		}
		totalDist += dist
		totalTicks += ticks

		marker := ""
		if poi.Type == "station" {
			marker = " *"
		}
		fmt.Printf("  %-3d %-28s %-16s %9.1f %9d%s\n",
			i+1, truncateName(poi.Name, 28), poi.Type, dist, ticks, marker)
		prevPos = poi.Position
	}
	fmt.Printf("  %s\n", strings.Repeat("-", 71))
	fmt.Printf("  %-48s %9.1f %9d\n", "Total", totalDist, totalTicks)
	fmt.Printf("\n  Est. time: ~%s (* = station, will dock for full update)\n\n", formatDuration(totalTicks*10))

	// Execute the route.

	// Survey system at the start to potentially reveal new POIs for exploration.
	surveySystem(client, ctx)
	startTime := time.Now()
	for i, poi := range route {
		// Skip travel to current POI.
		if i == 0 && poi.ID == state.CurrentPOI {
			fmt.Printf("[%d/%d] Already at %s\n", i+1, len(route), poi.Name)
		} else {
			fmt.Printf("[%d/%d] Traveling to %s (%s)...\n", i+1, len(route), poi.Name, poi.Type)
			result, err := client.Travel(ctx, poi.ID)
			if err != nil {
				fmt.Printf("  Travel failed: %v\n", err)
				continue
			}
			if result.Canceled {
				fmt.Printf("  Travel interrupted!\n")
				return fmt.Errorf("explore interrupted at POI %d/%d", i+1, len(route))
			}
		}

		if poi.Type == "station" {
			// Dock and run full update.
			fmt.Printf("  Docking at %s...\n", poi.Name)
			if err := client.Dock(ctx); err != nil {
				fmt.Printf("  Dock failed: %v (continuing)\n", err)
			} else {
				time.Sleep(game.SleepQuick)
				if globalKB != nil {
					if err := kbUpdateAll(client, ctx); err != nil {
						fmt.Printf("  (update_all failed: %v)\n", err)
					}
				}
				if refuelAtStations {
					fmt.Printf("  Refueling...\n")
					if err := client.Refuel(ctx); err != nil {
						// Tank-full is an expected "error" here, not worth
						// surfacing. Only print on unexpected failures.
						if !strings.Contains(err.Error(), "tank_full") {
							fmt.Printf("  Refuel warning: %v\n", err)
						}
					}
					time.Sleep(game.SleepQuick)
				}
				fmt.Printf("  Undocking...\n")
				if err := client.Undock(ctx); err != nil {
					fmt.Printf("  Undock failed: %v\n", err)
				}
				time.Sleep(game.SleepTick)
			}
		} else {
			// Non-station: just update POI data.
			if globalKB != nil {
				if err := kbUpdatePOI(client, ctx); err != nil {
					fmt.Printf("  (update_poi failed: %v)\n", err)
				}
			}
		}
	}

	// Refresh state for statusline.
	_ = client.GetStatus(ctx)

	elapsed := time.Since(startTime)
	fmt.Printf("\nExploration of %s complete: %d POIs in %s\n",
		state.System.Name, len(route), formatDuration(int(elapsed.Seconds())))
	return nil
}

// Any newly revealed POIs are saved to the knowledge base.
// surveySystem runs survey_system if the ship has a survey scanner module installed.
// Any newly revealed POIs are saved to the knowledge base.
// surveySystem runs survey_system if the ship has a survey scanner module installed.
// Any newly revealed POIs are saved to the knowledge base.
func surveySystem(client game.GameClient, ctx context.Context) {
	state := client.GetState()

	if !checkForSurveyScanner(state) {
		fmt.Printf("\nNo survey scanner installed — skipping system survey\n")
		return
	}

	fmt.Printf("\nSurveying system with survey scanner...\n")
	if err := client.SurveySystem(ctx); err != nil {
		fmt.Printf("  Survey failed: %v\n", err)
		return
	}
	time.Sleep(game.SleepQuick)

	rawJSON := client.GetRawJSON("_last")
	if rawJSON == nil {
		fmt.Printf("  Survey complete (no response data)\n")
		return
	}

	var resp serverapi.SurveySystemResponse
	if err := json.Unmarshal(rawJSON, &resp); err != nil {
		fmt.Printf("  Failed to parse survey response: %v\n", err)
		return
	}

	fmt.Printf("  Survey power: %d | System: %s\n", resp.SurveyPower, resp.SystemName)

	if len(resp.NewlyRevealed) > 0 {
		fmt.Printf("  Newly revealed POIs:\n")
		for _, poi := range resp.NewlyRevealed {
			fmt.Printf("    + %s (%s)\n", poi.Name, poi.Type)
			if poi.Description != "" {
				fmt.Printf("      %s\n", poi.Description)
			}
			for _, r := range poi.Resources {
				fmt.Printf("      Resource: %s (richness: %s)\n", r.ResourceID, r.Richness)
			}
		}
		// Save newly revealed POIs to the knowledge base.
		if globalKB != nil {
			saveSurveyPOIs(client, ctx, resp)
		}
	}

	if len(resp.AlreadyRevealed) > 0 {
		fmt.Printf("  Already revealed: %d POIs\n", len(resp.AlreadyRevealed))
	}

	if len(resp.FaintSignatures) > 0 {
		fmt.Printf("  Faint signatures detected:\n")
		for _, sig := range resp.FaintSignatures {
			hint := sig.Hint
			if hint == "" {
				hint = "unknown"
			}
			fmt.Printf("    ? %s (difficulty: %d, hint: %s)\n", sig.Type, sig.Difficulty, hint)
		}
	}

	if resp.XPGained != nil {
		var xpParts []string
		for skill, xp := range resp.XPGained {
			if xp > 0 {
				xpParts = append(xpParts, fmt.Sprintf("%s +%d", skill, xp))
			}
		}
		if len(xpParts) > 0 {
			fmt.Printf("  XP: %s\n", strings.Join(xpParts, ", "))
		}
	}

	if resp.Message != "" {
		fmt.Printf("  %s\n", resp.Message)
	}
}

// saveSurveyPOIs saves newly revealed POIs from a survey to the knowledge base.
func saveSurveyPOIs(client game.GameClient, ctx context.Context, resp serverapi.SurveySystemResponse) {
	state := client.GetState()
	for _, revealed := range resp.NewlyRevealed {
		kbPOI := knowledge.POI{
			ID:              revealed.ID,
			SystemID:        resp.SystemID,
			Name:            revealed.Name,
			Type:            revealed.Type,
			Description:     revealed.Description,
			LastUpdatedTick: currentTick(state),
		}
		for _, r := range revealed.Resources {
			kbPOI.Resources = append(kbPOI.Resources, game.POIResource{
				ResourceID: r.ResourceID,
			})
		}
		if err := globalKB.RememberPOI(ctx, kbPOI); err != nil {
			fmt.Printf("    Warning: failed to save revealed POI %s: %v\n", revealed.Name, err)
		}
	}
}

// planExploreRoute orders POIs using nearest-neighbor heuristic starting from startPOI.
// If startPOI is in the list, it becomes the first entry. Otherwise, the nearest POI is first.
func planExploreRoute(pois []game.POI, startPOI string) []game.POI {
	if len(pois) <= 1 {
		result := make([]game.POI, len(pois))
		copy(result, pois)
		return result
	}

	// Build index and find start.
	remaining := make(map[int]bool, len(pois))
	for i := range pois {
		remaining[i] = true
	}

	// Find starting POI index.
	startIdx := -1
	for i, poi := range pois {
		if poi.ID == startPOI {
			startIdx = i
			break
		}
	}

	var route []game.POI
	var curPos game.Position

	if startIdx >= 0 {
		route = append(route, pois[startIdx])
		curPos = pois[startIdx].Position
		delete(remaining, startIdx)
	} else if len(pois) > 0 {
		// No matching start POI — use first POI as fallback.
		route = append(route, pois[0])
		curPos = pois[0].Position
		delete(remaining, 0)
	}

	// Nearest-neighbor greedy.
	for len(remaining) > 0 {
		bestIdx := -1
		bestDist := math.MaxFloat64

		for idx := range remaining {
			d := poiDistance(curPos, pois[idx].Position)
			if d < bestDist {
				bestDist = d
				bestIdx = idx
			}
		}

		route = append(route, pois[bestIdx])
		curPos = pois[bestIdx].Position
		delete(remaining, bestIdx)
	}

	return route
}

// poiDistance returns the Euclidean distance between two positions.
func poiDistance(a, b game.Position) float64 {
	dx := b.X - a.X
	dy := b.Y - a.Y
	return math.Sqrt(dx*dx + dy*dy)
}

// currentPOIPosition finds the position of a POI by ID in a list.
func currentPOIPosition(pois []game.POI, poiID string) game.Position {
	for _, poi := range pois {
		if poi.ID == poiID {
			return poi.Position
		}
	}
	if len(pois) > 0 {
		return pois[0].Position
	}
	return game.Position{}
}

// truncateName truncates a string to maxLen, adding "..." if needed.
func truncateName(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// checkForSurveyScanner checks if the ship has a survey scanner installed.
// Results are cached until the ship might change (switch_ship, install_mod, etc.).
// checkForSurveyScanner checks if the ship has a survey scanner installed.
// Results are cached until the ship might change (switch_ship, install_mod, etc.).
func checkForSurveyScanner(state *game.State) bool {
	if surveyScannerCached {
		return hasSurveyScanner
	}

	// Not cached - check modules
	surveyScanners := []string{
		"survey_scanner_i",
		"survey_scanner_ii",
		"deep_core_survey_scanner",
	}
	for _, scanner := range surveyScanners {
		if game.HasModuleType(state, scanner) {
			hasSurveyScanner = true
			break
		}
	}

	surveyScannerCached = true
	return hasSurveyScanner
}

// invalidateSurveyScannerCache clears the cached scanner check.
// Call this when the ship might have changed.
func invalidateSurveyScannerCache() {
	surveyScannerCached = false
	hasSurveyScanner = false
}
