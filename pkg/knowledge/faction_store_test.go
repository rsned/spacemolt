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
