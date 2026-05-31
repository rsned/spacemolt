package main

import "testing"

func TestCoerceBoolFlags(t *testing.T) {
	// parseFlagArgs yields strings ("true"/"false") and ints (1/0) depending on
	// the input form; coerceBoolFlags must turn all recognized forms into bools.
	payload := map[string]any{
		"ally_fuel_access":   "true", // --ally_fuel_access=true or bare flag
		"ally_intel_opt_out": "false",
		"description":        "Best faction", // untouched non-bool key
		"primary_color":      1,              // not in the bool key list -> untouched
	}
	if err := coerceBoolFlags(payload, "ally_fuel_access", "ally_intel_opt_out"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v, ok := payload["ally_fuel_access"].(bool); !ok || v != true {
		t.Errorf("ally_fuel_access: want bool true, got %#v", payload["ally_fuel_access"])
	}
	if v, ok := payload["ally_intel_opt_out"].(bool); !ok || v != false {
		t.Errorf("ally_intel_opt_out: want bool false, got %#v", payload["ally_intel_opt_out"])
	}
	if payload["description"] != "Best faction" {
		t.Errorf("non-bool key must be untouched, got %#v", payload["description"])
	}
	if payload["primary_color"] != 1 {
		t.Errorf("key not in bool list must be untouched, got %#v", payload["primary_color"])
	}
}

func TestCoerceBoolFlags_IntForm(t *testing.T) {
	// `--ally_fuel_access=1` is parsed to int 1 by parseFlagArgs.
	payload := map[string]any{"ally_fuel_access": 1}
	if err := coerceBoolFlags(payload, "ally_fuel_access"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if payload["ally_fuel_access"] != true {
		t.Errorf("int 1 should coerce to bool true, got %#v", payload["ally_fuel_access"])
	}
}

func TestCoerceBoolFlags_MissingKeyOK(t *testing.T) {
	payload := map[string]any{"description": "x"}
	if err := coerceBoolFlags(payload, "ally_fuel_access", "ally_intel_opt_out"); err != nil {
		t.Fatalf("missing keys should be a no-op, got %v", err)
	}
	if len(payload) != 1 {
		t.Errorf("payload should be unchanged, got %#v", payload)
	}
}

func TestCoerceBoolFlags_InvalidValue(t *testing.T) {
	payload := map[string]any{"ally_fuel_access": "yes"}
	if err := coerceBoolFlags(payload, "ally_fuel_access"); err == nil {
		t.Error("non-boolean value should error")
	}
}
