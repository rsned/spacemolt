package worker

import (
	"context"
	"encoding/json"
	"io"
	"slices"
	"testing"
)

type checkMod struct {
	ID         string `json:"id"`
	TypeID     string `json:"type_id"`
	Slot       string `json:"slot"`
	CPUUsage   int    `json:"cpu_usage"`
	PowerUsage int    `json:"power_usage"`
}

// checkShip builds a get_ship reply with the live field names.
func checkShip(t *testing.T, cpu, power, utility int, mods ...checkMod) []byte {
	t.Helper()
	cpuUsed, powerUsed := 0, 0
	for _, m := range mods {
		cpuUsed += m.CPUUsage
		powerUsed += m.PowerUsage
	}
	raw, err := json.Marshal(map[string]any{
		"ship": map[string]any{"cpu_used": cpuUsed, "cpu_capacity": cpu, "power_used": powerUsed,
			"power_capacity": power, "utility_slots": utility},
		"modules": mods,
	})
	if err != nil {
		t.Fatal(err)
	}

	return raw
}

func fitCheckClient(raw []byte) (*WorkerDispatch, *fakeClient) {
	fc := &fakeClient{state: havenState(), raw: map[string][]byte{"ship": raw}}

	return &WorkerDispatch{Client: fc, Out: io.Discard}, fc
}

func TestFitCheckReportsModulesWithoutMutating(t *testing.T) {
	d, fc := fitCheckClient(checkShip(t, 13, 26, 2,
		checkMod{ID: "m1", TypeID: "mining_laser_i", Slot: "weapon", CPUUsage: 4, PowerUsage: 5}))

	got, err := d.FitCheck(context.Background(), "advanced_drone_bay")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Fits || len(got.MustStrip) != 1 || got.MustStrip[0].TypeID != "mining_laser_i" {
		t.Fatalf("got %+v", got)
	}
	if !slices.Equal(fc.calls, []string{"get_ship"}) {
		t.Fatalf("fit_check must only read: %v", fc.calls)
	}
}

func TestFitCheckNoUtilitySlot(t *testing.T) {
	d, _ := fitCheckClient(checkShip(t, 20, 30, 0))

	got, err := d.FitCheck(context.Background(), "advanced_drone_bay")
	if err != nil {
		t.Fatal(err)
	}
	if got.Fits {
		t.Fatalf("a hull with no utility slot cannot fit a bay: %+v", got)
	}
}

func TestFitCheckAlreadyFitted(t *testing.T) {
	d, _ := fitCheckClient(checkShip(t, 13, 26, 2,
		checkMod{ID: "b1", TypeID: "advanced_drone_bay", Slot: "utility", CPUUsage: 12, PowerUsage: 15}))

	got, err := d.FitCheck(context.Background(), "advanced_drone_bay")
	if err != nil {
		t.Fatal(err)
	}
	if got.BaysFitted != 1 || len(got.MustStrip) != 0 || !got.Fits {
		t.Fatalf("got %+v", got)
	}
}

func TestFitCheckStripsLargestFirstAndOnlyWhatItNeeds(t *testing.T) {
	d, _ := fitCheckClient(checkShip(t, 18, 34, 4,
		checkMod{ID: "a", TypeID: "cargo_expander_i", Slot: "utility", CPUUsage: 2, PowerUsage: 2},
		checkMod{ID: "b", TypeID: "mining_laser_i", Slot: "weapon", CPUUsage: 6, PowerUsage: 8},
		checkMod{ID: "c", TypeID: "mining_laser_i", Slot: "weapon", CPUUsage: 5, PowerUsage: 8}))

	got, err := d.FitCheck(context.Background(), "advanced_drone_bay")
	if err != nil {
		t.Fatal(err)
	}
	// cpu free 5, needs 12: taking 6 then 5 reaches 16, and the expander stays.
	if !got.Fits || len(got.MustStrip) != 2 || got.MustStrip[0].ID != "b" || got.MustStrip[1].ID != "c" {
		t.Fatalf("got %+v", got)
	}
}

func TestFitCheckCannotFreeEnough(t *testing.T) {
	d, _ := fitCheckClient(checkShip(t, 9, 18, 2,
		checkMod{ID: "h", TypeID: "gas_harvester_i", Slot: "utility", CPUUsage: 3, PowerUsage: 3}))

	got, err := d.FitCheck(context.Background(), "advanced_drone_bay")
	if err != nil {
		t.Fatal(err)
	}
	if got.Fits || len(got.MustStrip) != 0 {
		t.Fatalf("a hull under cpu 12 must not report a strip: %+v", got)
	}
}
