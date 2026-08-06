package assets

import (
	"context"
	"testing"
	"time"
)

func TestFactionProfileFromDecodesTreasury(t *testing.T) {
	raw := []byte(`{"id":"fac1","name":"Iron Compact","tag":"IRON","leader_id":"L1",` +
		`"member_count":7,"owned_bases":2,"treasury":329427,"is_member":true}`)
	got, ok, err := FactionProfileFrom(raw)
	if err != nil || !ok {
		t.Fatalf("FactionProfileFrom = ok %v err %v", ok, err)
	}
	if got.FactionID != "fac1" || got.Treasury != 329427 || got.MemberCount != 7 {
		t.Errorf("decoded = %+v", got)
	}
}

func TestFactionStorageFromDecodesFuelBunker(t *testing.T) {
	raw := []byte(`{"faction_id":"fac1","base_id":"central_nexus","credits":1000,` +
		`"faction_fuel_reserve":4200,"faction_fuel_capacity":50000,` +
		`"hint":"9 items in storage at central_nexus",` +
		`"items":[{"item_id":"iron_ore","name":"Iron Ore","quantity":9,"size":1}]}`)
	got, hint, ok, err := FactionStorageFrom(raw)
	if err != nil || !ok {
		t.Fatalf("FactionStorageFrom = ok %v err %v", ok, err)
	}
	if got.FuelReserve != 4200 || got.FuelCapacity != 50000 {
		t.Errorf("fuel bunker = %d/%d, want 4200/50000", got.FuelReserve, got.FuelCapacity)
	}
	if hint != "9 items in storage at central_nexus" {
		t.Errorf("hint = %q", hint)
	}
}

// TestReplaceFactionStorageDropsVanishedBases pins the same two deletion grains
// as the agent tables, keyed on faction_id.
func TestReplaceFactionStorageDropsVanishedBases(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)

	first := []FactionStorageBase{
		{BaseID: "A", Items: []StorageItem{{ItemID: "x", Quantity: 5}}},
		{BaseID: "B", Items: []StorageItem{{ItemID: "y", Quantity: 7}}},
	}
	if err := st.ReplaceFactionStorage(ctx, "fac1", first, now); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := st.ReplaceFactionStorage(ctx, "fac1", first[:1], now.Add(time.Hour)); err != nil {
		t.Fatalf("second: %v", err)
	}

	var bases, items int
	if err := st.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM faction_storage WHERE faction_id='fac1'`).Scan(&bases); err != nil {
		t.Fatalf("count bases: %v", err)
	}
	if err := st.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM faction_storage_items WHERE faction_id='fac1'`).Scan(&items); err != nil {
		t.Fatalf("count items: %v", err)
	}
	if bases != 1 || items != 1 {
		t.Errorf("bases=%d items=%d, want 1/1", bases, items)
	}
}

func TestUpsertFactionProfileOverwrites(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)

	p := FactionProfile{FactionID: "fac1", Name: "Iron Compact", Treasury: 100}
	if err := st.UpsertFactionProfile(ctx, p, now); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	p.Treasury = 250
	if err := st.UpsertFactionProfile(ctx, p, now.Add(time.Hour)); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	var treasury, n int
	if err := st.DB().QueryRowContext(ctx,
		`SELECT COUNT(*), MAX(treasury) FROM faction_profile WHERE faction_id='fac1'`).Scan(&n, &treasury); err != nil {
		t.Fatalf("read: %v", err)
	}
	if n != 1 || treasury != 250 {
		t.Errorf("rows=%d treasury=%d, want 1/250", n, treasury)
	}
}
