package main

import (
	"strings"
	"testing"
)

// reload took two positional arguments and nothing else, so a --flag=value
// invocation was passed through VERBATIM as the ids:
//
//	reload --weapon_instance_id=487919a1... --ammo_id=tungsten_slug_case
//	-> {"weapon_instance_id":"--weapon_instance_id=487919a1...", ...}
//	-> action_error unknown_weapon
//
// The server rejected it a tick later, which is the good case. The bad case is
// a flag token that happens to be a valid id for something else.
func TestReloadArgsPositional(t *testing.T) {
	weapon, ammo, err := reloadArgs([]string{"reload", "WEAP1", "Tungsten_Slug_Case"})
	if err != nil {
		t.Fatalf("positional form: %v", err)
	}
	if weapon != "WEAP1" {
		t.Errorf("weapon = %q, want WEAP1", weapon)
	}
	// Item ids are lower-case on the wire; the original positional path
	// lower-cased the ammo id and that must survive.
	if ammo != "tungsten_slug_case" {
		t.Errorf("ammo = %q, want tungsten_slug_case (lower-cased)", ammo)
	}
}

func TestReloadArgsFlagForm(t *testing.T) {
	for _, args := range [][]string{
		{"reload", "--weapon_instance_id=WEAP1", "--ammo_item_id=tungsten_slug_case"},
		{"reload", "--weapon_instance_id", "WEAP1", "--ammo_item_id", "tungsten_slug_case"},
		// --ammo_id is the natural guess and what the operator actually typed;
		// accepting it costs nothing and the alternative is an error for a
		// difference that carries no meaning.
		{"reload", "--weapon_instance_id=WEAP1", "--ammo_id=tungsten_slug_case"},
	} {
		weapon, ammo, err := reloadArgs(args)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if weapon != "WEAP1" {
			t.Errorf("%v: weapon = %q, want WEAP1", args, weapon)
		}
		if ammo != "tungsten_slug_case" {
			t.Errorf("%v: ammo = %q, want tungsten_slug_case", args, ammo)
		}
	}
}

// The guard that would have caught the reported bug outright: a positional
// argument that looks like a flag is never a real id, so it must be refused
// here rather than sent and rejected a tick later by the server.
func TestReloadArgsRefusesFlagShapedPositional(t *testing.T) {
	_, _, err := reloadArgs([]string{"reload", "--weapon_instance_id=W", "--nonsense=X"})
	if err == nil {
		t.Fatal("an unrecognised --flag must be refused, not sent as an id")
	}
	if !strings.Contains(err.Error(), "--nonsense") {
		t.Errorf("error %q should name the offending flag", err)
	}
}

func TestReloadArgsUsage(t *testing.T) {
	for _, args := range [][]string{
		{"reload"},
		{"reload", "WEAP1"},
		{"reload", "--weapon_instance_id=WEAP1"},        // ammo missing
		{"reload", "--ammo_item_id=tungsten_slug_case"}, // weapon missing
	} {
		if _, _, err := reloadArgs(args); err == nil {
			t.Errorf("%v: incomplete arguments must be a usage error", args)
		}
	}
}
