package serverapi

import (
	"encoding/json"
	"testing"
)

func TestPOIHazard_DoseAndKinds(t *testing.T) {
	for sev, want := range map[int]int{1: 1, 2: 3, 3: 8, 4: 25, 0: 0, 5: 0} {
		if got := (&POIHazard{Type: HazardRadiation, Severity: sev}).DosePct(); got != want {
			t.Errorf("severity %d: DosePct %d, want %d", sev, got, want)
		}
	}
	var none *POIHazard
	if none.Damaging() || none.DosePct() != 0 {
		t.Error("nil hazard must be harmless")
	}
	cases := []struct {
		typ            string
		steady, mining bool
	}{
		{HazardRadiation, true, false},
		{HazardTidalShear, true, true},
		{HazardChargedSeam, false, true},
		{HazardDebris, false, true},
		{HazardScarDust, true, false},
		{HazardIonStorm, false, false},
	}
	for _, c := range cases {
		h := &POIHazard{Type: c.typ, Severity: 1}
		if h.Steady() != c.steady || h.DosesOnMining() != c.mining || h.Damaging() != (c.steady || c.mining) {
			t.Errorf("%s: steady=%v mining=%v", c.typ, h.Steady(), h.DosesOnMining())
		}
	}
}

func TestHazardFieldsDecode(t *testing.T) {
	var tr TravelResponse
	if err := json.Unmarshal([]byte(`{"action":"arrived","poi_hazard":{"type":"tidal_shear","severity":3}}`), &tr); err != nil {
		t.Fatal(err)
	}
	if tr.POIHazard == nil || tr.POIHazard.Type != HazardTidalShear || tr.POIHazard.Severity != 3 {
		t.Fatalf("travel poi_hazard: %+v", tr.POIHazard)
	}
	var w HazardWarning
	if err := json.Unmarshal([]byte(`{"poi_id":"the_maw","poi_name":"The Maw","hazard":{"type":"radiation","severity":4},"hull":75,"max_hull":100,"dose_pct":25,"message":"leave"}`), &w); err != nil {
		t.Fatal(err)
	}
	if w.Hazard == nil || w.Hazard.Severity != 4 || w.Hull != 75 || w.MaxHull != 100 || w.DosePct != 25 {
		t.Fatalf("hazard_warning: %+v", w)
	}
}
