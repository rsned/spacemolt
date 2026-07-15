package worker

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/overmind/tasks"
)

func TestLoadRolesParsesResident(t *testing.T) {
	roles, err := LoadRoles(filepath.Join("..", "..", "data", "overmind", "roles.yaml"))
	if err != nil {
		t.Fatalf("LoadRoles: %v", err)
	}
	r, ok := roles["resident"]
	if !ok {
		t.Fatal("resident role missing")
	}
	// resident switched idle_mine→idle_market (570d148), then idle_market→
	// resident_market (home-return guard added; see resident_home spec).
	if r.Idle != "resident_market" {
		t.Fatalf("idle=%q", r.Idle)
	}
	if len(r.Schedule) == 0 {
		t.Fatal("resident has no schedule entries")
	}
	if r.IdleParams["N"] != "20" {
		t.Fatalf("idle_params N=%q", r.IdleParams["N"])
	}
}

func TestLoadRolesRejectsMissing(t *testing.T) {
	if _, err := LoadRoles(filepath.Join(t.TempDir(), "nope.yaml")); err == nil {
		t.Fatal("expected error for missing file")
	}
}

// TestSeededCommandsAreDispatchable kills lean-dispatch divergence: every
// command named in roles.yaml schedule entries AND in every data/scripts script
// referenced by a role must be in the WorkerDispatch curated vocabulary.
// Tokens, loop headers, blank lines, and comments are skipped.
func TestSeededCommandsAreDispatchable(t *testing.T) {
	// Chdir to the repo root so that ResolveScriptArg can find data/scripts/.
	t.Chdir(filepath.Join("..", ".."))

	d := NewWorkerDispatch(nil, nil, nil, io.Discard)
	roles, err := LoadRoles(filepath.Join("data", "overmind", "roles.yaml"))
	if err != nil {
		t.Fatalf("LoadRoles: %v", err)
	}
	check := func(cmdLine string) {
		for _, st := range mustStatements(t, cmdLine) {
			head := strings.ToLower(firstWord(st))
			if head == "loop" || head == "" {
				continue // loop bodies are checked statement-by-statement below
			}
			if !d.Supports(head) {
				t.Errorf("command %q (from %q) not supported by WorkerDispatch", head, cmdLine)
			}
		}
	}
	for name, r := range roles {
		for _, se := range r.Schedule {
			check(se.Command)
		}
		if r.Idle != "" {
			path, ok := ResolveScriptArg(r.Idle, name)
			if !ok {
				t.Fatalf("role %q idle script %q not found", name, r.Idle)
			}
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			cmds, err := SplitScriptCommands(string(content))
			if err != nil {
				t.Fatalf("split %s: %v", path, err)
			}
			for _, c := range cmds {
				check(c)
			}
		}
	}

	// Also validate scripts referenced by tasks.yaml so task-only scripts
	// (e.g. mining_run.smolt) are covered even when no role references them.
	taskList, err := tasks.LoadTasks(filepath.Join("data", "overmind", "tasks.yaml"))
	if err != nil {
		t.Fatalf("LoadTasks: %v", err)
	}
	seen := make(map[string]bool)
	for _, task := range taskList {
		if seen[task.Script] {
			continue
		}
		seen[task.Script] = true
		path, ok := ResolveScriptArg(task.Script, "")
		if !ok {
			t.Fatalf("task %q script %q not found in data/scripts/", task.ID, task.Script)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		cmds, err := SplitScriptCommands(string(content))
		if err != nil {
			t.Fatalf("split task script %s: %v", path, err)
		}
		for _, c := range cmds {
			check(c)
		}
	}
}

func firstWord(s string) string {
	toks := SplitArgs(s)
	if len(toks) == 0 {
		return ""
	}
	return toks[0]
}

func mustStatements(t *testing.T, body string) []string {
	t.Helper()
	stmts, err := ParseStatements(body)
	if err != nil {
		t.Fatalf("ParseStatements(%q): %v", body, err)
	}
	var out []string
	for _, s := range stmts {
		// For a loop block, also pull the inner statements so their commands
		// (e.g. "mine") are validated.
		if len(s.Tokens) > 0 && strings.EqualFold(s.Tokens[0], "loop") {
			_, _, inner, isBlock, perr := ParseLoopHeader(s)
			if perr == nil {
				if isBlock {
					innerStmts, _ := ParseStatements(inner)
					for _, is := range innerStmts {
						out = append(out, is.Raw)
					}
				} else {
					out = append(out, inner)
				}
				continue
			}
		}
		out = append(out, s.Raw)
	}
	return out
}
