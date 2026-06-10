package main

import (
	"fmt"
	"strconv"
	"strings"
)

// Rename is one explorer id move (from -> to).
type Rename struct {
	From string
	To   string
}

// explorerRenames is the source of truth for the renumbering. Every existing
// explorer moves; slots 9 and 10 are vacated for outerrim placeholders.
var explorerRenames = []Rename{
	{"explorer-7", "explorer-1"},   // nebula
	{"explorer-10", "explorer-2"},  // nebula
	{"explorer-1", "explorer-3"},   // solarian
	{"explorer-2", "explorer-4"},   // solarian
	{"explorer-3", "explorer-5"},   // voidborn
	{"explorer-4", "explorer-6"},   // voidborn
	{"explorer-5", "explorer-7"},   // crimson
	{"explorer-6", "explorer-8"},   // crimson
	{"explorer-8", "explorer-11"},  // solarian (parked surplus)
	{"explorer-9", "explorer-12"},  // solarian (parked surplus)
}

// placeholderSlots are new outerrim placeholder agents created after renames.
var placeholderSlots = []string{"explorer-9", "explorer-10"}

// expectedEmpire maps a final explorer slot number to its required empire.
// 11 and 12 are parked-surplus solarian slots outside the band scheme.
var expectedEmpire = map[int]string{
	1: "nebula", 2: "nebula",
	3: "solarian", 4: "solarian",
	5: "voidborn", 6: "voidborn",
	7: "crimson", 8: "crimson",
	9: "outerrim", 10: "outerrim",
	11: "solarian", 12: "solarian",
}

// explorerNum extracts the trailing integer from an explorer id.
func explorerNum(id string) (int, error) {
	suffix, ok := strings.CutPrefix(id, "explorer-")
	if !ok {
		return 0, fmt.Errorf("id %q lacks explorer- prefix", id)
	}
	return strconv.Atoi(suffix)
}

// validateRenames checks the map is a clean permutation: distinct sources,
// distinct targets, all well-formed explorer ids.
func validateRenames(rs []Rename) error {
	froms := map[string]bool{}
	tos := map[string]bool{}
	for _, r := range rs {
		if _, err := explorerNum(r.From); err != nil {
			return fmt.Errorf("bad from %q: %w", r.From, err)
		}
		if _, err := explorerNum(r.To); err != nil {
			return fmt.Errorf("bad to %q: %w", r.To, err)
		}
		if froms[r.From] {
			return fmt.Errorf("duplicate source %q", r.From)
		}
		if tos[r.To] {
			return fmt.Errorf("duplicate target %q", r.To)
		}
		froms[r.From] = true
		tos[r.To] = true
	}
	return nil
}

// renameMap returns the from->to lookup used for report rewriting.
func renameMap(rs []Rename) map[string]string {
	m := make(map[string]string, len(rs))
	for _, r := range rs {
		m[r.From] = r.To
	}
	return m
}
