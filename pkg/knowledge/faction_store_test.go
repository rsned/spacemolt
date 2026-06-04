package knowledge

import (
	"context"
	"testing"
	"time"
)

func TestStoreAndLoadFaction(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()
	now := time.Now()

	rec := FactionRecord{
		FactionID: "f1", Name: "Crafters Union", Tag: "CRFT",
		LeaderID: "p1", LeaderUsername: "boss", Treasury: 1000,
		MemberCount: 2, OwnedBases: 1, Description: "lore", Charter: "rules",
		FoundedUTC: "2026-01-01T00:00:00Z", CapturedAt: now,
	}
	if err := kb.StoreFaction(ctx, rec); err != nil {
		t.Fatalf("StoreFaction: %v", err)
	}

	if err := kb.ReplaceFactionMembers(ctx, "f1", []FactionMember{
		{FactionID: "f1", PlayerID: "p1", Username: "boss", Role: "Leader", IsOnline: true, CapturedAt: now},
		{FactionID: "f1", PlayerID: "p2", Username: "grunt", Role: "Member", CapturedAt: now},
	}); err != nil {
		t.Fatalf("ReplaceFactionMembers: %v", err)
	}

	if err := kb.ReplaceFactionStorage(ctx, FactionStorageRow{
		FactionID: "f1", BaseID: "b1", Credits: 500, ItemCount: 1,
		Items:      []FactionStorageItem{{ItemID: "iron_ore", Name: "Iron Ore", Quantity: 42, Size: 1}},
		CapturedAt: now,
	}); err != nil {
		t.Fatalf("ReplaceFactionStorage: %v", err)
	}

	view, err := kb.LoadFactionView(ctx, "f1")
	if err != nil {
		t.Fatalf("LoadFactionView: %v", err)
	}
	if view.Faction.Tag != "CRFT" || view.Faction.Treasury != 1000 {
		t.Errorf("faction header wrong: %+v", view.Faction)
	}
	if len(view.Members) != 2 {
		t.Fatalf("want 2 members, got %d", len(view.Members))
	}
	if len(view.Storage) != 1 || len(view.Storage[0].Items) != 1 || view.Storage[0].Items[0].Quantity != 42 {
		t.Fatalf("storage roundtrip wrong: %+v", view.Storage)
	}

	// Replace-within-scope: re-store members with only one; the removed one must vanish.
	if err := kb.ReplaceFactionMembers(ctx, "f1", []FactionMember{
		{FactionID: "f1", PlayerID: "p1", Username: "boss", Role: "Leader", IsOnline: true, CapturedAt: now},
	}); err != nil {
		t.Fatalf("ReplaceFactionMembers (2): %v", err)
	}
	view, err = kb.LoadFactionView(ctx, "f1")
	if err != nil {
		t.Fatalf("LoadFactionView (2): %v", err)
	}
	if len(view.Members) != 1 {
		t.Errorf("want 1 member after replace, got %d", len(view.Members))
	}

	ids, err := kb.ListFactionIDs(ctx)
	if err != nil || len(ids) != 1 || ids[0] != "f1" {
		t.Errorf("ListFactionIDs wrong: %v err=%v", ids, err)
	}
}

func TestFactionCapturedAt(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()

	// Missing faction -> exists=false, no error.
	if _, ok, err := kb.FactionCapturedAt(ctx, "nope"); err != nil || ok {
		t.Fatalf("missing faction: got ok=%v err=%v, want ok=false err=nil", ok, err)
	}

	want := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	if err := kb.StoreFaction(ctx, FactionRecord{FactionID: "f1", Name: "Crafters", Tag: "CRFT", CapturedAt: want}); err != nil {
		t.Fatalf("StoreFaction: %v", err)
	}

	got, ok, err := kb.FactionCapturedAt(ctx, "f1")
	if err != nil || !ok {
		t.Fatalf("stored faction: got ok=%v err=%v, want ok=true", ok, err)
	}
	if !got.Equal(want) {
		t.Errorf("captured_at = %v, want %v", got, want)
	}
}

func TestUpsertFactionListEntry(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()

	// Fresh insert: reports inserted, and lands a deliberately stale captured_utc
	// so the backfiller will still enrich the row with full faction_info later.
	inserted, err := kb.UpsertFactionListEntry(ctx, FactionListEntry{
		FactionID: "f1", Name: "Data Bots", Tag: "DB", LeaderUsername: "databot",
		MemberCount: 1, OwnedBases: 0, PrimaryColor: "#FFFFFF", SecondaryColor: "#000000",
	})
	if err != nil || !inserted {
		t.Fatalf("insert: inserted=%v err=%v, want inserted=true", inserted, err)
	}
	view, err := kb.LoadFactionView(ctx, "f1")
	if err != nil || view == nil {
		t.Fatalf("LoadFactionView: %v (nil=%v)", err, view == nil)
	}
	if view.Faction.Name != "Data Bots" || view.Faction.MemberCount != 1 {
		t.Errorf("seeded header wrong: %+v", view.Faction)
	}
	if !view.Faction.CapturedAt.Before(time.Now().Add(-24 * time.Hour)) {
		t.Errorf("captured_at = %v, want a stale sentinel far in the past", view.Faction.CapturedAt)
	}

	// A faction already captured in full via faction_info: a later lightweight
	// upsert must refresh only the faction_list columns and preserve the rich
	// columns and the real captured_utc.
	rich := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	if err := kb.StoreFaction(ctx, FactionRecord{
		FactionID: "f2", Name: "Musse", Tag: "muse", LeaderID: "p9", LeaderUsername: "Charlotte",
		Treasury: 5000, MemberCount: 4, OwnedBases: 0, Description: "lore", Charter: "rules",
		FoundedUTC: "2026-01-01T00:00:00Z", IntelSystems: 7, IntelTrade: 3, CapturedAt: rich,
	}); err != nil {
		t.Fatalf("StoreFaction f2: %v", err)
	}

	inserted, err = kb.UpsertFactionListEntry(ctx, FactionListEntry{
		FactionID: "f2", Name: "Musse Reborn", Tag: "MUSE", LeaderUsername: "Charlotte",
		MemberCount: 9, OwnedBases: 2, PrimaryColor: "#112233", SecondaryColor: "#445566",
	})
	if err != nil || inserted {
		t.Fatalf("update: inserted=%v err=%v, want inserted=false", inserted, err)
	}
	view, err = kb.LoadFactionView(ctx, "f2")
	if err != nil || view == nil {
		t.Fatalf("LoadFactionView f2: %v (nil=%v)", err, view == nil)
	}
	f := view.Faction
	// Lightweight columns refreshed.
	if f.Name != "Musse Reborn" || f.MemberCount != 9 || f.OwnedBases != 2 || f.PrimaryColor != "#112233" {
		t.Errorf("lightweight columns not refreshed: %+v", f)
	}
	// Rich columns preserved (not clobbered to zero).
	if f.Treasury != 5000 || f.Description != "lore" || f.Charter != "rules" ||
		f.LeaderID != "p9" || f.IntelSystems != 7 || f.IntelTrade != 3 || f.FoundedUTC != "2026-01-01T00:00:00Z" {
		t.Errorf("rich columns clobbered: %+v", f)
	}
	// Real full-capture timestamp preserved.
	if !f.CapturedAt.Equal(rich) {
		t.Errorf("captured_at = %v, want preserved %v", f.CapturedAt, rich)
	}
}
