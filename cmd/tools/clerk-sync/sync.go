package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/rsned/spacemolt/pkg/game/clerk"
)

// Result reports what Sync did, keyed by agent directory name except Orphans,
// which holds player usernames.
type Result struct {
	Written   []string // credentials.json gained or changed player_id
	Unchanged []string // player_id already correct
	Unmatched []string // agent dirs whose username matches no player
	Orphans   []string // owned usernames with no agent dir
	Ambiguous []string // agent dirs whose username matches several players
}

// Sync records each owned player's Clerk id in the matching agent's
// credentials.json.
//
// Files are decoded into a generic map rather than game.Credentials so fields
// this tool does not know about survive the rewrite; credentials.json is also
// edited by hand and by other tools. The rewrite goes through a temp file in
// the same directory and a rename, so a crash mid-write cannot leave a
// truncated credentials file behind for a running worker to load, and the
// original file mode is kept because these files hold passwords.
//
// A username shared by two players is refused rather than guessed at: binding
// an agent to the wrong player would log it in as someone else. With dryRun
// the result is computed exactly as for a real run but nothing is written.
func Sync(agentsRoot string, players []clerk.Player, dryRun bool) (Result, error) {
	byName := make(map[string][]clerk.Player)
	for _, p := range players {
		byName[p.Username] = append(byName[p.Username], p)
	}

	entries, err := os.ReadDir(agentsRoot)
	if err != nil {
		return Result{}, fmt.Errorf("read agents dir: %w", err)
	}

	var res Result
	seen := make(map[string]bool)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(agentsRoot, e.Name(), "credentials.json")
		raw, err := os.ReadFile(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return res, fmt.Errorf("read %s: %w", path, err)
		}
		var creds map[string]any
		if err := json.Unmarshal(raw, &creds); err != nil {
			return res, fmt.Errorf("parse %s: %w", path, err)
		}
		username, _ := creds["username"].(string)
		matches := byName[username]
		switch {
		case username == "" || len(matches) == 0:
			res.Unmatched = append(res.Unmatched, e.Name())
			continue
		case len(matches) > 1:
			seen[username] = true
			res.Ambiguous = append(res.Ambiguous, e.Name())
			continue
		}
		seen[username] = true

		id := matches[0].ID
		if cur, _ := creds["player_id"].(string); cur == id {
			res.Unchanged = append(res.Unchanged, e.Name())
			continue
		}
		creds["player_id"] = id
		if !dryRun {
			if err := writeJSONAtomic(path, creds); err != nil {
				return res, err
			}
		}
		res.Written = append(res.Written, e.Name())
	}

	for name := range byName {
		if !seen[name] {
			res.Orphans = append(res.Orphans, name)
		}
	}
	slices.Sort(res.Orphans)
	return res, nil
}

// writeJSONAtomic replaces path with the indented JSON of v, keeping the
// existing file mode.
func writeJSONAtomic(path string, v map[string]any) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat %s: %w", path, err)
	}
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	out = append(out, '\n')

	tmp, err := os.CreateTemp(filepath.Dir(path), ".credentials-*.tmp")
	if err != nil {
		return fmt.Errorf("temp file for %s: %w", path, err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.Write(out); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close %s: %w", tmpName, err)
	}
	if err := os.Chmod(tmpName, info.Mode().Perm()); err != nil {
		cleanup()
		return fmt.Errorf("chmod %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return fmt.Errorf("rename over %s: %w", path, err)
	}
	return nil
}
