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
