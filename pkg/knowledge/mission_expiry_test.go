package knowledge

import (
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// expires_in_ticks is a COUNTDOWN on a live board, not a property of the
// template. Diffing it made every capture report the whole board as changed --
// 9 spurious "template changed" lines in one grand_exchange_station read on
// 2026-08-28, all of the form 198 -> 176, which is just 22 ticks passing.
func TestMissionTemplate_CountdownIsNotATemplateChange(t *testing.T) {
	t.Parallel()
	kb := NewMemoryKB()
	ctx := t.Context()
	entry := serverapi.MissionBoardEntry{
		MissionID: "smuggling_courier", TemplateID: "smuggling_courier",
		Title: "Courier Run", Type: "smuggling", ExpiresInTicks: 198,
	}
	if _, err := kb.UpsertMissionTemplate(ctx, entry, "grand_exchange_station", "haven", 100); err != nil {
		t.Fatalf("seed: %v", err)
	}

	entry.ExpiresInTicks = 176 // 22 ticks later, same offer
	res, err := kb.UpsertMissionTemplate(ctx, entry, "grand_exchange_station", "haven", 122)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	for _, d := range res.Diffs {
		if d.Field == "expires_in_ticks" {
			t.Errorf("countdown reported as a template change: %q -> %q", d.OldValue, d.NewValue)
		}
	}
}

// What IS worth keeping is the offer window: how long the mission is offered
// when freshly posted. The largest value ever seen converges on it, since any
// later sighting of the same template is somewhere down its countdown.
func TestMissionTemplate_KeepsTheLongestOfferWindow(t *testing.T) {
	t.Parallel()
	kb := NewMemoryKB()
	ctx := t.Context()
	entry := serverapi.MissionBoardEntry{
		MissionID: "courier", TemplateID: "courier", Title: "Courier", Type: "smuggling",
		ExpiresInTicks: 176, // first seen mid-life
	}
	if _, err := kb.UpsertMissionTemplate(ctx, entry, "grand_exchange_station", "haven", 100); err != nil {
		t.Fatalf("seed: %v", err)
	}
	entry.ExpiresInTicks = 198 // a fresh posting of the same template
	if _, err := kb.UpsertMissionTemplate(ctx, entry, "grand_exchange_station", "haven", 200); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	entry.ExpiresInTicks = 12 // nearly expired; must not shrink what we learned
	if _, err := kb.UpsertMissionTemplate(ctx, entry, "grand_exchange_station", "haven", 300); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	if len(kb.missionCatalog) != 1 {
		t.Fatalf("missionCatalog holds %d rows, want 1", len(kb.missionCatalog))
	}
	var row missionCatalogRow
	for _, r := range kb.missionCatalog {
		row = r
	}
	if row.ExpiresInTicks != 198 {
		t.Errorf("ExpiresInTicks = %d, want 198 (the longest window observed)", row.ExpiresInTicks)
	}
}
