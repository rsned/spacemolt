package game

import (
	"strings"
	"testing"
)

// v0.601.3 documented what the craft routing presets do, and the DEFAULT is
// expensive: `fast` picks the soonest finish across your own, faction,
// ally-granted and public facilities, and when it lands on another player's
// public facility it PREPAYS their per-run rental fee. Ownership only breaks
// ties.
//
// That matters here because our own faction (CRFT) owns Bob's Iron Smeltery
// (refine_steel at grand_exchange_station), and `cheap` makes own/faction
// facilities free. Sending no preset means silently taking `fast` and
// potentially paying a stranger to do a job our own idle mill would do for
// nothing -- at draw_copper_piping's 30/run, across ~90 agents hourly, that
// is real money.
//
// The valid set is fixed and small, so a typo must be rejected rather than
// silently falling back to the expensive default.
func TestCraftPresetValidation(t *testing.T) {
	for _, ok := range []string{"fast", "prefer_own", "cheap", "workshop"} {
		if err := validateCraftPreset(ok); err != nil {
			t.Errorf("preset %q must be accepted: %v", ok, err)
		}
	}
	if err := validateCraftPreset(""); err != nil {
		t.Errorf("empty preset means 'server default', must be accepted: %v", err)
	}
	for _, bad := range []string{"cheapest", "Cheap", "own", "hand", "fastest"} {
		if err := validateCraftPreset(bad); err == nil {
			t.Errorf("preset %q must be rejected, not silently defaulted to fast", bad)
		}
	}
}

// The error has to name the valid options: the cost of guessing wrong is
// paying another player's rent, which is invisible until the credits are gone.
func TestCraftPresetErrorNamesTheOptions(t *testing.T) {
	err := validateCraftPreset("cheapest")
	if err == nil {
		t.Fatal("want an error")
	}
	for _, want := range []string{"fast", "prefer_own", "cheap", "workshop"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should list %q", err.Error(), want)
		}
	}
}
