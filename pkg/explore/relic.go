package explore

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// poiTypeRelic is the POI type of a relic.
const poiTypeRelic = "relic"

// relicsTried holds the "agent/poi" pairs this process has already attempted,
// so a relic is interacted with once however often a tour passes it.
var relicsTried = struct {
	sync.Mutex
	seen map[string]bool
}{seen: map[string]bool{}}

// relicStep interacts once with the relic the ship is at and records what came
// back. A POI is a relic when its type says so or its get_poi reply prompts an
// interaction. It reports whether interact_poi was sent.
//
// A prompt that offers a mission is recorded and NOT interacted with: relics
// are not expected to offer one, and accepting a mission is the operator's
// call.
func relicStep(ctx context.Context, d Deps, p game.POI) (tried bool) {
	if err := d.Client.GetPOI(ctx); err != nil {
		if p.Type != poiTypeRelic {
			return false
		}
		d.Report.Printf("  (relic %s: get_poi failed: %v)\n", p.Name, err)
	}
	var reply serverapi.GetPOIResponse
	if raw := d.Client.GetRawJSON("_last"); len(raw) > 0 {
		_ = json.Unmarshal(unwrapActionResult(raw), &reply)
	}
	prompt := reply.Interaction
	if p.Type != poiTypeRelic && (prompt == nil || prompt.Command == "") {
		return false
	}

	key := d.AgentID + "/" + p.ID
	relicsTried.Lock()
	seen := relicsTried.seen[key]
	relicsTried.seen[key] = true
	relicsTried.Unlock()
	if seen {
		return false
	}

	rec := knowledge.RelicInteraction{
		POIID:       p.ID,
		AgentID:     d.AgentID,
		Tick:        d.Tick(),
		ObservedUTC: time.Now().UTC(),
	}
	if st := d.Client.GetState(); st != nil {
		rec.SystemID = st.System.ID
	}
	if prompt != nil {
		rec.Prompt = prompt.Message
		if hasJSON(prompt.Mission) {
			rec.Outcome = knowledge.RelicMissionSkipped
			rec.MissionJSON = string(prompt.Mission)
			d.Report.Printf("  Relic %s offers a mission — NOT interacting (recorded for the operator)\n", p.Name)
			recordRelic(ctx, d, rec)
			return false
		}
	}

	if err := d.Client.RawCommand(ctx, "interact_poi", nil); err != nil {
		rec.Outcome, rec.Error = knowledge.RelicRefused, err.Error()
		if prompt == nil {
			rec.Outcome = knowledge.RelicNoPrompt
		}
		d.Report.Printf("  Relic %s: interact_poi refused: %v\n", p.Name, err)
		recordRelic(ctx, d, rec)
		return true
	}
	d.Settle(ctx, game.SleepQuick)

	var got serverapi.InteractPOIResponse
	if raw := d.Client.GetRawJSON("_last"); len(raw) > 0 {
		d.Report.Raw(RawExplore, raw)
		_ = json.Unmarshal(unwrapActionResult(raw), &got)
	}
	rec.Message, rec.DataCopied, rec.Items = got.Message, got.DataCopied, got.ItemsRecovered
	if hasJSON(got.Mission) {
		rec.MissionJSON = string(got.Mission)
	}
	switch {
	case len(got.ItemsRecovered) > 0:
		rec.Outcome = knowledge.RelicItems
	case got.DataCopied:
		rec.Outcome = knowledge.RelicDataCopied
	default:
		rec.Outcome = knowledge.RelicNothing
	}
	d.Report.Printf("  Relic %s: %s — %s\n", p.Name, rec.Outcome, got.Message)
	recordRelic(ctx, d, rec)
	return true
}

func recordRelic(ctx context.Context, d Deps, rec knowledge.RelicInteraction) {
	if d.KB == nil {
		return
	}
	if err := d.KB.RecordRelicInteraction(ctx, rec); err != nil {
		d.Report.Printf("  (relic interaction not saved: %v)\n", err)
	}
}

// hasJSON reports whether raw holds a value other than null.
func hasJSON(raw json.RawMessage) bool {
	return len(raw) > 0 && string(raw) != "null"
}
