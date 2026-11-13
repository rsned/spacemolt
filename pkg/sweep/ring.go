package sweep

import "math"

// NextTarget is the ring point the ship at (x,y) should head for next. On
// its ring (within RejoinTolerance) that is StepDeg further clockwise than
// the progress angle; off it, the ring point at the saved progress angle
// itself, so a rejoin neither skips nor re-sweeps arc.
func NextTarget(s *State, x, y float64) (tx, ty, deg float64) {
	r, _ := Polar(x, y)
	deg = s.ProgressDeg
	if math.Abs(r-s.Ring) <= RejoinTolerance {
		deg -= StepDeg
	}
	tx, ty = RingPoint(s.Ring, deg)
	return tx, ty, deg
}

// Advance records that the ship reached the ring point at deg. Progress is
// unwrapped: clockwise means it only ever decreases, past -180 as needed.
func Advance(s *State, deg float64) {
	s.SweptDeg += s.ProgressDeg - deg
	s.ProgressDeg = deg
}

// LapDone reports whether the ship has swept the full circle.
func LapDone(s *State) bool { return s.SweptDeg >= 360 }
