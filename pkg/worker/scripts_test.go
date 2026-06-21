package worker

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSplitScriptCommands(t *testing.T) {
	content := `# portable mining loop
loop 3 {
    travel $ASTEROID_BELT$
    mine
}

mine

# trailing comment
dock
`
	got, err := SplitScriptCommands(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{
		"loop 3 {\n    travel $ASTEROID_BELT$\n    mine\n}",
		"mine",
		"dock",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v\nwant %#v", got, want)
	}
}

func TestSplitScriptCommandsUnbalanced(t *testing.T) {
	if _, err := SplitScriptCommands("loop 3 { mine\n"); err == nil {
		t.Fatal("expected error for unbalanced braces")
	}
}

func TestIsExplicitScriptPath(t *testing.T) {
	cases := map[string]bool{
		"mining-loop":  false,
		"mining.smolt": true,
		"./x.smolt":    true,
		"/tmp/x.smolt": true,
		"sub/dir/name": true,
	}
	for in, want := range cases {
		if got := isExplicitScriptPath(in); got != want {
			t.Errorf("isExplicitScriptPath(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestResolveScriptArgPrecedence(t *testing.T) {
	t.Chdir(t.TempDir())
	agentID := "miner-1"
	agentDir := filepath.Join("data", "agents", agentID, "scripts")
	sharedDir := filepath.Join("data", "scripts")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sharedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// "loop" exists in both; per-agent must win. "shared-only" only in shared.
	mustWrite(t, filepath.Join(agentDir, "loop.smolt"), "mine")
	mustWrite(t, filepath.Join(sharedDir, "loop.smolt"), "dock")
	mustWrite(t, filepath.Join(sharedDir, "shared-only.smolt"), "scan")

	if got, ok := ResolveScriptArg("loop", agentID); !ok || got != filepath.Join(agentDir, "loop.smolt") {
		t.Errorf("loop resolved to %q (ok=%v); want per-agent path", got, ok)
	}
	if got, ok := ResolveScriptArg("shared-only", agentID); !ok || got != filepath.Join(sharedDir, "shared-only.smolt") {
		t.Errorf("shared-only resolved to %q (ok=%v); want shared path", got, ok)
	}
	if _, ok := ResolveScriptArg("missing", agentID); ok {
		t.Error("missing script unexpectedly resolved")
	}

	// Explicit path bypasses name resolution.
	mustWrite(t, "adhoc.smolt", "refuel")
	if got, ok := ResolveScriptArg("adhoc.smolt", agentID); !ok || got != "adhoc.smolt" {
		t.Errorf("explicit path resolved to %q (ok=%v)", got, ok)
	}
}

func TestSaveAndListScripts(t *testing.T) {
	t.Chdir(t.TempDir())
	agentID := "miner-1"
	if err := SaveScript("my-loop", "loop 3 { mine }"); err != nil {
		t.Fatalf("SaveScript: %v", err)
	}
	data, err := os.ReadFile(filepath.Join("data", "scripts", "my-loop.smolt"))
	if err != nil {
		t.Fatalf("reading saved script: %v", err)
	}
	if string(data) != "loop 3 { mine }\n" {
		t.Errorf("saved content = %q", string(data))
	}
	perAgent, shared := ListScripts(agentID)
	if len(perAgent) != 0 {
		t.Errorf("perAgent = %v, want empty", perAgent)
	}
	if !reflect.DeepEqual(shared, []string{"my-loop"}) {
		t.Errorf("shared = %v, want [my-loop]", shared)
	}
}

func TestSaveScriptInvalidName(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, name := range []string{"", "a/b", "../escape"} {
		if err := SaveScript(name, "mine"); err == nil {
			t.Errorf("SaveScript(%q) expected error", name)
		}
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
