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
	"github.com/rsned/spacemolt/pkg/worker"
)

// explore visits all POIs in the current system in distance-optimized order,
// running update_poi at each and update_all at stations. Refuels at every
// station dock.
func explore(client game.GameClient, ctx context.Context, format outputFormat) error {
	return exploreSystem(client, ctx, true, format)
}

// exploreSystem runs the explore loop. When refuelAtStations is true, every
// station dock is followed by a refuel command (which uses station credits
// if a refuel service is available, or cargo fuel cells otherwise). Used
// by auto_explore so long exploration runs can replenish opportunistically.
func exploreSystem(client game.GameClient, ctx context.Context, refuelAtStations bool, format outputFormat) error {
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

	// Collect raw responses for non-styled formats
	var allResponses []json.RawMessage

	// Capture the system-wide agent roster (feeds seen_players via the player
	// observer) once per system, right after get_system.
	captureSightings(client, ctx, client.GetSystemAgents, "get_system_agents", format, &allResponses)

	if format == formatStyled {
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
		fmt.Printf("\n  Est. time: ~%s (* = station, will dock for full update)\n\n", worker.FormatDuration(totalTicks*10))
	}

	// Execute the route.

	// Survey system at the start to potentially reveal new POIs for exploration.
	surveySystem(client, ctx, format)
	startTime := time.Now()
	for i, poi := range route {
		// Skip travel to current POI.
		if i == 0 && poi.ID == state.CurrentPOI {
			if format == formatStyled {
				fmt.Printf("[%d/%d] Already at %s\n", i+1, len(route), poi.Name)
			}
		} else {
			if format == formatStyled {
				fmt.Printf("[%d/%d] Traveling to %s (%s)...\n", i+1, len(route), poi.Name, poi.Type)
			}
			result, err := client.Travel(ctx, poi.ID)
			if err != nil {
				if format == formatStyled {
					fmt.Printf("  Travel failed: %v\n", err)
				}
				// Back off a full tick before the next attempt — a fast
				// retry loop here triggers server rate-limit / IP blocking
				// when Travel returns prematurely on a state-flag desync.
				time.Sleep(game.SleepTick)
				continue
			}
			if result.Canceled {
				if format == formatStyled {
					fmt.Printf("  Travel interrupted!\n")
				}
				return fmt.Errorf("explore interrupted at POI %d/%d", i+1, len(route))
			}
			// Collect raw response if in raw/json mode
			if format != formatStyled {
				if raw := client.GetRawJSON("_last"); raw != nil {
					allResponses = append(allResponses, raw)
				}
			}
		}

		// Now at this POI: capture other players here (POI-scoped sighting,
		// feeds seen_players via the player observer).
		captureSightings(client, ctx, client.GetNearby, "get_nearby", format, &allResponses)

		if poi.Type == "station" {
			// Dock and run full update.
			if format == formatStyled {
				fmt.Printf("  Docking at %s...\n", poi.Name)
			}
			if err := client.Dock(ctx); err != nil {
				if format == formatStyled {
					fmt.Printf("  Dock failed: %v (continuing)\n", err)
				}
			} else {
				time.Sleep(game.SleepQuick)
				// Collect dock response
				if format != formatStyled {
					if raw := client.GetRawJSON("_last"); raw != nil {
						allResponses = append(allResponses, raw)
					}
				}
				if globalKB != nil {
					if err := kbUpdateAll(client, ctx); err != nil {
						if format == formatStyled {
							fmt.Printf("  (update_all failed: %v)\n", err)
						}
					}
				}
				if refuelAtStations {
					if format == formatStyled {
						fmt.Printf("  Refueling...\n")
					}
					if err := client.Refuel(ctx); err != nil {
						// Tank-full is an expected "error" here, not worth
						// surfacing. Only print on unexpected failures.
						if !isTankFullError(err) {
							if format == formatStyled {
								fmt.Printf("  Refuel warning: %v\n", err)
							}
						}
					}
					time.Sleep(game.SleepQuick)
					// Collect refuel response
					if format != formatStyled {
						if raw := client.GetRawJSON("_last"); raw != nil {
							allResponses = append(allResponses, raw)
						}
					}
				}
				if format == formatStyled {
					fmt.Printf("  Undocking...\n")
				}
				if err := client.Undock(ctx); err != nil {
					if format == formatStyled {
						fmt.Printf("  Undock failed: %v\n", err)
					}
				}
				time.Sleep(game.SleepTick)
				// Collect undock response
				if format != formatStyled {
					if raw := client.GetRawJSON("_last"); raw != nil {
						allResponses = append(allResponses, raw)
					}
				}
			}
		} else {
			// Non-station: just update POI data.
			if globalKB != nil {
				if err := kbUpdatePOI(client, ctx); err != nil {
					if format == formatStyled {
						fmt.Printf("  (update_poi failed: %v)\n", err)
					}
				}
			}
		}
	}

	// Refresh state for statusline.
	_ = client.GetStatus(ctx)

	elapsed := time.Since(startTime)
	if format == formatStyled {
		fmt.Printf("\nExploration of %s complete: %d POIs in %s\n",
			state.System.Name, len(route), worker.FormatDuration(int(elapsed.Seconds())))
	} else {
		// Print all collected raw responses
		for i, raw := range allResponses {
			if len(allResponses) > 1 {
				fmt.Printf("\n--- Explore response %d ---\n", i+1)
			}
			fmt.Printf("%s\n", string(raw))
		}
	}
	return nil
}

// surveySystem runs survey_system if the ship has a survey scanner module
// installed. It repeats until no more hidden POIs are revealed, captures all
// newly revealed POIs (including full resource data) into the knowledge base,
// enriches each revealed POI via get_poi so reveal_difficulty/hidden flags
// land in SQLite, and prints a summary of aggregate XP gained.
func surveySystem(client game.GameClient, ctx context.Context, format outputFormat) {
	state := client.GetState()

	if !checkForSurveyScanner(state) {
		// State may simply be out of date: it learns the module list only from
		// a get_ship reply, so a scanner fitted during this session is not in
		// it yet. Re-read the ship before refusing — get_ship is a query and
		// costs no tick, and this runs only on the path that was about to
		// refuse anyway.
		if err := client.GetShip(ctx); err == nil {
			state = client.GetState()
		}
	}

	if !checkForSurveyScanner(state) {
		if format == formatStyled {
			fmt.Printf("\nNo survey scanner installed — skipping system survey\n")
		} else {
			fmt.Printf("{\"error\": \"no survey scanner installed\"}\n")
		}
		return
	}

	if format == formatStyled {
		fmt.Printf("\nSurveying system with survey scanner...\n")
	}

	totalXP := make(map[string]int)
	iteration := 0
	var allResponses []json.RawMessage

	for {
		iteration++
		if err := client.SurveySystem(ctx); err != nil {
			if format == formatStyled {
				fmt.Printf("  Survey failed: %v\n", err)
			} else {
				fmt.Printf("{\"error\": \"survey failed: %v\"}\n", err)
			}
			return
		}
		time.Sleep(game.SleepQuick)

		rawJSON := client.GetRawJSON("_last")
		if rawJSON == nil {
			if format == formatStyled {
				fmt.Printf("  Survey complete (no response data)\n")
			} else {
				fmt.Printf("{\"error\": \"no response data from server\"}\n")
			}
			return
		}

		// For raw/json mode, collect responses to print at the end
		if format != formatStyled {
			allResponses = append(allResponses, rawJSON)
		}

		// survey_system now terminates with an action_result frame that nests
		// the payload under "result" ({"command":...,"result":{...},"tick":N}).
		// Unwrap it so the flat SurveySystemResponse fields bind; no-op for the
		// legacy flat OK shape.
		surveyJSON := unwrapActionResult(rawJSON)

		var resp serverapi.SurveySystemResponse
		if err := json.Unmarshal(surveyJSON, &resp); err != nil {
			if format == formatStyled {
				fmt.Printf("  Failed to parse survey response: %v\n", err)
			} else {
				fmt.Printf("{\"error\": \"failed to parse response: %v\", \"raw\": %s}\n", err, string(rawJSON))
			}
			return
		}

		// Accumulate XP across iterations.
		for skill, xp := range resp.XPGained {
			totalXP[skill] += xp
		}

		if format == formatStyled {
			if iteration == 1 {
				fmt.Printf("  Survey power: %d | System: %s\n", resp.SurveyPower, resp.SystemName)
			} else {
				fmt.Printf("  Re-survey #%d (checking for additional hidden POIs)...\n", iteration)
			}

			if len(resp.NewlyRevealed) > 0 {
				fmt.Printf("  Newly revealed POIs:\n")
				for _, poi := range resp.NewlyRevealed {
					fmt.Printf("    + %s (%s)\n", poi.Name, poi.Type)
					if poi.Description != "" {
						fmt.Printf("      %s\n", poi.Description)
					}
					for _, r := range poi.Resources {
						fmt.Printf("      Resource: %s (richness: %.0f, remaining: %.0f/%.0f)\n",
							r.ResourceID, r.Richness, r.Remaining, r.MaxRemaining)
					}
				}
				if globalKB != nil {
					saveSurveyPOIs(client, ctx, resp)
				}
			}

			if len(resp.AlreadyRevealed) > 0 && iteration == 1 {
				fmt.Printf("  Already revealed POIs (%d):\n", len(resp.AlreadyRevealed))
				for _, poi := range resp.AlreadyRevealed {
					fmt.Printf("    = %s (%s)\n", poi.Name, poi.Type)
					if poi.Description != "" {
						fmt.Printf("      %s\n", poi.Description)
					}
					for _, r := range poi.Resources {
						fmt.Printf("      Resource: %s (richness: %.0f, remaining: %.0f/%.0f)\n",
							r.ResourceID, r.Richness, r.Remaining, r.MaxRemaining)
					}
				}
			}

			if len(resp.FaintSignatures) > 0 {
				fmt.Printf("  Faint signatures detected:\n")
				for _, sig := range resp.FaintSignatures {
					hint := sig.Hint
					if hint == "" {
						hint = "unknown"
					}
					fmt.Printf("    ? %s (difficulty: %s, hint: %s)\n", sig.Type, sig.Difficulty, hint)
				}
				if globalKB != nil {
					saveFaintSignatures(client, ctx, resp)
				}
			}

			if resp.Message != "" {
				fmt.Printf("  %s\n", resp.Message)
			}

			if resp.AnomalyHint != "" {
				fmt.Printf("  Anomaly: %s\n", resp.AnomalyHint)
				if globalKB != nil {
					saveSurveyAnomaly(client, ctx, resp)
				}
			}
		} else {
			// In raw/json mode, still save POIs to KB
			if len(resp.NewlyRevealed) > 0 && globalKB != nil {
				saveSurveyPOIs(client, ctx, resp)
			}
			if len(resp.FaintSignatures) > 0 && globalKB != nil {
				saveFaintSignatures(client, ctx, resp)
			}
			if resp.AnomalyHint != "" && globalKB != nil {
				saveSurveyAnomaly(client, ctx, resp)
			}
		}

		// Stop if nothing new was revealed this pass.
		if len(resp.NewlyRevealed) == 0 {
			break
		}
		// Safety cap — shouldn't be needed, but avoid a runaway loop.
		if iteration >= 10 {
			if format == formatStyled {
				fmt.Printf("  (hit survey iteration cap of 10, stopping)\n")
			}
			break
		}
	}

	if format == formatStyled {
		if len(totalXP) > 0 {
			var xpParts []string
			for skill, xp := range totalXP {
				if xp > 0 {
					xpParts = append(xpParts, fmt.Sprintf("%s +%d", skill, xp))
				}
			}
			if len(xpParts) > 0 {
				fmt.Printf("  Total XP gained from survey: %s\n", strings.Join(xpParts, ", "))
			}
		}
	} else {
		// For raw/json mode, print all collected responses
		for i, rawResp := range allResponses {
			if len(allResponses) > 1 {
				fmt.Printf("\n--- Survey iteration %d ---\n", i+1)
			}
			fmt.Printf("%s\n", string(rawResp))
		}
	}
}

// saveSurveyPOIs saves newly revealed POIs from a survey to the knowledge
// base, preserving full resource data and explicitly marking them as revealed.
// reveal_difficulty is not included in the survey response; it will be picked
// up later when the agent visits the POI and runs update_poi.
func saveSurveyPOIs(client game.GameClient, ctx context.Context, resp serverapi.SurveySystemResponse) {
	state := client.GetState()
	for _, revealed := range resp.NewlyRevealed {
		kbPOI := knowledge.POI{
			ID:          revealed.ID,
			SystemID:    resp.SystemID,
			Name:        revealed.Name,
			Type:        revealed.Type,
			Description: revealed.Description,
			// The POI was just revealed by this survey — it is no longer hidden.
			Hidden:          false,
			LastUpdatedTick: currentTick(state),
			DetectedBy:      globalAgentID,
		}
		for _, r := range revealed.Resources {
			kbPOI.Resources = append(kbPOI.Resources, game.POIResource{
				ResourceID: r.ResourceID,
				Richness:   r.Richness,
				Remaining:  r.Remaining,
			})
		}
		if err := globalKB.RememberPOI(ctx, kbPOI); err != nil {
			fmt.Printf("    Warning: failed to save revealed POI %s: %v\n", revealed.Name, err)
		}
	}
}

// saveFaintSignatures saves faint (unresolved) survey signatures as placeholder
// POI records in the knowledge base so they can be investigated later with better
// equipment or higher skills.
func saveFaintSignatures(client game.GameClient, ctx context.Context, resp serverapi.SurveySystemResponse) {
	state := client.GetState()
	for i, sig := range resp.FaintSignatures {
		// Generate a deterministic placeholder ID from system + signature index + type
		placeholderID := fmt.Sprintf("faint_%s_%s_%d", resp.SystemID, sig.Type, i)
		name := "Faint Signature"
		if sig.Hint != "" {
			name = fmt.Sprintf("Faint Signature: %s", sig.Hint)
		}
		desc := fmt.Sprintf("Unresolved survey signature (type: %s, difficulty: %s). Requires better scanner or higher skills to identify.", sig.Type, sig.Difficulty)

		kbPOI := knowledge.POI{
			ID:              placeholderID,
			SystemID:        resp.SystemID,
			Name:            name,
			Type:            "faint_signature",
			Description:     desc,
			Hidden:          false,
			LastUpdatedTick: currentTick(state),
			DetectedBy:      globalAgentID,
		}
		if err := globalKB.RememberPOI(ctx, kbPOI); err != nil {
			fmt.Printf("    Warning: failed to save faint signature: %v\n", err)
		}
	}
}

// captureSightings runs a best-effort player-enumeration query (get_nearby /
// get_system_agents) whose response feeds the player observer, recording the
// agents into seen_players. Errors are non-fatal (sighting capture is not
// required for exploration); in raw/JSON mode the response is appended to
// responses so it still appears in the output stream.
func captureSightings(client game.GameClient, ctx context.Context, fn func(context.Context) error, label string, format outputFormat, responses *[]json.RawMessage) {
	if err := fn(ctx); err != nil {
		if format == formatStyled {
			fmt.Printf("  (%s failed: %v)\n", label, err)
		}
		return
	}
	if format != formatStyled && responses != nil {
		if raw := client.GetRawJSON("_last"); raw != nil {
			*responses = append(*responses, raw)
		}
	}
	time.Sleep(game.SleepQuick)
}

// saveSurveyAnomaly persists a survey spatial-anomaly hint to the knowledge
// base so it can be reviewed after the session. Directional hints ("toward X
// (N jumps)") are parsed so callers can later tell which need explicit travel.
// Repeat detections of the same anomaly are deduped by CaptureSurveyAnomaly.
func saveSurveyAnomaly(client game.GameClient, ctx context.Context, resp serverapi.SurveySystemResponse) {
	state := client.GetState()
	if _, err := knowledge.CaptureSurveyAnomaly(ctx, globalKB, resp.AnomalyHint, resp.SystemID, globalAgentID, currentTick(state)); err != nil {
		fmt.Printf("    Warning: failed to record survey anomaly: %v\n", err)
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
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return string(runes[:maxLen])
	}
	return string(runes[:maxLen-3]) + "..."
}

// checkForSurveyScanner checks if the ship can survey systems, either via an
// installed survey scanner module or a survey scanner built into the ship hull
// (an "integrated_survey_scanner" inherent capability, e.g. the survey_vessel).
//
// Only a POSITIVE result is cached. A negative is deliberately not: the module
// list in state (Ship.Modules plus the ModuleDefinitions that map those instance
// ids to type ids) is populated exclusively by a get_ship reply, and
// install_mod's reply carries nothing but module_id/cpu_used/power_used. A
// scanner fitted mid-session is therefore invisible here until the ship is
// re-read, and caching the "no" made that refusal last the whole session.
// Recomputing a negative is a walk over a handful of map entries, so caching it
// bought nothing in the first place.
func checkForSurveyScanner(state *game.State) bool {
	if surveyScannerCached {
		return hasSurveyScanner
	}
	if state == nil {
		return false
	}

	// Not cached - check installed modules
	surveyScanners := []string{
		"survey_scanner_i",
		"survey_scanner_ii",
		"deep_core_survey_scanner",
	}
	for _, scanner := range surveyScanners {
		if game.HasModuleType(state, scanner) {
			surveyScannerCached = true
			hasSurveyScanner = true
			return true
		}
	}

	// Fall back to the ship hull's inherent capabilities (built-in scanners are
	// not listed in state.Ship.Modules), looked up from the ship class catalog.
	if shipClassHasIntegratedSurveyScanner(state.Ship.ClassID) {
		surveyScannerCached = true
		hasSurveyScanner = true
		return true
	}

	return false
}

// shipClassHasIntegratedSurveyScanner reports whether the given ship class has a
// built-in survey scanner, per its inherent_capabilities in the ship catalog.
func shipClassHasIntegratedSurveyScanner(classID string) bool {
	if globalKB == nil || classID == "" {
		return false
	}
	sc, err := globalKB.GetShipClass(context.Background(), classID)
	if err != nil || sc == nil {
		return false
	}
	for _, capability := range sc.InherentCapabilities {
		if capability.Type == "integrated_survey_scanner" {
			return true
		}
	}
	return false
}

// invalidateSurveyScannerCache clears the cached scanner check.
// Call this when the ship might have changed.
func invalidateSurveyScannerCache() {
	surveyScannerCached = false
	hasSurveyScanner = false
}
