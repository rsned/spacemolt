package sweep

import (
	"slices"
	"strings"
	"testing"
)

// Scanner II costs, before any Engineering trim.
const (
	testScannerCPU   = 5
	testScannerPower = 7
)

func fm(id, typeID, slot string, cpu, power int) FitModule {
	return FitModule{ID: id, TypeID: typeID, Name: typeID, Slot: slot, CPU: cpu, Power: power}
}

func removedIDs(p FitPlan) []string {
	ids := make([]string, len(p.Remove))
	for i, m := range p.Remove {
		ids[i] = m.ID
	}
	return ids
}

var drive = fm("m-drive", FitDrive, SlotUtility, 18, 30)

// A Reserve (3 utility slots, CPU 28) has room for the drive and two
// scanners and nothing else.
func TestPlanFitReserveStripsEverythingElse(t *testing.T) {
	fitted := []FitModule{
		drive,
		fm("m-fuel", "fuel_optimizer_i", SlotUtility, 4, 6),
		fm("m-gun", "autocannon_i", SlotWeapon, 6, 8),
		fm("m-shield", "shield_booster_i", SlotDefense, 5, 9),
	}
	p := PlanFit(fitted, 3, 28, 72, testScannerCPU, testScannerPower)
	if !p.OK {
		t.Fatalf("plan not OK: %s", p.Why)
	}
	if got, want := removedIDs(p), []string{"m-fuel", "m-gun", "m-shield"}; !slices.Equal(got, want) {
		t.Errorf("removes %v, want %v (utility, then weapon, then defence)", got, want)
	}
	if !slices.Equal(p.Add, []string{FitScanner, FitScanner}) {
		t.Errorf("adds %v, want two scanners", p.Add)
	}
	if p.CPUAfter != 28 || p.CPUCap != 28 || p.PowerAfter != 44 || p.PowerCap != 72 {
		t.Errorf("after: cpu %d/%d power %d/%d, want 28/28 and 44/72", p.CPUAfter, p.CPUCap, p.PowerAfter, p.PowerCap)
	}
}

// A Compendium (CPU 32) has 4 CPU to spare, so a small defence module stays.
func TestPlanFitCompendiumKeepsASmallDefenceModule(t *testing.T) {
	fitted := []FitModule{
		drive,
		fm("m-fuel", "fuel_optimizer_i", SlotUtility, 4, 6),
		fm("m-gun", "autocannon_i", SlotWeapon, 6, 8),
		fm("m-plate", "armor_plate_i", SlotDefense, 4, 5),
	}
	p := PlanFit(fitted, 3, 32, 80, testScannerCPU, testScannerPower)
	if !p.OK {
		t.Fatalf("plan not OK: %s", p.Why)
	}
	if got, want := removedIDs(p), []string{"m-fuel", "m-gun"}; !slices.Equal(got, want) {
		t.Errorf("removes %v, want %v", got, want)
	}
	if p.CPUAfter != 32 {
		t.Errorf("CPUAfter=%d, want 32", p.CPUAfter)
	}
}

// Nothing is removed that does not have to be.
func TestPlanFitRemovesOnlyWhatIsNeeded(t *testing.T) {
	fitted := []FitModule{
		drive,
		fm("m-gun-big", "autocannon_ii", SlotWeapon, 9, 8),
		fm("m-gun-small", "pulse_laser_i", SlotWeapon, 2, 3),
	}
	// 18 + 10 = 28 of 30: only the big gun has to go.
	p := PlanFit(fitted, 3, 30, 80, testScannerCPU, testScannerPower)
	if !p.OK {
		t.Fatalf("plan not OK: %s", p.Why)
	}
	if got, want := removedIDs(p), []string{"m-gun-big"}; !slices.Equal(got, want) {
		t.Errorf("removes %v, want %v", got, want)
	}
}

func TestPlanFitAlreadyFitted(t *testing.T) {
	fitted := []FitModule{
		drive,
		fm("m-s1", FitScanner, SlotUtility, 5, 7),
		fm("m-s2", FitScanner, SlotUtility, 5, 7),
	}
	p := PlanFit(fitted, 3, 28, 72, testScannerCPU, testScannerPower)
	if !p.OK || len(p.Remove) != 0 || len(p.Add) != 0 {
		t.Errorf("already-fitted ship got plan %+v, want an empty OK plan", p)
	}
}

func TestPlanFitOneScannerAlreadyFitted(t *testing.T) {
	fitted := []FitModule{
		drive,
		fm("m-s1", FitScanner, SlotUtility, 5, 7),
		fm("m-old", "survey_scanner_i", SlotUtility, 3, 4),
	}
	p := PlanFit(fitted, 3, 28, 72, testScannerCPU, testScannerPower)
	if !p.OK {
		t.Fatalf("plan not OK: %s", p.Why)
	}
	if !slices.Equal(p.Add, []string{FitScanner}) {
		t.Errorf("adds %v, want one scanner", p.Add)
	}
	if got, want := removedIDs(p), []string{"m-old"}; !slices.Equal(got, want) {
		t.Errorf("removes %v, want the Scanner I to free the slot", got)
	}
}

func TestPlanFitNeedsTheDrive(t *testing.T) {
	p := PlanFit([]FitModule{fm("m-gun", "autocannon_i", SlotWeapon, 6, 8)}, 3, 28, 72, testScannerCPU, testScannerPower)
	if p.OK || !strings.Contains(p.Why, FitDrive) {
		t.Errorf("ship without a drive: OK=%v Why=%q", p.OK, p.Why)
	}
}

// A hull too small even when stripped is reported, not half-planned.
func TestPlanFitHullTooSmall(t *testing.T) {
	p := PlanFit([]FitModule{drive}, 2, 28, 72, testScannerCPU, testScannerPower)
	if p.OK || !strings.Contains(p.Why, "utility slot") {
		t.Errorf("2-slot hull: OK=%v Why=%q", p.OK, p.Why)
	}
	p = PlanFit([]FitModule{drive}, 3, 24, 72, testScannerCPU, testScannerPower)
	if p.OK || !strings.Contains(p.Why, "CPU") {
		t.Errorf("24-CPU hull: OK=%v Why=%q", p.OK, p.Why)
	}
}
