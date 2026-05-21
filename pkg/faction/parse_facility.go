package faction

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

// parseFacilities converts raw faction facility maps into KB rows. baseID is the
// fallback when a facility map omits its own base_id.
func parseFacilities(factionID, baseID string, raw []map[string]any) []knowledge.FactionFacilityRow {
	now := time.Now()
	out := make([]knowledge.FactionFacilityRow, 0, len(raw))
	for _, f := range raw {
		row := knowledge.FactionFacilityRow{FactionID: factionID, BaseID: baseID, CapturedAt: now}
		if v, ok := f["facility_id"].(string); ok {
			row.FacilityID = v
		}
		if v, ok := f["facility_type"].(string); ok {
			row.FacilityType = v
		}
		if v, ok := f["category"].(string); ok {
			row.Category = v
		}
		if v, ok := f["status"].(string); ok {
			row.Status = v
		}
		if v, ok := f["recipe_id"].(string); ok {
			row.RecipeID = v
		}
		if v, ok := f["base_id"].(string); ok && v != "" {
			row.BaseID = v
		}
		if v, ok := f["level"].(float64); ok {
			row.Level = int(v)
		}
		if b, err := json.Marshal(f); err == nil {
			row.DetailsJSON = string(b)
		}
		out = append(out, row)
	}
	return out
}

// isStorageFacility reports whether a facility type holds shared storage.
func isStorageFacility(facilityType string) bool {
	t := strings.ToLower(facilityType)
	for _, st := range []string{"lockbox", "vault", "warehouse", "depot", "storage"} {
		if strings.Contains(t, st) {
			return true
		}
	}
	return false
}
