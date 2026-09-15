package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeCampaign(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "campaign.json")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

const validCampaign = `{
  "arena_system": "gsc_test",
  "staging_system": "sys_x",
  "staging_station": "station_x",
  "duels": [{
    "id": "S1-ring2",
    "purpose": "hit table @ distance 4",
    "attacker": "battle_bot1",
    "fit_a": {"hull": "prospect", "modules": ["missile_launcher_i"]},
    "fit_b": {"hull": "prospect", "modules": ["missile_launcher_i"]},
    "script": [
      {"from_tick": 1, "stance_a": "fire", "stance_b": "fire", "hold_ring": 2},
      {"from_tick": 20, "stance_a": "flee", "stance_b": "flee"}
    ],
    "max_ticks": 25,
    "repeats": 2
  }]
}`

func TestLoadCampaignValid(t *testing.T) {
	c, err := LoadCampaign(writeCampaign(t, validCampaign))
	if err != nil {
		t.Fatalf("LoadCampaign: %v", err)
	}
	if c.ArenaSystem != "gsc_test" || len(c.Duels) != 1 {
		t.Fatalf("parsed = %+v", c)
	}
	d := c.Duels[0]
	if d.Attacker != "battle_bot1" || d.MaxTicks != 25 || d.Repeats != 2 {
		t.Errorf("duel = %+v", d)
	}
	if d.Script[0].HoldRing == nil || *d.Script[0].HoldRing != 2 {
		t.Errorf("hold_ring not parsed: %+v", d.Script[0])
	}
	if d.Script[1].HoldRing != nil {
		t.Errorf("phase 2 must have nil HoldRing")
	}
}

func TestPhaseAtPicksLatestPhase(t *testing.T) {
	c, _ := LoadCampaign(writeCampaign(t, validCampaign))
	d := c.Duels[0]
	if p := d.PhaseAt(1); p.StanceA != "fire" {
		t.Errorf("tick 1 = %+v", p)
	}
	if p := d.PhaseAt(19); p.StanceA != "fire" {
		t.Errorf("tick 19 = %+v", p)
	}
	if p := d.PhaseAt(20); p.StanceA != "flee" {
		t.Errorf("tick 20 = %+v", p)
	}
	if p := d.PhaseAt(999); p.StanceB != "flee" {
		t.Errorf("tick 999 = %+v", p)
	}
}

const asymmetricHoldCampaign = `{
  "arena_system": "gsc_test",
  "staging_system": "sys_x",
  "staging_station": "station_x",
  "duels": [{
    "id": "S1-odd-ring1",
    "purpose": "hit table @ zone_distance 1 (one-side hold)",
    "attacker": "battle_bot1",
    "fit_a": {"hull": "prospect", "modules": ["pulse_laser_i"]},
    "fit_b": {"hull": "prospect", "modules": ["pulse_laser_i"]},
    "script": [
      {"from_tick": 1, "stance_a": "fire", "stance_b": "fire", "hold_ring_a": 0, "hold_ring_b": 1},
      {"from_tick": 21, "stance_a": "flee", "stance_b": "flee"}
    ],
    "max_ticks": 25,
    "repeats": 1
  }]
}`

func TestLoadCampaignParsesAsymmetricHoldRing(t *testing.T) {
	c, err := LoadCampaign(writeCampaign(t, asymmetricHoldCampaign))
	if err != nil {
		t.Fatalf("LoadCampaign: %v", err)
	}
	phase := c.Duels[0].Script[0]
	if phase.HoldRingA == nil || *phase.HoldRingA != 0 {
		t.Fatalf("hold_ring_a not parsed: %+v", phase)
	}
	if phase.HoldRingB == nil || *phase.HoldRingB != 1 {
		t.Fatalf("hold_ring_b not parsed: %+v", phase)
	}
	if phase.HoldRing != nil {
		t.Errorf("hold_ring must be nil when only per-side fields are set: %+v", phase)
	}
	// HoldA/HoldB resolve to the per-side override.
	if got := phase.HoldA(); got == nil || *got != 0 {
		t.Errorf("HoldA() = %v, want 0", got)
	}
	if got := phase.HoldB(); got == nil || *got != 1 {
		t.Errorf("HoldB() = %v, want 1", got)
	}
	// The plain hold_ring phase falls back to the shared value on both sides.
	shared := c.Duels[0].Script[1]
	if shared.HoldA() != nil || shared.HoldB() != nil {
		t.Errorf("flee phase (no hold_ring set) must resolve to nil holds: %+v", shared)
	}
}

func TestPhaseHoldFallsBackToSharedHoldRing(t *testing.T) {
	r := ringPtr(2)
	p := Phase{HoldRing: r}
	if got := p.HoldA(); got == nil || *got != 2 {
		t.Errorf("HoldA() = %v, want 2 (fallback to shared hold_ring)", got)
	}
	if got := p.HoldB(); got == nil || *got != 2 {
		t.Errorf("HoldB() = %v, want 2 (fallback to shared hold_ring)", got)
	}
}

func TestLoadCampaignRejectsBadInput(t *testing.T) {
	cases := map[string]string{
		"empty duels":           `{"arena_system":"a","staging_station":"s","duels":[]}`,
		"no id":                 `{"arena_system":"a","staging_station":"s","duels":[{"attacker":"x","max_ticks":5,"repeats":1,"script":[{"from_tick":1,"stance_a":"fire","stance_b":"fire"}]}]}`,
		"bad stance":            `{"arena_system":"a","staging_station":"s","duels":[{"id":"d","attacker":"x","max_ticks":5,"repeats":1,"script":[{"from_tick":1,"stance_a":"charge","stance_b":"fire"}]}]}`,
		"no script":             `{"arena_system":"a","staging_station":"s","duels":[{"id":"d","attacker":"x","max_ticks":5,"repeats":1}]}`,
		"zero max_ticks":        `{"arena_system":"a","staging_station":"s","duels":[{"id":"d","attacker":"x","max_ticks":0,"repeats":1,"script":[{"from_tick":1,"stance_a":"fire","stance_b":"fire"}]}]}`,
		"dup id":                `{"arena_system":"a","staging_station":"s","duels":[{"id":"d","attacker":"x","max_ticks":5,"repeats":1,"script":[{"from_tick":1,"stance_a":"fire","stance_b":"fire"}]},{"id":"d","attacker":"x","max_ticks":5,"repeats":1,"script":[{"from_tick":1,"stance_a":"fire","stance_b":"fire"}]}]}`,
		"bad hold_ring_a":       `{"arena_system":"a","staging_station":"s","duels":[{"id":"d","attacker":"x","max_ticks":5,"repeats":1,"script":[{"from_tick":1,"stance_a":"fire","stance_b":"fire","hold_ring_a":4}]}]}`,
		"bad hold_ring_b":       `{"arena_system":"a","staging_station":"s","duels":[{"id":"d","attacker":"x","max_ticks":5,"repeats":1,"script":[{"from_tick":1,"stance_a":"fire","stance_b":"fire","hold_ring_b":-1}]}]}`,
		"negative reload_every": `{"arena_system":"a","staging_station":"s","duels":[{"id":"d","attacker":"x","max_ticks":5,"repeats":1,"reload_every":-1,"script":[{"from_tick":1,"stance_a":"fire","stance_b":"fire"}]}]}`,
	}
	for name, body := range cases {
		if _, err := LoadCampaign(writeCampaign(t, body)); err == nil {
			t.Errorf("%s: want error, got nil", name)
		}
	}
}

const ammoReloadCampaign = `{
  "arena_system": "gsc_test",
  "staging_system": "sys_x",
  "staging_station": "station_x",
  "duels": [{
    "id": "S2-ammo",
    "purpose": "test ammo and reload",
    "attacker": "battle_bot1",
    "fit_a": {
      "hull": "prospect",
      "modules": ["missile_launcher_i"],
      "ammo": {"missile_launcher_i": "missile_standard"}
    },
    "fit_b": {
      "hull": "prospect",
      "modules": ["missile_launcher_i"],
      "ammo": {"missile_launcher_i": "missile_standard"}
    },
    "script": [
      {"from_tick": 1, "stance_a": "fire", "stance_b": "fire"}
    ],
    "max_ticks": 20,
    "repeats": 1,
    "reload_every": 3
  }]
}`

func TestLoadCampaignParsesAmmoAndReloadEvery(t *testing.T) {
	c, err := LoadCampaign(writeCampaign(t, ammoReloadCampaign))
	if err != nil {
		t.Fatalf("LoadCampaign: %v", err)
	}
	d := c.Duels[0]
	if d.ReloadEvery != 3 {
		t.Errorf("reload_every not parsed: %d, want 3", d.ReloadEvery)
	}
	if len(d.FitA.Ammo) == 0 {
		t.Fatalf("FitA.Ammo not parsed: %+v", d.FitA.Ammo)
	}
	if ammo := d.FitA.Ammo["missile_launcher_i"]; ammo != "missile_standard" {
		t.Errorf("FitA ammo for missile_launcher_i = %q, want missile_standard", ammo)
	}
	if ammo := d.FitB.Ammo["missile_launcher_i"]; ammo != "missile_standard" {
		t.Errorf("FitB ammo for missile_launcher_i = %q, want missile_standard", ammo)
	}
}

// --- modes ----------------------------------------------------------------

// arenaCampaign exercises the v0.586.0 geography: a real arena POI in
// Krynn for consequence-free scenarios, lawless Ashford for the ones that
// measure flee (which forfeits in the arena) or the emergency modules
// (which never trigger there).
const arenaCampaign = `{
  "arena_system": "krynn",
  "arena_poi": "blood_arena",
  "lawless_system": "ashford",
  "staging_system": "sys_x",
  "staging_station": "station_x",
  "default_mode": "arena",
  "duels": [{
    "id": "S7-armor-4", "attacker": "battle_bot1",
    "fit_a": {"hull": "prospect"}, "fit_b": {"hull": "prospect"},
    "script": [{"from_tick": 1, "stance_a": "fire", "stance_b": "brace"}],
    "max_ticks": 36, "repeats": 1
  }, {
    "id": "S6a-flee-base", "attacker": "battle_bot1", "mode": "lawless",
    "fit_a": {"hull": "prospect"}, "fit_b": {"hull": "prospect"},
    "script": [{"from_tick": 1, "stance_a": "fire", "stance_b": "flee"}],
    "max_ticks": 30, "repeats": 1
  }]
}`

func TestDuelModeResolution(t *testing.T) {
	c, err := LoadCampaign(writeCampaign(t, arenaCampaign))
	if err != nil {
		t.Fatalf("LoadCampaign: %v", err)
	}
	if got := c.ModeOf(c.Duels[0]); got != ModeArena {
		t.Errorf("S7 mode = %q, want arena (from default_mode)", got)
	}
	if got := c.ModeOf(c.Duels[1]); got != ModeLawless {
		t.Errorf("S6a mode = %q, want lawless (per-duel override)", got)
	}
}

// A campaign written before v0.586.0 has no mode fields at all. It must
// keep running exactly as it did: every duel lawless, fought in the system
// its arena_system named.
func TestLegacyCampaignDefaultsToLawless(t *testing.T) {
	c, err := LoadCampaign(writeCampaign(t, validCampaign))
	if err != nil {
		t.Fatalf("LoadCampaign: %v", err)
	}
	if got := c.ModeOf(c.Duels[0]); got != ModeLawless {
		t.Errorf("legacy duel mode = %q, want lawless", got)
	}
	sys, err := c.FightSystem(c.Duels[0])
	if err != nil {
		t.Fatalf("FightSystem: %v", err)
	}
	if sys != "gsc_test" {
		t.Errorf("legacy fight system = %q, want the arena_system fallback gsc_test", sys)
	}
}

func TestFightSystemByMode(t *testing.T) {
	c, err := LoadCampaign(writeCampaign(t, arenaCampaign))
	if err != nil {
		t.Fatalf("LoadCampaign: %v", err)
	}
	if sys, err := c.FightSystem(c.Duels[0]); err != nil || sys != "krynn" {
		t.Errorf("arena fight system = %q, %v; want krynn", sys, err)
	}
	if sys, err := c.FightSystem(c.Duels[1]); err != nil || sys != "ashford" {
		t.Errorf("lawless fight system = %q, %v; want ashford", sys, err)
	}
}

func TestLoadCampaignRejectsUnknownMode(t *testing.T) {
	body := `{"arena_system":"a","staging_system":"s","staging_station":"st","duels":[{
	  "id":"X","attacker":"bot","fit_a":{},"fit_b":{},
	  "script":[{"from_tick":1,"stance_a":"fire","stance_b":"fire"}],
	  "max_ticks":5,"repeats":1,"mode":"sandbox"}]}`
	if _, err := LoadCampaign(writeCampaign(t, body)); err == nil {
		t.Errorf("LoadCampaign accepted mode \"sandbox\", want an error naming the duel")
	}
}

func TestLoadCampaignArenaModeRequiresPOI(t *testing.T) {
	body := `{"arena_system":"krynn","staging_system":"s","staging_station":"st","duels":[{
	  "id":"X","attacker":"bot","fit_a":{},"fit_b":{},
	  "script":[{"from_tick":1,"stance_a":"fire","stance_b":"fire"}],
	  "max_ticks":5,"repeats":1,"mode":"arena"}]}`
	if _, err := LoadCampaign(writeCampaign(t, body)); err == nil {
		t.Errorf("LoadCampaign accepted an arena duel with no arena_poi, want an error")
	}
}

// The dangerous half-migration: arena_system has been repointed at Krynn
// for the arena duels, but lawless_system was never added -- so the legacy
// fallback would silently send the flee scenarios to Krynn, where fleeing
// forfeits and the measurement is garbage. That must not load.
func TestLoadCampaignRejectsAmbiguousLawlessSystem(t *testing.T) {
	body := `{"arena_system":"krynn","arena_poi":"blood_arena","staging_system":"s","staging_station":"st","duels":[{
	  "id":"A","attacker":"bot","fit_a":{},"fit_b":{},
	  "script":[{"from_tick":1,"stance_a":"fire","stance_b":"fire"}],
	  "max_ticks":5,"repeats":1,"mode":"arena"},{
	  "id":"B","attacker":"bot","fit_a":{},"fit_b":{},
	  "script":[{"from_tick":1,"stance_a":"fire","stance_b":"flee"}],
	  "max_ticks":5,"repeats":1,"mode":"lawless"}]}`
	_, err := LoadCampaign(writeCampaign(t, body))
	if err == nil {
		t.Fatalf("LoadCampaign accepted a mixed campaign with no lawless_system, want an error")
	}
	if !strings.Contains(err.Error(), "lawless_system") {
		t.Errorf("error = %v, want it to name lawless_system", err)
	}
}

func TestLoadCampaignRejectsUnknownDefaultMode(t *testing.T) {
	body := `{"arena_system":"a","staging_system":"s","staging_station":"st","default_mode":"nope","duels":[{
	  "id":"X","attacker":"bot","fit_a":{},"fit_b":{},
	  "script":[{"from_tick":1,"stance_a":"fire","stance_b":"fire"}],
	  "max_ticks":5,"repeats":1}]}`
	if _, err := LoadCampaign(writeCampaign(t, body)); err == nil {
		t.Errorf("LoadCampaign accepted default_mode \"nope\", want an error")
	}
}
