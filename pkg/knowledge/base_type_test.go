package knowledge

import (
	"context"
	"testing"
)

// The base type (station | outpost) is written by stations-refresh, never by
// RememberBase, and must come back on both base reads so routing can refuse an
// outpost: outposts are members-only and refuse an outsider's dock.
func TestGetBase_ReturnsStoredType(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()

	if err := kb.RememberBase(ctx, SpaceBase{ID: "b1", POIID: "p1", Name: "Ironlight Ferrous Range", PublicAccess: true}); err != nil {
		t.Fatalf("RememberBase: %v", err)
	}
	if _, err := kb.db.ExecContext(ctx, `UPDATE bases SET type = 'outpost' WHERE id = 'b1'`); err != nil {
		t.Fatalf("set type: %v", err)
	}
	// A later RememberBase (a dock refresh) must not blank the stored type.
	if err := kb.RememberBase(ctx, SpaceBase{ID: "b1", POIID: "p1", Name: "Ironlight Ferrous Range", PublicAccess: true}); err != nil {
		t.Fatalf("RememberBase again: %v", err)
	}

	byPOI, err := kb.GetBaseByPOI(ctx, "p1")
	if err != nil {
		t.Fatalf("GetBaseByPOI: %v", err)
	}
	byID, err := kb.GetBase(ctx, "b1")
	if err != nil {
		t.Fatalf("GetBase: %v", err)
	}
	for name, b := range map[string]*SpaceBase{"GetBaseByPOI": byPOI, "GetBase": byID} {
		if b.Type != "outpost" || !b.IsOutpost() {
			t.Errorf("%s: Type = %q, IsOutpost = %v; want outpost", name, b.Type, b.IsOutpost())
		}
	}
}
