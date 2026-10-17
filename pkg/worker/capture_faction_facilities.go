package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

// factionFacilityStore is the write side of faction_facilities. Narrow on
// purpose: the full faction.Store wants ten methods, and this capture needs
// exactly one.
type factionFacilityStore interface {
	ReplaceFactionFacilities(ctx context.Context, factionID, baseID string, fs []knowledge.FactionFacilityRow) error
}

// upsertFactionFromFacilityList records OUR faction's production lines from a
// facility faction_list reply.
//
// WHY THIS LIVES HERE -- the hourly `facilities` task already fetches exactly
// this payload and already decodes faction_facilities to merge into the base's
// facility list, but nothing ever wrote the faction_facilities TABLE. The
// scheduled `capture_faction` command is an unrelated code path
// (assets.CaptureFaction -> assets.db: profile, storage, storage items) that
// touches no facilities at all, and the only code that did write the table
// lives in pkg/faction.Collector, which runs from play_as, kb_update and the
// faction-dashboard tool and is on no schedule. The result: public_facilities
// held 2,509 rows minutes old while faction_facilities held ONE row from
// 2026-05-21, so we could see every other faction's factories live and none of
// our own.
//
// Extending this capture rather than adding a new scheduled command is
// deliberate: schedule seeding is ADDITIVE ONLY, so a new command fires
// forever once seeded, and this reuses a read already on the wire.
//
// ReplaceFactionFacilities PRUNES, so the guards below are load-bearing rather
// than defensive. GetRawJSON("_last") can hand back another command's reply,
// and treating that as "our faction has no facilities here" would erase live
// rows. A reply must decode, name its station, and carry at least one facility
// SECTION before it is trusted. A listing that names sections but contains no
// line of ours is a real observation -- ours there are gone -- and does prune.
func upsertFactionFromFacilityList(ctx context.Context, store factionFacilityStore,
	raw []byte, factionID string,
) (captured int, err error) {
	if store == nil || factionID == "" {
		// No faction means there is nothing of ours to record, and pruning
		// against an unknown faction id would delete another faction's rows.
		return 0, nil
	}

	var resp struct {
		BaseID            string           `json:"base_id"`
		StationFacilities []map[string]any `json:"station_facilities"`
		PlayerFacilities  []map[string]any `json:"player_facilities"`
		FactionFacilities []map[string]any `json:"faction_facilities"`
		PublicFacilities  []map[string]any `json:"public_facilities"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return 0, err
	}
	if resp.BaseID == "" {
		return 0, nil
	}
	// "Carries a section" counts ANY of the four, including an empty
	// faction_facilities: a station where we own nothing still returns the
	// other sections, and that is the reply that legitimately prunes.
	sections := len(resp.StationFacilities) + len(resp.PlayerFacilities) +
		len(resp.FactionFacilities) + len(resp.PublicFacilities)
	if sections == 0 {
		return 0, nil
	}

	now := time.Now().UTC()
	rows := make([]knowledge.FactionFacilityRow, 0, len(resp.FactionFacilities))
	for _, m := range resp.FactionFacilities {
		prod, _ := m["production"].(map[string]any)
		recipe := facStr(m, "recipe_id")
		if recipe == "" && prod != nil {
			recipe = facStr(prod, "recipe")
		}
		id := facStr(m, "facility_id")
		if id == "" {
			id = facStr(m, "id")
		}
		details, _ := json.Marshal(m)
		rows = append(rows, knowledge.FactionFacilityRow{
			FactionID:    factionID,
			BaseID:       resp.BaseID,
			FacilityID:   id,
			FacilityType: facStr(m, "name"),
			Category:     facStr(m, "category"),
			Level:        facInt(m, "level"),
			Status:       facStr(m, "status"),
			RecipeID:     recipe,
			DetailsJSON:  string(details),
			CapturedAt:   now,
		})
	}

	if err := store.ReplaceFactionFacilities(ctx, factionID, resp.BaseID, rows); err != nil {
		return 0, fmt.Errorf("replace faction facilities at %s: %w", resp.BaseID, err)
	}

	return len(rows), nil
}
