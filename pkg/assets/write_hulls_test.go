package assets

import (
	"context"
	"testing"
	"time"
)

// TestReplaceHullsDropsSoldShips pins the replacement invariant for hulls: a
// ship the agent no longer owns must disappear. A phantom hull would make the
// ledger claim capacity the agent does not have.
func TestReplaceHullsDropsSoldShips(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)

	first := []Hull{
		{ShipID: "s1", ClassID: "survey_vessel", FuelCurrent: 1020, FuelMax: 1020, FuelRaw: "1020/1020"},
		{ShipID: "s2", ClassID: "reclaim", FuelCurrent: 150, FuelMax: 200, FuelRaw: "150/200"},
	}
	if err := st.ReplaceHulls(ctx, "abc123", first, now); err != nil {
		t.Fatalf("first ReplaceHulls: %v", err)
	}
	if err := st.ReplaceHulls(ctx, "abc123", first[:1], now.Add(time.Hour)); err != nil {
		t.Fatalf("second ReplaceHulls: %v", err)
	}

	var n int
	if err := st.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM agent_hulls WHERE player_id = ?`, "abc123").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("agent_hulls rows = %d, want 1 (s2 must be deleted)", n)
	}
}

// TestReplaceHullsKeepsRawStrings pins that the raw current/max strings are
// persisted next to the parsed ints.
func TestReplaceHullsKeepsRawStrings(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)

	rows := []Hull{{
		ShipID: "s1", ClassID: "reclaim", ClassName: "Reclaim", IsActive: true,
		HullCurrent: 180, HullMax: 180, HullRaw: "180/180",
		FuelCurrent: 150, FuelMax: 200, FuelRaw: "150/200",
		LocationBaseID: "grand_exchange_station", Modules: 2,
	}}
	if err := st.ReplaceHulls(ctx, "abc123", rows, now); err != nil {
		t.Fatalf("ReplaceHulls: %v", err)
	}

	var (
		fuelRaw string
		fuelCur int
		base    string
	)
	if err := st.DB().QueryRowContext(ctx,
		`SELECT fuel_raw, fuel_current, location_base_id FROM agent_hulls
		 WHERE player_id = ? AND ship_id = ?`, "abc123", "s1").
		Scan(&fuelRaw, &fuelCur, &base); err != nil {
		t.Fatalf("read: %v", err)
	}
	if fuelRaw != "150/200" || fuelCur != 150 || base != "grand_exchange_station" {
		t.Errorf("got fuel_raw=%q fuel_current=%d base=%q", fuelRaw, fuelCur, base)
	}
}
