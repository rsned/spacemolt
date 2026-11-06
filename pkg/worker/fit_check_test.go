package worker

import (
	"context"
	"encoding/json"
	"io"
	"slices"
	"strings"
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

func namedStripDispatch(t *testing.T, role string, mods ...[2]string) (*WorkerDispatch, *fakeClient) {
	t.Helper()
	d, fc := fitDronesDispatch(t)
	d.Role = role
	fc.raw = map[string][]byte{"ship": shipJSON(mods...)}

	return d, fc
}

func TestFitDronesStripsNamedTypes(t *testing.T) {
	d, fc := namedStripDispatch(t, "resident",
		[2]string{"a", "mining_laser_i"}, [2]string{"b", "pulse_laser_i"}, [2]string{"c", "cargo_expander_i"})

	err := d.FitDrones(context.Background(), "mine_asteroid", 1, 2, "", "", FitDronesOpts{
		StripTypes: []string{"mining_laser_i", "pulse_laser_i"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"uninstall_mod:a", "uninstall_mod:b"} {
		if !slices.Contains(fc.calls, want) {
			t.Fatalf("missing %s in %v", want, fc.calls)
		}
	}
	if slices.Contains(fc.calls, "uninstall_mod:c") {
		t.Fatalf("stripped an unnamed module: %v", fc.calls)
	}
	last := slices.Index(fc.calls, "uninstall_mod:b")
	if install := slices.Index(fc.calls, "install_mod:advanced_drone_bay"); install < last {
		t.Fatalf("bay must install after the strip: %v", fc.calls)
	}
}

func TestFitDronesRefusesHarvesterOnGasRole(t *testing.T) {
	for _, tc := range []struct{ role, module string }{
		{"resident_gas", "gas_harvester_i"},
		{"resident_ice", "ice_harvester_i"},
	} {
		t.Run(tc.role, func(t *testing.T) {
			d, fc := namedStripDispatch(t, tc.role, [2]string{"h", tc.module})

			err := d.FitDrones(context.Background(), "mine_gas", 1, 2, "", "", FitDronesOpts{StripTypes: []string{tc.module}})
			if err == nil {
				t.Fatal("stripping the role's harvester must be refused")
			}
			if len(fc.calls) != 0 {
				t.Fatalf("a refused fit changes nothing: %v", fc.calls)
			}
		})
	}
}

func TestFitDronesResumeDoesNotStripAgain(t *testing.T) {
	d, fc := namedStripDispatch(t, "resident",
		[2]string{"b1", "advanced_drone_bay"}, [2]string{"a", "mining_laser_i"})
	fc.raw["drones"] = []byte(`{"drones":[]}`)

	err := d.FitDrones(context.Background(), "mine_asteroid", 1, 2, "", "", FitDronesOpts{
		Resume: true, StripTypes: []string{"mining_laser_i"}})
	if err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(fc.calls, func(c string) bool { return strings.HasPrefix(c, "uninstall_mod") }) {
		t.Fatalf("resume with a bay fitted must not strip: %v", fc.calls)
	}
}
