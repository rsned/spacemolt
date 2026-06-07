package fitting

import "testing"

// cobble is a small hull: 12 CPU, 24 power, 1 weapon / 1 defense / 2 utility slots.
func cobble() Ship {
	return Ship{ID: "cobble", Name: "Cobble", CPUCapacity: 12, PowerCapacity: 24,
		WeaponSlots: 1, DefenseSlots: 1, UtilitySlots: 2, Tier: 0,
		DefaultModules: []string{"mining_laser_i"}}
}

// droneBay: utility module, 12 CPU / 15 power, no bonuses.
func droneBay() Module {
	return Module{ID: "advanced_drone_bay", Name: "Advanced Drone Bay",
		Type: "utility", Slot: "utility", CPUUsage: 12, PowerUsage: 15}
}

// reactor: utility module, consumes 14 CPU / 8 power, adds 30 power capacity.
func reactor() Module {
	return Module{ID: "nuclear_reactor_ii", Name: "Nuclear Reactor II",
		Type: "utility", Slot: "utility", CPUUsage: 14, PowerUsage: 8, PowerBonus: 30,
		RequiredSkills: map[string]int{"mining": 8}}
}

func TestMaxFit_CPULimited(t *testing.T) {
	// Real cobble (CPU 12) + drone bay (CPU 12): 1 fits; the 2nd needs CPU 24 > 12.
	// CPU is checked before power, so CPU is the binding constraint.
	got := MaxFit(cobble(), droneBay(), DefaultEngineering(0))
	if got.MaxCount != 1 {
		t.Fatalf("MaxCount = %d, want 1", got.MaxCount)
	}
	if got.BindingConstraint != "CPU" {
		t.Errorf("BindingConstraint = %q, want %q", got.BindingConstraint, "CPU")
	}
	if !got.Fits {
		t.Errorf("Fits = false, want true")
	}
}

func TestMaxFit_PowerLimited(t *testing.T) {
	// Hull with ample CPU but tight power, so power is the clean binding limit.
	hull := Ship{ID: "ph", Name: "PowerHull", CPUCapacity: 100, PowerCapacity: 24, UtilitySlots: 2}
	mod := Module{ID: "ph_mod", Name: "Mod", Type: "utility", Slot: "utility", CPUUsage: 1, PowerUsage: 15}
	got := MaxFit(hull, mod, DefaultEngineering(0))
	if got.MaxCount != 1 {
		t.Fatalf("MaxCount = %d, want 1", got.MaxCount)
	}
	if got.BindingConstraint != "power" {
		t.Errorf("BindingConstraint = %q, want %q", got.BindingConstraint, "power")
	}
}

func TestMaxFit_SlotLimited(t *testing.T) {
	// A tiny no-cost module limited purely by the 2 utility slots.
	cheap := Module{ID: "x", Name: "X", Type: "utility", Slot: "utility", CPUUsage: 0, PowerUsage: 0}
	got := MaxFit(cobble(), cheap, DefaultEngineering(0))
	if got.MaxCount != 2 {
		t.Fatalf("MaxCount = %d, want 2", got.MaxCount)
	}
	if got.BindingConstraint != "utility slots" {
		t.Errorf("BindingConstraint = %q, want %q", got.BindingConstraint, "utility slots")
	}
}

func TestMaxFit_EngineeringRaisesCount(t *testing.T) {
	// A power-only-limited module to show engineering raising the count cleanly.
	powerHog := Module{ID: "p", Name: "P", Type: "utility", Slot: "utility", PowerUsage: 15}
	// At eng 0: 24/15 -> 1 fits. At eng 20: 15*0.8=12 -> 24/12 = 2 fit (2 slots).
	if got := MaxFit(cobble(), powerHog, DefaultEngineering(0)); got.MaxCount != 1 {
		t.Fatalf("eng0 MaxCount = %d, want 1", got.MaxCount)
	}
	if got := MaxFit(cobble(), powerHog, DefaultEngineering(20)); got.MaxCount != 2 {
		t.Fatalf("eng20 MaxCount = %d, want 2", got.MaxCount)
	}
}

func TestMaxFit_ReactorAddsBudget(t *testing.T) {
	// Reactor: per copy +30 power capacity, costs 8 power / 14 CPU.
	// CPU is the limiter: cap 12, each costs 14 -> 0 fit. Verify CPU binding.
	got := MaxFit(cobble(), reactor(), DefaultEngineering(0))
	if got.MaxCount != 0 {
		t.Fatalf("MaxCount = %d, want 0 (CPU-limited)", got.MaxCount)
	}
	if got.BindingConstraint != "CPU" {
		t.Errorf("BindingConstraint = %q, want CPU", got.BindingConstraint)
	}
	if got.Fits {
		t.Errorf("Fits = true, want false")
	}
}

func TestMaxFit_ReactorBudgetIterative(t *testing.T) {
	// A high-power hull where a self-powering reactor fits multiple times and
	// its power_bonus matters. Hull: 100 CPU, 10 power, 4 utility slots.
	hull := Ship{ID: "h", CPUCapacity: 100, PowerCapacity: 10, UtilitySlots: 4}
	// Each reactor: cpu 10, power 5, power_bonus +20. Net power per copy: +15.
	r := Module{ID: "r", Type: "utility", Slot: "utility", CPUUsage: 10, PowerUsage: 5, PowerBonus: 20}
	// copy1: power used 5 <= 10+20=30 ok, cpu 10<=100. copy2: power 10 <= 50 ok, cpu 20.
	// copy3: power 15 <= 70 ok. copy4: power 20 <= 90 ok, cpu 40. -> 4 fit (slot-limited).
	got := MaxFit(hull, r, DefaultEngineering(0))
	if got.MaxCount != 4 {
		t.Fatalf("MaxCount = %d, want 4", got.MaxCount)
	}
	if got.BindingConstraint != "utility slots" {
		t.Errorf("BindingConstraint = %q, want utility slots", got.BindingConstraint)
	}
}

func TestMaxFit_SkillWarnings(t *testing.T) {
	got := MaxFit(cobble(), reactor(), DefaultEngineering(0))
	if len(got.SkillWarnings) != 1 || got.SkillWarnings[0] != "requires mining 8" {
		t.Errorf("SkillWarnings = %v, want [requires mining 8]", got.SkillWarnings)
	}
}
