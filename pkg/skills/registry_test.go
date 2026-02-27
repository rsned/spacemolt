package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRegistry_LoadDir(t *testing.T) {
	dir := t.TempDir()

	mine := `
name: mine
description: Mine resources
steps:
  - id: do_mine
    action: mine
    next: done
  - id: done
    terminal: true
`
	sell := `
name: sell
description: Sell cargo
steps:
  - id: do_sell
    action: sell
    next: done
  - id: done
    terminal: true
`
	if err := os.WriteFile(filepath.Join(dir, "mine.yaml"), []byte(mine), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sell.yaml"), []byte(sell), 0o644); err != nil {
		t.Fatal(err)
	}
	// Non-yaml file should be ignored
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("ignore"), 0o644); err != nil {
		t.Fatal(err)
	}

	reg, err := LoadRegistry(dir)
	if err != nil {
		t.Fatalf("LoadRegistry() error: %v", err)
	}

	if reg.Get("mine") == nil {
		t.Error("registry missing 'mine' skill")
	}
	if reg.Get("sell") == nil {
		t.Error("registry missing 'sell' skill")
	}
	if reg.Get("nonexistent") != nil {
		t.Error("registry should return nil for unknown skill")
	}

	names := reg.Names()
	if len(names) != 2 {
		t.Errorf("Names() count = %d, want 2", len(names))
	}
	if names[0] != "mine" || names[1] != "sell" {
		t.Errorf("Names() = %v, want [mine sell]", names)
	}
}

func TestRegistry_Has(t *testing.T) {
	reg := &Registry{skills: map[string]*Skill{
		"mine": {Name: "mine"},
	}}
	if !reg.Has("mine") {
		t.Error("Has(mine) should be true")
	}
	if reg.Has("sell") {
		t.Error("Has(sell) should be false")
	}
}

func TestRegistry_EmptyDir(t *testing.T) {
	dir := t.TempDir()

	reg, err := LoadRegistry(dir)
	if err != nil {
		t.Fatalf("LoadRegistry() error: %v", err)
	}
	if len(reg.Names()) != 0 {
		t.Errorf("expected empty registry, got %d skills", len(reg.Names()))
	}
}

func TestRegistry_DuplicateSkillName(t *testing.T) {
	dir := t.TempDir()

	skill := "name: dupe\ndescription: test\nsteps:\n  - id: done\n    terminal: true\n"
	if err := os.WriteFile(filepath.Join(dir, "a.yaml"), []byte(skill), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.yaml"), []byte(skill), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadRegistry(dir)
	if err == nil {
		t.Error("expected error for duplicate skill names")
	}
}

func TestRegistry_InvalidYAML(t *testing.T) {
	dir := t.TempDir()

	// Missing name should fail validation
	if err := os.WriteFile(filepath.Join(dir, "bad.yaml"), []byte("steps: []"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadRegistry(dir)
	if err == nil {
		t.Error("expected error for invalid YAML skill")
	}
}

func TestRegistry_YmlExtension(t *testing.T) {
	dir := t.TempDir()

	skill := "name: yml_skill\ndescription: test\nsteps:\n  - id: done\n    terminal: true\n"
	if err := os.WriteFile(filepath.Join(dir, "test.yml"), []byte(skill), 0o644); err != nil {
		t.Fatal(err)
	}

	reg, err := LoadRegistry(dir)
	if err != nil {
		t.Fatalf("LoadRegistry() error: %v", err)
	}
	if !reg.Has("yml_skill") {
		t.Error("should load .yml files")
	}
}
