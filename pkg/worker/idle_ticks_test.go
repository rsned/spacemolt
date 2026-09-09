package worker

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

// The lever the 2026-09-09 IP block asked for: a role that does low-value work
// should idle less often, without cutting headcount. Expressed in TICKS, not a
// duration, because the loop's unit is one game tick — a duration would silently
// desync if SleepTick ever changed.
func TestIdleIntervalFromRoleTicks(t *testing.T) {
	got := idleIntervalFor(Role{IdleTicks: 2}, 0)
	if want := 2 * game.SleepTick; got != want {
		t.Fatalf("idleIntervalFor = %v, want %v", got, want)
	}
}

// Unset must stay exactly one tick — the default every non-pool role relies on.
func TestIdleIntervalDefaultsToOneTick(t *testing.T) {
	if got := idleIntervalFor(Role{}, 0); got != game.SleepTick {
		t.Fatalf("idleIntervalFor = %v, want one tick %v", got, game.SleepTick)
	}
}

// An explicit deps override wins over the role, so a test or an operator can
// still pin the interval directly.
func TestIdleIntervalExplicitOverrideWins(t *testing.T) {
	if got := idleIntervalFor(Role{IdleTicks: 5}, game.SleepQuick); got != game.SleepQuick {
		t.Fatalf("idleIntervalFor = %v, want the explicit override %v", got, game.SleepQuick)
	}
}

// 1 is meaningful and distinct from unset; both mean one tick.
func TestIdleIntervalOneTickExplicit(t *testing.T) {
	if got := idleIntervalFor(Role{IdleTicks: 1}, 0); got != game.SleepTick {
		t.Fatalf("idleIntervalFor = %v, want %v", got, game.SleepTick)
	}
}

// RunStanding must actually apply the role's value — the wiring is the point,
// and a defaulting helper nobody calls would pass its own tests while the fleet
// kept hammering at one tick.
func TestRunStandingAppliesRoleIdleTicks(t *testing.T) {
	deps := StandingDeps{}
	applyStandingDefaultsForRole(Role{IdleTicks: 2}, &deps)
	if want := 2 * game.SleepTick; deps.IdleInterval != want {
		t.Fatalf("deps.IdleInterval = %v, want %v", deps.IdleInterval, want)
	}
}

// A negative tick count is an operator typo that would make the loop spin with
// no delay — the opposite of the intent. It must be rejected at load, loudly,
// rather than silently producing the worst possible cadence.
func TestLoadRolesRejectsNegativeIdleTicks(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "roles.yaml")
	if err := os.WriteFile(p, []byte("roles:\n  pool:\n    idle_ticks: -1\n    idle: idle_mine\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := LoadRoles(p); err == nil {
		t.Fatal("LoadRoles accepted a negative idle_ticks")
	}
}

// The real config must parse and carry the pool roles' value, so this cannot
// pass while roles.yaml says something else.
func TestRealRolesFileParsesIdleTicks(t *testing.T) {
	roles, err := LoadRoles(filepath.Join("..", "..", "data", "overmind", "roles.yaml"))
	if err != nil {
		t.Fatalf("LoadRoles: %v", err)
	}
	for _, name := range []string{"unlock", "missionrunner", "miner"} {
		r, ok := roles[name]
		if !ok {
			t.Fatalf("role %q missing from roles.yaml", name)
		}
		if r.IdleTicks != 2 {
			t.Errorf("role %q idle_ticks = %d, want 2", name, r.IdleTicks)
		}
	}
	// The earning roles must NOT have been slowed down.
	for _, name := range []string{"hauler", "resident", "assist"} {
		if r, ok := roles[name]; ok && r.IdleTicks > 1 {
			t.Errorf("role %q idle_ticks = %d, want unset or 1 (earning roles keep the fast loop)", name, r.IdleTicks)
		}
	}
}
