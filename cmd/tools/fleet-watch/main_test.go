package main

import "testing"

// After a rebuild replaces bin/worker on disk, every running worker's exe link
// reads ".../bin/worker (deleted)". Those processes are still the fleet; the
// census must count them, or one rebuild raises "19 running, expected at
// least 200" (2026-11-04).
func TestExeName_CountsReplacedBinaries(t *testing.T) {
	for link, want := range map[string]string{
		"/home/u/spacemolt/bin/worker":             "worker",
		"/home/u/spacemolt/bin/worker (deleted)":   "worker",
		"/home/u/spacemolt/bin/overmind (deleted)": "overmind",
		"/usr/bin/bash":                            "bash",
	} {
		if got := exeName(link); got != want {
			t.Errorf("exeName(%q) = %q, want %q", link, got, want)
		}
	}
}
