package ovdash

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// The frontend carries its own fleet->colour map annotated "must mirror
// pkg/ovdash Fleets". That comment was the only thing enforcing it, and it
// drifted: the hunt fleet launched 2026-08-10 and the unlock pool on
// 2026-08-11, and neither list gained an entry, so 38 live agents were absent
// from the dashboard while every pool reported healthy. Nothing failed -- a
// fleet that is not in the list is simply invisible, which is
// indistinguishable from a fleet that has no workers.
//
// This is the third hardcoded fleet list found drifting in one day (the others
// being ovstatus defaultSources and this one's frontend twin), so the mirror is
// now checked rather than asserted in prose.
func TestFleetsMirrorTheFrontendColourMap(t *testing.T) {
	path := filepath.Join("..", "..", "frontend", "src", "lib", "useFleetStream.ts")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	block := regexp.MustCompile(`(?s)export const FLEETS: Record<string, string> = \{(.*?)\}`).FindSubmatch(src)
	if block == nil {
		t.Fatalf("could not find the FLEETS map in %s; if it was renamed, update this test rather than deleting it", path)
	}
	entries := regexp.MustCompile(`(\w+):\s*'(#[0-9a-fA-F]{3,8})'`).FindAllStringSubmatch(string(block[1]), -1)
	frontend := make(map[string]string, len(entries))
	for _, e := range entries {
		frontend[e[1]] = e[2]
	}

	if len(frontend) != len(Fleets) {
		t.Errorf("frontend FLEETS has %d entries, Go Fleets has %d", len(frontend), len(Fleets))
	}
	for _, f := range Fleets {
		colour, ok := frontend[f.Label]
		if !ok {
			t.Errorf("fleet %q is in Go Fleets but missing from the frontend FLEETS map (it would render uncoloured)", f.Label)

			continue
		}
		if colour != f.Color {
			t.Errorf("fleet %q: Go colour %s, frontend colour %s", f.Label, f.Color, colour)
		}
	}
	for label := range frontend {
		found := false
		for _, f := range Fleets {
			if f.Label == label {
				found = true
			}
		}
		if !found {
			t.Errorf("fleet %q is in the frontend FLEETS map but not in Go Fleets (it can never receive data)", label)
		}
	}
}

// Every fleet must name a distinct status file and socket: two entries sharing
// a file would double-count its workers, and two sharing a socket would send an
// admin remove/readd to the wrong overmind.
func TestFleetDefsAreDistinct(t *testing.T) {
	files, sockets, labels := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, f := range Fleets {
		if files[f.File] {
			t.Errorf("duplicate status file %q", f.File)
		}
		if sockets[f.Socket] {
			t.Errorf("duplicate socket %q", f.Socket)
		}
		if labels[f.Label] {
			t.Errorf("duplicate label %q", f.Label)
		}
		files[f.File], sockets[f.Socket], labels[f.Label] = true, true, true
	}
}
