package supervisor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFleet(t *testing.T) {
	p := filepath.Join(t.TempDir(), "fleet.yaml")
	if err := os.WriteFile(p, []byte("workers:\n"+
		"  - agent_id: r1\n    role: resident\n    station: S1\n"+
		"  - agent_id: h1\n    role: hauler\n    station: S2\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	specs, err := LoadFleet(p)
	if err != nil {
		t.Fatalf("LoadFleet: %v", err)
	}
	if len(specs) != 2 || specs[0].AgentID != "r1" || specs[1].Role != "hauler" {
		t.Fatalf("parsed wrong: %+v", specs)
	}
}

func TestLoadFleetRejectsMissingID(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bad.yaml")
	if err := os.WriteFile(p, []byte("workers:\n  - role: resident\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := LoadFleet(p); err == nil {
		t.Fatal("expected error for missing agent_id")
	}
}

// enable_freight is the per-worker canary knob for the /shipping carrier path.
// It must parse from the fleet yaml and default false when absent — the
// default is what keeps freight dormant across the rest of the fleet.
func TestLoadFleetEnableFreight(t *testing.T) {
	p := filepath.Join(t.TempDir(), "fleet.yaml")
	if err := os.WriteFile(p, []byte("workers:\n"+
		"  - agent_id: canary\n    role: missionrunner\n    enable_freight: true\n    freight_max_packages: 3\n"+
		"  - agent_id: rest\n    role: missionrunner\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	specs, err := LoadFleet(p)
	if err != nil {
		t.Fatalf("LoadFleet: %v", err)
	}
	if !specs[0].EnableFreight {
		t.Error("canary must parse enable_freight: true")
	}
	if specs[1].EnableFreight {
		t.Error("enable_freight must default false when absent")
	}
	if specs[0].FreightMaxPackages != 3 {
		t.Errorf("canary must parse freight_max_packages: 3, got %d", specs[0].FreightMaxPackages)
	}
	if specs[1].FreightMaxPackages != 0 {
		t.Errorf("freight_max_packages must default 0 when absent, got %d", specs[1].FreightMaxPackages)
	}
}
