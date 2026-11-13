package sweep

import "math"

const (
	// FuelCellRestore and PremiumCellRestore are the fuel one cell restores.
	FuelCellRestore    = 20
	PremiumCellRestore = 50
	// MinHomeFuel is the operator's end-of-lap floor: at or below it the
	// ship makes its final jump toward the nearest empire station.
	MinHomeFuel = 75.0

	itemFuelCell    = "fuel_cell"
	itemPremiumCell = "premium_fuel_cell"
)

// Total is all the fuel a ship carries: tank plus every cell in the hold.
func Total(tank float64, cells, premium int) float64 {
	return tank + float64(FuelCellRestore*cells+PremiumCellRestore*premium)
}

// HomeThreshold is the fuel below which the ship must head home: enough for
// this launch, one more, and the final station jump, and never under 75.
func HomeThreshold(launchCost float64) float64 {
	return math.Max(MinHomeFuel, 3*launchCost)
}

// CanContinue reports whether the ship may make another lap launch without
// eating into its way home. This is what keeps a ship from dying adrift.
func CanContinue(tank float64, cells, premium int, launchCost float64) bool {
	return Total(tank, cells, premium) >= HomeThreshold(launchCost)
}

// Refill is one use_item call.
type Refill struct {
	Item string
	Qty  int
}

// Refills is the cells to burn before a launch: none while the tank holds
// two launches, otherwise as much as fits without overflow, premium cells
// first (they free more hold space per fuel).
func Refills(tank, maxTank float64, cells, premium int, launchCost float64) []Refill {
	if tank >= 2*launchCost {
		return nil
	}
	room := maxTank - tank
	var out []Refill
	if p := min(premium, int(room/PremiumCellRestore)); p > 0 {
		out = append(out, Refill{Item: itemPremiumCell, Qty: p})
		room -= float64(p * PremiumCellRestore)
	}
	if c := min(cells, int(room/FuelCellRestore)); c > 0 {
		out = append(out, Refill{Item: itemFuelCell, Qty: c})
	}
	return out
}

// MeasuredLaunchCost is the fuel one launch took, from readings before and
// after; ok is false when the readings do not show a plausible charge.
func MeasuredLaunchCost(before, after float64) (float64, bool) {
	c := before - after
	if c <= 0 || c > before {
		return 0, false
	}
	return c, true
}
