package knowledge

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// realCarcassFixture is the operator's verbatim get_wrecks capture of a killed
// Drift-Ray. Decoding the drop table from the actual wire bytes rather than a
// hand-written literal is the point: it is what proves ship_class arrives EMPTY
// on a creature wreck, which is why a kill row has to carry the species learned
// at engage time.
func realCarcassFixture(t *testing.T) serverapi.Wreck {
	t.Helper()
	raw, err := os.ReadFile("testdata/wildlife/REAL-creature-wreck-payloads.json")
	if err != nil {
		t.Fatalf("read carcass fixture: %v", err)
	}
	var doc struct {
		Capture1 serverapi.GetWrecksResponse `json:"capture_1_drift_ray"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode carcass fixture: %v", err)
	}
	if len(doc.Capture1.Wrecks) != 1 {
		t.Fatalf("want 1 wreck in fixture, got %d", len(doc.Capture1.Wrecks))
	}
	return doc.Capture1.Wrecks[0]
}

// TestRealCarcassFixtureShape pins the facts the schema was designed around, so
// a server change that invalidates them fails here rather than silently
// producing a wrong drop table.
func TestRealCarcassFixtureShape(t *testing.T) {
	w := realCarcassFixture(t)

	if w.Type != "creature" {
		t.Errorf("wreck type = %q, want creature", w.Type)
	}
	// The carcass does NOT name the species — only the display name. If this
	// ever starts arriving populated, harvest attribution could stop depending
	// on the engage-time species.
	if w.ShipClass != "" {
		t.Errorf("ship_class = %q, want empty: a creature carcass names no species", w.ShipClass)
	}
	if w.VictimName != "Drift-Ray" {
		t.Errorf("victim_name = %q, want Drift-Ray", w.VictimName)
	}
	if len(w.Cargo) != 1 || w.Cargo[0].ItemID != "crystallized_biogas" || w.Cargo[0].Quantity != 1 {
		t.Errorf("cargo = %+v, want one crystallized_biogas x1", w.Cargo)
	}
}

// TestRecordWildlifeKill_DropRatesFromRealCarcass runs the fixture drop through
// the store and back out as a rate.
func TestRecordWildlifeKill_DropRatesFromRealCarcass(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()

	w := realCarcassFixture(t)
	drops := make([]WildlifeDrop, 0, len(w.Cargo))
	for _, c := range w.Cargo {
		drops = append(drops, WildlifeDrop{ItemID: c.ItemID, Quantity: c.Quantity})
	}
	if err := kb.RecordWildlifeKill(ctx, WildlifeKill{
		CreatureID: w.VictimID, GameTick: 1565621, Species: "drift_ray",
		CreatureName: w.VictimName, Role: "grazer", SystemID: w.SystemID,
		POIID: w.POIID, WreckID: w.ID, SalvageValue: w.SalvageValue,
		CarcassRead: true, Drops: drops, AgentID: "craftsman-1",
	}); err != nil {
		t.Fatal(err)
	}

	rates, err := kb.GetWildlifeDropRates(ctx, "drift_ray")
	if err != nil {
		t.Fatal(err)
	}
	if len(rates) != 1 {
		t.Fatalf("want 1 drop rate, got %d: %+v", len(rates), rates)
	}
	r := rates[0]
	if r.ItemID != "crystallized_biogas" || r.Carcasses != 1 || r.Appearances != 1 {
		t.Errorf("rate = %+v, want crystallized_biogas 1/1", r)
	}
	if r.MeanPerCarcass != 1 || r.MeanPerDrop != 1 {
		t.Errorf("means = %v/%v, want 1/1", r.MeanPerCarcass, r.MeanPerDrop)
	}
}

// TestGetWildlifeDropRates_EmptyCarcassIsTheDenominator is the reason kills and
// drops are separate tables. Three carcasses read, two holding the item: the
// rate must be 2/3. If an empty carcass were dropped for having no drop rows,
// every rate would read 100% and the schema would be lying.
func TestGetWildlifeDropRates_EmptyCarcassIsTheDenominator(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()

	kills := []WildlifeKill{
		{CreatureID: "crt_a", GameTick: 10, Species: "belt_grazer", CarcassRead: true,
			Drops: []WildlifeDrop{{ItemID: "carapace", Quantity: 1}}},
		{CreatureID: "crt_b", GameTick: 11, Species: "belt_grazer", CarcassRead: true,
			Drops: []WildlifeDrop{{ItemID: "carapace", Quantity: 2}}},
		// Read and empty — a real zero-drop roll.
		{CreatureID: "crt_c", GameTick: 12, Species: "belt_grazer", CarcassRead: true},
	}
	for _, k := range kills {
		if err := kb.RecordWildlifeKill(ctx, k); err != nil {
			t.Fatal(err)
		}
	}

	rates, err := kb.GetWildlifeDropRates(ctx, "belt_grazer")
	if err != nil {
		t.Fatal(err)
	}
	if len(rates) != 1 {
		t.Fatalf("want 1 drop rate, got %d", len(rates))
	}
	r := rates[0]
	if r.Carcasses != 3 {
		t.Errorf("Carcasses = %d, want 3: a read-but-empty carcass is an observation", r.Carcasses)
	}
	if r.Appearances != 2 {
		t.Errorf("Appearances = %d, want 2", r.Appearances)
	}
	// The variable line size the operator observed: sometimes 1, sometimes 2.
	if r.MinQuantity != 1 || r.MaxQuantity != 2 {
		t.Errorf("quantity range = %v..%v, want 1..2", r.MinQuantity, r.MaxQuantity)
	}
	if r.MeanPerDrop != 1.5 {
		t.Errorf("MeanPerDrop = %v, want 1.5 (mean given it dropped)", r.MeanPerDrop)
	}
	if r.MeanPerCarcass != 1 {
		t.Errorf("MeanPerCarcass = %v, want 1 (3 units over 3 carcasses)", r.MeanPerCarcass)
	}
}

// TestGetWildlifeDropRates_UnreadCarcassExcluded is the other half of the same
// rule: a kill whose carcass we never opened knows nothing about drops and must
// not dilute the rate.
func TestGetWildlifeDropRates_UnreadCarcassExcluded(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()

	if err := kb.RecordWildlifeKill(ctx, WildlifeKill{
		CreatureID: "crt_read", GameTick: 1, Species: "soot_grazer", CarcassRead: true,
		Drops: []WildlifeDrop{{ItemID: "biogas", Quantity: 1}},
	}); err != nil {
		t.Fatal(err)
	}
	// Killed, but the pass died or the carcass expired before we looked.
	if err := kb.RecordWildlifeKill(ctx, WildlifeKill{
		CreatureID: "crt_unread", GameTick: 2, Species: "soot_grazer", CarcassRead: false,
	}); err != nil {
		t.Fatal(err)
	}

	n, err := kb.CountWildlifeCarcassesRead(ctx, "soot_grazer")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("carcasses read = %d, want 1", n)
	}
	rates, err := kb.GetWildlifeDropRates(ctx, "soot_grazer")
	if err != nil {
		t.Fatal(err)
	}
	if len(rates) != 1 || rates[0].Carcasses != 1 {
		t.Fatalf("want one rate over 1 carcass, got %+v", rates)
	}
}

// TestRecordWildlifeKill_Idempotent guards the drop denominator against a retry:
// re-recording the same kill must not count it twice, or every rate silently
// halves.
func TestRecordWildlifeKill_Idempotent(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()

	k := WildlifeKill{
		CreatureID: "crt_dup", GameTick: 99, Species: "inkwyrm", CarcassRead: true,
		Drops: []WildlifeDrop{{ItemID: "molt_goods", Quantity: 2}},
	}
	for range 3 {
		if err := kb.RecordWildlifeKill(ctx, k); err != nil {
			t.Fatal(err)
		}
	}

	n, err := kb.CountWildlifeCarcassesRead(ctx, "inkwyrm")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("carcasses read = %d after 3 identical records, want 1", n)
	}
	rates, err := kb.GetWildlifeDropRates(ctx, "inkwyrm")
	if err != nil {
		t.Fatal(err)
	}
	if len(rates) != 1 || rates[0].TotalQuantity != 2 {
		t.Errorf("rates = %+v, want a single line totalling 2 (quantity replaced, not summed)", rates)
	}
}

// TestUpsertWildlifeSpecies_MergesPartialObservations covers the split in what
// each sensor can see: get_nearby knows hull and never abundance, survey_system
// knows role and never hull. Neither may erase the other's contribution.
func TestUpsertWildlifeSpecies_MergesPartialObservations(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()

	// From get_nearby at a belt: name, role, hull, habitat.
	if err := kb.UpsertWildlifeSpecies(ctx, []WildlifeSpecies{{
		Species: "cauldronback", Name: "Cauldronback", Role: "grazer",
		MaxHull: 120, Habitats: []string{"belt"},
	}}); err != nil {
		t.Fatal(err)
	}
	// From a later survey_system: no hull at all, and a second habitat.
	if err := kb.UpsertWildlifeSpecies(ctx, []WildlifeSpecies{{
		Species: "cauldronback", Name: "Cauldronback", Role: "grazer",
		Habitats: []string{"cryobelt"},
	}}); err != nil {
		t.Fatal(err)
	}

	got, err := kb.GetWildlifeSpecies(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 species, got %d", len(got))
	}
	s := got[0]
	if s.MaxHull != 120 {
		t.Errorf("MaxHull = %d, want 120: a hull-less observation must not erase it", s.MaxHull)
	}
	if len(s.Habitats) != 2 || s.Habitats[0] != "belt" || s.Habitats[1] != "cryobelt" {
		t.Errorf("Habitats = %v, want [belt cryobelt] accumulated and sorted", s.Habitats)
	}
	if s.FirstSeenUTC == "" || s.LastSeenUTC == "" {
		t.Errorf("first/last seen = %q/%q, want both stamped", s.FirstSeenUTC, s.LastSeenUTC)
	}
}

// TestGetUnscannedWildlifeSpecies is the scan campaign's work list. scan is a
// mutation (1 tick each) and danger is a species property, so the list must
// shrink to nothing once a species has been scanned even once.
func TestGetUnscannedWildlifeSpecies(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()

	if err := kb.UpsertWildlifeSpecies(ctx, []WildlifeSpecies{
		{Species: "belt_grazer", Role: "grazer"},
		{Species: "slag_tortoise", Role: "grazer"},
	}); err != nil {
		t.Fatal(err)
	}
	todo, err := kb.GetUnscannedWildlifeSpecies(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(todo) != 2 {
		t.Fatalf("want both species unscanned, got %v", todo)
	}

	if err := kb.UpsertWildlifeSpecies(ctx, []WildlifeSpecies{{
		Species: "belt_grazer", Danger: "low", DangerScannedUTC: "2026-08-17T00:00:00Z",
	}}); err != nil {
		t.Fatal(err)
	}
	todo, err = kb.GetUnscannedWildlifeSpecies(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(todo) != 1 || todo[0] != "slag_tortoise" {
		t.Errorf("work list = %v, want [slag_tortoise]", todo)
	}
}

// TestRecordWildlifeSightings_AppendsAndKeepsSource checks that repeat
// observations accumulate instead of overwriting — a bloom is only visible as a
// series — and that the two sources stay distinguishable, since one counts
// individuals at a POI and the other estimates a whole system.
func TestRecordWildlifeSightings_AppendsAndKeepsSource(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()

	if err := kb.RecordWildlifeSightings(ctx, []WildlifeSighting{
		{Species: "belt_grazer", SystemID: "kochab", POIID: "kochab_belt",
			Source: WildlifeSourceNearby, ObservedCount: 5, ObservedUTC: "2026-08-17T00:00:00Z"},
		{Species: "belt_grazer", SystemID: "kochab", Source: WildlifeSourceSurvey,
			ObservedCount: 40, Abundance: "abundant", BloomStatus: "rising",
			BloomIntensity: 1.4, ObservedUTC: "2026-08-17T00:01:00Z"},
	}); err != nil {
		t.Fatal(err)
	}
	// A second headcount at the same belt is a new fact, not a correction.
	if err := kb.RecordWildlifeSightings(ctx, []WildlifeSighting{
		{Species: "belt_grazer", SystemID: "kochab", POIID: "kochab_belt",
			Source: WildlifeSourceNearby, ObservedCount: 3, ObservedUTC: "2026-08-17T00:02:00Z"},
	}); err != nil {
		t.Fatal(err)
	}

	got, err := kb.GetWildlifeSightings(ctx, "belt_grazer", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 sightings, got %d", len(got))
	}
	if got[0].ObservedCount != 3 || got[0].Source != WildlifeSourceNearby {
		t.Errorf("newest = %+v, want the count of 3 from get_nearby", got[0])
	}
	// The survey row carries the system estimate and no POI.
	var survey *WildlifeSighting
	for i := range got {
		if got[i].Source == WildlifeSourceSurvey {
			survey = &got[i]
		}
	}
	if survey == nil {
		t.Fatal("no survey_system sighting stored")
	}
	if survey.POIID != "" {
		t.Errorf("survey POIID = %q, want empty: the census is system-wide", survey.POIID)
	}
	if survey.ObservedCount != 40 || survey.Abundance != "abundant" ||
		survey.BloomStatus != "rising" || survey.BloomIntensity != 1.4 {
		t.Errorf("survey row = %+v, want the estimate/abundance/bloom preserved", *survey)
	}
}
