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
	wrote, err := kb.RecordMeasuredSystemPosition(ctx, "whitecap", "Whitecap", -4790.2, -6130.1)
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
	wrote, err := kb.RecordMeasuredSystemPosition(ctx, "sol", "Sol", 5, 95)
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
	wrote, err := kb.RecordMeasuredSystemPosition(ctx, "new_one", "New One", -6000, 4100)
	if err != nil || !wrote {
		t.Fatalf("wrote=%v err=%v, want true", wrote, err)
	}
	if x, y, src := storedPosition(t, kb, "new_one"); x != -6000 || y != 4100 || src != "measured" {
		t.Fatalf("stored (%v,%v,%q)", x, y, src)
	}
}

// A frontier system first reached by drift has no KB row at all (the map
// capture skips systems without coordinates): the measurement creates it.
// Patient Span's position was silently dropped this way on 2026-11-13.
func TestRecordMeasuredCreatesUnknownSystem(t *testing.T) {
	kb := newTestSQLiteKB(t)
	ctx := context.Background()
	wrote, err := kb.RecordMeasuredSystemPosition(ctx, "patient_span", "Patient Span", -6165, 5004)
	if err != nil || !wrote {
		t.Fatalf("wrote=%v err=%v, want true,nil", wrote, err)
	}
	if x, y, src := storedPosition(t, kb, "patient_span"); x != -6165 || y != 5004 || src != "measured" {
		t.Fatalf("stored (%v,%v,%q), want the measurement", x, y, src)
	}
	if sys, _ := kb.GetSystem(ctx, "patient_span"); sys == nil || sys.Name != "Patient Span" {
		t.Fatalf("system = %+v, want named Patient Span", sys)
	}
}

// The first measurement stands. Later landings are caught anywhere inside
// the capture radius (explorer-7 came in at an angle and read Ash Pearl
// 87 LY off), so they must not overwrite it.
func TestRecordMeasuredKeepsFirstMeasurement(t *testing.T) {
	kb := newTestSQLiteKB(t)
	ctx := context.Background()
	if _, err := kb.RecordMeasuredSystemPosition(ctx, "ash_pearl", "Ash Pearl", -6259, 3300); err != nil {
		t.Fatal(err)
	}
	wrote, err := kb.RecordMeasuredSystemPosition(ctx, "ash_pearl", "Ash Pearl", -6174, 3318)
	if err != nil || wrote {
		t.Fatalf("wrote=%v err=%v, want the second measurement refused", wrote, err)
	}
	if x, y, _ := storedPosition(t, kb, "ash_pearl"); x != -6259 || y != 3300 {
		t.Fatalf("stored (%v,%v), want the first measurement", x, y)
	}
}

func TestMemoryRecordMeasuredFillsZeroKeepsKnown(t *testing.T) {
	kb := NewMemoryKB()
	ctx := context.Background()
	_ = kb.RememberSystem(ctx, System{ID: "blank", Name: "Blank"})
	_ = kb.RememberSystem(ctx, System{ID: "known", Name: "Known", Position: game.Position{X: 10, Y: 10}})
	if wrote, _ := kb.RecordMeasuredSystemPosition(ctx, "blank", "Blank", 7, 8); !wrote {
		t.Error("blank system not written")
	}
	if wrote, _ := kb.RecordMeasuredSystemPosition(ctx, "known", "Known", 7, 8); wrote {
		t.Error("known system overwritten")
	}
	s, _ := kb.GetSystem(ctx, "blank")
	if s.Position.X != 7 || s.Position.Y != 8 {
		t.Errorf("blank position = %+v", s.Position)
	}
	if wrote, _ := kb.RecordMeasuredSystemPosition(ctx, "fresh", "Fresh", 3, 4); !wrote {
		t.Error("unknown system not created")
	}
	if f, _ := kb.GetSystem(ctx, "fresh"); f == nil || f.Name != "Fresh" || f.Position.X != 3 {
		t.Errorf("fresh = %+v, want created with its measurement", f)
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

// Frontier systems report position 0,0 from the API. Once a row holds a real
// position (measured by drift, fitted, triangulated), no later write may put
// it back at 0,0: Sol is the only system at the origin.
func TestNoWriterZeroesALearnedPosition(t *testing.T) {
	ctx := context.Background()
	learned := func(t *testing.T) *SQLiteKB {
		t.Helper()
		kb := newTestSQLiteKB(t)
		if _, err := kb.RecordMeasuredSystemPosition(ctx, "elbow", "Elbow", 1976, -8405); err != nil {
			t.Fatal(err)
		}
		return kb
	}
	for name, write := range map[string]func(*SQLiteKB) error{
		"visit (get_system)": func(kb *SQLiteKB) error {
			return kb.RememberSystem(ctx, System{ID: "elbow", Name: "Elbow", LastUpdatedTick: 2403000})
		},
		"map import": func(kb *SQLiteKB) error {
			return kb.UpsertSystemFromMap(ctx, MapSystemData{ID: "elbow", Name: "Elbow"})
		},
		"zero measurement": func(kb *SQLiteKB) error {
			_, err := kb.RecordMeasuredSystemPosition(ctx, "elbow", "Elbow", 0, 0)
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			kb := learned(t)
			if err := write(kb); err != nil {
				t.Fatal(err)
			}
			if x, y, src := storedPosition(t, kb, "elbow"); x != 1976 || y != -8405 || src != "measured" {
				t.Fatalf("stored (%v,%v,%q), want (1976,-8405,measured)", x, y, src)
			}
		})
	}
}

// A 0,0 reading for a system with no row yet must not invent one at the
// origin through the measurement path.
func TestZeroMeasurementCreatesNoOriginRow(t *testing.T) {
	kb := newTestSQLiteKB(t)
	wrote, err := kb.RecordMeasuredSystemPosition(context.Background(), "kettle", "Kettle", 0, 0)
	if err != nil || wrote {
		t.Fatalf("wrote=%v err=%v, want no write", wrote, err)
	}
}

// Sol itself really is at 0,0 and the map import may say so.
func TestMapImportKeepsSolAtOrigin(t *testing.T) {
	kb := newTestSQLiteKB(t)
	if err := kb.UpsertSystemFromMap(context.Background(), MapSystemData{ID: "sol", Name: "Sol"}); err != nil {
		t.Fatal(err)
	}
	if x, y, _ := storedPosition(t, kb, "sol"); x != 0 || y != 0 {
		t.Fatalf("sol at (%v,%v)", x, y)
	}
}
