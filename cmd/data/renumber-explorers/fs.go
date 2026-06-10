package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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

var idLineRe = regexp.MustCompile(`("id"\s*:\s*")explorer-\d+(")`)

// rewritePersonalityID replaces only the id field in a personality.json,
// leaving all other formatting and fields byte-for-byte intact.
func rewritePersonalityID(path, newID string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	out := idLineRe.ReplaceAll(b, []byte("${1}"+newID+"${2}"))
	if string(out) == string(b) {
		return fmt.Errorf("no id field rewritten in %s", path)
	}
	return os.WriteFile(path, out, 0o644)
}

type placeholderDoc struct {
	ID          string `json:"id"`
	Role        string `json:"role"`
	Empire      string `json:"empire"`
	Placeholder bool   `json:"placeholder"`
}

// createPlaceholder writes a stub outerrim explorer slot with no credentials.
func createPlaceholder(agentsDir, id string, apply bool) error {
	if !apply {
		return nil
	}
	dir := filepath.Join(agentsDir, id)
	if _, err := os.Stat(dir); err == nil {
		return fmt.Errorf("placeholder target exists: %s", dir)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	doc := placeholderDoc{ID: id, Role: "Explorer", Empire: "outerrim", Placeholder: true}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "personality.json"), append(b, '\n'), 0o644)
}
