package tasks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadTasksValid(t *testing.T) {
	got, err := LoadTasks(filepath.Join("testdata", "tasks_valid.yaml"))
	if err != nil {
		t.Fatalf("LoadTasks: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d tasks, want 2", len(got))
	}
	if got[0].ID != "mine-bunda-iron" || got[0].Script != "mining_run" ||
		got[0].RoleRequired != "miner" || got[0].Params["TARGET_SYSTEM"] != "bunda" {
		t.Fatalf("task[0] mismatch: %+v", got[0])
	}
	if got[0].Status != StatusPending {
		t.Fatalf("task[0] status = %q, want %q", got[0].Status, StatusPending)
	}
	if got[1].AgentID != "miner-3" {
		t.Fatalf("task[1] agent_id = %q, want miner-3", got[1].AgentID)
	}
}

func TestLoadTasksRejectsDuplicateID(t *testing.T) {
	if _, err := LoadTasks(filepath.Join("testdata", "tasks_dup.yaml")); err == nil {
		t.Fatal("expected error on duplicate id, got nil")
	}
}

func TestLoadTasksRejectsMissingFields(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(p, []byte("tasks:\n  - id: x\n    role_required: miner\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadTasks(p); err == nil {
		t.Fatal("expected error on missing script, got nil")
	}
}

func TestLoadTasksRejectsColonInID(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "colon.yaml")
	content := "tasks:\n  - id: \"mine:bunda\"\n    script: mining_run\n    role_required: miner\n"
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadTasks(p)
	if err == nil {
		t.Fatal("expected error for task id containing ':', got nil")
	}
	if !strings.Contains(err.Error(), ":") {
		t.Fatalf("error message should name the offending id, got: %v", err)
	}
}
