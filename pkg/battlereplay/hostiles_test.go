package battlereplay

import (
	"testing"
)

func TestHostileType(t *testing.T) {
	for _, tc := range []struct {
		p    Participant
		want string
	}{
		{Participant{PlayerID: "repeater_383b6aeca7f9224a7eb52dfca1da288b", Username: "Repeater Pane"}, "repeater_pane"},
		{Participant{PlayerID: "x", Kind: "repeater", ShipClass: "monolith"}, "repeater_monolith"},
		{Participant{PlayerID: "y", Kind: "npc", Username: "Repeater Spire"}, "repeater_spire"},
		{Participant{PlayerID: "a509", Kind: "player", Username: "Repeater Fan"}, ""},
		{Participant{PlayerID: "crt_1", Kind: "creature", Username: "Belt Grazer"}, ""},
	} {
		if got := HostileType(tc.p); got != tc.want {
			t.Errorf("HostileType(%+v) = %q, want %q", tc.p, got, tc.want)
		}
	}
}

// Repeater fire aggregates per class; player fire is ignored.
func TestHostileAttacksAggregates(t *testing.T) {
	m := &ReplayModel{
		BattleID: "b1",
		Participants: []Participant{
			{PlayerID: "repeater_m1", Username: "Repeater Monolith", ShipClass: "monolith"},
			{PlayerID: "repeater_m2", Username: "Repeater Monolith", ShipClass: "monolith"},
			{PlayerID: "p1", Kind: "player", Username: "Arthur"},
		},
		Frames: []Frame{
			{Shots: []Shot{
				{FromID: "repeater_m1", WeaponName: "Lattice Lance", DamageType: "em", Hit: true, Damage: 150},
				{FromID: "repeater_m2", WeaponName: "Lattice Lance", DamageType: "em", Hit: true, Damage: 155},
				{FromID: "repeater_m2", WeaponName: "Lattice Lance", DamageType: "em", Hit: false},
				{FromID: "p1", WeaponName: "Railgun", DamageType: "kinetic", Hit: true, Damage: 40},
			}},
		},
	}
	got := HostileAttacks(m)
	if len(got) != 1 {
		t.Fatalf("rows = %+v, want 1", got)
	}
	a := got[0]
	if a.HostileType != "repeater_monolith" || a.Shots != 3 || a.Hits != 2 || a.DamageTotal != 305 || a.DamageMin != 150 || a.DamageMax != 155 {
		t.Errorf("row = %+v", a)
	}
}

// Snapshots give class, maxima and a counted loadout per repeater.
func TestHostileSpecsFromSnapshots(t *testing.T) {
	m := &ReplayModel{
		BattleID: "b1",
		Participants: []Participant{
			{PlayerID: "repeater_m1", Username: "Repeater Monolith", ShipClass: "monolith", MaxHull: 6400, MaxShield: 9600,
				Modules: []Module{{Name: "Heavy Pulse Laser", Category: "weapon"}, {Name: "Heavy Pulse Laser", Category: "weapon"}, {Name: "Shield Lattice", Category: "defense"}}},
			{PlayerID: "p1", Kind: "player", Username: "Arthur", Modules: []Module{{Name: "Railgun"}}},
		},
	}
	got := HostileSpecs(m)
	if len(got) != 1 {
		t.Fatalf("specs = %+v, want 1", got)
	}
	s := got[0]
	if s.HostileType != "repeater_monolith" || s.Class != "monolith" || s.MaxHull != 6400 || s.MaxShield != 9600 || s.BattleID != "b1" {
		t.Errorf("spec = %+v", s)
	}
	if len(s.Modules) != 2 || s.Modules[0].Name != "Heavy Pulse Laser" || s.Modules[0].Count != 2 || s.Modules[1].Count != 1 {
		t.Errorf("modules = %+v", s.Modules)
	}
}
