package main

import (
	"math"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// The first live drift: bearing 51 from Haven, 3 ticks out, with Market
// Prime 377 units ahead and 2.7 off the line.
func TestDriftTargetsFirstLiveDrift(t *testing.T) {
	systems := []knowledge.System{
		{ID: "haven", Name: "Haven", Position: game.Position{X: 1999.99, Y: -4500.0}},
		{ID: "market_prime", Name: "Market Prime", Position: game.Position{X: 2254.06393880684, Y: -4181.90470912945}},
		{ID: "far_off", Position: game.Position{X: 9000, Y: 9000}},
	}
	x, y := 2018.879611731495, -4476.685621156291

	ahead, nearest := driftTargets(systems, 51, x, y)
	if len(ahead) != 1 || ahead[0].ID != "market_prime" {
		t.Fatalf("ahead = %+v, want only market_prime", ahead)
	}
	if math.Abs(ahead[0].Along-377.1) > 1 || ahead[0].Off > 5 {
		t.Errorf("market_prime along=%.1f off=%.1f, want ~377 and <5", ahead[0].Along, ahead[0].Off)
	}
	if nearest == nil || nearest.ID != "haven" {
		t.Errorf("nearest = %+v, want haven (just left)", nearest)
	}
	if b := pathfinderBearing(x, y, 2254.06393880684, -4181.90470912945); math.Abs(b-51) > 1.5 {
		t.Errorf("bearing to market_prime = %.1f, want ~51", b)
	}
}

func TestPathfinderBearingWraps(t *testing.T) {
	if b := pathfinderBearing(0, 0, 0, -10); b != 270 {
		t.Errorf("bearing straight down = %v, want 270", b)
	}
}
