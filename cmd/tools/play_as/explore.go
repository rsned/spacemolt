package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"slices"
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
	return exploreSystem(client, ctx, true, false, format)
}

// exploreSystem runs the explore loop. When refuelAtStations is true, every
// station dock is followed by a refuel command (which uses station credits
// if a refuel service is available, or cargo fuel cells otherwise). Used
// by auto_explore so long exploration runs can replenish opportunistically.
func exploreSystem(client game.GameClient, ctx context.Context, refuelAtStations, stopOnUnscanned bool, format outputFormat) error {
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
		// The same reply lists any wildlife here. This is the only headcount
		// that names a POI — survey_system's census is system-wide — so it is
		// what ties a species to a habitat.
		unscanned := captureWildlifeAtPOI(client, ctx, poi.ID, poi.Type, format)
		// Halt BEFORE the POI's dock/update work: creatures drift or despawn
		// (ashford went 0→3 creatures in 101s), so the moment of sighting is
		// the moment to hand control back for a scan.
		if stopOnUnscanned && len(unscanned) > 0 {
			return &unscannedHalt{Species: unscanned, POIID: poi.ID, POIName: poi.Name}
		}

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

		// Second wildlife reading, now that the POI's work has put real time
		// between the two samples. See captureWildlifeSecondLook.
		unscanned = captureWildlifeSecondLook(client, ctx, poi.ID, poi.Type, format)
		if stopOnUnscanned && len(unscanned) > 0 {
			return &unscannedHalt{Species: unscanned, POIID: poi.ID, POIName: poi.Name}
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

		// The census rides along on every survey and costs nothing extra, so
		// capture it outside the format branch, like the revealed POIs below.
		// The game ships no wildlife catalog, so this and get_nearby are the
		// whole field guide.
		if n, err := knowledge.CaptureWildlifeSurvey(ctx, globalKB, resp, globalAgentID, currentTick(client.GetState())); err != nil {
			if format == formatStyled {
				fmt.Printf("  (wildlife census not saved: %v)\n", err)
			}
		} else if n > 0 && format == formatStyled {
			fmt.Printf("  Wildlife census: %d species", n)
			if resp.BloomStatus != "" {
				fmt.Printf(" | bloom %s (%.2f)", resp.BloomStatus, resp.BloomIntensity)
			}
			fmt.Println()
		}

		// The census is a sighting source in its own right, so it gets the
		// unscanned check too -- see reportUnscannedCensus. It runs
		// unconditionally, not inside the n > 0 branch above, because a
		// capture error must not also cost the operator the notice.
		reportUnscannedCensus(ctx, resp, format)

		// Revealed POIs, both lists, in every output format. Capture is not a
		// property of how the caller wants the result printed.
		if globalKB != nil {
			saveSurveyPOIs(client, ctx, resp)
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

// saveSurveyPOIs saves every POI a survey revealed -- newly and already -- to
// base, preserving full resource data and explicitly marking them as revealed.
// reveal_difficulty is not included in the survey response; it will be picked
// up later when the agent visits the POI and runs update_poi.
func saveSurveyPOIs(client game.GameClient, ctx context.Context, resp serverapi.SurveySystemResponse) {
	for _, kbPOI := range surveyPOIsToKB(resp, globalAgentID, currentTick(client.GetState())) {
		if err := globalKB.RememberPOI(ctx, kbPOI); err != nil {
			fmt.Printf("    Warning: failed to save revealed POI %s: %v\n", kbPOI.Name, err)
		}
	}
}

// surveyPOIsToKB converts a survey's revealed POIs into knowledge rows.
//
// BOTH lists are captured. A hidden POI is newly revealed exactly once, ever;
// every survey after that reports it under already_revealed, which this used to
// ignore -- so its resources could only be refreshed by physically flying to
// it. Live 2026-08-28: prismatic_gas_pocket's resource row sat at tick
// 1,020,715 while the POI row read 1,736,298, and the survey reply carrying the
// current numbers was thrown away.
//
// Hidden is TRUE for both lists. It is intrinsic to the POI, not "not yet
// revealed to you" -- get_poi reports hidden:true for prismatic_gas_pocket, a
// POI already revealed to us. This code used to hardcode false with the comment
// "it is no longer hidden", writing false over the correct value; because the
// upsert is tick-guarded, the newer survey write then clobbered it. SurveyedPOI
// carries no hidden field, but needing a survey to see it is exactly what
// hidden means.
func surveyPOIsToKB(resp serverapi.SurveySystemResponse, agentID string, tick int64) []knowledge.POI {
	revealed := make([]serverapi.RevealedPOI, 0, len(resp.NewlyRevealed)+len(resp.AlreadyRevealed))
	revealed = append(revealed, resp.NewlyRevealed...)
	revealed = append(revealed, resp.AlreadyRevealed...)

	out := make([]knowledge.POI, 0, len(revealed))
	for _, r := range revealed {
		kbPOI := knowledge.POI{
			ID:              r.ID,
			SystemID:        resp.SystemID,
			Name:            r.Name,
			Type:            r.Type,
			Description:     r.Description,
			Hidden:          true,
			LastUpdatedTick: tick,
			DetectedBy:      agentID,
		}
		for _, res := range r.Resources {
			kbPOI.Resources = append(kbPOI.Resources, game.POIResource{
				ResourceID:   res.ResourceID,
				Richness:     res.Richness,
				Remaining:    res.Remaining,
				MaxRemaining: res.MaxRemaining,
			})
		}
		out = append(out, kbPOI)
	}
	return out
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

// captureWildlifeSecondLook takes a second wildlife reading at a POI the pass
// is about to leave.
//
// A get_nearby creature list is a snapshot of a moment, not a census of the
// POI. Live 2026-08-28: ashford_ice_shelf reported 0 creatures at 12:41:35 and
// 3 at 12:43:16, and prismatic_gas_pocket did the same -- creatures move or
// spawn faster than a POI visit takes. One look per POI therefore records a
// false negative as fact, which is worse than no reading at all.
//
// It is placed at the END of the POI's work rather than beside the first look,
// so the dock/update in between (get_location plus get_poi) supplies several
// seconds of real separation. Sampling the same instant twice would learn
// nothing, and an artificial sleep would cost the pass wall-clock for no gain.
//
// get_nearby is a query and costs no tick. It is still a call, so this doubles
// the per-POI nearby volume -- acceptable at operator-session scale, and worth
// re-checking against the coverage rows before any fleet role adopts it.
func captureWildlifeSecondLook(client game.GameClient, ctx context.Context, poiID, poiType string, format outputFormat) []string {
	if err := client.GetNearby(ctx); err != nil {
		if format == formatStyled {
			fmt.Printf("  (second wildlife look failed: %v)\n", err)
		}
		return nil
	}
	time.Sleep(game.SleepQuick)
	return captureWildlifeAtPOI(client, ctx, poiID, poiType, format)
}

// captureWildlifeAtPOI records the creatures listed in the get_nearby reply
// already sitting in the client's raw cache. It issues no command of its own —
// the caller has just run get_nearby for player sightings, and creatures come
// back in the same payload for free.
//
// poiType becomes the species' habitat. It is passed in rather than looked up
// because the explore loop already holds the POI it travelled to, and the KB may
// not know a belt that a survey only just revealed.
func captureWildlifeAtPOI(client game.GameClient, ctx context.Context, poiID, poiType string, format outputFormat) []string {
	raw := client.GetRawJSON("nearby")
	if len(raw) == 0 {
		return nil
	}
	var nearby serverapi.GetNearbyResponse
	if err := json.Unmarshal(raw, &nearby); err != nil {
		return nil
	}
	// NO early return on an empty creature list. CaptureWildlifeNearby records
	// its coverage row first and unconditionally, precisely so a look that
	// found nothing still leaves a trace -- returning here skipped that and
	// reintroduced the failure its comment warns about, a fully surveyed system
	// reading as half unvisited.
	//
	// Wildlife is transient: ashford_ice_shelf reported 0 creatures at 12:41:35
	// and 3 at 12:43:16 on 2026-08-28, 101 seconds apart. So an empty reading
	// is real data about a moment, not an absence of habitat, and only the
	// coverage row can tell "looked, found none" from "never looked".
	state := client.GetState()
	systemID := ""
	if state != nil {
		systemID = state.System.ID
	}
	// The reply names its own POI; prefer it over the caller's idea of where we
	// are, which can be a tick stale after an interrupted travel.
	if nearby.POIID != "" {
		poiID = nearby.POIID
	}

	n, err := knowledge.CaptureWildlifeNearby(ctx, globalKB, nearby.Creatures,
		systemID, poiID, poiType, globalAgentID, currentTick(state))
	if err != nil {
		if format == formatStyled {
			fmt.Printf("  (wildlife not saved: %v)\n", err)
		}
		return nil
	}
	if n > 0 && format == formatStyled {
		fmt.Printf("  Wildlife: %d creature(s), %d species\n", len(nearby.Creatures), n)
	}
	reportUnscannedSpecies(ctx, nearby.Creatures, poiID, format)

	return detectUnscanned(ctx, globalKB, nearby.Creatures)
}

// reportUnscannedSpecies flags species present here that no scan has ever
// characterised, so the operator can decide whether to spend the tick.
//
// It deliberately does not scan. scan is a mutation costing a tick per
// creature, and the thing worth knowing -- the danger bracket -- is exactly
// what you do not have before scanning: a Rainbow Leviathan does 130
// energy/tick and kills a starter hull in two. Automating that would spend
// ticks to walk into fights the operator never chose. Naming the species and
// the POI puts the decision where the judgement is.
//
// Reporting is best-effort: a KB that cannot answer produces silence, never an
// error, since this runs inside an exploration loop that must not break.
func reportUnscannedSpecies(ctx context.Context, creatures []serverapi.NearbyCreature, poiID string, format outputFormat) {
	cands := make([]unscannedCandidate, 0, len(creatures))
	for _, c := range creatures {
		cands = append(cands, unscannedCandidate{Species: c.Species, Name: c.Name, Role: c.Role})
	}
	reportUnscanned(ctx, cands, "at POI "+poiID, format)
}

// reportUnscannedCensus flags unscanned species named by a survey_system
// census.
//
// The census was the ONLY sighting of two species for a full exploration pass,
// and both slipped through silently because nothing ran this check over it:
// pall_jelly and tempest_eel, seen in redmarsh 2026-08-29T01:13:37Z. That is
// the gap biting in the worst direction. Of the ten species awaiting a scan,
// seven are grazers of 35-85 hull; the three that are not are a 400-hull
// predator, and those two -- a predator and a scavenger whose hull the census
// does not report at all. The notice exists so the operator can judge whether a
// scan is safe, and it stayed quiet on exactly the two where safety was least
// knowable.
//
// A census names no POI -- it is system-wide -- so the notice says the system
// and admits it cannot say where. That is still actionable, and for predators
// it is arguably the RIGHT granularity: predators come and go from the herds
// (observed in Goldcrest), so which POI holds one is a fact with a short shelf
// life, while "this system has an uncharacterised predator in it" stays true.
// A get_nearby that found only grazers is therefore not evidence the POI is
// safe -- it is evidence about one moment, at one POI, in a system the census
// may already have told us holds something worse.
func reportUnscannedCensus(ctx context.Context, resp serverapi.SurveySystemResponse, format outputFormat) {
	cands := make([]unscannedCandidate, 0, len(resp.Wildlife))
	for _, w := range resp.Wildlife {
		cands = append(cands, unscannedCandidate{Species: w.Species, Name: w.Name, Role: w.Role})
	}
	where := "somewhere in " + resp.SystemID + " (census: no POI)"
	reportUnscanned(ctx, cands, where, format)
}

// unscannedCandidate is one species a reply mentioned, in the fields the notice
// needs. It exists so the get_nearby and survey_system paths -- whose replies
// agree on species/name/role and on nothing else -- can share one report.
type unscannedCandidate struct {
	Species string
	Name    string
	Role    string
}

// reportUnscanned is the shared body: it dedupes, asks the KB which species
// have never been characterised, and prints one line each.
//
// Role is included in the line because the whole point of the notice is a
// safety judgement, and role is the only danger signal available BEFORE the
// scan that would reveal the danger bracket. "grazer" and "predator" are
// different decisions.
func reportUnscanned(ctx context.Context, cands []unscannedCandidate, where string, format outputFormat) {
	if format != formatStyled || globalKB == nil || len(cands) == 0 {
		return
	}
	seen := make(map[string]unscannedCandidate, len(cands))
	ids := make([]string, 0, len(cands))
	for _, c := range cands {
		if c.Species == "" {
			continue
		}
		if _, dup := seen[c.Species]; dup {
			continue
		}
		seen[c.Species] = c
		ids = append(ids, c.Species)
	}
	unscanned, err := knowledge.UnscannedSpecies(ctx, globalKB, ids)
	if err != nil || len(unscanned) == 0 {
		return
	}
	slices.Sort(unscanned)
	for _, sp := range unscanned {
		c := seen[sp]
		label := sp
		switch {
		case c.Name != "" && c.Name != sp && c.Role != "":
			label = fmt.Sprintf("%s (%s, %s)", sp, c.Name, c.Role)
		case c.Name != "" && c.Name != sp:
			label = fmt.Sprintf("%s (%s)", sp, c.Name)
		case c.Role != "":
			label = fmt.Sprintf("%s (%s)", sp, c.Role)
		}
		fmt.Printf("  ** NEW UNSCANNED CREATURE CLASS %s %s -- `scan <creature_id>` to characterise\n", label, where)
	}
}

// captureCreatureScan files what a scan revealed about a creature's species.
//
// It runs only for crt_ targets, and needs the species id — which the scan reply
// does NOT carry, since it packs a display name into username and nothing else.
// The species comes from the cached get_nearby reply for the same creature id,
// which is how the herd was found in the first place; without that entry the
// scan is left unrecorded rather than filed under a species guessed from the
// display name.
func captureCreatureScan(client game.GameClient, ctx context.Context, targetID string, format outputFormat) {
	if !strings.HasPrefix(targetID, "crt_") {
		return
	}
	raw := client.GetRawJSON("_last")
	if len(raw) == 0 {
		return
	}
	var resp serverapi.ScanResponse
	if err := json.Unmarshal(unwrapActionResult(raw), &resp); err != nil {
		return
	}
	if !resp.Success {
		return
	}

	species := speciesForCreature(client, targetID)
	if species == "" {
		if format == formatStyled {
			fmt.Printf("  (scan not recorded: run get_nearby first so %s can be tied to a species)\n", targetID)
		}

		return
	}

	s := knowledge.ParseCreatureScan(resp.Username, resp.RevealedInfo, resp.Hull, resp.Description)
	if err := knowledge.CaptureWildlifeScan(ctx, globalKB, species, s, time.Now()); err != nil {
		if format == formatStyled {
			fmt.Printf("  (scan not recorded: %v)\n", err)
		}

		return
	}
	if format == formatStyled && s.Traits != "" {
		ranch := ""
		if s.Ranchable {
			ranch = " | ranchable stock"
		}
		fmt.Printf("  Field guide: %s = %q%s\n", species, s.Traits, ranch)
		if s.Description != "" {
			fmt.Printf("  Lore: %s\n", s.Description)
		}
	}
}

// speciesForCreature resolves a crt_ id to its species using the cached
// get_nearby reply. Returns "" when the creature is not in it.
func speciesForCreature(client game.GameClient, creatureID string) string {
	raw := client.GetRawJSON("nearby")
	if len(raw) == 0 {
		return ""
	}
	var nearby serverapi.GetNearbyResponse
	if err := json.Unmarshal(raw, &nearby); err != nil {
		return ""
	}
	for _, c := range nearby.Creatures {
		if c.CreatureID == creatureID {
			return c.Species
		}
	}

	return ""
}

// poiTypeFromState resolves a POI id to its type from the loaded system, which
// becomes the species' habitat (belt, gas_cloud, cryobelt, nebula...). Returns
// "" when the POI is not in state, so an unknown habitat is recorded as absent
// rather than guessed.
func poiTypeFromState(state *game.State, poiID string) string {
	if state == nil || poiID == "" {
		return ""
	}
	for _, p := range state.System.POIs {
		if p.ID == poiID {
			return p.Type
		}
	}

	return ""
}

// captureWildlifeFromSurveyReply files the wildlife census out of whatever
// survey reply is sitting in the client's raw cache. It exists so the bare
// `survey` command captures as much as `survey_system` does: the census rides on
// both, and which alias an operator typed should not decide whether the
// observation is kept.
func captureWildlifeFromSurveyReply(client game.GameClient, ctx context.Context, format outputFormat) {
	raw := client.GetRawJSON("_last")
	if len(raw) == 0 {
		return
	}
	var resp serverapi.SurveySystemResponse
	if err := json.Unmarshal(unwrapActionResult(raw), &resp); err != nil {
		return
	}
	n, err := knowledge.CaptureWildlifeSurvey(ctx, globalKB, resp, globalAgentID, currentTick(client.GetState()))
	if err != nil {
		if format == formatStyled {
			fmt.Printf("  (wildlife census not saved: %v)\n", err)
		}

		return
	}
	if n > 0 && format == formatStyled {
		fmt.Printf("  Wildlife census: %d species", n)
		if resp.BloomStatus != "" {
			fmt.Printf(" | bloom %s (%.2f)", resp.BloomStatus, resp.BloomIntensity)
		}
		fmt.Println()
	}
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
