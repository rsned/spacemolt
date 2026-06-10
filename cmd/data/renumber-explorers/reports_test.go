package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRewriteReportsSinglePass(t *testing.T) {
	dir := t.TempDir()
	// 1->3 and 3->5: a chained replacement would turn explorer-1 into explorer-5.
	p := filepath.Join(dir, "daily.md")
	if err := os.WriteFile(p, []byte("explorer-1 mined; explorer-3 scouted; explorer-10 idle; miner-1 ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := map[string]string{"explorer-1": "explorer-3", "explorer-3": "explorer-5", "explorer-10": "explorer-2"}

	n, err := rewriteReports(dir, m, true)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("changed files = %d, want 1", n)
	}
	got, _ := os.ReadFile(p)
	want := "explorer-3 mined; explorer-5 scouted; explorer-2 idle; miner-1 ok"
	if string(got) != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
