package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// stageRenameDirs renames every from-dir to its to-dir using a two-phase
// staging move so a full permutation never clobbers a live directory.
// When apply is false it only validates that every source exists.
func stageRenameDirs(agentsDir string, rs []Rename, apply bool) error {
	for _, r := range rs {
		src := filepath.Join(agentsDir, r.From)
		if fi, err := os.Stat(src); err != nil || !fi.IsDir() {
			return fmt.Errorf("source dir missing: %s", src)
		}
	}
	if !apply {
		return nil
	}
	// Phase 1: from -> from.staging
	for _, r := range rs {
		src := filepath.Join(agentsDir, r.From)
		stg := filepath.Join(agentsDir, r.From+".staging")
		if err := os.Rename(src, stg); err != nil {
			return fmt.Errorf("stage %s: %w", r.From, err)
		}
	}
	// Phase 2: from.staging -> to
	for _, r := range rs {
		stg := filepath.Join(agentsDir, r.From+".staging")
		dst := filepath.Join(agentsDir, r.To)
		if _, err := os.Stat(dst); err == nil {
			return fmt.Errorf("target dir already exists: %s", dst)
		}
		if err := os.Rename(stg, dst); err != nil {
			return fmt.Errorf("finalize %s -> %s: %w", r.From, r.To, err)
		}
	}
	return nil
}
