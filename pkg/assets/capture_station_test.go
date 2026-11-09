package assets

import (
	"context"
	"math"
	"os"
	"strings"
	"testing"
	"time"
)

func readTestdata(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestFacilityRowsFrom_CJ(t *testing.T) {
	baseID, rows, ok := FacilityRowsFrom(readTestdata(t, "cj_facility_list.json"))
	if !ok {
		t.Fatal("ok = false")
	}
	if baseID != "c976fa7df53c87f9616165d7d411b41a" {
		t.Fatalf("baseID = %q", baseID)
	}
	if len(rows) != 28 {
		t.Fatalf("rows = %d, want 28", len(rows))
	}
	byType := map[string]FacilityRow{}
	for _, r := range rows {
		byType[r.Type] = r
	}
	mill := byType["copper_wire_mill"]
	if mill.Category != "production" || mill.Recipe != "Process Copper Wiring" ||
		mill.ItemsPerHour != 7428 || mill.QueuedRuns != 0 {
		t.Fatalf("copper_wire_mill = %+v", mill)
	}
	if pd := byType["point_defense_battery"]; pd.Category != "defense" ||
		pd.MaintenanceSatisfied == nil || !*pd.MaintenanceSatisfied || pd.MaintenanceLevel != 1 {
		t.Fatalf("point_defense_battery = %+v", pd)
	}
	if fo := byType["faction_office"]; fo.MaintenanceSatisfied != nil {
		t.Fatalf("faction_office maintenance = %v, want nil", *fo.MaintenanceSatisfied)
	}
}

func TestFacilityRowsFrom_Rejects(t *testing.T) {
	for _, raw := range []string{``, `{}`, `not json`} {
		if _, _, ok := FacilityRowsFrom([]byte(raw)); ok {
			t.Fatalf("%q: ok = true, want false", raw)
		}
	}
}

func TestPassengerSnapshotFrom_CJ(t *testing.T) {
	got, ok := PassengerSnapshotFrom(readTestdata(t, "cj_station_passengers.json"))
	if !ok {
		t.Fatal("ok = false")
	}
	if got.DemandLevel != "discounted" || got.FareSurge != 0.6 || got.LoungeCapacity != 20 ||
		got.LoungeOccupancy != 0 || got.Waiting != 0 ||
		!strings.HasPrefix(got.MarketConditions, "Conjunction Junction is well served") {
		t.Fatalf("got %+v", got)
	}
	for _, raw := range []string{``, `{}`, `not json`} {
		if _, ok := PassengerSnapshotFrom([]byte(raw)); ok {
			t.Fatalf("%q: ok = true, want false", raw)
		}
	}
}

var t0 = time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)

func insertBase(t *testing.T, st *Store, revenue int64, at time.Time) {
	t.Helper()
	snap, _ := BaseSnapshotFrom([]byte(cjBaseRaw))
	snap.TourismRevenue = revenue
	if err := st.InsertBaseSnapshot(context.Background(), "a", snap, at); err != nil {
		t.Fatal(err)
	}
}

func TestLatestStation_TourismRate(t *testing.T) {
	const base = "c976fa7df53c87f9616165d7d411b41a"
	ctx := context.Background()

	st := openTestStore(t)
	insertBase(t, st, 3956, t0)
	got, ok, err := st.LatestStation(ctx, base)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if got.TourismPerHour != nil {
		t.Fatalf("one row: rate = %v, want nil", *got.TourismPerHour)
	}
	insertBase(t, st, 7638, t0.Add(90*time.Minute))
	got, _, _ = st.LatestStation(ctx, base)
	if got.TourismPerHour == nil || math.Abs(*got.TourismPerHour-2454.67) > 0.5 {
		t.Fatalf("rate = %v, want ~2454.67", got.TourismPerHour)
	}
	if !got.CapturedAt.Equal(t0.Add(90 * time.Minute)) {
		t.Fatalf("CapturedAt = %v", got.CapturedAt)
	}

	st2 := openTestStore(t)
	insertBase(t, st2, 3956, t0)
	insertBase(t, st2, 100, t0.Add(90*time.Minute))
	got, _, _ = st2.LatestStation(ctx, base)
	if got.TourismPerHour != nil {
		t.Fatalf("negative delta: rate = %v, want nil", *got.TourismPerHour)
	}
}

func TestLatestStation_NewestFacilitiesAndPassengers(t *testing.T) {
	const base = "c976fa7df53c87f9616165d7d411b41a"
	ctx := context.Background()
	st := openTestStore(t)
	insertBase(t, st, 1, t0)
	_, rows, _ := FacilityRowsFrom(readTestdata(t, "cj_facility_list.json"))
	if err := st.InsertFacilityRows(ctx, base, "a", rows[:3], t0); err != nil {
		t.Fatal(err)
	}
	if err := st.InsertFacilityRows(ctx, base, "a", rows, t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	p, _ := PassengerSnapshotFrom(readTestdata(t, "cj_station_passengers.json"))
	if err := st.InsertPassengerSnapshot(ctx, base, "a", p, t0); err != nil {
		t.Fatal(err)
	}
	got, ok, err := st.LatestStation(ctx, base)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if len(got.Facilities) != 28 {
		t.Fatalf("facilities = %d, want 28 (newest pass)", len(got.Facilities))
	}
	if got.Passengers == nil || got.Passengers.DemandLevel != "discounted" || got.Passengers.LoungeCapacity != 20 {
		t.Fatalf("passengers = %+v", got.Passengers)
	}
	var pd *FacilityRow
	for i := range got.Facilities {
		if got.Facilities[i].Type == "point_defense_battery" {
			pd = &got.Facilities[i]
		}
	}
	if pd == nil || pd.MaintenanceSatisfied == nil || !*pd.MaintenanceSatisfied {
		t.Fatalf("maintenance_satisfied round trip failed: %+v", pd)
	}
}

func TestLatestStation_Empty(t *testing.T) {
	_, ok, err := openTestStore(t).LatestStation(context.Background(), "nope")
	if ok || err != nil {
		t.Fatalf("ok=%v err=%v, want false,nil", ok, err)
	}
}

func TestStationHistory_48h(t *testing.T) {
	const base = "c976fa7df53c87f9616165d7d411b41a"
	ctx := context.Background()
	st := openTestStore(t)
	insertBase(t, st, 100, t0)
	insertBase(t, st, 200, t0.Add(3*time.Hour))
	snap, _ := BaseSnapshotFrom([]byte(cjBaseRaw))
	snap.MaxShield, snap.Shield = 0, 0
	if err := st.InsertBaseSnapshot(ctx, "a", snap, t0.Add(50*time.Hour)); err != nil {
		t.Fatal(err)
	}
	rows := []FacilityRow{{FacilityID: "x", BacklogTicks: 5}, {FacilityID: "y", BacklogTicks: 7}}
	if err := st.InsertFacilityRows(ctx, base, "a", rows, t0.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	pts, err := st.StationHistory(ctx, base, t0.Add(50*time.Hour-48*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != 2 {
		t.Fatalf("points = %d, want 2", len(pts))
	}
	if pts[0].ShieldPct != 100 || pts[0].BacklogTicks != 12 {
		t.Fatalf("pts[0] = %+v, want shield 100 backlog 12", pts[0])
	}
	if pts[1].ShieldPct != 0 || math.IsNaN(pts[1].ShieldPct) || pts[1].BacklogTicks != 12 {
		t.Fatalf("pts[1] = %+v, want shield 0 (not NaN)", pts[1])
	}
}
