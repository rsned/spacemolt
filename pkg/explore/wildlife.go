package explore

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// Sightings runs a best-effort player-enumeration query (get_nearby,
// get_system_agents) whose reply feeds the client's player observer. A failure
// is reported and otherwise ignored: sighting capture is not required for an
// explore.
func Sightings(ctx context.Context, d Deps, fn func(context.Context) error, label string) {
	d = d.withDefaults()
	if err := fn(ctx); err != nil {
		d.Report.Printf("  (%s failed: %v)\n", label, err)
		return
	}
	if raw := d.Client.GetRawJSON("_last"); raw != nil {
		d.Report.Raw(RawExplore, raw)
	}
	d.Settle(ctx, game.SleepQuick)
}

// WildlifeSecondLook takes a second wildlife reading at a POI the tour is
// about to leave.
//
// A get_nearby creature list is a snapshot of a moment, not a census of the
// POI. Live 2026-08-28: ashford_ice_shelf reported 0 creatures at 12:41:35 and
// 3 at 12:43:16 -- creatures move or spawn faster than a POI visit takes. One
// look per POI therefore records a false negative as fact.
//
// It runs at the END of the POI's work, so the dock/update in between supplies
// several seconds of real separation. get_nearby is a query and costs no tick.
func WildlifeSecondLook(ctx context.Context, d Deps, poiID, poiType string) []string {
	d = d.withDefaults()
	if err := d.Client.GetNearby(ctx); err != nil {
		d.Report.Printf("  (second wildlife look failed: %v)\n", err)
		return nil
	}
	d.Settle(ctx, game.SleepQuick)
	return WildlifeAtPOI(ctx, d, poiID, poiType)
}

// WildlifeAtPOI records the creatures and hostiles listed in the get_nearby
// reply already in the client's raw cache; it issues no command of its own. It
// returns the species present that no scan has characterised.
//
// poiType becomes the species' habitat. It is passed in because the tour holds
// the POI it travelled to, and the KB may not know a belt a survey only just
// revealed.
func WildlifeAtPOI(ctx context.Context, d Deps, poiID, poiType string) []string {
	d = d.withDefaults()
	raw := d.Client.GetRawJSON("nearby")
	if len(raw) == 0 {
		return nil
	}
	var nearby serverapi.GetNearbyResponse
	if err := json.Unmarshal(raw, &nearby); err != nil {
		return nil
	}
	// NO early return on an empty creature list. CaptureWildlifeNearby records
	// its coverage row first and unconditionally, so a look that found nothing
	// still leaves a trace: wildlife is transient, and only the coverage row
	// can tell "looked, found none" from "never looked".
	state := d.Client.GetState()
	systemID := ""
	if state != nil {
		systemID = state.System.ID
	}
	// The reply names its own POI; prefer it over the caller's idea of where we
	// are, which can be a tick stale after an interrupted travel.
	if nearby.POIID != "" {
		poiID = nearby.POIID
	}
	tick := d.Tick()

	// Per-individual creature rows: creature hull stopped being a species
	// constant on 2026-10-14, and neither wildlife_species nor
	// wildlife_sightings keeps a per-individual number.
	if err := CaptureCreatureObservations(ctx, d.KB, nearby.Creatures, systemID, poiID, d.AgentID, tick); err != nil {
		d.Report.Printf("  (creature observations not recorded: %v)\n", err)
	}
	if d.OnCreatures != nil {
		d.OnCreatures(nearby.Creatures, systemID, poiID, tick)
	}

	if hn, herr := knowledge.CaptureHostilesNearby(ctx, d.KB, nearby.Repeaters, systemID, poiID, d.AgentID, tick); herr != nil {
		d.Report.Printf("  (hostiles not saved: %v)\n", herr)
	} else if hn > 0 {
		d.Report.Printf("  Hostiles: %d repeater(s) recorded\n", hn)
	}

	n, err := knowledge.CaptureWildlifeNearby(ctx, d.KB, nearby.Creatures, systemID, poiID, poiType, d.AgentID, tick)
	if err != nil {
		d.Report.Printf("  (wildlife not saved: %v)\n", err)
		return nil
	}
	if n > 0 {
		d.Report.Printf("  Wildlife: %d creature(s), %d species\n", len(nearby.Creatures), n)
	}
	reportUnscannedSpecies(ctx, d, nearby.Creatures, poiID)

	return DetectUnscanned(ctx, d.KB, nearby.Creatures)
}

// wildlifeObservationRecorder is the KB half of the per-individual capture.
type wildlifeObservationRecorder interface {
	RecordWildlifeObservations(ctx context.Context, rows []knowledge.WildlifeObservation) error
}

// CaptureCreatureObservations records a get_nearby creature list as
// per-individual rows.
//
// The kb is type-asserted, which is a known trap -- MemoryKB implements none
// of the wildlife interfaces, so the capture silently does nothing while
// reporting success. Tests must use a real SQLiteKB.
func CaptureCreatureObservations(ctx context.Context, kb knowledge.Base,
	creatures []serverapi.NearbyCreature, systemID, poiID, agentID string, tick int64,
) error {
	if kb == nil || len(creatures) == 0 {
		return nil
	}
	rec, ok := kb.(wildlifeObservationRecorder)
	if !ok {
		return nil
	}

	now := time.Now().UTC().Format(time.RFC3339)
	rows := make([]knowledge.WildlifeObservation, 0, len(creatures))
	for _, c := range creatures {
		if c.CreatureID == "" {
			continue
		}
		rows = append(rows, knowledge.WildlifeObservation{
			CreatureID:  c.CreatureID,
			GameTick:    tick,
			ObservedUTC: now,
			Species:     c.Species,
			Name:        c.Name,
			Role:        c.Role,
			Hull:        c.Hull,
			MaxHull:     c.MaxHull,
			InCombat:    c.InCombat,
			Branded:     c.Branded,
			SystemID:    systemID,
			POIID:       poiID,
			AgentID:     agentID,
		})
	}

	return rec.RecordWildlifeObservations(ctx, rows)
}

// DetectUnscanned reports which species in a get_nearby creature list have
// never been scanned, sorted for stable output. Best-effort: a nil or
// non-wildlife KB, or a KB error, yields nothing.
func DetectUnscanned(ctx context.Context, kb knowledge.Base, creatures []serverapi.NearbyCreature) []string {
	if kb == nil || len(creatures) == 0 {
		return nil
	}
	ids := make([]string, 0, len(creatures))
	seen := make(map[string]bool, len(creatures))
	for _, c := range creatures {
		if c.Species == "" || seen[c.Species] {
			continue
		}
		seen[c.Species] = true
		ids = append(ids, c.Species)
	}
	unscanned, err := knowledge.UnscannedSpecies(ctx, kb, ids)
	if err != nil || len(unscanned) == 0 {
		return nil
	}
	slices.Sort(unscanned)
	return unscanned
}

// reportUnscannedSpecies flags species present here that no scan has ever
// characterised, so the operator can decide whether to spend the tick.
//
// It deliberately does not scan. scan is a mutation costing a tick per
// creature, and the thing worth knowing -- the danger bracket -- is exactly
// what you do not have before scanning: a Rainbow Leviathan does 130
// energy/tick and kills a starter hull in two.
func reportUnscannedSpecies(ctx context.Context, d Deps, creatures []serverapi.NearbyCreature, poiID string) {
	cands := make([]unscannedCandidate, 0, len(creatures))
	for _, c := range creatures {
		cands = append(cands, unscannedCandidate{Species: c.Species, Name: c.Name, Role: c.Role})
	}
	reportUnscanned(ctx, d, cands, "at POI "+poiID)
}

// reportUnscannedCensus flags unscanned species named by a survey_system
// census. The census was the ONLY sighting of two species for a full
// exploration pass (pall_jelly and tempest_eel, redmarsh 2026-08-29), and both
// slipped through silently because nothing ran this check over it.
//
// A census names no POI -- it is system-wide -- so the notice says the system
// and admits it cannot say where.
func reportUnscannedCensus(ctx context.Context, d Deps, resp serverapi.SurveySystemResponse) {
	cands := make([]unscannedCandidate, 0, len(resp.Wildlife))
	for _, w := range resp.Wildlife {
		cands = append(cands, unscannedCandidate{Species: w.Species, Name: w.Name, Role: w.Role})
	}
	reportUnscanned(ctx, d, cands, "somewhere in "+resp.SystemID+" (census: no POI)")
}

// unscannedCandidate is one species a reply mentioned, in the fields the notice
// needs.
type unscannedCandidate struct {
	Species string
	Name    string
	Role    string
}

// reportUnscanned dedupes, asks the KB which species have never been
// characterised, and prints one line each. Role is in the line because it is
// the only danger signal available BEFORE the scan.
func reportUnscanned(ctx context.Context, d Deps, cands []unscannedCandidate, where string) {
	if d.KB == nil || len(cands) == 0 {
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
	unscanned, err := knowledge.UnscannedSpecies(ctx, d.KB, ids)
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
		d.Report.Printf("  ** NEW UNSCANNED CREATURE CLASS %s %s -- `scan <creature_id>` to characterise\n", label, where)
	}
}
