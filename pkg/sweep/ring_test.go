package sweep

import "testing"

func TestNextTargetOnRingSteps5(t *testing.T) {
	s := &State{Ring: 7500, ProgressDeg: 148}
	x, y := RingPoint(7500, 148)
	_, _, deg := NextTarget(s, x, y)
	if deg != 143 {
		t.Fatalf("deg = %v, want 143", deg)
	}
}

func TestNextTargetRejoinsAtSavedAngle(t *testing.T) {
	s := &State{Ring: 7950, ProgressDeg: 148}
	x, y := RingPoint(7582, 148)
	tx, ty, deg := NextTarget(s, x, y)
	if deg != 148 {
		t.Fatalf("deg = %v, want 148 (rejoin without advancing)", deg)
	}
	wx, wy := RingPoint(7950, 148)
	if !near(tx, wx, 0.01) || !near(ty, wy, 0.01) {
		t.Fatalf("target = (%v,%v), want (%v,%v)", tx, ty, wx, wy)
	}
}

func TestAdvanceAcrossWrap(t *testing.T) {
	s := &State{Ring: 7500, ProgressDeg: -178}
	Advance(s, -183)
	if s.SweptDeg != 5 || s.ProgressDeg != -183 {
		t.Fatalf("after wrap: swept %v progress %v, want 5 / -183", s.SweptDeg, s.ProgressDeg)
	}

	lap := &State{Ring: 7500, ProgressDeg: 148}
	for i := range 72 {
		if LapDone(lap) {
			t.Fatalf("lap done after only %d steps", i)
		}
		Advance(lap, lap.ProgressDeg-StepDeg)
	}
	if !LapDone(lap) {
		t.Fatalf("lap not done after 72 steps (swept %v)", lap.SweptDeg)
	}
}

// A partial pass (the bottom arc, 148° to 48°) ends at its span; a state
// without one is a full lap (operator 2026-11-14).
func TestLapDoneHonoursSpan(t *testing.T) {
	s := NewState(7200, 148, 23)
	s.SweptDeg = 99
	s.SpanDeg = 100
	if LapDone(s) {
		t.Error("99° of a 100° span reported done")
	}
	s.SweptDeg = 100
	if !LapDone(s) {
		t.Error("100° of a 100° span not done")
	}
	s.SpanDeg = 0
	if LapDone(s) {
		t.Error("no span: 100° must not end a full lap")
	}
	s.SweptDeg = 360
	if !LapDone(s) {
		t.Error("no span: 360° must end the lap")
	}
}
