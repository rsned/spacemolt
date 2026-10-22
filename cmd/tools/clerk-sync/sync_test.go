package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/rsned/spacemolt/pkg/game/clerk"
)

const basicCreds = `{"username":"a","password":"p","empire":"e"}`

func writeCreds(t *testing.T, root, dir, body string, mode os.FileMode) string {
	t.Helper()
	d := filepath.Join(root, dir)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(d, "credentials.json")
	if err := os.WriteFile(p, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil {
		t.Fatal(err)
	}
	return p
}

func readMap(t *testing.T, p string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestSync_WritesPlayerIDForMatch(t *testing.T) {
	root := t.TempDir()
	p := writeCreds(t, root, "dir", basicCreds, 0o600)
	res, err := Sync(root, []clerk.Player{{ID: "1", Username: "a"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(res.Written, []string{"dir"}) {
		t.Errorf("Written = %v", res.Written)
	}
	if got := readMap(t, p)["player_id"]; got != "1" {
		t.Errorf("player_id = %v", got)
	}
}

func TestSync_PreservesUnknownFieldsAndMode(t *testing.T) {
	root := t.TempDir()
	p := writeCreds(t, root, "dir", `{"username":"a","password":"p","empire":"e","note":"keep"}`, 0o600)
	if _, err := Sync(root, []clerk.Player{{ID: "1", Username: "a"}}, false); err != nil {
		t.Fatal(err)
	}
	if got := readMap(t, p)["note"]; got != "keep" {
		t.Errorf("note = %v", got)
	}
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v", fi.Mode().Perm())
	}
}

func TestSync_UnchangedWhenIDMatches(t *testing.T) {
	root := t.TempDir()
	writeCreds(t, root, "dir", `{"username":"a","password":"p","empire":"e","player_id":"1"}`, 0o600)
	res, err := Sync(root, []clerk.Player{{ID: "1", Username: "a"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(res.Unchanged, []string{"dir"}) || len(res.Written) != 0 {
		t.Errorf("res = %+v", res)
	}
}

func TestSync_ReportsUnmatchedAndOrphans(t *testing.T) {
	root := t.TempDir()
	writeCreds(t, root, "dir", basicCreds, 0o600)
	writeCreds(t, root, "stray", `{"username":"zzz","password":"p","empire":"e"}`, 0o600)
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := Sync(root, []clerk.Player{{ID: "1", Username: "a"}, {ID: "2", Username: "nodir"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(res.Unmatched, []string{"stray"}) {
		t.Errorf("Unmatched = %v", res.Unmatched)
	}
	if !slices.Equal(res.Orphans, []string{"nodir"}) {
		t.Errorf("Orphans = %v", res.Orphans)
	}
}

func TestSync_RefusesAmbiguousMatch(t *testing.T) {
	root := t.TempDir()
	p := writeCreds(t, root, "dir", basicCreds, 0o600)
	res, err := Sync(root, []clerk.Player{{ID: "1", Username: "a"}, {ID: "2", Username: "a"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(res.Ambiguous, []string{"dir"}) {
		t.Errorf("Ambiguous = %v", res.Ambiguous)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != basicCreds {
		t.Errorf("file modified: %s", b)
	}
}

func TestSync_DryRunWritesNothing(t *testing.T) {
	root := t.TempDir()
	p := writeCreds(t, root, "dir", basicCreds, 0o600)
	res, err := Sync(root, []clerk.Player{{ID: "1", Username: "a"}}, true)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(res.Written, []string{"dir"}) {
		t.Errorf("Written = %v", res.Written)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != basicCreds {
		t.Errorf("file modified: %s", b)
	}
}
