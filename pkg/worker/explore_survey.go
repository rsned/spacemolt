package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// maxSurveyPasses caps how many times one system is re-surveyed. A pass that
// reveals a POI is repeated, because a stronger reading can surface the next
// layer, and the cap keeps a server that reveals something every time from
// holding an explorer in one system.
const maxSurveyPasses = 10

// ErrNoSurveyScanner means the ship has nothing that can run survey_system.
// The explore step treats it as "nothing to do here", not as a failure.
var ErrNoSurveyScanner = errors.New("no survey scanner")

// SurveyHere surveys the current system and records every POI it reveals,
// newly and already, with resources, plus the wildlife census. Until
// 2026-10-30 the explore step only flew between systems, so an explorer's
// survey and detection gear never ran and hidden POIs stayed hidden. Returns
// how many POIs were newly revealed.
//
// The server decides whether the ship can survey (a module, an integrated hull
// scanner, or a consumable all count), so there is no local module check: its
// refusal comes back as ErrNoSurveyScanner.
func SurveyHere(ctx context.Context, client game.GameClient, kb knowledge.Base, agentID string, out io.Writer) (int, error) {
	if out == nil {
		out = io.Discard
	}
	newly := 0
	for pass := 1; pass <= maxSurveyPasses; pass++ {
		if err := client.SurveySystem(ctx); err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "scanner") {
				return newly, fmt.Errorf("%w: %v", ErrNoSurveyScanner, err)
			}
			return newly, fmt.Errorf("survey_system: %w", err)
		}
		raw := client.GetRawJSON("_last")
		if raw == nil {
			return newly, nil
		}
		var resp serverapi.SurveySystemResponse
		if err := json.Unmarshal(unwrapActionResult(raw), &resp); err != nil {
			return newly, fmt.Errorf("survey_system: parse reply: %w", err)
		}
		tick := int64(0)
		if st := client.GetState(); st != nil {
			tick = st.CurrentTick
		}
		if kb != nil {
			for _, poi := range SurveyPOIsToKB(resp, agentID, tick) {
				if err := kb.RememberPOI(ctx, poi); err != nil {
					fmt.Fprintf(out, "survey: save %s: %v\n", poi.ID, err) //nolint:errcheck
				}
			}
			if _, err := knowledge.CaptureWildlifeSurvey(ctx, kb, resp, agentID, tick); err != nil {
				fmt.Fprintf(out, "survey: wildlife census not saved: %v\n", err) //nolint:errcheck
			}
		}
		newly += len(resp.NewlyRevealed)
		fmt.Fprintf(out, "survey: %s pass %d: %d newly revealed, %d already known\n", //nolint:errcheck
			resp.SystemID, pass, len(resp.NewlyRevealed), len(resp.AlreadyRevealed))
		if len(resp.NewlyRevealed) == 0 {
			break
		}
		settle(ctx, game.SleepQuick)
	}

	return newly, nil
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
