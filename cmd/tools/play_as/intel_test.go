package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSaveIntelPOI_WritesSlugFile(t *testing.T) {
	dir := t.TempDir()
	orig := globalIntelDir
	globalIntelDir = dir
	t.Cleanup(func() { globalIntelDir = orig })

	raw := []byte(`{"action":"get_poi","poi":{"id":"haven_star","name":"Haven Star"},"services":[]}`)
	path, err := saveIntelPOI("haven", "haven_star", raw)
	if err != nil {
		t.Fatalf("saveIntelPOI: %v", err)
	}

	want := filepath.Join(dir, "haven", "haven___haven_star.json")
	if path != want {
		t.Errorf("path = %q, want %q", path, want)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}
	// Content must remain valid JSON equal to the original payload.
	var a, b map[string]any
	if err := json.Unmarshal(raw, &a); err != nil {
		t.Fatalf("unmarshal original: %v", err)
	}
	if err := json.Unmarshal(got, &b); err != nil {
		t.Fatalf("written file is not valid JSON: %v", err)
	}
	if a["action"] != b["action"] {
		t.Errorf("action mismatch: written=%v original=%v", b["action"], a["action"])
	}
}

func TestSaveIntelPOI_NewerOverwrites(t *testing.T) {
	dir := t.TempDir()
	orig := globalIntelDir
	globalIntelDir = dir
	t.Cleanup(func() { globalIntelDir = orig })

	if _, err := saveIntelPOI("sol", "earth", []byte(`{"v":1}`)); err != nil {
		t.Fatalf("first write: %v", err)
	}
	path, err := saveIntelPOI("sol", "earth", []byte(`{"v":2}`))
	if err != nil {
		t.Fatalf("second write: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(got, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m["v"] != float64(2) {
		t.Errorf("expected overwrite with v=2, got v=%v", m["v"])
	}
}

func TestSaveIntelPOI_Disabled(t *testing.T) {
	orig := globalIntelDir
	globalIntelDir = ""
	t.Cleanup(func() { globalIntelDir = orig })

	path, err := saveIntelPOI("sol", "earth", []byte(`{"v":1}`))
	if err != nil {
		t.Fatalf("unexpected error when disabled: %v", err)
	}
	if path != "" {
		t.Errorf("expected empty path when disabled, got %q", path)
	}
}

func TestSaveIntelPOI_SkipsEmptyIDs(t *testing.T) {
	dir := t.TempDir()
	orig := globalIntelDir
	globalIntelDir = dir
	t.Cleanup(func() { globalIntelDir = orig })

	path, err := saveIntelPOI("", "earth", []byte(`{"v":1}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != "" {
		t.Errorf("expected empty path for missing system id, got %q", path)
	}
}

func TestSanitizeIntelComponent(t *testing.T) {
	cases := map[string]string{
		"haven":        "haven",
		"a/b":          "a_b",
		"x:y*z?":       "x_y_z_",
		"sys\\poi":     "sys_poi",
		"clean_slug_1": "clean_slug_1",
	}
	for in, want := range cases {
		if got := sanitizeIntelComponent(in); got != want {
			t.Errorf("sanitizeIntelComponent(%q) = %q, want %q", in, got, want)
		}
	}
}
