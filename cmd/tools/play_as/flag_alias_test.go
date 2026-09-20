package main

import "testing"

// Flag names are inconsistent across the REPL: payload flags use underscores
// (--item_id, --faction_id) while option flags use hyphens (--include-faction,
// --dry-run). Typing the wrong separator parsed as an unknown flag and was
// silently ignored, so `plan craft_fuel_cell 1000 --include_faction` quietly
// ran WITHOUT faction stock and reported 2,000 liquid_hydrogen short while
// 38,097 sat in the faction lockbox (2026-09-20).
//
// Accept either separator at lookup time. Normalising at parse time instead
// would corrupt the underscore payload flags.
func TestPartitionFlagBoolAcceptsEitherSeparator(t *testing.T) {
	for name, flags := range map[string]map[string]string{
		"hyphen given":     {"include-faction": ""},
		"underscore given": {"include_faction": ""},
		"hyphen =true":     {"include-faction": "true"},
		"underscore =1":    {"include_faction": "1"},
	} {
		t.Run(name, func(t *testing.T) {
			if !partitionFlagBool(flags, "include-faction") {
				t.Errorf("lookup by hyphen failed for %v", flags)
			}
			if !partitionFlagBool(flags, "include_faction") {
				t.Errorf("lookup by underscore failed for %v", flags)
			}
		})
	}
}

func TestPartitionFlagBoolStillFalseWhenAbsentOrOff(t *testing.T) {
	for name, tc := range map[string]struct {
		flags map[string]string
		want  bool
	}{
		"absent":         {map[string]string{}, false},
		"explicit false": {map[string]string{"include-faction": "false"}, false},
		"unrelated flag": {map[string]string{"reachable": ""}, false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := partitionFlagBool(tc.flags, "include-faction"); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}
