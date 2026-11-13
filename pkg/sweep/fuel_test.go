package sweep

import (
	"reflect"
	"testing"
)

func TestHomeThreshold(t *testing.T) {
	if got := HomeThreshold(23); got != 75 {
		t.Errorf("HomeThreshold(23) = %v, want 75", got)
	}
	if got := HomeThreshold(45); got != 135 {
		t.Errorf("HomeThreshold(45) = %v, want 135", got)
	}
}

func TestCanContinue(t *testing.T) {
	if !CanContinue(40, 5, 0, 45) {
		t.Error("tank 40 + 5 cells = 140 >= 135 should continue")
	}
	if CanContinue(40, 4, 0, 45) {
		t.Error("tank 40 + 4 cells = 120 < 135 should not continue")
	}
}

func TestRefillsNoneWhenTankHealthy(t *testing.T) {
	if got := Refills(100, 1020, 10, 0, 45); got != nil {
		t.Errorf("Refills = %v, want nil", got)
	}
}

func TestRefillsPremiumThenCells(t *testing.T) {
	got := Refills(30, 350, 100, 3, 23)
	want := []Refill{{Item: "premium_fuel_cell", Qty: 3}, {Item: "fuel_cell", Qty: 8}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Refills = %v, want %v", got, want)
	}
}

func TestRefillsNoOverflow(t *testing.T) {
	got := Refills(10, 45, 10, 0, 23)
	want := []Refill{{Item: "fuel_cell", Qty: 1}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Refills = %v, want %v", got, want)
	}
}

func TestMeasuredLaunchCost(t *testing.T) {
	if c, ok := MeasuredLaunchCost(1120, 1075); !ok || c != 45 {
		t.Errorf("(1120,1075) = %v,%v want 45,true", c, ok)
	}
	if _, ok := MeasuredLaunchCost(100, 100); ok {
		t.Error("no change must not measure")
	}
	if _, ok := MeasuredLaunchCost(100, 120); ok {
		t.Error("a rise must not measure")
	}
}
