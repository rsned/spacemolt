package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/explore"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// The per-system explore itself lives in pkg/explore, shared with the worker.
// This file adapts it to play_as: the output formats, the session's KB and
// clock, and the play_as-only captures (intel files, the wildlife growth log).

// exploreReporter renders an explore's progress the way play_as prints it:
// the human lines in styled mode, the raw replies otherwise.
type exploreReporter struct {
	format outputFormat
	raws   map[string][]json.RawMessage
}

func (r *exploreReporter) Printf(format string, args ...any) {
	if r.format == formatStyled {
		fmt.Printf(format, args...)
	}
}

func (r *exploreReporter) Raw(label string, payload []byte) {
	if r.format != formatStyled {
		r.raws[label] = append(r.raws[label], payload)
	}
}

// flush prints the raw replies collected under label, then forgets them.
func (r *exploreReporter) flush(label, heading string) {
	replies := r.raws[label]
	for i, raw := range replies {
		if len(replies) > 1 {
			fmt.Printf("\n--- %s %d ---\n", heading, i+1)
		}
		fmt.Printf("%s\n", string(raw))
	}
	delete(r.raws, label)
}

// exploreDeps wires pkg/explore to this session. It issues no command.
func exploreDeps(client game.GameClient, format outputFormat) (explore.Deps, *exploreReporter) {
	rep := &exploreReporter{format: format, raws: map[string][]json.RawMessage{}}
	d := explore.Deps{
		Client:     client,
		KB:         globalKB,
		AgentID:    globalAgentID,
		Report:     rep,
		Tick:       func() int64 { return currentTick(client.GetState()) },
		HasScanner: checkForSurveyScanner,
		// The JSONL dump is append-only, gitignored and still the only copy of
		// the lifecycle data from before the wildlife_observations table, so
		// it stays until the table is confirmed to reproduce the same
		// histogram.
		OnCreatures: func(creatures []serverapi.NearbyCreature, systemID, poiID string, tick int64) {
			if err := appendCreatureObservations(globalWildlifeGrowthDir, creatures,
				systemID, poiID, globalAgentID, tick); err != nil {
				rep.Printf("  (creature observations not logged: %v)\n", err)
			}
		},
	}
	if globalKB != nil {
		d.UpdateSystem = func(ctx context.Context) error { return kbUpdateSystem(client, ctx) }
		d.UpdatePOI = func(ctx context.Context) error { return kbUpdatePOI(client, ctx) }
		d.StationCapture = func(ctx context.Context) error { return kbUpdateAll(client, ctx) }
	}
	return d, rep
}

// explore visits all POIs in the current system in distance-optimized order,
// running update_poi at each and update_all at stations. Refuels at every
// station dock.
func exploreCmd(client game.GameClient, ctx context.Context, format outputFormat) error {
	return exploreSystem(client, ctx, true, false, format)
}

// exploreSystem surveys and tours the current system. When refuelAtStations
// is true, every station dock is followed by a refuel. stopOnUnscanned parks
// the ship at a POI holding a never-scanned species and returns an
// *unscannedHalt.
func exploreSystem(client game.GameClient, ctx context.Context, refuelAtStations, stopOnUnscanned bool, format outputFormat) error {
	d, rep := exploreDeps(client, format)
	sr := runSurvey(ctx, d, rep)

	res, err := explore.Tour(ctx, d, explore.Options{
		RefuelAtStations: refuelAtStations,
		StopOnUnscanned:  stopOnUnscanned,
	}, sr)
	rep.flush(explore.RawExplore, "Explore response")
	if err != nil {
		return err
	}

	stoppedAt := res.StoppedAt
	for _, p := range res.POIs {
		if p.ID == res.StoppedAt && p.Name != "" {
			stoppedAt = p.Name
		}
	}
	switch res.Stopped {
	case explore.StoppedUnscanned:
		return &unscannedHalt{Species: res.Unscanned, POIID: res.StoppedAt, POIName: stoppedAt}
	case explore.StoppedBattle:
		return fmt.Errorf("in_battle: explore stopped before %s", stoppedAt)
	case explore.StoppedFuel:
		return fmt.Errorf("explore stopped before %s: out of fuel for in-system travel", stoppedAt)
	}
	return nil
}

// surveySystem runs survey_system if the ship can survey, repeating until no
// more hidden POIs are revealed, and saves what each pass lists.
func surveySystem(client game.GameClient, ctx context.Context, format outputFormat) {
	d, rep := exploreDeps(client, format)
	runSurvey(ctx, d, rep)
}

// runSurvey runs the shared survey and prints its raw-mode output.
func runSurvey(ctx context.Context, d explore.Deps, rep *exploreReporter) explore.SurveyResult {
	sr := explore.Survey(ctx, d)
	if rep.format != formatStyled {
		if sr.Problem != "" {
			fmt.Printf("{\"error\": %q}\n", sr.Problem)
		}
		rep.flush(explore.RawSurvey, "Survey iteration")
	}
	return sr
}

// captureSightings runs a best-effort player-enumeration query (get_nearby;
// get_system_agents) whose response feeds the player observer. In raw/JSON
// mode the response is appended to responses so it still appears in the
// output stream.
func captureSightings(client game.GameClient, ctx context.Context, fn func(context.Context) error, label string, format outputFormat, responses *[]json.RawMessage) {
	d, rep := exploreDeps(client, format)
	explore.Sightings(ctx, d, fn, label)
	if responses != nil {
		*responses = append(*responses, rep.raws[explore.RawExplore]...)
	}
}

// captureWildlifeSecondLook takes a second wildlife reading at a POI the pass
// is about to leave. See explore.WildlifeSecondLook.
func captureWildlifeSecondLook(client game.GameClient, ctx context.Context, poiID, poiType string, format outputFormat) []string {
	d, _ := exploreDeps(client, format)
	return explore.WildlifeSecondLook(ctx, d, poiID, poiType)
}

// captureWildlifeAtPOI records the creatures listed in the get_nearby reply
// already sitting in the client's raw cache. See explore.WildlifeAtPOI.
func captureWildlifeAtPOI(client game.GameClient, ctx context.Context, poiID, poiType string, format outputFormat) []string {
	d, _ := exploreDeps(client, format)
	return explore.WildlifeAtPOI(ctx, d, poiID, poiType)
}

// planExploreRoute orders POIs nearest-neighbour from startPOI.
func planExploreRoute(pois []game.POI, startPOI string) []game.POI {
	return explore.PlanRoute(pois, startPOI)
}

// poiDistance returns the Euclidean distance between two positions.
func poiDistance(a, b game.Position) float64 { return explore.Distance(a, b) }

// checkForSurveyScanner checks if the ship can survey systems, either via an
// installed survey scanner module or a survey scanner built into the ship hull
// (an "integrated_survey_scanner" inherent capability, e.g. the survey_vessel).
//
// Only a POSITIVE result is cached. A negative is deliberately not: the module
// list in state is populated exclusively by a get_ship reply, and install_mod's
// reply carries nothing but module_id/cpu_used/power_used. A scanner fitted
// mid-session is therefore invisible here until the ship is re-read, and
// caching the "no" made that refusal last the whole session.
func checkForSurveyScanner(state *game.State) bool {
	if surveyScannerCached {
		return hasSurveyScanner
	}
	if explore.HasSurveyScanner(context.Background(), globalKB, state) {
		surveyScannerCached = true
		hasSurveyScanner = true
		return true
	}
	return false
}

// invalidateSurveyScannerCache clears the cached scanner check.
// Call this when the ship might have changed.
func invalidateSurveyScannerCache() {
	surveyScannerCached = false
	hasSurveyScanner = false
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

	// The gate is whether the id resolves to a creature in the get_nearby
	// roster, NOT how the id is spelled. It used to require a "crt_" prefix,
	// which silently discarded every scan of the leviathan family introduced
	// on 2026-10-14 -- their ids are "lev_...", so scanning a Young Cloudwhale
	// returned a good reply, recorded nothing, and left explore announcing the
	// species as unscanned on every later pass with no error to explain it.
	species := speciesForCreature(client, targetID)
	if species == "" {
		// Either a player scan, which has nothing to file, or a creature whose
		// roster is not cached. Only the latter deserves the hint, and the id
		// prefix is the only available tell -- used for the MESSAGE alone, so a
		// future prefix costs a missing hint rather than a dropped scan.
		if format == formatStyled && looksLikeCreatureID(targetID) {
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

// creatureIDPrefixes are the id prefixes the server has used for creatures:
// "crt_" for the original fauna and "lev_" for the leviathan family added on
// 2026-10-14. This drives only the diagnostic hint in captureCreatureScan,
// never whether a scan is recorded -- gating capture on a prefix list is the
// bug that discarded every leviathan scan.
var creatureIDPrefixes = []string{"crt_", "lev_"}

// looksLikeCreatureID reports whether targetID resembles a creature id, for
// message purposes only.
func looksLikeCreatureID(targetID string) bool {
	for _, p := range creatureIDPrefixes {
		if strings.HasPrefix(targetID, p) {
			return true
		}
	}

	return false
}

// speciesForCreature resolves a creature id to its species using the cached
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
