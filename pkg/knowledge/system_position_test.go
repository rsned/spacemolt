package knowledge

import (
	"context"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

func storedPosition(t *testing.T, kb *SQLiteKB, id string) (x, y float64, source string) {
	t.Helper()
	if err := kb.DB().QueryRow(`SELECT position_x, position_y, position_source FROM systems WHERE id = ?`, id).Scan(&x, &y, &source); err != nil {
		t.Fatal(err)
	}
	return x, y, source
}

// A frontier system holds the operator's estimate; a measured drift reading
// replaces it and marks the row measured.
func TestRecordMeasuredOverwritesEstimate(t *testing.T) {
	kb := newTestSQLiteKB(t)
	ctx := context.Background()
	if err := kb.RememberSystem(ctx, System{ID: "whitecap", Name: "Whitecap", Position: game.Position{X: -4788, Y: -6131}}); err != nil {
		t.Fatal(err)
	}
	if _, err := kb.DB().Exec(`UPDATE systems SET position_source = 'estimate' WHERE id = 'whitecap'`); err != nil {
		t.Fatal(err)
	}
	wrote, err := kb.RecordMeasuredSystemPosition(ctx, "whitecap", -4790.2, -6130.1)
	if err != nil || !wrote {
		t.Fatalf("wrote=%v err=%v, want true", wrote, err)
	}
	x, y, src := storedPosition(t, kb, "whitecap")
	if x != -4790.2 || y != -6130.1 || src != "measured" {
		t.Fatalf("stored (%v,%v,%q), want (-4790.2,-6130.1,measured)", x, y, src)
	}
}

// An API position is exact; a drift reading (up to one tick off) never
// replaces it.
func TestRecordMeasuredKeepsAPI(t *testing.T) {
	kb := newTestSQLiteKB(t)
	ctx := context.Background()
	if err := kb.RememberSystem(ctx, System{ID: "sol", Name: "Sol", Position: game.Position{X: 0, Y: 100}}); err != nil {
		t.Fatal(err)
	}
	wrote, err := kb.RecordMeasuredSystemPosition(ctx, "sol", 5, 95)
	if err != nil || wrote {
		t.Fatalf("wrote=%v err=%v, want false", wrote, err)
	}
	x, y, src := storedPosition(t, kb, "sol")
	if x != 0 || y != 100 || src != "api" {
		t.Fatalf("stored (%v,%v,%q), want (0,100,api)", x, y, src)
	}
}

// A system first seen by lane has no coordinates at all; any measurement
// beats (0,0).
func TestRecordMeasuredFillsZero(t *testing.T) {
	kb := newTestSQLiteKB(t)
	ctx := context.Background()
	if err := kb.RememberSystem(ctx, System{ID: "new_one", Name: "New One"}); err != nil {
		t.Fatal(err)
	}
	wrote, err := kb.RecordMeasuredSystemPosition(ctx, "new_one", -6000, 4100)
	if err != nil || !wrote {
		t.Fatalf("wrote=%v err=%v, want true", wrote, err)
	}
	if x, y, src := storedPosition(t, kb, "new_one"); x != -6000 || y != 4100 || src != "measured" {
		t.Fatalf("stored (%v,%v,%q)", x, y, src)
	}
}

func TestRecordMeasuredUnknownSystem(t *testing.T) {
	kb := newTestSQLiteKB(t)
	wrote, err := kb.RecordMeasuredSystemPosition(context.Background(), "nowhere", 1, 2)
	if err != nil || wrote {
		t.Fatalf("wrote=%v err=%v, want false,nil", wrote, err)
	}
}

func TestMemoryRecordMeasuredFillsZeroKeepsKnown(t *testing.T) {
	kb := NewMemoryKB()
	ctx := context.Background()
	_ = kb.RememberSystem(ctx, System{ID: "blank", Name: "Blank"})
	_ = kb.RememberSystem(ctx, System{ID: "known", Name: "Known", Position: game.Position{X: 10, Y: 10}})
	if wrote, _ := kb.RecordMeasuredSystemPosition(ctx, "blank", 7, 8); !wrote {
		t.Error("blank system not written")
	}
	if wrote, _ := kb.RecordMeasuredSystemPosition(ctx, "known", 7, 8); wrote {
		t.Error("known system overwritten")
	}
	s, _ := kb.GetSystem(ctx, "blank")
	if s.Position.X != 7 || s.Position.Y != 8 {
		t.Errorf("blank position = %+v", s.Position)
	}
}

func TestSystemPositionSource(t *testing.T) {
	kb := newTestSQLiteKB(t)
	ctx := context.Background()
	if err := kb.RememberSystem(ctx, System{ID: "sol", Name: "Sol", Position: game.Position{X: 0, Y: 100}}); err != nil {
		t.Fatal(err)
	}
	if src, err := kb.SystemPositionSource(ctx, "sol"); err != nil || src != "api" {
		t.Errorf("sol source = %q, %v; want api", src, err)
	}
	if src, err := kb.SystemPositionSource(ctx, "nowhere"); err != nil || src != "" {
		t.Errorf("unknown source = %q, %v; want empty", src, err)
	}
}
