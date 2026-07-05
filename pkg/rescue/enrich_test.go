package rescue

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

func TestFuelForHops(t *testing.T) {
	cases := []struct{ hops, want int }{
		{0, 10}, // 5*0+5=5 → floor 10
		{1, 10}, // 5+5=10
		{2, 15},
		{5, 30},
	}
	for _, tc := range cases {
		if got := fuelForHops(tc.hops); got != tc.want {
			t.Errorf("fuelForHops(%d) = %d, want %d", tc.hops, got, tc.want)
		}
	}
}

func TestResolveUsername(t *testing.T) {
	dir := t.TempDir()
	agentDir := filepath.Join(dir, "trader-8")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	creds := `{"username": "Jaxon 'JunkKing' Jarvis", "password": "x", "empire": "nebula"}`
	if err := os.WriteFile(filepath.Join(agentDir, "credentials.json"), []byte(creds), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveUsername(dir, "trader-8")
	if err != nil || got != "Jaxon 'JunkKing' Jarvis" {
		t.Fatalf("ResolveUsername = %q, %v", got, err)
	}
	if _, err := ResolveUsername(dir, "missing-agent"); err == nil {
		t.Fatal("missing agent must error")
	}
}

func TestResolveSystemID(t *testing.T) {
	systems := []knowledge.System{
		{ID: "first_step", Name: "First Step"},
		{ID: "bd20_2457", Name: "BD+20 2457"},
	}
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"first_step", "first_step", true}, // already an id
		{"First Step", "first_step", true}, // display name
		{"bd+20 2457", "bd20_2457", true},  // name, case-insensitive
		{"Atlantis", "", false},
	}
	for _, tc := range cases {
		got, ok := ResolveSystemID(systems, tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("ResolveSystemID(%q) = %q,%v want %q,%v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestTransferQuantity(t *testing.T) {
	cases := []struct {
		name                                        string
		strandeeMax, strandeeFuel, rescuerFuel, hops int
		want                                        int
	}{
		// Big-tank strandee, healthy near rescuer: capped by rescuer spare.
		// spare = 130 - (5*1 + 5) = 120; need = 420 - 0 = 420 -> 120.
		{"capped by rescuer spare", 420, 0, 130, 1, 120},
		// Small strandee, healthy rescuer: capped by need.
		// need = 75 - 5 = 70; spare = 130 - (5*1+5) = 120 -> 70.
		{"capped by strandee need", 75, 5, 130, 1, 70},
		// Far / low-fuel rescuer: spare clamps to 0 -> caller declines.
		// spare = 20 - (5*3 + 5) = 0; need = 420 -> 0.
		{"rescuer cannot spare", 420, 0, 20, 3, 0},
		// Strandee already full: need 0 -> 0.
		{"strandee already full", 120, 120, 130, 0, 0},
		// hops 0 (station in-system): reserve is just the buffer.
		// spare = 100 - (0 + 5) = 95; need = 100 -> 95.
		{"zero hops home reserves buffer only", 100, 0, 100, 0, 95},
	}
	for _, tc := range cases {
		if got := TransferQuantity(tc.strandeeMax, tc.strandeeFuel, tc.rescuerFuel, tc.hops); got != tc.want {
			t.Errorf("%s: TransferQuantity(%d,%d,%d,%d) = %d, want %d",
				tc.name, tc.strandeeMax, tc.strandeeFuel, tc.rescuerFuel, tc.hops, got, tc.want)
		}
	}
}
