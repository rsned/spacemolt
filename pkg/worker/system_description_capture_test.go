package worker

import (
	"context"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// v0.576.0 gave the nine starless systems chart descriptions. The description
// rides in on get_system and is the only place it appears, so the capture path
// must carry it into the KB — every builder previously dropped the field, which
// is why systems.description held 0 rows across all 505 systems.
func TestKBUpdateSystem_CapturesDescription(t *testing.T) {
	const lore = "Navigators keep this dark waypoint charted for its prospecting drift."
	st := &game.State{}
	st.System.ID = "redmarsh"
	st.System.Name = "Redmarsh"
	st.System.Description = lore

	kb, err := knowledge.NewSQLiteKB(knowledge.Config{DBPath: ":memory:"})
	if err != nil {
		t.Fatalf("NewSQLiteKB: %v", err)
	}
	t.Cleanup(func() { _ = kb.Close() })

	ctx := context.Background()
	if err := KBUpdateSystem(ctx, &fakeClient{state: st}, kb, "tester"); err != nil {
		t.Fatalf("KBUpdateSystem: %v", err)
	}

	got, err := kb.GetSystem(ctx, "redmarsh")
	if err != nil {
		t.Fatalf("GetSystem: %v", err)
	}
	if got.Description != lore {
		t.Errorf("stored description = %q, want %q", got.Description, lore)
	}
}
