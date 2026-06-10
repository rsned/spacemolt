package main

import (
	"os"
	"path/filepath"
	"testing"
)

func mkAgent(t *testing.T, agentsDir, id, empire string) {
	t.Helper()
	dir := filepath.Join(agentsDir, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{
  "empire": "` + empire + `",
  "id": "` + id + `",
  "role": "Explorer"
}`
	if err := os.WriteFile(filepath.Join(dir, "personality.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestStageRenameDirsPermutation(t *testing.T) {
	agentsDir := t.TempDir()
	// Minimal 2-cycle: a->b, b->a, which naive renaming would clobber.
	mkAgent(t, agentsDir, "explorer-1", "solarian")
	mkAgent(t, agentsDir, "explorer-3", "voidborn")
	rs := []Rename{{"explorer-1", "explorer-3"}, {"explorer-3", "explorer-1"}}

	if err := stageRenameDirs(agentsDir, rs, true); err != nil {
		t.Fatal(err)
	}
	// explorer-3 dir should now hold the formerly-explorer-1 (solarian) content.
	got := readEmpire(t, filepath.Join(agentsDir, "explorer-3", "personality.json"))
	if got != "solarian" {
		t.Fatalf("explorer-3 empire = %q, want solarian", got)
	}
	if _, err := os.Stat(filepath.Join(agentsDir, "explorer-1.staging")); !os.IsNotExist(err) {
		t.Fatal("staging dir left behind")
	}
}

func readEmpire(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// crude extract for test only
	s := string(b)
	i := indexAfter(s, `"empire": "`)
	j := i
	for j < len(s) && s[j] != '"' {
		j++
	}
	return s[i:j]
}

func indexAfter(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i + len(sub)
		}
	}
	return -1
}

func TestRewritePersonalityID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "personality.json")
	if err := os.WriteFile(path, []byte("{\n  \"empire\": \"crimson\",\n  \"id\": \"explorer-5\",\n  \"role\": \"Explorer\"\n}"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := rewritePersonalityID(path, "explorer-7"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if !contains(string(b), `"id": "explorer-7"`) {
		t.Fatalf("id not rewritten:\n%s", b)
	}
	if contains(string(b), "explorer-5") {
		t.Fatalf("old id still present:\n%s", b)
	}
	if !contains(string(b), `"empire": "crimson"`) {
		t.Fatalf("empire was disturbed:\n%s", b)
	}
}

func TestCreatePlaceholder(t *testing.T) {
	agentsDir := t.TempDir()
	if err := createPlaceholder(agentsDir, "explorer-9", true); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(agentsDir, "explorer-9", "personality.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"id": "explorer-9"`, `"empire": "outerrim"`, `"placeholder": true`} {
		if !contains(string(b), want) {
			t.Fatalf("placeholder missing %q:\n%s", want, b)
		}
	}
}

func contains(s, sub string) bool { return indexAfter(s, sub) >= 0 }
