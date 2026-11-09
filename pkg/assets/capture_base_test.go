package assets

import (
	"context"
	"testing"
	"time"
)

const cjBaseRaw = `{"action":"get_base","base":{"id":"c976fa7df53c87f9616165d7d411b41a","hull":85000,"max_hull":85000,
"shield":12000,"max_shield":12000,"weapon_dps":30,"weapon_reach":1},
"condition":{"condition":"thriving","satisfaction_pct":100},
"dining_points":0,"leisure_points":2,"tourism_revenue":3956,
"life_support":{"supply":12,"demand":10},
"power":{"supply":40,"current_draw":0,"battery_capacity":0,"battery_stored":0},
"faction_fuel_reserve":4200,"faction_fuel_capacity":55000}`

func TestBaseSnapshotFrom_ParsesCJ(t *testing.T) {
	got, ok := BaseSnapshotFrom([]byte(cjBaseRaw))
	if !ok {
		t.Fatal("ok = false, want true")
	}
	want := BaseSnapshot{
		BaseID: "c976fa7df53c87f9616165d7d411b41a", Hull: 85000, MaxHull: 85000,
		Shield: 12000, MaxShield: 12000, WeaponDPS: 30, WeaponReach: 1,
		Condition: "thriving", SatisfactionPct: 100, LeisurePoints: 2, TourismRevenue: 3956,
		LifeSupportSupply: 12, LifeSupportDemand: 10, PowerSupply: 40,
		FuelReserve: 4200, FuelCapacity: 55000,
	}
	if got != want {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestBaseSnapshotFrom_RejectsNoBase(t *testing.T) {
	for _, raw := range []string{``, `{}`, `{"base":{}}`, `not json`} {
		if _, ok := BaseSnapshotFrom([]byte(raw)); ok {
			t.Fatalf("%q: ok = true, want false", raw)
		}
	}
}

func TestInsertBaseSnapshot_AppendsHistory(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	snap, _ := BaseSnapshotFrom([]byte(cjBaseRaw))
	t0 := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)

	if err := st.InsertBaseSnapshot(ctx, "miningbot_0000", snap, t0); err != nil {
		t.Fatal(err)
	}
	snap.TourismRevenue = 9000
	if err := st.InsertBaseSnapshot(ctx, "miningbot_0000", snap, t0.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}

	var n int
	var latest int64
	if err := st.DB().QueryRow(`SELECT count(*), max(tourism_revenue) FROM base_snapshots WHERE base_id=?`,
		snap.BaseID).Scan(&n, &latest); err != nil {
		t.Fatal(err)
	}
	if n != 2 || latest != 9000 {
		t.Fatalf("rows=%d latest=%d, want 2 and 9000", n, latest)
	}
}
