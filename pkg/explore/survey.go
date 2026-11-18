package explore

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// maxSurveyPasses caps how many times one system is re-surveyed. A pass that
// reveals a POI is repeated, because a stronger reading can surface the next
// layer; the cap keeps a server that reveals something every time from holding
// a ship in one system.
const maxSurveyPasses = 10

// scannerModules are the fitted modules that can run survey_system.
// anomaly_detector finds wormholes and anomalies; survey scanners find hidden
// POIs (operator, 2026-11-09).
var scannerModules = []string{"survey_scanner_i", "survey_scanner_ii", "deep_core_survey_scanner", "anomaly_detector"}

// HasSurveyScanner reports whether the ship can survey: a fitted scanner
// module, or a hull with an integrated one (not listed among the modules, so
// looked up in the ship class catalog).
func HasSurveyScanner(ctx context.Context, kb knowledge.Base, state *game.State) bool {
	if state == nil {
		return false
	}
	for _, m := range scannerModules {
		if game.HasModuleType(state, m) {
			return true
		}
	}
	if kb == nil || state.Ship.ClassID == "" {
		return false
	}
	sc, err := kb.GetShipClass(ctx, state.Ship.ClassID)
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

// SurveyResult is what a Survey learned.
type SurveyResult struct {
	Ran      bool   // at least one survey_system reply was read
	Power    int    // survey_power of the first reply
	Revealed int    // POIs newly revealed, over every pass
	Problem  string // why the survey did not run or stopped early; "" when it completed
}

// Survey runs survey_system and repeats it until a pass reveals nothing new.
// Every POI a pass lists (newly and already revealed, with resources), the
// wildlife census, faint signatures and the anomaly hint are saved to the KB.
// A ship that cannot survey is not an error: Problem says why.
func Survey(ctx context.Context, d Deps) SurveyResult {
	d = d.withDefaults()
	var res SurveyResult

	state := d.Client.GetState()
	if !d.HasScanner(state) {
		// State may simply be out of date: it learns the module list only from
		// a get_ship reply, so a scanner fitted during this session is not in
		// it yet. Re-read the ship before refusing — get_ship is a query and
		// costs no tick.
		if err := d.Client.GetShip(ctx); err == nil {
			state = d.Client.GetState()
		}
	}
	if !d.HasScanner(state) {
		d.Report.Printf("\nNo survey scanner or anomaly detector installed — skipping system survey\n")
		res.Problem = "no survey scanner installed"
		return res
	}

	d.Report.Printf("\nSurveying system with survey scanner...\n")

	totalXP := make(map[string]int)
	for pass := 1; ; pass++ {
		if err := d.Client.SurveySystem(ctx); err != nil {
			d.Report.Printf("  Survey failed: %v\n", err)
			res.Problem = fmt.Sprintf("survey failed: %v", err)
			return res
		}
		d.Settle(ctx, game.SleepQuick)

		rawJSON := d.Client.GetRawJSON("_last")
		if rawJSON == nil {
			d.Report.Printf("  Survey complete (no response data)\n")
			res.Problem = "no response data from server"
			return res
		}
		d.Report.Raw(RawSurvey, rawJSON)

		// survey_system terminates with an action_result frame that nests the
		// payload under "result"; unwrap it so the flat fields bind.
		var resp serverapi.SurveySystemResponse
		if err := json.Unmarshal(unwrapActionResult(rawJSON), &resp); err != nil {
			d.Report.Printf("  Failed to parse survey response: %v\n", err)
			res.Problem = fmt.Sprintf("failed to parse response: %v", err)
			return res
		}
		res.Ran = true
		if pass == 1 {
			res.Power = resp.SurveyPower
		}
		res.Revealed += len(resp.NewlyRevealed)
		for skill, xp := range resp.XPGained {
			totalXP[skill] += xp
		}

		// The census rides along on every survey and costs nothing extra. The
		// game ships no wildlife catalog, so this and get_nearby are the whole
		// field guide.
		if n, err := knowledge.CaptureWildlifeSurvey(ctx, d.KB, resp, d.AgentID, d.Tick()); err != nil {
			d.Report.Printf("  (wildlife census not saved: %v)\n", err)
		} else if n > 0 {
			bloom := ""
			if resp.BloomStatus != "" {
				bloom = fmt.Sprintf(" | bloom %s (%.2f)", resp.BloomStatus, resp.BloomIntensity)
			}
			d.Report.Printf("  Wildlife census: %d species%s\n", n, bloom)
		}
		// The census is a sighting source in its own right, so it gets the
		// unscanned check too, whether or not the capture above succeeded.
		reportUnscannedCensus(ctx, d, resp)

		if d.KB != nil {
			saveSurveyPOIs(ctx, d, resp)
			if len(resp.FaintSignatures) > 0 {
				saveFaintSignatures(ctx, d, resp)
			}
			if resp.AnomalyHint != "" {
				if _, err := knowledge.CaptureSurveyAnomaly(ctx, d.KB, resp.AnomalyHint, resp.SystemID, d.AgentID, d.Tick()); err != nil {
					d.Report.Printf("    Warning: failed to record survey anomaly: %v\n", err)
				}
			}
		}
		reportSurveyPass(d, resp, pass)

		// Stop if nothing new was revealed this pass.
		if len(resp.NewlyRevealed) == 0 {
			break
		}
		if pass >= maxSurveyPasses {
			d.Report.Printf("  (hit survey iteration cap of %d, stopping)\n", maxSurveyPasses)
			break
		}
	}

	var xpParts []string
	for skill, xp := range totalXP {
		if xp > 0 {
			xpParts = append(xpParts, fmt.Sprintf("%s +%d", skill, xp))
		}
	}
	if len(xpParts) > 0 {
		d.Report.Printf("  Total XP gained from survey: %s\n", strings.Join(xpParts, ", "))
	}
	return res
}

// reportSurveyPass prints one survey reply for a human reader.
func reportSurveyPass(d Deps, resp serverapi.SurveySystemResponse, pass int) {
	if pass == 1 {
		d.Report.Printf("  Survey power: %d | System: %s\n", resp.SurveyPower, resp.SystemName)
	} else {
		d.Report.Printf("  Re-survey #%d (checking for additional hidden POIs)...\n", pass)
	}
	list := func(mark string, pois []serverapi.RevealedPOI) {
		for _, poi := range pois {
			d.Report.Printf("    %s %s (%s)\n", mark, poi.Name, poi.Type)
			if poi.Description != "" {
				d.Report.Printf("      %s\n", poi.Description)
			}
			for _, r := range poi.Resources {
				d.Report.Printf("      Resource: %s (richness: %.0f, remaining: %.0f/%.0f)\n",
					r.ResourceID, r.Richness, r.Remaining, r.MaxRemaining)
			}
		}
	}
	if len(resp.NewlyRevealed) > 0 {
		d.Report.Printf("  Newly revealed POIs:\n")
		list("+", resp.NewlyRevealed)
	}
	if len(resp.AlreadyRevealed) > 0 && pass == 1 {
		d.Report.Printf("  Already revealed POIs (%d):\n", len(resp.AlreadyRevealed))
		list("=", resp.AlreadyRevealed)
	}
	if len(resp.FaintSignatures) > 0 {
		d.Report.Printf("  Faint signatures detected:\n")
		for _, sig := range resp.FaintSignatures {
			hint := sig.Hint
			if hint == "" {
				hint = "unknown"
			}
			d.Report.Printf("    ? %s (difficulty: %s, hint: %s)\n", sig.Type, sig.Difficulty, hint)
		}
	}
	if resp.Message != "" {
		d.Report.Printf("  %s\n", resp.Message)
	}
	if resp.AnomalyHint != "" {
		d.Report.Printf("  Anomaly: %s\n", resp.AnomalyHint)
	}
}

// saveSurveyPOIs saves every POI a survey revealed, newly and already, with
// full resource data. reveal_difficulty is not in the survey reply; the POI
// update at the visit picks it up.
func saveSurveyPOIs(ctx context.Context, d Deps, resp serverapi.SurveySystemResponse) {
	for _, kbPOI := range SurveyPOIsToKB(resp, d.AgentID, d.Tick()) {
		if err := d.KB.RememberPOI(ctx, kbPOI); err != nil {
			d.Report.Printf("    Warning: failed to save revealed POI %s: %v\n", kbPOI.Name, err)
		}
	}
}

// saveFaintSignatures saves faint (unresolved) survey signatures as placeholder
// POI records so they can be investigated later with better equipment or
// higher skills.
func saveFaintSignatures(ctx context.Context, d Deps, resp serverapi.SurveySystemResponse) {
	for i, sig := range resp.FaintSignatures {
		name := "Faint Signature"
		if sig.Hint != "" {
			name = fmt.Sprintf("Faint Signature: %s", sig.Hint)
		}
		kbPOI := knowledge.POI{
			// A deterministic placeholder ID from system + type + index.
			ID:       fmt.Sprintf("faint_%s_%s_%d", resp.SystemID, sig.Type, i),
			SystemID: resp.SystemID,
			Name:     name,
			Type:     "faint_signature",
			Description: fmt.Sprintf("Unresolved survey signature (type: %s, difficulty: %s). Requires better scanner or higher skills to identify.",
				sig.Type, sig.Difficulty),
			LastUpdatedTick: d.Tick(),
			DetectedBy:      d.AgentID,
		}
		if err := d.KB.RememberPOI(ctx, kbPOI); err != nil {
			d.Report.Printf("    Warning: failed to save faint signature: %v\n", err)
		}
	}
}

// SurveyPOIsToKB converts a survey's revealed POIs into knowledge rows.
//
// BOTH lists are captured. A hidden POI is newly revealed exactly once, ever;
// every survey after that reports it under already_revealed, which used to be
// ignored -- so its resources could only be refreshed by physically flying to
// it. Live 2026-08-28: prismatic_gas_pocket's resource row sat at tick
// 1,020,715 while the POI row read 1,736,298, and the survey reply carrying the
// current numbers was thrown away.
//
// Hidden is TRUE for both lists. It is intrinsic to the POI, not "not yet
// revealed to you" -- get_poi reports hidden:true for prismatic_gas_pocket, a
// POI already revealed to us. SurveyedPOI carries no hidden field, but needing
// a survey to see it is exactly what hidden means.
func SurveyPOIsToKB(resp serverapi.SurveySystemResponse, agentID string, tick int64) []knowledge.POI {
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
		if r.Hazard != nil {
			kbPOI.HazardType, kbPOI.HazardSeverity = r.Hazard.Type, r.Hazard.Severity
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

// unwrapActionResult returns the payload nested under "result" in an
// action_result frame, or raw itself for a flat reply.
func unwrapActionResult(raw []byte) []byte {
	var probe struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil || len(probe.Result) == 0 {
		return raw
	}
	return probe.Result
}
