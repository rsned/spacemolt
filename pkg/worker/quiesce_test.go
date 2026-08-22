package worker

import (
	"os"
	"path/filepath"
	"testing"
)

func TestQuiesceFilePath(t *testing.T) {
	got := QuiesceFile("craftsman-1")
	want := filepath.Join("data", "agents", "craftsman-1", "quiesce.json")
	if got != want {
		t.Errorf("QuiesceFile = %q, want %q", got, want)
	}
}

// Every failure mode must read as "not quiesced". A typo in a hand-edited file
// must never silently park a worker — still less all 160 of them.
func TestReadQuiesceFailsOpen(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name    string
		content string // "" means: do not create the file at all
		create  bool
	}{
		{name: "missing file", create: false},
		{name: "empty file", content: "", create: true},
		{name: "malformed json", content: `{"quiesce": tru`, create: true},
		{name: "not an object", content: `["quiesce"]`, create: true},
		{name: "wrong type for quiesce", content: `{"quiesce": "yes"}`, create: true},
		{name: "explicit false", content: `{"quiesce": false, "reason": "ignored"}`, create: true},
		{name: "absent key", content: `{"reason": "no flag set"}`, create: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, tc.name+".json")
			if tc.create {
				if err := os.WriteFile(path, []byte(tc.content), 0o644); err != nil {
					t.Fatalf("write fixture: %v", err)
				}
			}
			quiesced, reason := ReadQuiesce(path)
			if quiesced {
				t.Errorf("ReadQuiesce = true, want false (fail open)")
			}
			if reason != "" {
				t.Errorf("reason = %q, want empty when not quiesced", reason)
			}
		})
	}
}

// A directory where the file should be is another unreadable case.
func TestReadQuiesceDirectoryFailsOpen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "quiesce.json")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if quiesced, _ := ReadQuiesce(path); quiesced {
		t.Error("ReadQuiesce on a directory = true, want false")
	}
}

func TestReadQuiesceSet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "quiesce.json")
	if err := os.WriteFile(path, []byte(`{"quiesce": true, "reason": "wildlife testing", "set_at": "2026-08-22T18:40:00Z"}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	quiesced, reason := ReadQuiesce(path)
	if !quiesced {
		t.Fatal("ReadQuiesce = false, want true")
	}
	if reason != "wildlife testing" {
		t.Errorf("reason = %q, want %q", reason, "wildlife testing")
	}
}

// The reason is an operator convenience, not a requirement.
func TestReadQuiesceSetWithoutReason(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "quiesce.json")
	if err := os.WriteFile(path, []byte(`{"quiesce": true}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	quiesced, reason := ReadQuiesce(path)
	if !quiesced {
		t.Fatal("ReadQuiesce = false, want true")
	}
	if reason != "" {
		t.Errorf("reason = %q, want empty", reason)
	}
}
