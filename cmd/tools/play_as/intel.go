package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// globalIntelDir is the base directory for per-POI get_poi intel dumps. When
// empty, intel dumping is disabled. Set from the --intel-dir flag in main.
//
// Layout: <globalIntelDir>/<system_id>/<system_id>___<poi_id>.json
// Each file holds the raw get_poi payload, the exact blob the faction intel
// terminal consumes.
var globalIntelDir = "data/intel"

// intelFileSep separates the system and POI components of an intel filename.
const intelFileSep = "___"

// sanitizeIntelComponent makes an id safe to use as a path component: it strips
// directory separators and other characters that would break a filename. System
// and POI ids are already slug-like, so this is a defensive guard.
func sanitizeIntelComponent(s string) string {
	repl := func(r rune) rune {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|', 0:
			return '_'
		}
		return r
	}
	return strings.Map(repl, s)
}

// saveIntelPOI writes the raw get_poi payload for one POI to
// <globalIntelDir>/<system_id>/<system_id>___<poi_id>.json, creating the
// directory tree as needed. The payload is re-indented for readability; it
// remains valid JSON for the intel terminal. Returns the path written.
//
// It is a no-op (returns "", nil) when intel dumping is disabled, the payload
// is empty, or either id is missing — callers can ignore the empty path.
func saveIntelPOI(systemID, poiID string, raw []byte) (string, error) {
	if globalIntelDir == "" || len(raw) == 0 {
		return "", nil
	}
	systemID = sanitizeIntelComponent(systemID)
	poiID = sanitizeIntelComponent(poiID)
	if systemID == "" || poiID == "" {
		return "", nil
	}

	dir := filepath.Join(globalIntelDir, systemID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create intel dir %s: %w", dir, err)
	}

	// Pretty-print for human readability; fall back to the raw bytes if the
	// payload is not valid JSON (shouldn't happen for a get_poi response).
	var pretty bytes.Buffer
	out := raw
	if err := json.Indent(&pretty, raw, "", "  "); err == nil {
		out = pretty.Bytes()
	}

	path := filepath.Join(dir, systemID+intelFileSep+poiID+".json")
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return "", fmt.Errorf("write intel file %s: %w", path, err)
	}
	return path, nil
}
