package main

import (
	"os"
	"path/filepath"
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

func TestLoadCampaignRejectsBadInput(t *testing.T) {
	cases := map[string]string{
		"empty duels":    `{"arena_system":"a","staging_station":"s","duels":[]}`,
		"no id":          `{"arena_system":"a","staging_station":"s","duels":[{"attacker":"x","max_ticks":5,"repeats":1,"script":[{"from_tick":1,"stance_a":"fire","stance_b":"fire"}]}]}`,
		"bad stance":     `{"arena_system":"a","staging_station":"s","duels":[{"id":"d","attacker":"x","max_ticks":5,"repeats":1,"script":[{"from_tick":1,"stance_a":"charge","stance_b":"fire"}]}]}`,
		"no script":      `{"arena_system":"a","staging_station":"s","duels":[{"id":"d","attacker":"x","max_ticks":5,"repeats":1}]}`,
		"zero max_ticks": `{"arena_system":"a","staging_station":"s","duels":[{"id":"d","attacker":"x","max_ticks":0,"repeats":1,"script":[{"from_tick":1,"stance_a":"fire","stance_b":"fire"}]}]}`,
		"dup id":         `{"arena_system":"a","staging_station":"s","duels":[{"id":"d","attacker":"x","max_ticks":5,"repeats":1,"script":[{"from_tick":1,"stance_a":"fire","stance_b":"fire"}]},{"id":"d","attacker":"x","max_ticks":5,"repeats":1,"script":[{"from_tick":1,"stance_a":"fire","stance_b":"fire"}]}]}`,
	}
	for name, body := range cases {
		if _, err := LoadCampaign(writeCampaign(t, body)); err == nil {
			t.Errorf("%s: want error, got nil", name)
		}
	}
}
