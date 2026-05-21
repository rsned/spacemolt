package faction

import "testing"

func TestParseFacilities(t *testing.T) {
	raw := []map[string]any{
		{"facility_id": "fac1", "facility_type": "refinery", "category": "production", "level": float64(2), "status": "active", "recipe_id": "refine_iron", "base_id": "b1"},
		{"facility_id": "fac2", "facility_type": "vault", "category": "storage", "level": float64(1), "base_id": "b1"},
	}
	rows := parseFacilities("f1", "b1", raw)
	if len(rows) != 2 {
		t.Fatalf("want 2 rows, got %d", len(rows))
	}
	if rows[0].FacilityType != "refinery" || rows[0].Level != 2 || rows[0].RecipeID != "refine_iron" {
		t.Errorf("row0 wrong: %+v", rows[0])
	}
	if !isStorageFacility("vault") || isStorageFacility("refinery") {
		t.Errorf("isStorageFacility classification wrong")
	}
}
