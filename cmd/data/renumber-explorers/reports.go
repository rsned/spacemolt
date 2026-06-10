package main

import (
	"os"
	"path/filepath"
	"regexp"
)

var explorerTokenRe = regexp.MustCompile(`explorer-\d+`)

// rewriteReports walks reportsDir and replaces explorer ids per m in a single
// pass over each file (mapping the original token, so chained renames do not
// compound). Returns the number of files changed.
func rewriteReports(reportsDir string, m map[string]string, apply bool) (int, error) {
	var changed int
	err := filepath.WalkDir(reportsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out := explorerTokenRe.ReplaceAllStringFunc(string(b), func(tok string) string {
			if to, ok := m[tok]; ok {
				return to
			}
			return tok
		})
		if out == string(b) {
			return nil
		}
		changed++
		if apply {
			return os.WriteFile(path, []byte(out), 0o644)
		}
		return nil
	})
	return changed, err
}
