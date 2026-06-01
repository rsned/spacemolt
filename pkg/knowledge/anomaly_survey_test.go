package knowledge

import (
	"context"
	"encoding/json"
	"testing"
)

func TestParseSurveyAnomaly_Directional(t *testing.T) {
	a, ok := ParseSurveyAnomaly(
		"Spatial anomaly detected — faint readings toward Furud (3 jumps).",
		"nova_terra", "explorer-1", 1234)
	if !ok {
		t.Fatal("ok=false, want true for a non-empty hint")
	}
	if a.Type != "spatial_anomaly" {
		t.Errorf("Type=%q, want spatial_anomaly", a.Type)
	}
	if a.SystemID != "nova_terra" {
		t.Errorf("SystemID=%q, want nova_terra", a.SystemID)
	}
	if a.DetectedBy != "explorer-1" {
		t.Errorf("DetectedBy=%q, want explorer-1", a.DetectedBy)
	}
	if a.LastUpdatedTick != 1234 {
		t.Errorf("LastUpdatedTick=%d, want 1234", a.LastUpdatedTick)
	}

	var d struct {
		Raw          string `json:"raw"`
		TargetSystem string `json:"target_system"`
		Jumps        int    `json:"jumps"`
		InSystem     bool   `json:"in_system"`
	}
	if err := json.Unmarshal([]byte(a.Details), &d); err != nil {
		t.Fatalf("details not JSON: %v (%q)", err, a.Details)
	}
	if d.TargetSystem != "Furud" {
		t.Errorf("target_system=%q, want Furud", d.TargetSystem)
	}
	if d.Jumps != 3 {
		t.Errorf("jumps=%d, want 3", d.Jumps)
	}
	if d.InSystem {
		t.Error("in_system=true, want false for a directional hint")
	}
}

func TestParseSurveyAnomaly_SingularJump(t *testing.T) {
	a, ok := ParseSurveyAnomaly(
		"Spatial anomaly detected — faint readings toward Nova Terra (1 jump).",
		"sys", "e", 0)
	if !ok {
		t.Fatal("ok=false")
	}
	var d struct {
		TargetSystem string `json:"target_system"`
		Jumps        int    `json:"jumps"`
	}
	_ = json.Unmarshal([]byte(a.Details), &d)
	if d.TargetSystem != "Nova Terra" {
		t.Errorf("target_system=%q, want 'Nova Terra' (multi-word)", d.TargetSystem)
	}
	if d.Jumps != 1 {
		t.Errorf("jumps=%d, want 1", d.Jumps)
	}
}

func TestParseSurveyAnomaly_InSystem(t *testing.T) {
	a, ok := ParseSurveyAnomaly(
		"Strong spatial anomaly detected in this system.",
		"sys", "e", 0)
	if !ok {
		t.Fatal("ok=false")
	}
	var d struct {
		TargetSystem string `json:"target_system"`
		Jumps        int    `json:"jumps"`
		InSystem     bool   `json:"in_system"`
	}
	_ = json.Unmarshal([]byte(a.Details), &d)
	if !d.InSystem {
		t.Error("in_system=false, want true")
	}
	if d.TargetSystem != "" {
		t.Errorf("target_system=%q, want empty for in-system", d.TargetSystem)
	}
	if d.Jumps != 0 {
		t.Errorf("jumps=%d, want 0", d.Jumps)
	}
}

func TestParseSurveyAnomaly_Empty(t *testing.T) {
	if _, ok := ParseSurveyAnomaly("", "sys", "e", 0); ok {
		t.Error("ok=true for empty hint, want false")
	}
}

func TestCaptureSurveyAnomaly_RecordsThenDedups(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()
	hint := "Spatial anomaly detected — faint readings toward Furud (3 jumps)."

	rec, err := CaptureSurveyAnomaly(ctx, kb, hint, "nova_terra", "explorer-1", 1)
	if err != nil {
		t.Fatalf("first capture: %v", err)
	}
	if !rec {
		t.Fatal("first capture recorded=false, want true")
	}

	// Same anomaly re-detected on a later survey of the same system.
	rec, err = CaptureSurveyAnomaly(ctx, kb, hint, "nova_terra", "explorer-1", 2)
	if err != nil {
		t.Fatalf("second capture: %v", err)
	}
	if rec {
		t.Error("second capture recorded=true, want false (dedup)")
	}

	got, err := kb.GetActiveAnomalies(ctx, "nova_terra")
	if err != nil {
		t.Fatalf("GetActiveAnomalies: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d active anomalies, want 1 (no duplicate)", len(got))
	}
	if got[0].Description != hint {
		t.Errorf("Description=%q, want the raw hint", got[0].Description)
	}
}

func TestCaptureSurveyAnomaly_NoHintIsNoOp(t *testing.T) {
	kb := newTestKB(t)
	rec, err := CaptureSurveyAnomaly(context.Background(), kb, "", "sys", "e", 0)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if rec {
		t.Error("recorded=true for empty hint, want false")
	}
}
