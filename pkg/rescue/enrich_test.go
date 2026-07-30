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
		// perJump 0 selects the flat FuelPerJump fallback, which is what these
		// cases were computed against.
		if got := TransferQuantity(tc.strandeeMax, tc.strandeeFuel, tc.rescuerFuel, tc.hops, 0); got != tc.want {
			t.Errorf("%s: TransferQuantity(%d,%d,%d,%d) = %d, want %d",
				tc.name, tc.strandeeMax, tc.strandeeFuel, tc.rescuerFuel, tc.hops, got, tc.want)
		}
	}
}

// The flat FuelPerJump=5 over-reserves for a small hull, and that is not
// academic: assist-krynn burns ~3/jump, flew 20 hops to a strandee, reserved
// 5*20+5 = 105 of its 110 remaining fuel for the trip home, concluded it had
// nothing to spare and abandoned the rescue. With the measured rate the same
// trip reserves 65 and the rescue goes ahead.
func TestTransferQuantityUsesMeasuredRateNotTheFlatConstant(t *testing.T) {
	const (
		strandeeMax, strandeeFuel = 100, 0
		rescuerFuel, hopsHome     = 110, 20
		measuredPerJump           = 3
	)
	if got := TransferQuantity(strandeeMax, strandeeFuel, rescuerFuel, hopsHome, 0); got != 5 {
		t.Fatalf("flat-rate baseline: got %d, want 5 (110 - (5*20+5))", got)
	}
	want := rescuerFuel - (measuredPerJump*hopsHome + FuelBuffer) // 110 - 65 = 45
	if got := TransferQuantity(strandeeMax, strandeeFuel, rescuerFuel, hopsHome, measuredPerJump); got != want {
		t.Errorf("measured rate: TransferQuantity(...,%d) = %d, want %d", measuredPerJump, got, want)
	}
}

// A large hull burns far MORE than the flat 5, so the constant under-reserves
// and the rescuer gives away fuel it needs to get home — creating the next
// rescue. The measured rate must be able to drive spare down to zero.
func TestTransferQuantityUnderReserveForLargeHullIsCorrected(t *testing.T) {
	const rescuerFuel, hopsHome, measuredPerJump = 100, 6, 16
	if got := TransferQuantity(500, 0, rescuerFuel, hopsHome, 0); got != 65 {
		t.Fatalf("flat-rate baseline: got %d, want 65 (100 - (5*6+5)) — it would strand the rescuer", got)
	}
	if got := TransferQuantity(500, 0, rescuerFuel, hopsHome, measuredPerJump); got != 0 {
		t.Errorf("measured rate: got %d, want 0 (100 - (16*6+5) clamps) so the caller declines", got)
	}
}
