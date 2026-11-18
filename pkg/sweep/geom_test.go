package sweep

import (
	"math"
	"testing"
)

func near(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

func TestPolarFacingAway(t *testing.T) {
	r, deg := Polar(-6439, 4003)
	if !near(r, 7581.9, 1) || !near(deg, 148.1, 0.1) {
		t.Fatalf("Polar = (%v, %v), want (~7581.9, ~148.1)", r, deg)
	}
}

func TestBearingToAxes(t *testing.T) {
	cases := []struct {
		x, y, want float64
	}{{1, 0, 0}, {0, 1, 90}, {-1, 0, 180}, {0, -1, 270}}
	for _, c := range cases {
		if got := BearingTo(0, 0, c.x, c.y); !near(got, c.want, 1e-9) {
			t.Errorf("BearingTo(0,0,%v,%v) = %v, want %v", c.x, c.y, got, c.want)
		}
	}
}

func TestReachedWithinOneTick(t *testing.T) {
	if !Reached(0, 0, 9, 0, 0) {
		t.Error("9 LY ahead should count as reached")
	}
	if Reached(0, 0, 50, 0, 0) {
		t.Error("50 LY ahead should not count as reached")
	}
}

func TestReachedWhenPassed(t *testing.T) {
	if !Reached(60, 0, 50, 0, 0) {
		t.Error("a target 10 LY behind along the bearing should count as reached")
	}
}

func TestInterrupts(t *testing.T) {
	for _, tc := range []struct {
		name   string
		px, py float64
		want   bool
	}{
		{"on the line, short of the target", 500, 20, true},
		{"beside the line, outside the margin", 500, 150, false},
		{"behind the origin", -50, 0, false},
		{"beyond the target", 900, 0, false},
		{"just inside the margin", 300, -99, true},
	} {
		if got := Interrupts(0, 0, 700, 0, tc.px, tc.py, 100); got != tc.want {
			t.Errorf("%s: Interrupts = %v, want %v", tc.name, got, tc.want)
		}
	}
}
