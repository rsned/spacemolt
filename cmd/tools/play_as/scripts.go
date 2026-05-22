package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// scriptExt is the file extension for saved play_as scripts.
const scriptExt = ".smolt"

// scriptSearchPaths returns the script directories searched by bare-name
// resolution, in precedence order: per-agent first (shadows shared), then the
// shared library.
func scriptSearchPaths(agentID string) []string {
	return []string{
		filepath.Join("data", "agents", agentID, "scripts"),
		filepath.Join("data", "scripts"),
	}
}

// isExplicitScriptPath reports whether arg should be treated as a literal file
// path rather than a bare script name: it contains a '/' or ends in ".smolt".
func isExplicitScriptPath(arg string) bool {
	return strings.Contains(arg, "/") || strings.HasSuffix(arg, scriptExt)
}

// resolveScriptArg maps a `run` argument to a file path. An explicit path
// (see isExplicitScriptPath) is used verbatim if it exists. A bare name is
// resolved as "<dir>/<name>.smolt" against scriptSearchPaths in order.
func resolveScriptArg(arg, agentID string) (string, bool) {
	if isExplicitScriptPath(arg) {
		if st, err := os.Stat(arg); err == nil && !st.IsDir() {
			return arg, true
		}
		return "", false
	}
	for _, dir := range scriptSearchPaths(agentID) {
		p := filepath.Join(dir, arg+scriptExt)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, true
		}
	}
	return "", false
}

// splitScriptCommands splits a script file's content into logical commands.
// Top-level blank lines and '#' comment lines are skipped; a multi-line block
// (e.g. a loop { ... }) is kept together until its braces balance, using the
// same brace/quote scanning the REPL uses for multi-line prompt input.
func splitScriptCommands(content string) ([]string, error) {
	var cmds []string
	var cur strings.Builder
	flush := func() {
		s := strings.TrimSpace(cur.String())
		cur.Reset()
		if s != "" {
			cmds = append(cmds, s)
		}
	}
	for _, ln := range strings.Split(content, "\n") {
		if cur.Len() == 0 {
			t := strings.TrimSpace(ln)
			if t == "" || strings.HasPrefix(t, "#") {
				continue
			}
		}
		if cur.Len() > 0 {
			cur.WriteByte('\n')
		}
		cur.WriteString(ln)
		depth, inQuote := scanBraceDepth(cur.String())
		if depth < 0 {
			return nil, fmt.Errorf("unbalanced braces in script")
		}
		if depth == 0 && !inQuote {
			flush()
		}
	}
	if cur.Len() > 0 {
		depth, inQuote := scanBraceDepth(cur.String())
		if depth != 0 || inQuote {
			return nil, fmt.Errorf("unbalanced braces in script")
		}
		flush()
	}
	return cmds, nil
}

// validateScriptName rejects empty names and names that could escape the
// shared scripts directory.
func validateScriptName(name string) error {
	if name == "" {
		return fmt.Errorf("save: empty script name")
	}
	if strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return fmt.Errorf("save: invalid script name %q", name)
	}
	return nil
}

// saveScript writes content to the shared scripts dir as "<name>.smolt",
// creating the directory if needed. A trailing newline is appended.
func saveScript(name, content string) error {
	if err := validateScriptName(name); err != nil {
		return err
	}
	dir := filepath.Join("data", "scripts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("save: %w", err)
	}
	path := filepath.Join(dir, name+scriptExt)
	if err := os.WriteFile(path, []byte(content+"\n"), 0o644); err != nil {
		return fmt.Errorf("save: %w", err)
	}
	return nil
}

// listScripts returns the sorted script names (without extension) in the
// per-agent and shared directories.
func listScripts(agentID string) (perAgent, shared []string) {
	read := func(dir string) []string {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil
		}
		var names []string
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), scriptExt) {
				continue
			}
			names = append(names, strings.TrimSuffix(e.Name(), scriptExt))
		}
		slices.Sort(names)
		return names
	}
	paths := scriptSearchPaths(agentID)
	return read(paths[0]), read(paths[1])
}
