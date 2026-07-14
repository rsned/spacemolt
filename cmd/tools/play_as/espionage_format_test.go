package main

import (
	"strings"
	"testing"
)

const espionageSample = `{"action":"espionage","outcome":"intel","intel_type":"ship_orders",` +
	`"story":"Your operative lingered by the shipyard queue and copied a manifest."}`

// Espionage is a mutation, so the terminal frame the server sends is an
// action_result envelope with the real payload nested under "result". The
// formatter must unwrap that — binding the outer frame directly yields an empty
// struct rather than a decode error, which would silently print nothing.
func TestFormatEspionage_UnwrapsActionResult(t *testing.T) {
	raw := []byte(`{"command":"espionage","tick":9,"result":` + espionageSample + `}`)

	got := formatEspionage(raw)

	if got == "" {
		t.Fatal("formatEspionage returned empty for a wrapped action_result frame")
	}
	if !strings.Contains(got, "Your operative lingered by the shipyard queue") {
		t.Errorf("story missing from output:\n%s", got)
	}
	if !strings.Contains(got, "intel") {
		t.Errorf("outcome missing from output:\n%s", got)
	}
	if !strings.Contains(got, "ship_orders") {
		t.Errorf("intel_type missing from output:\n%s", got)
	}
}

// The flat (unwrapped) OK payload must format identically.
func TestFormatEspionage_FlatPayload(t *testing.T) {
	got := formatEspionage([]byte(espionageSample))

	if got == "" {
		t.Fatal("formatEspionage returned empty for a flat payload")
	}
	if !strings.Contains(got, "Your operative lingered by the shipyard queue") {
		t.Errorf("story missing from output:\n%s", got)
	}
}

// A spy who turns up nothing reports an outcome and a story but no intel_type;
// the header must not render a dangling empty parenthetical.
func TestFormatEspionage_NoIntelType(t *testing.T) {
	raw := []byte(`{"action":"espionage","outcome":"nothing","story":"The spy found only a quiet dock."}`)

	got := formatEspionage(raw)

	if got == "" {
		t.Fatal("formatEspionage returned empty")
	}
	if strings.Contains(got, "()") {
		t.Errorf("empty intel_type rendered as a dangling parenthetical:\n%s", got)
	}
	if !strings.Contains(got, "The spy found only a quiet dock.") {
		t.Errorf("story missing from output:\n%s", got)
	}
}
