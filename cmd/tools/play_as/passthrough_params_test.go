package main

import "testing"

func TestPassthroughParamName(t *testing.T) {
	for _, tc := range []struct {
		cmd  string
		n    int
		want string
	}{
		{"faction_scan_poi", 1, "poi_id"},
		{"faction_scan_poi", 2, "arg2"},
		{"some_unknown_cmd", 1, "arg1"},
	} {
		if got := passthroughParamName(tc.cmd, tc.n); got != tc.want {
			t.Errorf("passthroughParamName(%q, %d) = %q, want %q", tc.cmd, tc.n, got, tc.want)
		}
	}
}
