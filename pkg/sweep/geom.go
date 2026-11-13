// Package sweep holds the pure, I/O-free logic of the pathfinder frontier
// sweep: ring geometry, clockwise progress, the fuel budget and the persisted
// per-agent state. The worker verb pathfinder_sweep drives it.
//
// Spec: docs/superpowers/specs/2026-11-12-pathfinder-frontier-sweep-design.md
package sweep

import "math"

const (
	// StepDeg is the arc a ship covers between course changes.
	StepDeg = 5.0
	// RejoinTolerance (LY) is half the 50 LY ring spacing: a ship further
	// than this from its ring is off it and must rejoin before stepping on.
	RejoinTolerance = 25.0
	// DriftPerTick is the measured Pathfinder drift speed in map units (LY).
	DriftPerTick = 10.0
)

// Polar returns the distance from the galaxy centre and the angle in
// degrees, counter-clockwise from +x, in (-180, 180].
func Polar(x, y float64) (r, deg float64) {
	return math.Hypot(x, y), math.Atan2(y, x) * 180 / math.Pi
}

// RingPoint is the map position at angle deg on a ring of the given radius.
// deg may be unwrapped (below -180); only its cosine and sine matter.
func RingPoint(radius, deg float64) (x, y float64) {
	rad := deg * math.Pi / 180
	return radius * math.Cos(rad), radius * math.Sin(rad)
}

// BearingTo is the Pathfinder bearing from (x0,y0) to (x1,y1): degrees
// counter-clockwise from +x, in [0, 360).
func BearingTo(x0, y0, x1, y1 float64) float64 {
	b := math.Atan2(y1-y0, x1-x0) * 180 / math.Pi
	if b < 0 {
		b += 360
	}
	if b >= 360 {
		b -= 360
	}
	return b
}

// Reached reports whether a ship at (x,y) drifting on bearing has reached
// the target (tx,ty): within one tick of drift, or already past it (the
// target lies behind along the bearing). A passed target is reached, never
// chased back: turning round would cost a launch and re-sweep the arc.
func Reached(x, y, tx, ty, bearing float64) bool {
	dx, dy := tx-x, ty-y
	if math.Hypot(dx, dy) <= DriftPerTick {
		return true
	}
	rad := bearing * math.Pi / 180
	return dx*math.Cos(rad)+dy*math.Sin(rad) <= 0
}
